package hume

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR92provcTypedContextParsesLikeZod (r92 provc-06, typed-option ruling):
// TS parses providerOptions.hume.context with zod, so a voice with an id
// matches the {id, provider?} branch and drops its name. The exported
// SpeechModelOptions struct must produce the same context as the map form.
func TestR92provcTypedContextParsesLikeZod(t *testing.T) {
	m := &SpeechModel{}
	speed := 1.5
	typed := SpeechModelOptions{Context: &Context{Utterances: []Utterance{{
		Text: "hi", Speed: &speed, Voice: &UtteranceVoice{ID: "v1", Name: "Ito", Provider: "HUME_AI"},
	}}}}
	asMap := map[string]interface{}{"context": map[string]interface{}{"utterances": []interface{}{map[string]interface{}{
		"text": "hi", "speed": 1.5, "voice": map[string]interface{}{"id": "v1", "name": "Ito", "provider": "HUME_AI"},
	}}}}
	want := map[string]interface{}{"utterances": []map[string]interface{}{{
		"text": "hi", "speed": 1.5, "voice": map[string]interface{}{"id": "v1", "provider": "HUME_AI"},
	}}}
	for name, options := range map[string]interface{}{"struct": typed, "pointer": &typed, "map": asMap} {
		body, _ := m.buildRequestBody(&provider.SpeechGenerateOptions{Text: "x", ProviderOptions: map[string]interface{}{"hume": options}})
		if !reflect.DeepEqual(body["context"], want) {
			t.Errorf("%s: context = %#v", name, body["context"])
		}
	}
}
