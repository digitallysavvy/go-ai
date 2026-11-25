package huggingface

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS huggingfaceErrorDataSchema: {error:{message,type?,code?}} gives its
// message; every other body gives the HTTP status text.
func TestParseHuggingFaceErrorFollowsTSSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want, wantCode string
		status                     int
	}{
		{"schema", `{"error":{"message":"bad","type":"t","code":"c"}}`, "bad", "c", 400},
		{"flat error string is off schema", `{"error":"nope"}`, "Unauthorized", "", 401},
		{"non string code is off schema", `{"error":{"message":"bad","code":5}}`, "Bad Request", "", 400},
		{"plain text", `oops`, "Bad Gateway", "", 502},
		{"empty", ``, "Service Unavailable", "", 503},
	} {
		err := parseHuggingFaceError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: %T", c.name, err)
		}
		if perr.Message != c.want || perr.ErrorCode != c.wantCode {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", c.name, perr.Message, perr.ErrorCode, c.want, c.wantCode)
		}
	}
}
