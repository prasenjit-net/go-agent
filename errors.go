package agent

import (
	"errors"
	"fmt"
	"time"
)

// ErrorCode is a provider-agnostic classification of what went wrong. Every
// first-class provider adapter maps its vendor-specific error taxonomy onto
// this common set so application code can write one switch regardless of
// which provider is behind an Agent.
type ErrorCode string

const (
	ErrAuthentication    ErrorCode = "authentication_error"
	ErrPermission        ErrorCode = "permission_error"
	ErrInvalidRequest    ErrorCode = "invalid_request_error"
	ErrRateLimited       ErrorCode = "rate_limit_error"
	ErrOverloaded        ErrorCode = "overloaded_error"
	ErrContextExceeded   ErrorCode = "context_length_exceeded"
	ErrRefusal           ErrorCode = "refusal"
	ErrProviderInternal  ErrorCode = "provider_error"
	ErrMaxIterations     ErrorCode = "max_iterations_exceeded"
	ErrStreamUnsupported ErrorCode = "streaming_unsupported"
	ErrNotFound          ErrorCode = "not_found_error"
	ErrUnknown           ErrorCode = "unknown_error"
)

// Error is the unified error type returned by Provider implementations and
// by the Agent run loop. Use errors.As to recover one from a wrapped error.
type Error struct {
	Code     ErrorCode
	Provider string // the Provider.Name() that produced this error, if any
	Message  string

	// Retryable indicates whether retrying the same request may succeed.
	Retryable bool
	// RetryAfter is honored when the provider supplied an explicit delay
	// (e.g. a 429 Retry-After header); zero means "use the configured
	// RetryPolicy's computed backoff instead".
	RetryAfter time.Duration

	// Cause is the wrapped original error, including the provider SDK's own
	// error type where applicable.
	Cause error
}

func (e *Error) Error() string {
	if e.Provider != "" {
		if e.Cause != nil {
			return fmt.Sprintf("agent: %s [%s]: %s: %v", e.Provider, e.Code, e.Message, e.Cause)
		}
		return fmt.Sprintf("agent: %s [%s]: %s", e.Provider, e.Code, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("agent [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("agent [%s]: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// IsRetryable reports whether err is (or wraps) an *Error marked Retryable.
func IsRetryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}

// CodeOf returns the ErrorCode of err if it is (or wraps) an *Error, and
// ErrUnknown otherwise.
func CodeOf(err error) ErrorCode {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ErrUnknown
}
