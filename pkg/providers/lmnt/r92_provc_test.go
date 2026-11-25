package lmnt

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcTypedOptionLiterals (r92 provc-07, typed-option ruling):
// typed maps and every integer kind read like the decoded JSON options.
func TestR92provcTypedOptionLiterals(t *testing.T) {
	sm := &SpeechModel{provider: New(Config{APIKey: "k"}), modelID: "aurora"}
	for name, options := range map[string]interface{}{
		"map[string]float64":           map[string]float64{"speed": 1.5, "sampleRate": 16000},
		"map[string]interface{} int32": map[string]interface{}{"speed": float32(1.5), "sampleRate": int32(16000)},
	} {
		body, _ := sm.buildRequestBody(&provider.SpeechGenerateOptions{Text: "hi", ProviderOptions: map[string]interface{}{"lmnt": options}})
		if body["speed"] != 1.5 || body["sample_rate"] != 16000 {
			t.Errorf("%s: body = %v", name, body)
		}
	}
}
