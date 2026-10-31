package groq

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS groq: stop: stopSequences, so an explicitly empty list is sent.
func TestR74prova_GroqEmptyStopSequencesSent(t *testing.T) {
	model := NewLanguageModel(makeTestGroqProvider(), "llama-3.3-70b-versatile")
	body := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}, false)
	if v, ok := body["stop"]; !ok || len(v.([]string)) != 0 {
		t.Fatalf("stop = %#v (present %v), want []", v, ok)
	}
	body = model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["stop"]; ok {
		t.Fatalf("nil stopSequences sent: %#v", body["stop"])
	}
}
