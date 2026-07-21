// Package providererr holds the HTTP-status-to-agent.ErrorCode mapping
// shared verbatim by every first-class provider adapter's errors.go. It
// exists purely to avoid maintaining the same switch in three places; it is
// not part of the public API.
package providererr

import agent "github.com/prasenjit-net/go-agent"

// ClassifyStatus maps a vendor HTTP response status onto the unified
// agent.ErrorCode taxonomy and reports whether that class of error is
// generally safe to retry. Providers with a vendor-specific status this
// mapping doesn't cover (e.g. Claude's 529 "overloaded") check for it before
// falling back to ClassifyStatus.
func ClassifyStatus(status int) (code agent.ErrorCode, retryable bool) {
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
