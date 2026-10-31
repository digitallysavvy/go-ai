package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func runGenerateMiddleware(t *testing.T, mw *LanguageModelMiddleware, in *types.GenerateResult) *types.GenerateResult {
	t.Helper()
	out, err := mw.WrapGenerate(context.Background(), func() (*types.GenerateResult, error) { return in, nil }, nil, &provider.GenerateOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TS #23895: structuredOutputText goes through the same transforms as text.
func TestExtractJSONTransformsStructuredOutputText(t *testing.T) {
	s := "```json\n{\"a\":1}\n```"
	out := runGenerateMiddleware(t, ExtractJSONMiddleware(nil), &types.GenerateResult{Text: s, StructuredOutputText: &s})
	if out.StructuredOutputText == nil || *out.StructuredOutputText != `{"a":1}` {
		t.Fatalf("StructuredOutputText = %v", out.StructuredOutputText)
	}
	out = runGenerateMiddleware(t, ExtractJSONMiddleware(nil), &types.GenerateResult{Text: "x"})
	if out.StructuredOutputText != nil {
		t.Fatal("StructuredOutputText must stay nil")
	}
}

func TestExtractReasoningStripsReasoningFromStructuredOutputText(t *testing.T) {
	s := `<think>why</think>{"a":1}`
	out := runGenerateMiddleware(t, ExtractReasoningMiddleware(nil), &types.GenerateResult{Text: s, StructuredOutputText: &s})
	if out.StructuredOutputText == nil || *out.StructuredOutputText != `{"a":1}` {
		t.Fatalf("StructuredOutputText = %v", out.StructuredOutputText)
	}
	empty := ""
	out = runGenerateMiddleware(t, ExtractReasoningMiddleware(nil), &types.GenerateResult{Text: "x", StructuredOutputText: &empty})
	if out.StructuredOutputText == nil || *out.StructuredOutputText != "" {
		t.Fatal("explicit empty selection must be preserved")
	}
}
