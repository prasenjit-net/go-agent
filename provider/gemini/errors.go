package gemini

import (
	"errors"

	"google.golang.org/genai"

	agent "github.com/prasenjit-net/go-agent"
)

// translateError maps an error returned by the GenAI SDK onto the unified
// agent.Error taxonomy. Errors that aren't a genai.APIError (e.g. a network
// failure before any HTTP response) are wrapped as agent.ErrProviderInternal
// and marked retryable, since a transport-level failure is generally safe
// to retry.
func translateError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr genai.APIError
	if !errors.As(err, &apiErr) {
		return &agent.Error{Provider: "gemini", Code: agent.ErrProviderInternal, Message: err.Error(), Retryable: true, Cause: err}
	}

	code, retryable := classifyStatus(apiErr.Code)
	return &agent.Error{
		Provider:  "gemini",
		Code:      code,
		Message:   apiErr.Error(),
		Retryable: retryable,
		Cause:     err,
	}
}

func classifyStatus(status int) (agent.ErrorCode, bool) {
	switch {
	case status == 400:
		return agent.ErrInvalidRequest, false
	case status == 401:
		return agent.ErrAuthentication, false
	case status == 403:
		return agent.ErrPermission, false
	case status == 404:
		return agent.ErrNotFound, false
	case status == 429:
		return agent.ErrRateLimited, true
	case status >= 500:
		return agent.ErrProviderInternal, true
	default:
		return agent.ErrUnknown, false
	}
}
