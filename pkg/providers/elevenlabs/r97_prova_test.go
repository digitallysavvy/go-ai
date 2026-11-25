package elevenlabs

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r97provaForbiddenCause reports a JSONParseError whose cause is the
// secureJsonParse SyntaxError itself (TS safeParseJSON), not a nested
// JSONParseError.
func r97provaForbiddenCause(t *testing.T, err error) {
	t.Helper()
	var parseErr *providererrors.JSONParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("err = %T %v, want a JSONParseError", err, err)
	}
	if parseErr.Cause != jsonexact.ErrForbiddenPrototype {
		t.Fatalf("cause = %T %v, want the forbidden prototype SyntaxError", parseErr.Cause, parseErr.Cause)
	}
}

// The music metadata part and the /with-timestamps body go through TS
// safeParseJSON: a __proto__ key is a JSONParseError caused by the
// SyntaxError.
func TestR97provaForbiddenPrototypeParseErrors(t *testing.T) {
	_, err := parseMusicMetadata(`{"words_timestamps":null,"__proto__":{}}`)
	r97provaForbiddenCause(t, err)

	_, err = parseSpeechTimestampsResponse(200, nil, []byte(`{"audio_base64":"","__proto__":{}}`))
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != providererrors.InvalidJSONResponseMessage {
		t.Fatalf("err = %v, want the Invalid JSON response APICallError", err)
	}
	r97provaForbiddenCause(t, pe.Cause)
}
