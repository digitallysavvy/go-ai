package elevenlabs

import (
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS elevenlabsErrorDataSchema: only {"error":{"message","code"}} bodies give
// their message; ElevenLabs' common {"detail":...} body gives the status text.
func TestHandleTranscriptionErrorFollowsTSErrorSchema(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
		status           int
	}{
		{"schema", `{"error":{"message":"bad","code":1}}`, "bad", 400},
		{"detail string is off schema", `{"detail":"nope"}`, "Unprocessable Entity", 422},
		{"detail object is off schema", `{"detail":{"status":"x","message":"y"}}`, "Unprocessable Entity", 422},
		{"plain text", `oops`, "Bad Gateway", 502},
	} {
		err := handleTranscriptionError(&internalhttp.HTTPStatusError{StatusCode: c.status, Body: []byte(c.body)})
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) || perr.Message != c.want {
			t.Errorf("%s: got %v, want message %q", c.name, err, c.want)
		}
	}
}
