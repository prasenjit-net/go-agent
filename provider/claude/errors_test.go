package claude

import (
	"errors"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

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
	if e.Provider != "claude" {
		t.Errorf("Provider = %q, want claude", e.Provider)
	}
}

func TestClassifyStatus_OverloadedIsClaudeSpecific(t *testing.T) {
	// 529 has no equivalent in the shared internal/providererr mapping (no
	// other first-class vendor has a literal "overloaded" status) — this is
	// the one status classifyStatus handles itself before delegating.
	code, retryable := classifyStatus(529)
	if code != agent.ErrOverloaded || !retryable {
		t.Errorf("classifyStatus(529) = (%v, %v), want (ErrOverloaded, true)", code, retryable)
	}
}

func TestClassifyStatus_DelegatesUnhandledStatuses(t *testing.T) {
	// Everything except 529 defers to internal/providererr.ClassifyStatus,
	// which has its own full table test; this just confirms the delegation
	// actually happens for a representative status.
	code, retryable := classifyStatus(429)
	if code != agent.ErrRateLimited || !retryable {
		t.Errorf("classifyStatus(429) = (%v, %v), want (ErrRateLimited, true)", code, retryable)
	}
}

// newTestAPIError builds an *anthropic.Error with a non-nil Request/Response
// (required by its Error() method, which dereferences both unconditionally)
// and the given status code and response headers.
func newTestAPIError(t *testing.T, status int, header http.Header) *anthropic.Error {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	if err != nil {
		t.Fatalf("building test request: %v", err)
	}
	if header == nil {
		header = http.Header{}
	}
	return &anthropic.Error{
		StatusCode: status,
		Request:    req,
		Response: &http.Response{
			StatusCode: status,
			Header:     header,
			Request:    req,
		},
	}
}
