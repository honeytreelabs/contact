package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	env "github.com/caarlos0/env/v6"
	"github.com/microcosm-cc/bluemonday"
)

const (
	maxRequestBodyBytes = 16 * 1024
	maxCapTokenLength   = 4096
)

var captchaHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

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
	ListenAddress            string        `env:"LISTEN_ADDRESS" envDefault:":8080"`
	QueueLength              int           `env:"QUEUE_LENGTH" envDefault:"5"`
	RateLimitingWindow       time.Duration `env:"RATE_LIMITING_WINDOW" envDefault:"5s"`
	Path                     string        `env:"URL_PATH" envDefault:"/contact"`
	AccessControlAllowOrigin  string        `env:"ACCESS_CONTROL_ALLOW_ORIGIN" envDefault:""`
	AccessControlAllowOrigins string        `env:"ACCESS_CONTROL_ALLOW_ORIGINS" envDefault:""`
	Mail                     ConfigEmail
	Captcha                  ConfigCaptcha
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
	email string
	text  string
}

type MessageChannel chan Message

type ContactHandler struct {
	cfg      Config
	contacts MessageChannel
}

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
		fmt.Printf("Path \"%s\" not found.\n", r.URL.Path)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("Wrong HTTP method \"%s\"\n", r.Method)
		return
	}

	// not using ioutil.ReadAll here: https://haisum.github.io/2017/09/11/golang-ioutil-readall/
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("Cannot parse form: %v\n", err)
		return
	}

	if r.PostFormValue("contact-dsgvo-checkbox") == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("DSGVO checkbox not activated.\n")
		return
	}

	userEmail := r.PostFormValue("email")
	if userEmail == "" || isExcludedEmail(userEmail) {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("Email address not submitted.\n")
		return
	}

	// message must not be empty
	userMessage := r.PostFormValue("message")
	if userMessage == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("No message given.\n")
		return
	}

	userMessage = sanitizePlainTextMessage(userMessage)

	if err := verifyCaptchaToken(c.cfg.Captcha, r.PostFormValue("cap-token")); err != nil {
		captchaErr, ok := err.(captchaVerificationError)
		if !ok {
			captchaErr = captchaVerificationError{
				statusCode: http.StatusServiceUnavailable,
				message:    "CAPTCHA verification is unavailable",
			}
		}
		http.Error(w, captchaErr.message, captchaErr.statusCode)
		fmt.Printf("CAPTCHA verification failed: %v\n", err)
		return
	}

	// Non-Blocking Channel Operations: https://gobyexample.com/non-blocking-channel-operations
	select {
	case c.contacts <- Message{
		email: userEmail,
		text:  userMessage,
	}:
		break
	default:
		http.Error(w, "Contact not processed.", http.StatusTooManyRequests)
		return
	}

	fmt.Fprintf(w, "Sent.")
}

func sanitizePlainTextMessage(input string) string {
	bmSanitizer := bluemonday.StrictPolicy()
	return html.UnescapeString(bmSanitizer.Sanitize(input))
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
		fmt.Printf("Cannot parse address: %v\n", err)
		return false
	}
	domain := strings.Split(address, "@")[1]
	if mx, errLookup := net.LookupMX(domain); errLookup != nil || len(mx) == 0 {
		fmt.Printf("Cannot lookup MX record: %v\n", errLookup)
		return false
	}
	return true
}

// sending mails with golang:
// - https://www.loginradius.com/blog/engineering/sending-emails-with-golang/
func sendMail(cfg Config, msg Message) {
	userEmail, err := parseMailboxAddress(msg.email)
	if err != nil || !isEmailAddressValid(userEmail) {
		fmt.Printf("Cannot parse given email address: %s\n", msg.email)
		return
	}

	sender, err := parseMailboxAddress(cfg.Mail.From)
	if err != nil {
		fmt.Printf("Cannot parse sender address: %v\n", err)
		return
	}
	receiver, err := parseMailboxAddress(cfg.Mail.To)
	if err != nil {
		fmt.Printf("Cannot parse receiver address: %v\n", err)
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
		fmt.Println(err)
		return
	}
	fmt.Printf("Email sent successfully for %s\n", msg.email)
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
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}
	if err := validateConfig(cfg); err != nil {
		fmt.Printf("%+v\n", err)
		os.Exit(1)
	}

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
	log.Fatal(server.ListenAndServe())
}
