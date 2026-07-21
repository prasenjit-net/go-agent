package openai

import (
	"errors"
	"strconv"
	"time"

	"github.com/openai/openai-go/v3"

	agent "github.com/prasenjit-net/go-agent"
	"github.com/prasenjit-net/go-agent/internal/providererr"
)

// translateError maps an error returned by the OpenAI SDK onto the unified
// agent.Error taxonomy. Errors that aren't *openai.Error (e.g. a network
// failure before any HTTP response) are wrapped as agent.ErrProviderInternal
// and marked retryable, since a transport-level failure is generally safe
// to retry.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return &agent.Error{Provider: "openai", Code: agent.ErrProviderInternal, Message: err.Error(), Retryable: true, Cause: err}
	}

	code, retryable := providererr.ClassifyStatus(apiErr.StatusCode)
	e := &agent.Error{
		Provider:  "openai",
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
