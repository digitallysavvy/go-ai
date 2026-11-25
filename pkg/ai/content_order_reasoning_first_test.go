package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS #23566: openai-compatible, groq and alibaba doGenerate put reasoning
// before text. Providers here report text in GenerateResult.Text and only the
// reasoning in Content, so the step content must rebuild reasoning, text, tool call.
func TestGenerateResultContentPartsReasoningBeforeText(t *testing.T) {
	parts := generateResultContentParts(&types.GenerateResult{
		Text:      "answer",
		Content:   []types.ContentPart{types.ReasoningContent{Text: "thinking"}},
		ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t"}},
	})
	if len(parts) != 3 {
		t.Fatalf("parts = %#v", parts)
	}
	if _, ok := parts[0].(types.ReasoningContent); !ok {
		t.Fatalf("part 0 = %#v", parts[0])
	}
	if p, ok := parts[1].(types.TextContent); !ok || p.Text != "answer" {
		t.Fatalf("part 1 = %#v", parts[1])
	}
}
