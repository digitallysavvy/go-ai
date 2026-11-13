package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// provg CR §2 (round 96): googleSpeechResponseSchema usageMetadata and the
// Interactions transcription usage are z.record(z.string(), z.json()): a
// non-finite number (JSON.parse of 1e400) fails, a finite one passes.
func TestR96integUsageJSONValue(t *testing.T) {
	speech := providerutils.NodeResponseValidator("google.speech", zGoogleSpeech)
	transcription := providerutils.NodeResponseValidator("google.transcription", zGoogleInteractionsTranscription)
	for _, c := range []struct {
		name    string
		ok, bad string
	}{
		{"speech", `{"usageMetadata":{"promptTokenCount":3,"d":[{"x":1}]}}`, `{"usageMetadata":{"d":[{"x":1e400}]}}`},
		{"transcription", `{"usage":{"total_tokens":3}}`, `{"usage":{"total_tokens":-1e400}}`},
	} {
		v := speech
		if c.name == "transcription" {
			v = transcription
		}
		if err := v(200, nil, []byte(c.ok)); err != nil {
			t.Errorf("%s: finite: err = %v", c.name, err)
		}
		if err := v(200, nil, []byte(c.bad)); err == nil {
			t.Errorf("%s: 1e400: err = nil, want Invalid JSON response", c.name)
		}
	}
}
