package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
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

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Internal Server Error.", http.StatusInternalServerError)
		return
	}

	// Non-Blocking Channel Operations: https://gobyexample.com/non-blocking-channel-operations
	select {
	case c.contacts <- Message{
		info: string(body),
	}:
		break
	default:
		http.Error(w, "Contact not processed.", http.StatusTooManyRequests)
		return
	}

	fmt.Fprintf(w, "Thanks.")
}

func sendMail(cfg Config, msg Message) {
	// sending mails with golang: https://www.loginradius.com/blog/engineering/sending-emails-with-golang/
	raw := `Subject: Contact Request
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
	fmt.Println("Email Sent Successfully!")
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
