package gemini

import (
	"testing"

	"google.golang.org/genai"

	"github.com/prasenjit-net/go-agent/internal/providererrtest"
)

func TestTranslateError(t *testing.T) {
	providererrtest.AssertNilPassesThrough(t, translateError)
	providererrtest.AssertWrapsUnknownError(t, translateError)
	// Unlike Claude/OpenAI, genai.APIError exposes no response headers, so
	// there is no Retry-After to propagate — pass 0 to assert it stays zero.
	apiErr := genai.APIError{Code: 429, Message: "rate limited", Status: "RESOURCE_EXHAUSTED"}
	providererrtest.AssertClassifiesRateLimit(t, translateError, apiErr, "gemini", 0)
}

// The status -> agent.ErrorCode mapping itself is tested once, in
// internal/providererr, which this package's translateError delegates to;
// TestTranslateError above already exercises that delegation end-to-end for
// the 429 case.
