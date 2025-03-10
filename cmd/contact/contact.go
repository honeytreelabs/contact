package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"time"

	env "github.com/caarlos0/env/v6"
	"github.com/microcosm-cc/bluemonday"
)

type ConfigEmail struct {
	From     string `env:"MAIL_FROM,notEmpty"`
	To       string `env:"MAIL_TO,notEmpty"`
	User     string `env:"MAIL_USER,notEmpty"`
	Password string `env:"MAIL_PASSWORD,notEmpty"`
	Host     string `env:"MAIL_HOST,notEmpty"`
	Port     uint16 `env:"MAIL_PORT,notEmpty"`
}

type Config struct {
	ListenAddress            string        `env:"LISTEN_ADDRESS" envDefault:":8080"`
	QueueLength              int           `env:"QUEUE_LENGTH" envDefault:"5"`
	RateLimitingWindow       time.Duration `env:"RATE_LIMITING_WINDOW" envDefault:"5s"`
	Path                     string        `env:"URL_PATH" envDefault:"/contact"`
	AccessControlAllowOrigin string        `env:"ACCESS_CONTROL_ALLOW_ORIGIN" envDefault:""`
	Mail                     ConfigEmail
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

func (c ContactHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if c.cfg.AccessControlAllowOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", c.cfg.AccessControlAllowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", "3600")
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
	r.Body = http.MaxBytesReader(w, r.Body, 512)
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

	bmSanitizer := bluemonday.StrictPolicy()
	userMessage = bmSanitizer.Sanitize(userMessage)

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

func isExcludedEmail(email string) bool {
	exclusions := []string{"@do-not-reply.", "dont-reply.me"}
	for _, exclusion := range exclusions {
		if strings.Contains(email, exclusion) {
			return true
		}
	}
	return false
}

// checking email addresses in go:
// - https://ayada.dev/posts/validate-email-address-in-go/
// - https://pkg.go.dev/net/mail#ParseAddress
func isEmailAddressValid(input string) bool {
	address, err := mail.ParseAddress(input)
	if err != nil {
		fmt.Printf("Cannot parse address: %v\n", err)
		return false
	}
	domain := strings.Split(address.Address, "@")[1]
	if mx, errLookup := net.LookupMX(domain); errLookup != nil || len(mx) == 0 {
		fmt.Printf("Cannot lookup MX record: %v\n", errLookup)
		return false
	}
	return true
}

// sending mails with golang:
// - https://www.loginradius.com/blog/engineering/sending-emails-with-golang/
func sendMail(cfg Config, msg Message) {
	if !isEmailAddressValid(msg.email) {
		fmt.Printf("Cannot parse given email address: %s\n", msg.email)
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
	raw = strings.ReplaceAll(raw, "{email}", msg.email)
	raw = strings.ReplaceAll(raw, "{userMessage}", msg.text)
	raw = strings.ReplaceAll(raw, "{sender}", cfg.Mail.From)
	raw = strings.ReplaceAll(raw, "{receiver}", cfg.Mail.To)
	auth := smtp.PlainAuth("", cfg.Mail.User, cfg.Mail.Password, cfg.Mail.Host)
	err := smtp.SendMail(fmt.Sprintf("%s:%d", cfg.Mail.Host, cfg.Mail.Port),
		auth,
		cfg.Mail.From,
		[]string{cfg.Mail.To},
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

	// Length and capacity of a channel in go: https://golangbyexample.com/length-and-capacity-channel-golang/
	contacts := make(MessageChannel, cfg.QueueLength)
	contactHandler := ContactHandler{
		contacts: contacts,
		cfg:      cfg,
	}
	// Go HTTP server: https://zetcode.com/golang/http-server/
	http.Handle(cfg.Path, contactHandler)
	go rateLimit(cfg, contacts, sendMail)
	// go rateLimit(cfg, contacts, func(_ Config, msg Message) {
	// 	fmt.Printf("Contact received: %s\n", msg.info)
	// })
	log.Fatal(http.ListenAndServe(cfg.ListenAddress, nil))
}
