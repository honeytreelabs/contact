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
	ListenAddress      string        `env:"LISTEN_ADDRESS" envDefault:":8080"`
	QueueLength        int           `env:"QUEUE_LENGTH" envDefault:"5"`
	RateLimitingWindow time.Duration `env:"RATE_LIMITING_WINDOW" envDefault:"5s"`
	Path               string        `env:"URL_PATH" envDefault:"/contact"`
	Mail               ConfigEmail
}

type Message struct {
	info string
}

type MessageChannel chan Message

type ContactHandler struct {
	cfg      Config
	contacts MessageChannel
}

func (c ContactHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != c.cfg.Path {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		return
	}

	// not using ioutil.ReadAll here: https://haisum.github.io/2017/09/11/golang-ioutil-readall/
	r.Body = http.MaxBytesReader(w, r.Body, 512)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("Cannot parse form: %v\n", err)
		return
	}

	if r.PostForm.Get("contact-dsgvo-checkbox") == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("DSGVO checkbox not activated.")
		return
	}

	email := r.PostForm.Get("email")
	if email == "" {
		http.Error(w, "Bad Request.", http.StatusBadRequest)
		fmt.Printf("Email address not submitted.\n")
		return
	}

	// Non-Blocking Channel Operations: https://gobyexample.com/non-blocking-channel-operations
	select {
	case c.contacts <- Message{
		info: email,
	}:
		break
	default:
		http.Error(w, "Contact not processed.", http.StatusTooManyRequests)
		return
	}

	fmt.Fprintf(w, "Sent.")
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
	if !isEmailAddressValid(msg.info) {
		fmt.Printf("Cannot parse given email address: %s\n", msg.info)
		return
	}

	raw := `Subject: Contact Request
From: honeytreeLabs ContactBot <contactbot@honeytreelabs.com>
Content-Type: text/plain; charset="UTF-8"

Please respond to the following mail address: {email}
`
	raw = strings.Replace(raw, "{email}", msg.info, -1)
	auth := smtp.PlainAuth("", cfg.Mail.From, cfg.Mail.Password, cfg.Mail.Host)
	err := smtp.SendMail(fmt.Sprintf("%s:%d", cfg.Mail.Host, cfg.Mail.Port),
		auth,
		cfg.Mail.From,
		[]string{cfg.Mail.To},
		[]byte(raw))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("Email sent successfully for %s\n", msg.info)
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
// curl -v http://localhost:8080/contact -d"some.one@somewhere.com"
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
