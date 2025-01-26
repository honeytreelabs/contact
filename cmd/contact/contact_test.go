package main

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ContactTestSuite struct {
	suite.Suite
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

func (s *ContactTestSuite) TestExcludedEmail() {
	s.Require().True(isExcludedEmail("@do-not-reply."))
	s.Require().True(isExcludedEmail("Hello <hello@do-not-reply.com>"))
	s.Require().True(isExcludedEmail("Someone <from@dont-reply.me>"))
	s.Require().False(isExcludedEmail("Validemail <from@example.com>"))
}

func TestContactTestSuite(t *testing.T) {
	suite.Run(t, new(ContactTestSuite))
}
