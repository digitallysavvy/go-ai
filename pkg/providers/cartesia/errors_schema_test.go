package cartesia

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS cartesiaErrorDataSchema: a body matching the error schema gives its message; any other
// body gives the HTTP status text.
func TestHandleErrorFollowsTSErrorSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"schema", `{"title":"Bad","message":"nope","request_id":"r"}`, "Bad: nope", 400},
		{"missing request_id", `{"title":"Bad","message":"nope"}`, "Bad Request", 400},
		{"doc_url string", `{"title":"Bad","message":"nope","request_id":"r","doc_url":"https://d"}`, "Bad: nope", 400},
		{"doc_url null is off schema (optional, not nullish)", `{"title":"Bad","message":"nope","request_id":"r","doc_url":null}`, "Bad Request", 400},
		{"plain text", `oops`, "Bad Gateway", 502},
		{"empty", ``, "Service Unavailable", 503},
	} {
		err := handleError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: %T", c.name, err)
		}
		if perr.Message != c.want {
			t.Errorf("%s: message = %q, want %q", c.name, perr.Message, c.want)
		}
	}
}
