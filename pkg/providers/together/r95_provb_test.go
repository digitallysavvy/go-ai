package together

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provbEmptyStopSequencesSent: TS sends `stop: stopSequences`, so an
// empty array is sent as `stop: []`; only an absent value is left out.
func TestR95provbEmptyStopSequencesSent(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}
	body, _ := NewLanguageModel(New(Config{APIKey: "k"}), "m").buildRequestBodyWithWarnings(opts, false)
	if stop, ok := body["stop"].([]string); !ok || len(stop) != 0 {
		t.Fatalf("stop = %#v, want []", body["stop"])
	}
}
