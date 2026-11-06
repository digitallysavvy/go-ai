package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r80provbContentShape(res *types.GenerateResult) []string {
	var out []string
	parts := append([]types.ContentPart(nil), res.Content...)
	hasText := false
	for _, p := range parts {
		if _, ok := p.(types.TextContent); ok {
			hasText = true
		}
	}
	if !hasText && res.Text != "" {
		parts = types.InsertLeadText(parts, res.Text)
	}
	for _, p := range parts {
		switch v := p.(type) {
		case types.TextContent:
			out = append(out, "text:"+v.Text)
		case types.ReasoningContent:
			out = append(out, "reasoning:"+v.Text)
		}
	}
	return out
}

// TS doGenerate: reasoning_content ?? reasoning first, then the content
// parts (text and thinking) in wire order.
func TestR80provbCompatResponseContentOrder(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want []string
	}{
		"reasoning field before thinking parts": {
			`{"choices":[{"message":{"reasoning_content":"R","content":[{"type":"thinking","thinking":[{"type":"text","text":"T"}]},{"type":"text","text":"A"}]}}]}`,
			[]string{"reasoning:R", "reasoning:T", "text:A"},
		},
		"text before thinking": {
			`{"choices":[{"message":{"content":[{"type":"text","text":"A"},{"type":"thinking","thinking":[{"type":"text","text":"T"}]},{"type":"text","text":"B"}]}}]}`,
			[]string{"text:A", "reasoning:T", "text:B"},
		},
		"plain": {
			`{"choices":[{"message":{"reasoning":"R","content":"A"}}]}`,
			[]string{"reasoning:R", "text:A"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			res := &types.GenerateResult{}
			if err := ApplyOpenAICompatibleChatResponse(res, []byte(tc.body), nil, "k"); err != nil {
				t.Fatal(err)
			}
			got := r80provbContentShape(res)
			if len(got) != len(tc.want) {
				t.Fatalf("content = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("content = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TS z.number() accepts a fractional prediction token count; Go truncates
// it (known difference "Fractional counts") instead of failing the call.
func TestR80provbCompatResponseFractionalPredictionTokens(t *testing.T) {
	res := &types.GenerateResult{}
	body := `{"choices":[{"message":{"content":"A"}}],"usage":{"prompt_tokens":1.5,"completion_tokens_details":{"accepted_prediction_tokens":3.7,"rejected_prediction_tokens":2}}}`
	if err := ApplyOpenAICompatibleChatResponse(res, []byte(body), nil, "k"); err != nil {
		t.Fatal(err)
	}
	meta, _ := res.ProviderMetadata["k"].(map[string]interface{})
	if meta["acceptedPredictionTokens"] != 3 || meta["rejectedPredictionTokens"] != 2 || res.Text != "A" {
		t.Fatalf("metadata = %v text %q", res.ProviderMetadata, res.Text)
	}
}
