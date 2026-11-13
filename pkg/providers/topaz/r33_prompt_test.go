package topaz

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// options.prompt.trim() is JS trim: NEL is a prompt, BOM-and-space is blank.
func TestR33_PromptTrimIsJSTrim(t *testing.T) {
	for prompt, want := range map[string]int{"\u0085": 1, "\ufeff ": 0} {
		var warnings []types.Warning
		addVideoUnsupportedWarnings(&provider.VideoModelV3CallOptions{Prompt: prompt, PromptSet: true}, &warnings)
		got := 0
		for _, w := range warnings {
			if w.Feature == "prompt" {
				got++
			}
		}
		if got != want {
			t.Errorf("prompt %q: %d prompt warnings, want %d", prompt, got, want)
		}
	}
}
