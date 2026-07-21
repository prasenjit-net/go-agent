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

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		status        int
		wantCode      agent.ErrorCode
		wantRetryable bool
	}{
		{400, agent.ErrInvalidRequest, false},
		{401, agent.ErrAuthentication, false},
		{403, agent.ErrPermission, false},
		{404, agent.ErrNotFound, false},
		{429, agent.ErrRateLimited, true},
		{529, agent.ErrOverloaded, true},
		{500, agent.ErrProviderInternal, true},
		{503, agent.ErrProviderInternal, true},
		{418, agent.ErrUnknown, false},
	}
	for _, tc := range cases {
		code, retryable := classifyStatus(tc.status)
		if code != tc.wantCode || retryable != tc.wantRetryable {
			t.Errorf("classifyStatus(%d) = (%v, %v), want (%v, %v)", tc.status, code, retryable, tc.wantCode, tc.wantRetryable)
		}
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
