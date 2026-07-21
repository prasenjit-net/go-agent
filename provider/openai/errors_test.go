package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"

	"github.com/prasenjit-net/go-agent/internal/providererrtest"
)

func TestTranslateError(t *testing.T) {
	providererrtest.AssertNilPassesThrough(t, translateError)
	providererrtest.AssertWrapsUnknownError(t, translateError)
	providererrtest.AssertClassifiesRateLimit(t, translateError,
		newTestAPIError(429, http.Header{"Retry-After": []string{"5"}}), "openai", 5)
}

// The status -> agent.ErrorCode mapping itself is tested once, in
// internal/providererr, which this package's translateError delegates to;
// TestTranslateError above already exercises that delegation end-to-end for
// the 429 case.

// newTestAPIError builds an *openai.Error with a non-nil Request/Response
// (required by its Error() method, which dereferences both unconditionally)
// and the given status code and response headers.
func newTestAPIError(status int, header http.Header) *openai.Error {
	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	if header == nil {
		header = http.Header{}
	}
	return &openai.Error{
		StatusCode: status,
		Request:    req,
		Response: &http.Response{
			StatusCode: status,
			Header:     header,
			Request:    req,
		},
	}
}
