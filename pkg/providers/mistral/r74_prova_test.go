package mistral

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS mistral chat and conversation models: stop: stopSequences, so an
// explicitly empty list is sent and nil stays absent.
func TestR74prova_MistralEmptyStopSequencesSent(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	chat := NewLanguageModel(prov, "mistral-large-latest")
	conv := prov.ConversationModel("mistral-medium-latest")
	for _, stop := range [][]string{{}, nil} {
		body := chat.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: stop}, false)
		v, ok := body["stop"]
		if ok != (stop != nil) || (ok && len(v.([]string)) != 0) {
			t.Fatalf("chat stop = %#v (present %v) for %#v", v, ok, stop)
		}
		req, _, err := conv.buildRequest(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: stop})
		if err != nil {
			t.Fatal(err)
		}
		v, ok = req["completion_args"].(map[string]interface{})["stop"]
		if ok != (stop != nil) || (ok && len(v.([]string)) != 0) {
			t.Fatalf("conversation stop = %#v (present %v) for %#v", v, ok, stop)
		}
	}
}
