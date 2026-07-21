package gemini

import (
	"errors"
	"testing"

	"google.golang.org/genai"

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
	apiErr := genai.APIError{Code: 429, Message: "rate limited", Status: "RESOURCE_EXHAUSTED"}
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
	if e.Provider != "gemini" {
		t.Errorf("Provider = %q, want gemini", e.Provider)
	}
	// Unlike Claude/OpenAI, genai.APIError exposes no response headers, so
	// there is no Retry-After to propagate — RetryAfter should stay zero.
	if e.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 (genai.APIError has no header access)", e.RetryAfter)
	}
}

// The status -> agent.ErrorCode mapping itself is tested once, in
// internal/providererr, which this package's translateError delegates to;
// TestTranslateError_APIError above already exercises that delegation
// end-to-end for the 429 case.
