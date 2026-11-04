package vercel

import (
	"testing"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

// TS utils.ts isSandboxNameConflictError over the @vercel/sandbox 3.9.0
// APIError: message "Status code N is not ok: <server message>" and
// json.error.message; the Go error prefix "vercel sandbox API" is not tested.
func TestR87toolsSandboxNameConflictUsesTSMessages(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   bool
	}{
		{409, `{"error":{"message":"Sandbox name already exists"}}`, true},
		{400, `{"error":{"message":"a sandbox with this name already exists"}}`, true},
		{409, `{"error":{"message":"name already exists"}}`, false},
		{409, `{"error":{"message":"Sandbox name already exists","code":7}}`, true},
		{500, `{"error":{"message":"Sandbox name already exists"}}`, false},
		{409, `{}`, false},
	} {
		err := toAPIError(&httpinternal.HTTPStatusError{StatusCode: tc.status, Body: []byte(tc.body)})
		if got := isSandboxNameConflictError(err); got != tc.want {
			t.Errorf("%d %s: conflict = %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}
