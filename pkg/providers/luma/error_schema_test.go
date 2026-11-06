package luma

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS lumaErrorSchema: every detail entry needs type, loc, msg and input (ctx
// optional with a required `expected`); the message is detail[0].msg, and any
// other body gives the HTTP status text.
func TestParseErrorResponseFollowsTSSchema(t *testing.T) {
	m := &ImageModel{}
	for _, c := range []struct{ name, body, want string }{
		{"valid", `{"detail":[{"type":"t","loc":["body","prompt"],"msg":"too long","input":"x"}]}`, "too long"},
		{"missing input", `{"detail":[{"type":"t","loc":[],"msg":"m"}]}`, "Bad Request"},
		{"missing loc", `{"detail":[{"type":"t","msg":"m","input":"x"}]}`, "Bad Request"},
		{"non string input", `{"detail":[{"type":"t","loc":[],"msg":"m","input":{"a":1}}]}`, "Bad Request"},
		{"ctx without expected", `{"detail":[{"type":"t","loc":[],"msg":"m","input":"x","ctx":{}}]}`, "Bad Request"},
		{"empty detail", `{"detail":[]}`, "Bad Request"},
		{"plain text", `oops`, "Bad Request"},
	} {
		var perr *providererrors.ProviderError
		if err := m.parseErrorResponse([]byte(c.body), 400); !errors.As(err, &perr) || perr.Message != c.want {
			t.Errorf("%s: got %v, want message %q", c.name, err, c.want)
		}
	}
}
