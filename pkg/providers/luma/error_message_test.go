package luma

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonErrorResponseHandler: a body that does not match the error
// schema gives the HTTP status text as the message.
func TestParseErrorResponseMessage(t *testing.T) {
	m := &ImageModel{}
	cases := []struct {
		body   string
		status int
		want   string
	}{
		{`{"detail":[{"type":"x","loc":["a"],"msg":"bad prompt","input":""}]}`, 422, "bad prompt"},
		{`<html>oops</html>`, 502, "Bad Gateway"},
		{``, 400, "Bad Request"},
		{`{"other":1}`, 400, "Bad Request"},
	}
	for _, tc := range cases {
		var perr *providererrors.ProviderError
		if err := m.parseErrorResponse([]byte(tc.body), tc.status); !errors.As(err, &perr) || perr.Message != tc.want || perr.StatusCode != tc.status {
			t.Errorf("body %q: err = %v, want %q/%d", tc.body, err, tc.want, tc.status)
		}
	}
}
