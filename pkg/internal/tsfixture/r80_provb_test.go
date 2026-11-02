package tsfixture

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// GenerateContentToTS places GenerateResult.Text after the leading reasoning
// parts, as pkg/ai generateResultContentParts does (TS doGenerate pushes
// reasoning_content before the text).
func TestR80provbGenerateContentTextAfterReasoning(t *testing.T) {
	got := GenerateContentToTS(&types.GenerateResult{
		Text:    "A",
		Content: []types.ContentPart{types.ReasoningContent{Text: "R"}},
	})
	want := []interface{}{
		map[string]interface{}{"type": "reasoning", "text": "R"},
		map[string]interface{}{"type": "text", "text": "A"},
	}
	if d := Diff("", Normalize(want), Normalize(jsonValue(got))); d != "" {
		t.Fatalf("content (- want, + got):\n%s", d)
	}
}

// A snapshot of a path over the stream (`events.filter(e => e.type ===
// 'text-delta')`) is canonicalized as a stream on both sides: the TS
// snapshot and the Go value (google "should stream server-side toolCall and
// toolResponse parts (tool combination)", R80 carry-in item 6).
func TestR80provbStreamPathSnapshotCanonicalized(t *testing.T) {
	cases := []Case{{
		Test:     "filtered stream snapshot",
		CallKind: "stream",
		ModelID:  "m",
		Call:     map[string]interface{}{},
		Response: &Response{Kind: "json", Body: []byte(`{}`)},
		Aliases:  map[string]string{"events": "result.stream"},
		Exps: []Expectation{{
			Expr: "events.filter(e => e.type === 'text-delta')",
			Snap: []interface{}{map[string]interface{}{"type": "text-delta", "id": "0", "delta": "It is sunny.", "providerMetadata": map[string]interface{}{"__undef": true}}},
		}},
	}}
	EvalPathSnapshots(t)
	Run(t, cases, func(string, string) provider.LanguageModel {
		return &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, ID: "0", Text: "It is "},
				{Type: provider.ChunkTypeText, ID: "0", Text: "sunny."},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
	}, nil)
}
