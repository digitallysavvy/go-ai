package cartesia

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `cartesiaOptions.language != null` forwards a present "" (and lets it
// override the generic language); an unset option stays absent.
func TestR34_SpeechLanguageEmptyStringForwardedOnlyWhenPresent(t *testing.T) {
	m := &SpeechModel{modelID: "sonic-3"}
	body, _ := m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hi", Voice: "v", Language: "en",
		ProviderOptions: map[string]interface{}{"cartesia": map[string]interface{}{"language": ""}},
	})
	if lang, ok := body["language"]; !ok || lang != "" {
		t.Errorf("present empty language: got %#v, %v", lang, ok)
	}

	body, _ = m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hi", Voice: "v",
		ProviderOptions: map[string]interface{}{"cartesia": map[string]interface{}{"speed": 1.0}},
	})
	if _, ok := body["language"]; ok {
		t.Errorf("unset language must be absent: %#v", body["language"])
	}
	body, _ = m.buildRequestBody(&provider.SpeechGenerateOptions{
		Text: "hi", Voice: "v",
		ProviderOptions: map[string]interface{}{"cartesia": map[string]interface{}{"language": nil}},
	})
	if _, ok := body["language"]; ok {
		t.Errorf("null language must be absent: %#v", body["language"])
	}
}

func TestR34_SpeechWithoutVoiceErrorTextMatchesTS(t *testing.T) {
	m := &SpeechModel{modelID: "sonic-3"}
	_, err := m.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err == nil || err.Error() != "Cartesia speech models require a `voice` to be set." {
		t.Fatalf("err = %v", err)
	}
}
