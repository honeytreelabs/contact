package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"time"
	"unicode"

	env "github.com/caarlos0/env/v6"
	"github.com/microcosm-cc/bluemonday"
)

const (
	maxRequestBodyBytes              = 16 * 1024
	maxCapTokenLength                = 4096
	randomTextCaseTransitionRate     = 0.30
	initialUpperRunCaseTransitionMin = 0.25
)

var captchaHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

var buildCommit = "unknown"
var appLogger = newLogger(os.Stdout)

type ConfigEmail struct {
	From     string `env:"MAIL_FROM,notEmpty"`
	To       string `env:"MAIL_TO,notEmpty"`
	User     string `env:"MAIL_USER,notEmpty"`
	Password string `env:"MAIL_PASSWORD,notEmpty"`
	Host     string `env:"MAIL_HOST,notEmpty"`
	Port     uint16 `env:"MAIL_PORT,notEmpty"`
}

type ConfigCaptcha struct {
	Enabled       bool          `env:"CAP_ENABLED" envDefault:"false"`
	APIEndpoint   string        `env:"CAP_API_ENDPOINT"`
	Secret        string        `env:"CAP_SECRET"`
	VerifyTimeout time.Duration `env:"CAP_VERIFY_TIMEOUT" envDefault:"5s"`
}

type Config struct {
	ListenAddress             string        `env:"LISTEN_ADDRESS" envDefault:":8080"`
	QueueLength               int           `env:"QUEUE_LENGTH" envDefault:"5"`
	RateLimitingWindow        time.Duration `env:"RATE_LIMITING_WINDOW" envDefault:"5s"`
	Path                      string        `env:"URL_PATH" envDefault:"/contact"`
	AccessControlAllowOrigin  string        `env:"ACCESS_CONTROL_ALLOW_ORIGIN" envDefault:""`
	AccessControlAllowOrigins string        `env:"ACCESS_CONTROL_ALLOW_ORIGINS" envDefault:""`
	Mail                      ConfigEmail
	Captcha                   ConfigCaptcha
}

func validateConfig(cfg Config) error {
	if cfg.QueueLength <= 0 {
		return fmt.Errorf("QUEUE_LENGTH must be greater than 0")
	}
	if cfg.RateLimitingWindow <= 0 {
		return fmt.Errorf("RATE_LIMITING_WINDOW must be greater than 0")
	}
	if cfg.Captcha.VerifyTimeout <= 0 {
		return fmt.Errorf("CAP_VERIFY_TIMEOUT must be greater than 0")
	}
	if cfg.Captcha.Enabled {
		if cfg.Captcha.APIEndpoint == "" {
			return fmt.Errorf("CAP_API_ENDPOINT must be set when CAP_ENABLED is true")
		}
		if cfg.Captcha.Secret == "" {
			return fmt.Errorf("CAP_SECRET must be set when CAP_ENABLED is true")
		}
		if err := validateHTTPURL(cfg.Captcha.APIEndpoint); err != nil {
			return fmt.Errorf("CAP_API_ENDPOINT is invalid: %w", err)
		}
	}
	return nil
}

func validateHTTPURL(input string) error {
	parsed, err := url.Parse(input)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if parsed.Host == "" {
		return fmt.Errorf("host must be set")
	}
	return nil
}

type Message struct {
	email   string
	text    string
	request requestMetadata
}

type MessageChannel chan Message

type ContactHandler struct {
	cfg      Config
	contacts MessageChannel
}

type lowQualityMessageRejectionReason string

const (
	lowQualityMessageTooShort             lowQualityMessageRejectionReason = "message_too_short"
	lowQualityMessageSingleASCIIWordLong  lowQualityMessageRejectionReason = "single_ascii_word_too_long"
	lowQualityMessageSingleASCIIWordMixed lowQualityMessageRejectionReason = "single_ascii_word_random_case"
)

type captchaVerificationError struct {
	statusCode int
	message    string
}

func (e captchaVerificationError) Error() string {
	return e.message
}

