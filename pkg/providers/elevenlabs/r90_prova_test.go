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
