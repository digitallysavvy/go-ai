package huggingface

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provbEmptyStopSequencesWarn: TS warns on `stopSequences != null`,
// so an empty array warns too.
func TestR95provbEmptyStopSequencesWarn(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}
	_, warnings, err := NewLanguageModel(New(Config{APIKey: "k"}), "m").getArgs(opts)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		if w.Feature == "stopSequences" {
			return
		}
	}
	t.Fatalf("warnings = %+v, want the stopSequences warning", warnings)
}
