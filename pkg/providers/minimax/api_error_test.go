package minimax

import (
	"errors"
	"testing"
)

// TS minimaxVideoFailedResponseHandler: message = error?.message ??
// 'MiniMax video generation error'; a non-JSON body gives the status text.
// The error keeps its status code (retryability) and raw body.
func TestParseAPIErrorFollowsTSHandler(t *testing.T) {
	m := &VideoModel{}
	cases := []struct {
		body      string
		status    int
		want      string
		retryable bool
	}{
		{`{"error":{"message":"bad key"}}`, 401, "bad key", false},
		{`{"type":"error","request_id":"r"}`, 401, "MiniMax video generation error", false},
		{`<html/>`, 502, "Bad Gateway", true},
		{``, 429, "Too Many Requests", true},
	}
	for _, tc := range cases {
		var e *Error
		if err := m.parseAPIError([]byte(tc.body), tc.status); !errors.As(err, &e) {
			t.Fatalf("err = %v, want *Error", err)
		} else if e.Message != tc.want || e.StatusCode != tc.status || e.ResponseBody != tc.body || e.IsRetryable() != tc.retryable {
			t.Errorf("body %q: %+v retryable=%v, want %q/%d/%v", tc.body, e, e.IsRetryable(), tc.want, tc.status, tc.retryable)
		}
	}
}
