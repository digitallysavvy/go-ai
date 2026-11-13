package ollama

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR96provbEmptyStopSequencesSent: the openai-compatible chat model sends
// `stop: stopSequences`, so an empty array is sent as `stop: []`; only an
// absent value is left out (class of r95 e10517ba9).
func TestR96provbEmptyStopSequencesSent(t *testing.T) {
	m := NewLanguageModel(New(Config{}), "llama3")
	body, _ := m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}, false)
	if stop, ok := body["stop"].([]string); !ok || len(stop) != 0 {
		t.Fatalf("stop = %#v, want []", body["stop"])
	}
	body, _ = m.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}, false)
	if _, ok := body["stop"]; ok {
		t.Fatalf("absent stopSequences sent stop = %#v", body["stop"])
	}
}
