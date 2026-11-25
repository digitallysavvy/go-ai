package topaz

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS topazErrorDataSchema: a body that does not match gives the HTTP status
// text; matching bodies keep the existing message rules.
func TestNewTopazAPIErrorFollowsTSSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"message with code", `{"message":"bad","errorCode":"E1"}`, "bad (E1)", 400},
		{"empty object", `{}`, "Unknown Topaz API error", 500},
		{"fastapi detail", `{"detail":[{"msg":"a"},{"msg":"b"}]}`, "a; b", 422},
		{"plain text", `oops`, "Bad Gateway", 502},
		{"empty body", ``, "Service Unavailable", 503},
		{"message wrong type", `{"message":5}`, "Bad Request", 400},
		{"detail wrong type", `{"detail":5}`, "Bad Request", 400},
		{"array body", `[]`, "Bad Request", 400},
	} {
		err := newTopazAPIError(c.status, []byte(c.body), nil)
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != c.want {
			t.Errorf("%s: got %v, want message %q", c.name, err, c.want)
		}
	}
}
