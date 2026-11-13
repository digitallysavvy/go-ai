package elevenlabs

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS elevenlabsErrorDataSchema: since TS 7062ab6818 the {"detail":...} shapes
// give their message too; a body outside the schema gives the status text.
func TestHandleTranscriptionErrorFollowsTSErrorSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"schema", `{"error":{"message":"bad","code":1}}`, "bad", 400},
		{"detail string", `{"detail":"nope"}`, "nope", 422},
		{"detail object", `{"detail":{"status":"x","message":"y"}}`, "y", 422},
		{"detail object off schema", `{"detail":{"status":"x"}}`, "Unprocessable Entity", 422},
		{"plain text", `oops`, "Bad Gateway", 502},
	} {
		err := handleTranscriptionError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != c.want {
			t.Errorf("%s: got %v, want message %q", c.name, err, c.want)
		}
	}
}
