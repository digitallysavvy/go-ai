package elevenlabs

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `if (options.previousRequestIds)` is true for an empty array, which is
// then sent as [].
func TestSpeechEmptyArraysAreSentLikeTS(t *testing.T) {
	m := &SpeechModel{modelID: "eleven_multilingual_v2"}
	body, _, _, _ := m.buildRequestArgs(&provider.SpeechGenerateOptions{
		Text: "x",
		ProviderOptions: map[string]interface{}{"elevenlabs": map[string]interface{}{
			"previousRequestIds":              []interface{}{},
			"nextRequestIds":                  []interface{}{},
			"pronunciationDictionaryLocators": []interface{}{},
		}},
	})
	for _, key := range []string{"previous_request_ids", "next_request_ids", "pronunciation_dictionary_locators"} {
		v, ok := body[key]
		if !ok || reflect.ValueOf(v).Len() != 0 {
			t.Errorf("%s = %v (present %v), want an empty array", key, v, ok)
		}
	}
	body, _, _, _ = m.buildRequestArgs(&provider.SpeechGenerateOptions{Text: "x"})
	if _, ok := body["previous_request_ids"]; ok {
		t.Error("absent option must not be sent")
	}
}