type captchaVerifyRequest struct {
	Secret   string `json:"secret"`
	Response string `json:"response"`
}

type captchaVerifyResponse struct {
	Success bool `json:"success"`
}

type requestMetadata struct {
	id            string
	remoteAddr    string
	xForwardedFor string
	xRealIP       string
	userAgent     string
	origin        string
	referer       string
}

func newRequestMetadata(r *http.Request) requestMetadata {
	return requestMetadata{
		id:            newRequestID(),
		remoteAddr:    remoteIP(r.RemoteAddr),
		xForwardedFor: r.Header.Get("X-Forwarded-For"),
		xRealIP:       r.Header.Get("X-Real-IP"),
		userAgent:     r.UserAgent(),
		origin:        r.Header.Get("Origin"),
		referer:       r.Referer(),
	}
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func newLogger(output io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.LevelKey {
				attr.Value = slog.StringValue(strings.ToLower(attr.Value.String()))
			}
			return attr
		},
	}))
}

func logEvent(level slog.Level, event string, attrs ...slog.Attr) {
	attrs = append([]slog.Attr{
		slog.String("event", event),
		slog.String("service_revision", serviceRevision()),
	}, attrs...)
	appLogger.LogAttrs(context.Background(), level, event, attrs...)
}

func logRequestEvent(level slog.Level, event string, req requestMetadata, email string, attrs ...slog.Attr) {
	attrs = append(requestLogAttrs(req, email), attrs...)
	logEvent(level, event, attrs...)
}

func logRequestError(level slog.Level, event string, req requestMetadata, email string, err error) {
	logRequestEvent(level, event, req, email, slog.String("error", err.Error()))
}

func requestLogAttrs(req requestMetadata, email string) []slog.Attr {
	return []slog.Attr{
		slog.String("request_id", req.id),
		slog.String("email", email),
		slog.String("remote_addr", req.remoteAddr),
		slog.String("x_forwarded_for", req.xForwardedFor),
		slog.String("x_real_ip", req.xRealIP),
		slog.String("user_agent", req.userAgent),
		slog.String("origin", req.origin),
		slog.String("referer", req.referer),
	}
}

func serviceRevision() string {
	if buildCommit != "" && buildCommit != "unknown" {
		return buildCommit
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return buildCommit
	}

	revision := buildCommit
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if setting.Value != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified && revision != "" && revision != "unknown" {
		return revision + "+modified"
	}
	return revision
}

func verifyCaptchaToken(cfg ConfigCaptcha, token string) error {
	if !cfg.Enabled {
		return nil
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return captchaVerificationError{
			statusCode: http.StatusBadRequest,
			message:    "CAPTCHA token is required",
		}
	}
	if len(token) > maxCapTokenLength {
		return captchaVerificationError{
			statusCode: http.StatusBadRequest,
			message:    "CAPTCHA token is too long",
		}
	}

	payload, err := json.Marshal(captchaVerifyRequest{
		Secret:   cfg.Secret,
		Response: token,
	})
	if err != nil {
		return err
	}

	request, err := http.NewRequest(
		http.MethodPost,
		strings.TrimRight(cfg.APIEndpoint, "/")+"/siteverify",
		bytes.NewReader(payload),
	)
	if err != nil {
		return captchaVerificationError{
			statusCode: http.StatusServiceUnavailable,
			message:    "CAPTCHA verification is unavailable",
		}
	}
	request.Header.Set("Content-Type", "application/json")

	client := *captchaHTTPClient
	client.Timeout = cfg.VerifyTimeout
	response, err := client.Do(request)
	if err != nil {
		return captchaVerificationError{
			statusCode: http.StatusServiceUnavailable,
			message:    "CAPTCHA verification is unavailable",
		}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return captchaVerificationError{
			statusCode: http.StatusServiceUnavailable,
			message:    "CAPTCHA verification is unavailable",
		}
	}

	var result captchaVerifyResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return captchaVerificationError{
			statusCode: http.StatusServiceUnavailable,
			message:    "CAPTCHA verification is unavailable",
		}
	}
	if !result.Success {
		return captchaVerificationError{
			statusCode: http.StatusBadRequest,
			message:    "CAPTCHA verification failed",
		}
	}

	return nil
}

