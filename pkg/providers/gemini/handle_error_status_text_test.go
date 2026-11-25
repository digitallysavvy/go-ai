package gemini

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler: a body that is empty or does not match
// googleErrorDataSchema gives the HTTP status text.
func TestHandleErrorFallsBackToStatusText(t *testing.T) {
	m := &LanguageModel{}
	m.cfg.ProviderName = "google"
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{500, ``, "Internal Server Error"},
		{429, `not json`, "Too Many Requests"},
		{400, `{"error":{"message":"bad"}}`, "Bad Request"},
		{400, `{"error":{"code":400,"message":"bad"}}`, "Bad Request"},
		{400, `{"message":"bad"}`, "Bad Request"},
		{400, `{"error":{"code":400,"message":"bad","status":"INVALID_ARGUMENT"}}`, "bad"},
		{400, `{"error":{"code":null,"message":"bad","status":"X","details":null}}`, "bad"},
	}
	for _, c := range cases {
		err := m.handleError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != c.want {
			t.Errorf("%d %q: got %v want %q", c.status, c.body, err, c.want)
		}
	}
}
