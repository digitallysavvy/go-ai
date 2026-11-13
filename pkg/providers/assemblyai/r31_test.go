package assemblyai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `?? undefined` keeps an empty array (and `wordBoost != null` warns for
// one); only an absent/null option is skipped.
func TestEmptyArrayOptionsAreSentAndWordBoostWarns(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), ModelUniversal3Pro)
	body, warnings := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{
		"assemblyai": map[string]interface{}{
			"wordBoost": []interface{}{}, "keytermsPrompt": []interface{}{}, "customSpelling": []interface{}{},
			"redactPiiPolicies": []interface{}{}, "redactStaticEntities": map[string]interface{}{},
		},
	}})
	for _, k := range []string{"word_boost", "keyterms_prompt", "custom_spelling", "redact_pii_policies", "redact_static_entities"} {
		if v, ok := body[k]; !ok || v == nil {
			t.Errorf("%s missing from %v", k, body)
		}
	}
	var deprecated, redact bool
	for _, w := range warnings {
		deprecated = deprecated || (w.Type == "deprecated" && w.Setting == "wordBoost")
		redact = redact || (w.Type == "other" && len(w.Message) > 20 && w.Message[:20] == "'redactPiiReturnUnre")
	}
	if !deprecated || !redact {
		t.Fatalf("warnings = %+v", warnings)
	}
}
