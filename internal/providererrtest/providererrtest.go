// Package providererrtest holds assertions shared by every first-class
// provider's errors_test.go. Each provider's translateError documents (and
// its errors.go comment states verbatim) the same two guarantees regardless
// of vendor: translate(nil) is nil, and a non-vendor error (a transport
// failure that never reached the API) is wrapped as agent.ErrProviderInternal,
// retryable, with the cause preserved. Testing that shared contract once
// here — instead of three near-identical copies — is what let this package
// clear SonarCloud's duplication gate without weakening any provider's
// coverage.
package providererrtest

import (
	"errors"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
)

// TranslateFunc matches the signature every first-class provider's
// unexported translateError implements.
type TranslateFunc func(err error) error

// AssertNilPassesThrough fails t unless translate(nil) == nil.
func AssertNilPassesThrough(t *testing.T, translate TranslateFunc) {
	t.Helper()
	if got := translate(nil); got != nil {
		t.Errorf("translate(nil) = %v, want nil", got)
	}
}

// AssertWrapsUnknownError fails t unless translate wraps a plain,
// non-vendor error as agent.ErrProviderInternal, retryable, with the cause
// preserved for errors.Is/errors.As.
func AssertWrapsUnknownError(t *testing.T, translate TranslateFunc) {
	t.Helper()
	cause := errors.New("connection reset")
	got := translate(cause)

	var e *agent.Error
	if !errors.As(got, &e) {
		t.Fatalf("translate result is not an *agent.Error: %v", got)
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

// AssertClassifiesRateLimit fails t unless translate(apiErr) — a
// vendor-specific fixture representing an HTTP 429 — maps to
// agent.ErrRateLimited, retryable, and tagged with provider. When
// wantRetryAfterSeconds is nonzero, RetryAfter must match it exactly.
func AssertClassifiesRateLimit(t *testing.T, translate TranslateFunc, apiErr error, provider string, wantRetryAfterSeconds float64) {
	t.Helper()
	got := translate(apiErr)

	var e *agent.Error
	if !errors.As(got, &e) {
		t.Fatalf("translate result is not an *agent.Error: %v", got)
	}
	if e.Code != agent.ErrRateLimited {
		t.Errorf("Code = %v, want ErrRateLimited", e.Code)
	}
	if !e.Retryable {
		t.Error("429 should be marked retryable")
	}
	if e.Provider != provider {
		t.Errorf("Provider = %q, want %q", e.Provider, provider)
	}
	if wantRetryAfterSeconds != 0 && e.RetryAfter.Seconds() != wantRetryAfterSeconds {
		t.Errorf("RetryAfter = %v, want %vs", e.RetryAfter, wantRetryAfterSeconds)
	}
}
