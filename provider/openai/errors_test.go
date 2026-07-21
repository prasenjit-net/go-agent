package openai

import (
	"errors"
	"net/http"
	"testing"

	"github.com/openai/openai-go/v3"

	agent "github.com/prasenjit-net/go-agent"
)

func TestTranslateError_Nil(t *testing.T) {
	if got := translateError(nil); got != nil {
		t.Errorf("translateError(nil) = %v, want nil", got)
	}
}

func TestTranslateError_NonAPIError(t *testing.T) {
	cause := errors.New("connection reset")
	got := translateError(cause)

	var e *agent.Error
	if !errors.As(got, &e) {
		t.Fatalf("translateError result is not an *agent.Error: %v", got)
	}
	if e.Code != agent.ErrProviderInternal {
		t.Errorf("Code = %v, want ErrProviderInternal", e.Code)
	}
	if !e.Retryable {
		t.Error("a transport-level error should be marked retryable")
	}
	if !errors.Is(got, cause) {
		t.Error("translated error should wrap the original cause")
	}
}

func TestTranslateError_APIError(t *testing.T) {
	apiErr := newTestAPIError(t, 429, http.Header{"Retry-After": []string{"5"}})
	got := translateError(apiErr)

	var e *agent.Error
	if !errors.As(got, &e) {
		t.Fatalf("translateError result is not an *agent.Error: %v", got)
	}
	if e.Code != agent.ErrRateLimited {
		t.Errorf("Code = %v, want ErrRateLimited", e.Code)
	}
	if !e.Retryable {
		t.Error("429 should be marked retryable")
	}
	if e.RetryAfter.Seconds() != 5 {
		t.Errorf("RetryAfter = %v, want 5s", e.RetryAfter)
	}
	if e.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", e.Provider)
	}
}

// The status -> agent.ErrorCode mapping itself is tested once, in
// internal/providererr, which this package's translateError delegates to;
// TestTranslateError_APIError above already exercises that delegation
// end-to-end for the 429 case.

// newTestAPIError builds an *openai.Error with a non-nil Request/Response
// (required by its Error() method, which dereferences both unconditionally)
// and the given status code and response headers.
func newTestAPIError(t *testing.T, status int, header http.Header) *openai.Error {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("building test request: %v", err)
	}
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
