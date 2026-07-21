package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/internal/providererrtest"
)

func TestTranslateError(t *testing.T) {
	providererrtest.AssertNilPassesThrough(t, translateError)
	providererrtest.AssertWrapsUnknownError(t, translateError)
	providererrtest.AssertClassifiesRateLimit(t, translateError,
		newTestAPIError(429, http.Header{"Retry-After": []string{"5"}}), "claude", 5)
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
func newTestAPIError(status int, header http.Header) *anthropic.Error {
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
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
