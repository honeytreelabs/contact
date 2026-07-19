package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ContactTestSuite struct {
	suite.Suite
}

func testConfig() Config {
	return Config{
		QueueLength:        1,
		RateLimitingWindow: time.Second,
		Path:               "/contact",
		Captcha: ConfigCaptcha{
			VerifyTimeout: time.Second,
		},
	}
}

func (s *ContactTestSuite) TestValidEmail() {
	s.Require().True(isEmailAddressValid("some.one@gmail.com"))
	s.Require().True(isEmailAddressValid("Some <some@gmail.com>"))
	s.Require().True(isEmailAddressValid("Some One <some.one@gmail.com>"))
}

func (s *ContactTestSuite) TestInvalidEmail() {
	s.Require().False(isEmailAddressValid("some.one@"))
	s.Require().False(isEmailAddressValid("Some <@gmail.com>"))
	s.Require().False(isEmailAddressValid("Some One <some.one@339cjgfu349fgj40g.co9t049>"))
}

func (s *ContactTestSuite) TestMailboxAddressParsing() {
	address, err := parseMailboxAddress("Some One <some.one@example.com>")
	s.Require().NoError(err)
	s.Require().Equal("some.one@example.com", address)
}

func (s *ContactTestSuite) TestMailboxAddressRejectsLineBreaks() {
	_, err := parseMailboxAddress("some.one@example.com\r\nBcc: attacker@example.com")
	s.Require().Error(err)
}

func (s *ContactTestSuite) TestValidConfig() {
	cfg := testConfig()
	s.Require().NoError(validateConfig(cfg))
}

func (s *ContactTestSuite) TestInvalidConfig() {
	cfg := testConfig()
	cfg.QueueLength = 0
	s.Require().Error(validateConfig(cfg))

	cfg = testConfig()
	cfg.RateLimitingWindow = 0
	s.Require().Error(validateConfig(cfg))

	cfg = testConfig()
	cfg.Captcha.VerifyTimeout = 0
	s.Require().Error(validateConfig(cfg))

	cfg = testConfig()
	cfg.Captcha.Enabled = true
	cfg.Captcha.APIEndpoint = ""
	cfg.Captcha.Secret = "secret"
	s.Require().Error(validateConfig(cfg))

	cfg = testConfig()
	cfg.Captcha.Enabled = true
	cfg.Captcha.APIEndpoint = "https://cap.example.com/site-key/"
	cfg.Captcha.Secret = ""
	s.Require().Error(validateConfig(cfg))

	cfg = testConfig()
	cfg.Captcha.Enabled = true
	cfg.Captcha.APIEndpoint = "ftp://cap.example.com/site-key/"
	cfg.Captcha.Secret = "secret"
	s.Require().Error(validateConfig(cfg))
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenDisabled() {
	s.Require().NoError(verifyCaptchaToken(ConfigCaptcha{}, ""))
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenRejectsMissingToken() {
	err := verifyCaptchaToken(ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   "https://cap.example.com/site-key/",
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}, "")
	s.Require().Error(err)
	s.Require().Equal(http.StatusBadRequest, err.(captchaVerificationError).statusCode)
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenRejectsOversizedToken() {
	err := verifyCaptchaToken(ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   "https://cap.example.com/site-key/",
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}, strings.Repeat("a", maxCapTokenLength+1))
	s.Require().Error(err)
	s.Require().Equal(http.StatusBadRequest, err.(captchaVerificationError).statusCode)
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenAcceptsSuccessfulVerification() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Require().Equal(http.MethodPost, r.Method)
		s.Require().Equal("/site-key/siteverify", r.URL.Path)
		s.Require().Equal("application/json", r.Header.Get("Content-Type"))

		var payload captchaVerifyRequest
		s.Require().NoError(json.NewDecoder(r.Body).Decode(&payload))
		s.Require().Equal("secret", payload.Secret)
		s.Require().Equal("good-token", payload.Response)

		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"success":true}`))
		s.Require().NoError(err)
	}))
	defer server.Close()

	err := verifyCaptchaToken(ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   server.URL + "/site-key/",
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}, "good-token")
	s.Require().NoError(err)
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenRejectsFailedVerification() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"success":false}`))
		s.Require().NoError(err)
	}))
	defer server.Close()

	err := verifyCaptchaToken(ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   server.URL,
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}, "bad-token")
	s.Require().Error(err)
	s.Require().Equal(http.StatusBadRequest, err.(captchaVerificationError).statusCode)
}

func (s *ContactTestSuite) TestVerifyCaptchaTokenFailsClosedWhenUnavailable() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()

	err := verifyCaptchaToken(ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   server.URL,
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}, "token")
	s.Require().Error(err)
	s.Require().Equal(http.StatusServiceUnavailable, err.(captchaVerificationError).statusCode)
}

