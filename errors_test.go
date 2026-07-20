package agent_test

import (
	"errors"
	"fmt"
	"testing"

	agent "github.com/prasenjit-net/go-agent"
)

func TestIsRetryable(t *testing.T) {
	retryable := &agent.Error{Code: agent.ErrRateLimited, Retryable: true}
	notRetryable := &agent.Error{Code: agent.ErrInvalidRequest, Retryable: false}
	plain := errors.New("boom")

	if !agent.IsRetryable(retryable) {
		t.Error("expected retryable error to report IsRetryable = true")
	}
	if agent.IsRetryable(notRetryable) {
		t.Error("expected non-retryable error to report IsRetryable = false")
	}
	if agent.IsRetryable(plain) {
		t.Error("a plain error should never be retryable")
	}
}

func TestCodeOf(t *testing.T) {
	e := &agent.Error{Code: agent.ErrRefusal}
	if agent.CodeOf(e) != agent.ErrRefusal {
		t.Errorf("CodeOf = %v, want ErrRefusal", agent.CodeOf(e))
	}
	if agent.CodeOf(errors.New("boom")) != agent.ErrUnknown {
		t.Errorf("CodeOf(plain error) should be ErrUnknown")
	}
}

func TestError_UnwrapAndErrorsAs(t *testing.T) {
	cause := errors.New("root cause")
	wrapped := fmt.Errorf("agent op failed: %w", &agent.Error{Code: agent.ErrProviderInternal, Cause: cause})

	var agentErr *agent.Error
	if !errors.As(wrapped, &agentErr) {
		t.Fatal("errors.As should find the wrapped *agent.Error")
	}
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is should see through *agent.Error to its Cause via Unwrap")
	}
}
