package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
)

// Real-world spam sample that passed the heuristics in production on
// 2026-10-19T06:27Z (request 44a15ee3bffb2f21fc51bae85cdafd7e,
// ter.on.ez.e.ke8.1@gmail.com via a Tor exit node).
//
// At the time it escaped because its case transition ratio (4/15 ~= 0.27) sat
// just below the 0.30 threshold. The thresholds were tightened (single-word
// ratio to 0.25) and a vowel-density floor was added: this string carries 3
// vowels out of 16 (~19%), far from German/English vowel density (~35-45%).
const leakedSpamSample = "wvwvWIUPQgfcCDTo"

func (s *ContactTestSuite) TestLowQualityMessageDetectionRejectsLeakedRealWorldSample() {
	rejected, reason := isLowQualityMessageRejected(leakedSpamSample)
	s.Require().True(rejected)
	s.Require().Equal(lowQualityMessageSingleASCIIWordMixed, reason)

	// Evidence for both tightened signals.
	s.Require().Greater(asciiCaseTransitionRatio(leakedSpamSample), randomTextCaseTransitionRate)
	s.Require().Equal(4.0/15.0, asciiCaseTransitionRatio(leakedSpamSample))
	s.Require().Less(asciiVowelRatio(leakedSpamSample), randomTextVowelRatioMin)
	s.Require().Equal(3.0/16.0, asciiVowelRatio(leakedSpamSample))
	s.Require().Equal(0, leadingASCIIUpperRunLength(leakedSpamSample))
}

// TestLowQualityMessageDetectionRejectsWhitespaceSplitSpam covers random
// strings split across whitespace, which used to skip the case analysis
// entirely because only single-word messages were inspected.
func (s *ContactTestSuite) TestLowQualityMessageDetectionRejectsWhitespaceSplitSpam() {
	splitSpam := []string{
		// Same words as the known spam samples, just split on whitespace.
		"ZlmqmRO nZHLMVDe pp",
		"diSoFZsF aSfrd waEf TzykIf",
		"GPPeauTC vZwusicd ogp",
		"RVLlOW VVGEPxZlmD fwaXHg",
		// Alternating case spread over several short words.
		"abCd EfGh IjKl MnOp",
	}

	for _, message := range splitSpam {
		rejected, reason := isLowQualityMessageRejected(message)
		s.Require().True(rejected, message)
		s.Require().Equal(lowQualityMessageSingleASCIIWordMixed, reason, message)
	}
}

// TestLowQualityMessageDetectionRejectsWhitespaceSplitLeakedSample verifies
// the whitespace variant of the production sample is also caught: its joined
// form evades the case-ratio rule alone (4/15 stays below 0.38), so it is
// flagged by the vowel floor.
func (s *ContactTestSuite) TestLowQualityMessageDetectionRejectsWhitespaceSplitLeakedSample() {
	message := "wvw vWIU PQgf cCDTo"

	s.Require().Equal(leakedSpamSample, strings.Join(strings.Fields(message), ""))
	rejected, reason := isLowQualityMessageRejected(message)
	s.Require().True(rejected)
	s.Require().Equal(lowQualityMessageSingleASCIIWordMixed, reason)
}

// TestLowQualityMessageDetectionAllowsGermanAndEnglishProse guards against
// false positives from the tightened thresholds and the vowel floor. German
// is the hard case: nouns are capitalized, so word chains carry many case
// transitions, and compounds are consonant-heavy.
func (s *ContactTestSuite) TestLowQualityMessageDetectionAllowsGermanAndEnglishProse() {
	usefulMessages := []string{
		// German word chains: ~37% case transitions from two per noun
		// capital, which is why the joined threshold has to stay at 0.38.
		"Mein Freund Peter Maier",
		"Ein Buchhaltungsplan",
		"Der Wartungsplan",
		"Callback Please",
		"Hello there my friend",
		// Consonant-heavy German compound: few transitions, low vowels,
		// both guards keep it accepted.
		"Buchhaltungsplan",
		// Joined prose: too long for the too-long rule and rich in vowels.
		"Call me at your earliest convenience",
		// Punctuation keeps these out of the letter-only analysis.
		"Please contact me.",
		"Hallo, wie geht's?",
		"CallbackPlease",
	}

	for _, message := range usefulMessages {
		s.Require().False(isLowQualityMessage(message), message)
	}
}

// TestLowQualityMessageDetectionKnownSingleCaseResidual documents strings
// that still pass on purpose: generated text that suppresses case entirely
// (uniform case) produces too few transitions to flag, and flagging keyboard
// mashing as a class would hit legitimate users too. That residual is better
// handled by captcha and rate limiting than by these heuristics.
func (s *ContactTestSuite) TestLowQualityMessageDetectionKnownSingleCaseResidual() {
	s.Require().False(isLowQualityMessage("xqzjvkdfhmnbvqzxt"))
	s.Require().False(isLowQualityMessage("xqzj vkdfh mnbv qzxt"))
}

// TestContactHandlerRejectsLeakedRealWorldSample exercises the full request
// path with the production spam sample and asserts it is now rejected.
func (s *ContactTestSuite) TestContactHandlerRejectsLeakedRealWorldSample() {
	handler := ContactHandler{
		cfg:      testConfig(),
		contacts: make(MessageChannel, 1),
	}
	form := validContactForm("")
	form.Set("message", leakedSpamSample)

	request := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	s.Require().Equal(http.StatusBadRequest, response.Code)
	s.Require().Len(handler.contacts, 0)
}