func (c ContactHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	request := newRequestMetadata(r)

	if allowOrigin := allowedCORSOrigin(c.cfg, r.Header.Get("Origin")); allowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.Header().Add("Vary", "Origin")
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.URL.Path != c.cfg.Path {
		http.Error(w, "Not found.", http.StatusNotFound)
		logRequestEvent(slog.LevelWarn, "Path not found", request, "", slog.String("path", r.URL.Path))
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestEvent(slog.LevelWarn, "Wrong HTTP method", request, "", slog.String("method", r.Method))
		return
	}

	// not using ioutil.ReadAll here: https://haisum.github.io/2017/09/11/golang-ioutil-readall/
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestError(slog.LevelWarn, "Cannot parse form", request, "", err)
		return
	}

	if r.PostFormValue("contact-dsgvo-checkbox") == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestEvent(slog.LevelWarn, "DSGVO checkbox not activated", request, "")
		return
	}

	userEmail := r.PostFormValue("email")
	if userEmail == "" || isExcludedEmail(userEmail) {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestEvent(slog.LevelWarn, "Email address not submitted", request, userEmail)
		return
	}

	// message must not be empty
	userMessage := r.PostFormValue("message")
	if userMessage == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestEvent(slog.LevelWarn, "No message given", request, userEmail)
		return
	}

	userMessage = sanitizePlainTextMessage(userMessage)
	if rejected, reason := isLowQualityMessageRejected(userMessage); rejected {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		logRequestEvent(slog.LevelWarn, "Low quality message rejected", request, userEmail, slog.String("reason", string(reason)))
		return
	}

	if err := verifyCaptchaToken(c.cfg.Captcha, r.PostFormValue("cap-token")); err != nil {
		captchaErr, ok := err.(captchaVerificationError)
		if !ok {
			captchaErr = captchaVerificationError{
				statusCode: http.StatusServiceUnavailable,
				message:    "CAPTCHA verification is unavailable",
			}
		}
		http.Error(w, captchaErr.message, captchaErr.statusCode)
		level := slog.LevelWarn
		if captchaErr.statusCode == http.StatusServiceUnavailable {
			level = slog.LevelError
		}
		logRequestError(level, "CAPTCHA verification failed", request, userEmail, err)
		return
	}
	if c.cfg.Captcha.Enabled {
		logRequestEvent(slog.LevelInfo, "CAPTCHA verification succeeded", request, userEmail)
	}

	// Non-Blocking Channel Operations: https://gobyexample.com/non-blocking-channel-operations
	select {
	case c.contacts <- Message{
		email:   userEmail,
		text:    userMessage,
		request: request,
	}:
		break
	default:
		http.Error(w, "Contact not processed.", http.StatusTooManyRequests)
		logRequestEvent(slog.LevelWarn, "Contact queue full", request, userEmail)
		return
	}

	fmt.Fprintf(w, "Sent.")
}

func sanitizePlainTextMessage(input string) string {
	bmSanitizer := bluemonday.StrictPolicy()
	return html.UnescapeString(bmSanitizer.Sanitize(input))
}

func isLowQualityMessage(input string) bool {
	rejected, _ := isLowQualityMessageRejected(input)
	return rejected
}

func isLowQualityMessageRejected(input string) (bool, lowQualityMessageRejectionReason) {
	message := strings.TrimSpace(input)
	if runeCount(message) < 10 {
		return true, lowQualityMessageTooShort
	}

	parts := strings.Fields(message)
	if len(parts) != 1 {
		return false, ""
	}

	token := parts[0]
	if !isASCIILetters(token) {
		return false, ""
	}

	if len(token) >= 24 {
		return true, lowQualityMessageSingleASCIIWordLong
	}
	if len(token) < 16 || !hasMixedASCIICase(token) {
		return false, ""
	}

	if asciiCaseTransitionRatio(token) > randomTextCaseTransitionRate || hasRandomCaseAfterInitialUpperRun(token) {
		return true, lowQualityMessageSingleASCIIWordMixed
	}

	return false, ""
}

