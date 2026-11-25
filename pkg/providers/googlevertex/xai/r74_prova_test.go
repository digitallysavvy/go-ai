package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-compatible: stop: stopSequences, so an explicitly empty list is
// sent.
func TestR74prova_VertexXAIEmptyStopSequencesSent(t *testing.T) {
	prov, err := New(Config{Project: "test-project", AccessToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	lm, err := prov.LanguageModel(ModelGrok41FastReasoning)
	if err != nil {
		t.Fatal(err)
	}
	model := lm.(*LanguageModel)
	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}, false)
	if v, ok := body["stop"]; !ok || len(v.([]string)) != 0 {
		t.Fatalf("stop = %#v (present %v), want []", v, ok)
	}
	body = model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["stop"]; ok {
		t.Fatalf("nil stopSequences sent: %#v", body["stop"])
	}
}
