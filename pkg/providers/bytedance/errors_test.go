package bytedance

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/retry"
)

func TestHTTPErrorMessageFollowsTSHandler(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"nested", 400, `{"error":{"message":"bad","code":"X"}}`, "bad"},
		{"top level", 400, `{"message":"top"}`, "top"},
		{"empty object", 400, `{}`, "Unknown error"},
		{"not json", 502, `<html>`, "Bad Gateway"},
		{"empty", 503, ``, "Service Unavailable"},
		{"nested without message", 400, `{"error":{"code":"X"}}`, "Bad Request"},
		{"numeric code", 400, `{"error":{"message":"m","code":5}}`, "Bad Request"},
	}
	for _, c := range cases {
		if got := newHTTPError(c.status, []byte(c.body)).Message; got != c.want {
			t.Errorf("%s: message = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestHTTPErrorRetryClassification(t *testing.T) {
	for status, want := range map[int]bool{400: false, 404: false, 408: true, 409: true, 429: true, 500: true} {
		if got := retry.IsRetryable(newHTTPError(status, []byte(`{}`))); got != want {
			t.Errorf("status %d retryable = %v, want %v", status, got, want)
		}
	}
}