func hasRandomCaseAfterInitialUpperRun(input string) bool {
	return leadingASCIIUpperRunLength(input) >= 2 && asciiCaseTransitionRatio(input) >= initialUpperRunCaseTransitionMin
}

func runeCount(input string) int {
	count := 0
	for range input {
		count++
	}
	return count
}

func isASCIILetters(input string) bool {
	if input == "" {
		return false
	}
	for i := 0; i < len(input); i++ {
		if !isASCIIUpper(input[i]) && !isASCIILower(input[i]) {
			return false
		}
	}
	return true
}

func hasMixedASCIICase(input string) bool {
	hasLower := false
	hasUpper := false
	for i := 0; i < len(input); i++ {
		hasLower = hasLower || isASCIILower(input[i])
		hasUpper = hasUpper || isASCIIUpper(input[i])
	}
	return hasLower && hasUpper
}

func leadingASCIIUpperRunLength(input string) int {
	runLength := 0
	for i := 0; i < len(input); i++ {
		if !isASCIIUpper(input[i]) {
			break
		}
		runLength++
	}
	return runLength
}

func asciiCaseTransitionRatio(input string) float64 {
	if len(input) < 2 {
		return 0
	}

	transitions := 0
	for i := 1; i < len(input); i++ {
		if isASCIILower(input[i-1]) != isASCIILower(input[i]) {
			transitions++
		}
	}
	return float64(transitions) / float64(len(input)-1)
}

func isASCIIUpper(b byte) bool {
	return b >= 'A' && b <= 'Z'
}

func isASCIILower(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func allowedCORSOrigin(cfg Config, requestOrigin string) string {
	if requestOrigin == "" {
		return ""
	}

	for _, allowedOrigin := range parseCORSOrigins(cfg.AccessControlAllowOrigin, cfg.AccessControlAllowOrigins) {
		if requestOrigin == allowedOrigin {
			return requestOrigin
		}
	}

	return ""
}

func parseCORSOrigins(values ...string) []string {
	seen := map[string]struct{}{}
	origins := []string{}

	for _, value := range values {
		for _, origin := range strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || unicode.IsSpace(r)
		}) {
			if origin == "" {
				continue
			}
			if _, exists := seen[origin]; exists {
				continue
			}
			seen[origin] = struct{}{}
			origins = append(origins, origin)
		}
	}

	return origins
}

func isExcludedEmail(email string) bool {
	exclusions := []string{"@do-not-reply.", "dont-reply.me"}
	for _, exclusion := range exclusions {
		if strings.Contains(email, exclusion) {
			return true
		}
	}
	return false
}

func parseMailboxAddress(input string) (string, error) {
	if strings.ContainsAny(input, "\r\n") {
		return "", fmt.Errorf("mail address contains a line break")
	}
	address, err := mail.ParseAddress(input)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(address.Address, "\r\n") {
		return "", fmt.Errorf("mail address contains a line break")
	}
	return address.Address, nil
}

// checking email addresses in go:
// - https://ayada.dev/posts/validate-email-address-in-go/
// - https://pkg.go.dev/net/mail#ParseAddress
func isEmailAddressValid(input string) bool {
	address, err := parseMailboxAddress(input)
	if err != nil {
		logEvent(slog.LevelWarn, "Cannot parse address", slog.String("error", err.Error()))
		return false
	}
	domain := strings.Split(address, "@")[1]
	if mx, errLookup := net.LookupMX(domain); errLookup != nil || len(mx) == 0 {
		logEvent(slog.LevelWarn, "Cannot lookup MX record", slog.String("domain", domain), slog.String("error", errorString(errLookup)))
		return false
	}
	return true
}

