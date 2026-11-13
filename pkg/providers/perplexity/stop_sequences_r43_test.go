package perplexity

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS warns when stopSequences != null, so an empty array warns as well.
func TestGetArgs_EmptyStopSequencesWarn(t *testing.T) {
	m := NewLanguageModel(New(Config{BaseURL: "http://localhost", APIKey: "k"}), "low")
	_, warnings, err := m.getArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0].Feature != "stopSequences" {
		t.Fatalf("warnings = %#v", warnings)
	}
}
