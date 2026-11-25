package azure

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS azure-mai-image-model.ts maiFailedResponseHandler: the message of a body
// that passes maiErrorSchema ({error: {code?: string|number|null, message:
// string}}), an empty one included; any other body gets the status text.
func TestR83provbMaiImageErrorMessageSchema(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"error":{"code":"BadRequest","message":"bad prompt"}}`, "bad prompt"},
		{`{"error":{"code":400,"message":""}}`, ""},
		{`{"error":{"code":null,"message":"m"}}`, "m"},
		{`{"error":{"code":true,"message":"m"}}`, "Bad Request"},
		{`{"error":{"code":"x"}}`, "Bad Request"},
		{`not json`, "Bad Request"},
	}
	for _, c := range cases {
		var perr *providererrors.ProviderError
		if !errors.As(azureMaiImageAPIError(400, nil, []byte(c.body)), &perr) {
			t.Fatalf("%s: not a ProviderError", c.body)
		}
		if perr.Message != c.want {
			t.Errorf("%s: message %q, want %q", c.body, perr.Message, c.want)
		}
	}
}
