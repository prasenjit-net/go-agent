package providererr

import (
	"testing"

	agent "github.com/prasenjit-net/go-agent"
)

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
		code, retryable := ClassifyStatus(tc.status)
		if code != tc.wantCode || retryable != tc.wantRetryable {
			t.Errorf("ClassifyStatus(%d) = (%v, %v), want (%v, %v)", tc.status, code, retryable, tc.wantCode, tc.wantRetryable)
		}
	}
}
