package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS onObjectOperationEnd records ai.usage.reasoningTokens and
// ai.usage.cachedInputTokens on the generateObject root span.
func TestGenerateObject_RootSpanUsageDetails(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text:         `{"a":1}`,
				FinishReason: types.FinishReasonStop,
				Usage: types.Usage{
					InputTokens: int64Ptr(10), OutputTokens: int64Ptr(5),
					InputDetails:  &types.InputTokenDetails{CacheReadTokens: int64Ptr(3)},
					OutputDetails: &types.OutputTokenDetails{ReasoningTokens: int64Ptr(2)},
				},
			}, nil
		},
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "p",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
	}); err != nil {
		t.Fatalf("GenerateObject() error = %v", err)
	}
	root := spanByOperationName(spanRecorder.Ended(), "ai.generateObject", "ai.generateObject")
	if root == nil {
		t.Fatal("no ai.generateObject root span")
	}
	for key, want := range map[string]string{"ai.usage.reasoningTokens": "2", "ai.usage.cachedInputTokens": "3"} {
		got := ""
		for _, a := range root.Attributes() {
			if string(a.Key) == key {
				got = a.Value.Emit()
			}
		}
		if got != want {
			t.Errorf("%s = %q, want %s", key, got, want)
		}
	}
}