func (s *ContactTestSuite) TestContactHandlerAcceptsFormWhenCaptchaDisabled() {
	handler := ContactHandler{
		cfg:      testConfig(),
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(validContactForm("").Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusOK, response.Code)
	s.Require().Equal("Sent.", response.Body.String())
	s.Require().Len(handler.contacts, 1)
}

func (s *ContactTestSuite) TestContactHandlerPreservesPlainTextPunctuation() {
	handler := ContactHandler{
		cfg:      testConfig(),
		contacts: make(MessageChannel, 1),
	}
	form := validContactForm("")
	form.Set("message", "Hallo, wie geht's?")

	request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusOK, response.Code)
	s.Require().Len(handler.contacts, 1)
	s.Require().Equal("Hallo, wie geht's?", (<-handler.contacts).text)
}

func (s *ContactTestSuite) TestContactHandlerStoresRequestMetadata() {
	handler := ContactHandler{
		cfg:      testConfig(),
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(validContactForm("").Encode()))
	request.RemoteAddr = "203.0.113.10:12345"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-For", "198.51.100.20")
	request.Header.Set("X-Real-IP", "198.51.100.21")
	request.Header.Set("User-Agent", "contact-test-agent")
	request.Header.Set("Origin", "https://embedded-focus.com")
	request.Header.Set("Referer", "https://embedded-focus.com/contact/")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusOK, response.Code)
	s.Require().Len(handler.contacts, 1)
	message := <-handler.contacts
	s.Require().NotEmpty(message.request.id)
	s.Require().Equal("203.0.113.10", message.request.remoteAddr)
	s.Require().Equal("198.51.100.20", message.request.xForwardedFor)
	s.Require().Equal("198.51.100.21", message.request.xRealIP)
	s.Require().Equal("contact-test-agent", message.request.userAgent)
	s.Require().Equal("https://embedded-focus.com", message.request.origin)
	s.Require().Equal("https://embedded-focus.com/contact/", message.request.referer)
}

func (s *ContactTestSuite) TestContactHandlerAllowsConfiguredCORSOrigin() {
	cfg := testConfig()
	cfg.AccessControlAllowOrigins = "https://embedded-focus.com, https://preview.embedded-focus.com"
	handler := ContactHandler{
		cfg:      cfg,
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodOptions, "/contact", nil)
	request.Header.Set("Origin", "https://preview.embedded-focus.com")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusNoContent, response.Code)
	s.Require().Equal("https://preview.embedded-focus.com", response.Header().Get("Access-Control-Allow-Origin"))
	s.Require().Equal("POST, OPTIONS", response.Header().Get("Access-Control-Allow-Methods"))
	s.Require().Equal("Content-Type", response.Header().Get("Access-Control-Allow-Headers"))
	s.Require().Contains(response.Header().Values("Vary"), "Origin")
}

func (s *ContactTestSuite) TestContactHandlerRejectsUnconfiguredCORSOrigin() {
	cfg := testConfig()
	cfg.AccessControlAllowOrigins = "https://embedded-focus.com, https://preview.embedded-focus.com"
	handler := ContactHandler{
		cfg:      cfg,
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodOptions, "/contact", nil)
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusNoContent, response.Code)
	s.Require().Empty(response.Header().Get("Access-Control-Allow-Origin"))
}

func (s *ContactTestSuite) TestContactHandlerKeepsLegacySingleCORSOrigin() {
	cfg := testConfig()
	cfg.AccessControlAllowOrigin = "https://embedded-focus.com"
	handler := ContactHandler{
		cfg:      cfg,
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodOptions, "/contact", nil)
	request.Header.Set("Origin", "https://embedded-focus.com")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusNoContent, response.Code)
	s.Require().Equal("https://embedded-focus.com", response.Header().Get("Access-Control-Allow-Origin"))
}

func (s *ContactTestSuite) TestPlainTextMessageSanitizationStripsHTMLTags() {
	s.Require().Equal(
		"Hello alert('x') world",
		sanitizePlainTextMessage("Hello <script>alert('x')</script> <strong>world</strong>"),
	)
}

func (s *ContactTestSuite) TestContactHandlerRequiresCaptchaTokenWhenEnabled() {
	cfg := testConfig()
	cfg.Captcha = ConfigCaptcha{
		Enabled:       true,
		APIEndpoint:   "https://cap.example.com/site-key/",
		Secret:        "secret",
		VerifyTimeout: time.Second,
	}
	handler := ContactHandler{
		cfg:      cfg,
		contacts: make(MessageChannel, 1),
	}

	request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(validContactForm("").Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusBadRequest, response.Code)
	s.Require().Len(handler.contacts, 0)
}

func validContactForm(capToken string) url.Values {
	form := url.Values{}
	form.Set("email", "sender@example.com")
	form.Set("message", "hello")
	form.Set("contact-dsgvo-checkbox", "on")
	if capToken != "" {
		form.Set("cap-token", capToken)
	}
	return form
}

func (s *ContactTestSuite) TestExcludedEmail() {
	s.Require().True(isExcludedEmail("@do-not-reply."))
	s.Require().True(isExcludedEmail("Hello <hello@do-not-reply.com>"))
	s.Require().True(isExcludedEmail("Someone <from@dont-reply.me>"))
	s.Require().False(isExcludedEmail("Validemail <from@example.com>"))
}

func TestContactTestSuite(t *testing.T) {
	suite.Run(t, new(ContactTestSuite))
}
