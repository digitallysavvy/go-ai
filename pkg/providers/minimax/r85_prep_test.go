package minimax

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 61a315e333: "none" goes through the shared rounding, so it maps to the
// lowest supported level with the shared compatibility warning text.
func TestR85prepMiniMaxReasoningNoneSharedWarning(t *testing.T) {
	s := newM31Server(t, false)
	maxTokens := 16384
	res, err := m31Generate(t, s, "minimax-m3.1-flash", &provider.GenerateOptions{
		Prompt: m31Prompt(m31Text("Hello")), Reasoning: levelPtr(types.ReasoningNone), MaxTokens: &maxTokens,
	})
	if err != nil {
		t.Fatal(err)
	}
	oc, _ := s.body(t, 0)["output_config"].(map[string]interface{})
	if oc["effort"] != "low" {
		t.Fatalf("output_config = %#v, want effort low", oc)
	}
	want := `reasoning "none" is not directly supported by this model. mapped to effort "low".`
	if len(res.Warnings) != 1 || res.Warnings[0].Type != "compatibility" || res.Warnings[0].Feature != "reasoning" || res.Warnings[0].Details != want {
		t.Fatalf("warnings = %#v, want one compatibility warning %q", res.Warnings, want)
	}
}
