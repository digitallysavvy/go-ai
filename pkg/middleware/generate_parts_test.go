package middleware

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestExtractReasoningGenerateEmitsReasoningContent(t *testing.T) {
	m := &mockLanguageModel{generateResult: &types.GenerateResult{
		Text:    "<think>plan</think>answer",
		Content: []types.ContentPart{types.TextContent{Text: "<think>plan</think>answer"}},
	}}
	w := WrapLanguageModel(m, []*LanguageModelMiddleware{ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think"})}, nil, nil)
	res, err := w.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 2 {
		t.Fatalf("content=%#v", res.Content)
	}
	if r, ok := res.Content[0].(types.ReasoningContent); !ok || r.Text != "plan" {
		t.Fatalf("content[0]=%#v", res.Content[0])
	}
	if tc, ok := res.Content[1].(types.TextContent); !ok || tc.Text != "answer" || res.Text != "answer" {
		t.Fatalf("content[1]=%#v text=%q", res.Content[1], res.Text)
	}
}

func TestExtractReasoningGenerateTextOnlyResult(t *testing.T) {
	m := &mockLanguageModel{generateResult: &types.GenerateResult{Text: "a<think>x</think>b"}}
	w := WrapLanguageModel(m, []*LanguageModelMiddleware{ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think"})}, nil, nil)
	res, _ := w.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if res.Text != "a\nb" || len(res.Content) != 2 {
		t.Fatalf("text=%q content=%#v", res.Text, res.Content)
	}
}

func TestExtractJSONGenerateTransformsContentTextParts(t *testing.T) {
	m := &mockLanguageModel{generateResult: &types.GenerateResult{
		Text:    "```json\n{}\n```",
		Content: []types.ContentPart{types.TextContent{Text: "```json\n{}\n```"}},
	}}
	w := WrapLanguageModel(m, []*LanguageModelMiddleware{ExtractJSONMiddleware(nil)}, nil, nil)
	res, _ := w.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if tc := res.Content[0].(types.TextContent); tc.Text != "{}" || res.Text != "{}" {
		t.Fatalf("content=%#v text=%q", res.Content, res.Text)
	}
}

func collectTypes(t *testing.T, chunks []*provider.StreamChunk) []string {
	t.Helper()
	m := &mockLanguageModel{stream: &mockTextStream{chunks: chunks}}
	w := WrapLanguageModel(m, []*LanguageModelMiddleware{ExtractJSONMiddleware(nil)}, nil, nil)
	s, err := w.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			out = append(out, "text:"+c.Text)
		} else {
			out = append(out, string(c.Type))
		}
	}
	return out
}

// The held-back tail is flushed before text-end and finish, never after.
func TestExtractJSONStreamFlushesBeforeTextEnd(t *testing.T) {
	got := collectTypes(t, []*provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "0"},
		{Type: provider.ChunkTypeText, ID: "0", Text: "{\"a\":1}``"},
		{Type: provider.ChunkTypeTextEnd, ID: "0"},
		{Type: provider.ChunkTypeFinish},
	})
	want := []string{string(provider.ChunkTypeTextStart), "text:{\"a\":1}", "text:``", string(provider.ChunkTypeTextEnd), string(provider.ChunkTypeFinish)}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
