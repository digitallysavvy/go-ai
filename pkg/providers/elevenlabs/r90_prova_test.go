package elevenlabs

import (
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createJsonResponseHandler(elevenLabsSpeechTimestampsResponseSchema)
// (elevenlabs-speech-model.ts 566): a /with-timestamps body that is not JSON
// or fails the schema is an APICallError "Invalid JSON response" carrying the
// status and body, caused by the JSONParseError / TypeValidationError.
func TestR90provaSpeechTimestampsInvalidJSONResponse(t *testing.T) {
	withTS := map[string]interface{}{"elevenlabs": map[string]interface{}{"withTimestamps": true}}
	for name, body := range map[string]string{
		"not json":        `{"audio_base64":`,
		"schema mismatch": `{"audio_base64":5}`,
	} {
		p, _ := r85provaServer(t, "application/json", []byte(body))
		_, err := NewSpeechModel(p, "eleven_multilingual_v2").DoGenerate(t.Context(), &provider.SpeechGenerateOptions{Text: "Hi", Voice: "v", ProviderOptions: withTS})
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" || pe.StatusCode != 200 || pe.ResponseBody != body || pe.Cause == nil {
			t.Errorf("%s: err = %#v", name, err)
			continue
		}
		if name == "schema mismatch" && !strings.Contains(pe.Cause.Error(), `"path": [`) {
			t.Errorf("%s: cause = %v", name, pe.Cause)
		}
	}
}

// TS elevenlabsErrorDataSchema is a zod union: a body whose detail does not
// match the {detail} member but whose error object does is read as the
// {error} member (zod strips detail), so the message is error.message.
func TestR90provaErrorDetailMismatchUsesErrorObject(t *testing.T) {
	for body, want := range map[string]string{
		`{"detail":5,"error":{"message":"boom","code":1}}`:     "boom",
		`{"detail":null,"error":{"message":"boom2","code":2}}`: "boom2",
		`{"detail":"d","error":{"message":"x","code":1}}`:      "d",
	} {
		if got, _ := elevenlabsErrorMessage(400, []byte(body)); got != want {
			t.Errorf("%s: message = %q, want %q", body, got, want)
		}
	}
}
