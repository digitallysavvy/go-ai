package ai

import (
	"context"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

func contentKinds(parts []types.ContentPart) []string {
	var out []string
	for _, p := range parts {
		switch p.(type) {
		case types.TextContent:
			out = append(out, "text")
		case types.ReasoningContent:
			out = append(out, "reasoning")
		case types.ToolCallContent:
			out = append(out, "tool-call")
		case types.SourceContent:
			out = append(out, "source")
		default:
			out = append(out, "other")
		}
	}
	return out
}

// Providers return the answer text in GenerateResult.Text and only the other
// parts in Content; core must rebuild TS's `content` (text included, reasoning
// first) for the result, the step and the call-end event, whatever the provider.
func TestGenerateTextContentIncludesTextPart(t *testing.T) {
	cases := []struct {
		name    string
		content []types.ContentPart
		want    []string
	}{
		{"text only", nil, []string{"text"}},
		{"reasoning then text", []types.ContentPart{types.ReasoningContent{Text: "think"}}, []string{"reasoning", "text"}},
		{"text before sources", []types.ContentPart{types.SourceContent{ID: "s1", URL: "https://x.test"}}, []string{"text", "source"}},
		{"reasoning, sources", []types.ContentPart{types.ReasoningContent{Text: "a"}, types.ReasoningContent{Text: "b"}, types.SourceContent{ID: "s1", URL: "https://x.test"}}, []string{"reasoning", "reasoning", "text", "source"}},
		{"provider already supplied text", []types.ContentPart{types.ReasoningContent{Text: "a"}, types.TextContent{Text: "hello"}}, []string{"reasoning", "text"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var evContent []types.ContentPart
			m := &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "hello", Content: tc.content, FinishReason: types.FinishReasonStop}, nil
			}}
			r, err := GenerateText(context.Background(), GenerateTextOptions{
				Model: m, Prompt: "x",
				OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { evContent = e.Content },
			})
			if err != nil {
				t.Fatal(err)
			}
			for name, got := range map[string][]types.ContentPart{"result": r.Content, "step": r.Steps[0].Content, "event": evContent} {
				if k := contentKinds(got); !reflect.DeepEqual(k, tc.want) {
					t.Errorf("%s content = %v, want %v", name, k, tc.want)
				}
			}
			if r.Text != "hello" {
				t.Errorf("text = %q", r.Text)
			}
		})
	}
}
