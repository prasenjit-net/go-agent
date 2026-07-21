package claude

import (
	"errors"
	"strconv"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/internal/providererr"
)

// translateError maps an error returned by the Anthropic SDK onto the
// unified agent.Error taxonomy. Errors that aren't *anthropic.Error (e.g. a
// network failure before any HTTP response) are wrapped as
// agent.ErrProviderInternal and marked retryable, since a transport-level
// failure is generally safe to retry.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *anthropic.Error
	if !errors.As(err, &apiErr) {
		return &agent.Error{Provider: "claude", Code: agent.ErrProviderInternal, Message: err.Error(), Retryable: true, Cause: err}
	}

	code, retryable := classifyStatus(apiErr.StatusCode)
	e := &agent.Error{
		Provider:  "claude",
		Code:      code,
		Message:   apiErr.Error(),
		Retryable: retryable,
		Cause:     err,
	}
	if apiErr.Response != nil {
		if ra := apiErr.Response.Header.Get("Retry-After"); ra != "" {
			if secs, perr := strconv.Atoi(ra); perr == nil {
				e.RetryAfter = time.Duration(secs) * time.Second
			}
		}
	}
	return e
}

// classifyStatus maps a Claude-specific status onto the unified taxonomy
// before falling back to the mapping every first-class adapter shares.
// Claude is the only vendor with a literal "overloaded" status (529); the
// rest of the mapping is identical across providers.
func classifyStatus(status int) (agent.ErrorCode, bool) {
	if status == 529 {
		return agent.ErrOverloaded, true
	}
	return providererr.ClassifyStatus(status)
}
