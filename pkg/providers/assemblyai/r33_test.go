package assemblyai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `?? undefined` keeps "" (only null/undefined are skipped), and
// `boostParam != null` / `languageCode != null` still drive the warnings.
func TestEmptyStringOptionsAreSent(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), ModelUniversal3Pro)
	body, warnings := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{
		"assemblyai": map[string]interface{}{
			"prompt": "", "domain": "", "webhookUrl": "", "boostParam": "", "languageCode": "",
			"languageDetection":        true,
			"languageDetectionOptions": map[string]interface{}{"fallbackLanguage": ""},
		},
	}})
	for _, k := range []string{"prompt", "domain", "webhook_url", "boost_param", "language_code"} {
		if v, ok := body[k]; !ok || v != "" {
			t.Errorf("%s = %v (present %v)", k, v, ok)
		}
	}
	ldo, _ := body["language_detection_options"].(map[string]interface{})
	if v, ok := ldo["fallback_language"]; !ok || v != "" {
		t.Errorf("fallback_language = %v", ldo)
	}
	var boost, lang bool
	for _, w := range warnings {
		boost = boost || (w.Type == "deprecated" && w.Setting == "boostParam")
		lang = lang || (w.Type == "other" && len(w.Message) > 18 && w.Message[:18] == "'languageDetection")
	}
	if !boost || !lang {
		t.Fatalf("warnings = %+v", warnings)
	}
}
