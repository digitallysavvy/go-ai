package mistral

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate pushes content parts in wire order (text before thinking stays
// before it) and skips empty parts; thinking keeps only its "text" chunks.
func TestConvertResponseKeepsWireOrderOfTextAndReasoning(t *testing.T) {
	m := &LanguageModel{modelID: "magistral-medium-latest"}
	var resp mistralResponse
	body := `{"choices":[{"message":{"content":[
		{"type":"text","text":"first"},
		{"type":"thinking","thinking":[{"type":"text","text":"think "},{"type":"reference","text":"x"},{"type":"text","text":"hard"}]},
		{"type":"text","text":""},
		{"type":"text","text":"second"}]},"finish_reason":"stop"}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	res := m.convertResponse(resp)
	if len(res.Content) != 3 {
		t.Fatalf("content = %#v", res.Content)
	}
	if p, ok := res.Content[0].(types.TextContent); !ok || p.Text != "first" {
		t.Fatalf("part 0 = %#v", res.Content[0])
	}
	if p, ok := res.Content[1].(types.ReasoningContent); !ok || p.Text != "think hard" {
		t.Fatalf("part 1 = %#v", res.Content[1])
	}
	if p, ok := res.Content[2].(types.TextContent); !ok || p.Text != "second" {
		t.Fatalf("part 2 = %#v", res.Content[2])
	}
	if res.Text != "firstsecond" {
		t.Fatalf("text = %q", res.Text)
	}
}
