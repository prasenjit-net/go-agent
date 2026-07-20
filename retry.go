package agent

import (
	"math/rand"
	"time"
)

// RetryPolicy controls how Agent retries a Provider.Generate/Stream call
// that failed with a retryable error (see IsRetryable). Errors that are not
// retryable (invalid request, authentication, refusal, ...) are never
// retried regardless of policy.
type RetryPolicy struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	Jitter     bool
}

// DefaultRetryPolicy returns a conservative default: 2 retries, exponential
// backoff starting at 500ms, capped at 20s, with jitter.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 2,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   20 * time.Second,
		Jitter:     true,
	}
}

// delay computes the backoff delay before retry attempt N (0-indexed:
// attempt 0 is the delay before the first retry).
func (p RetryPolicy) delay(attempt int) time.Duration {
	d := p.BaseDelay << attempt
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	if p.Jitter && d > 0 {
		// Full jitter: uniform in [0, d).
		d = time.Duration(rand.Int63n(int64(d)))
	}
	return d
}