// sending mails with golang:
// - https://www.loginradius.com/blog/engineering/sending-emails-with-golang/
func sendMail(cfg Config, msg Message) {
	userEmail, err := parseMailboxAddress(msg.email)
	if err != nil || !isEmailAddressValid(userEmail) {
		logRequestEvent(slog.LevelWarn, "Cannot parse given email address", msg.request, msg.email)
		return
	}

	sender, err := parseMailboxAddress(cfg.Mail.From)
	if err != nil {
		logRequestError(slog.LevelError, "Cannot parse sender address", msg.request, msg.email, err)
		return
	}
	receiver, err := parseMailboxAddress(cfg.Mail.To)
	if err != nil {
		logRequestError(slog.LevelError, "Cannot parse receiver address", msg.request, msg.email, err)
		return
	}

	raw := `From: honeytreeLabs ContactBot <{sender}>
To: Contact Handler <{receiver}>
Subject: Contact Request from <{email}>
Reply-To: <{email}>
Content-Type: text/plain; charset="UTF-8"

We have received a new contact request:
{email}

User Message:
'{userMessage}'
`
	raw = strings.ReplaceAll(raw, "{email}", userEmail)
	raw = strings.ReplaceAll(raw, "{userMessage}", msg.text)
	raw = strings.ReplaceAll(raw, "{sender}", sender)
	raw = strings.ReplaceAll(raw, "{receiver}", receiver)
	auth := smtp.PlainAuth("", cfg.Mail.User, cfg.Mail.Password, cfg.Mail.Host)
	err = smtp.SendMail(fmt.Sprintf("%s:%d", cfg.Mail.Host, cfg.Mail.Port),
		auth,
		sender,
		[]string{receiver},
		[]byte(raw))
	if err != nil {
		logRequestError(slog.LevelError, "Cannot send email", msg.request, msg.email, err)
		return
	}
	logRequestEvent(slog.LevelInfo, "Email sent successfully", msg.request, msg.email)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// rateLimit reads out of the queue of email addresses
func rateLimit(cfg Config, source MessageChannel, destination func(cfg Config, msg Message)) {
	// implement rate limiting
	// see:
	// - https://www.geeksforgeeks.org/time-newticker-function-in-golang-with-examples/
	// - https://medium.com/@justin.graber/rate-limiting-in-golang-f3ed2c62df36

	for {
		time.Sleep(cfg.RateLimitingWindow)
	Done:
		for {
			select {
			case message := <-source:
				destination(cfg, message)
			default:
				break Done
			}
		}
	}
}

// test using the following command:
// curl -v http://localhost:8080/contact -X POST --data-raw 'email=email%40test.example.com&message=message&contact-dsgvo-checkbox=on'
func main() {
	cfg := Config{}
	if err := env.Parse(&cfg); err != nil {
		logEvent(slog.LevelError, "Cannot parse config", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := validateConfig(cfg); err != nil {
		logEvent(slog.LevelError, "Invalid config", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logEvent(
		slog.LevelInfo,
		"ContactBot starting",
		slog.String("listen_address", cfg.ListenAddress),
		slog.String("url_path", cfg.Path),
		slog.Bool("captcha_enabled", cfg.Captcha.Enabled),
	)

	// Length and capacity of a channel in go: https://golangbyexample.com/length-and-capacity-channel-golang/
	contacts := make(MessageChannel, cfg.QueueLength)
	contactHandler := ContactHandler{
		contacts: contacts,
		cfg:      cfg,
	}
	// Go HTTP server: https://zetcode.com/golang/http-server/
	mux := http.NewServeMux()
	mux.Handle(cfg.Path, contactHandler)
	go rateLimit(cfg, contacts, sendMail)
	// go rateLimit(cfg, contacts, func(_ Config, msg Message) {
	// 	fmt.Printf("Contact received: %s\n", msg.info)
	// })
	server := http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		logEvent(slog.LevelError, "HTTP server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
