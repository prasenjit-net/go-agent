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
