package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS generate-image.ts keeps only `images` for non-gateway provider entries.
func TestGenerateImage_NonGatewayProviderMetadataKeepsOnlyImages(t *testing.T) {
	t.Parallel()
	model := &testutil.MockImageModel{
		MaxImages: 1,
		DoGenerateFunc: func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
			return &types.ImageResult{
				Images: [][]byte{pngBytes},
				ProviderMetadata: map[string]interface{}{
					"mock": map[string]interface{}{
						"images": []interface{}{map[string]interface{}{"a": 1}},
						"extra":  "dropped",
					},
				},
			}, nil
		},
	}
	res, err := GenerateImage(context.Background(), GenerateImageOptions{Model: model, Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	entry := res.ProviderMetadata["mock"].(map[string]interface{})
	if _, ok := entry["extra"]; ok {
		t.Fatalf("extra key kept: %v", entry)
	}
	if _, ok := entry["images"]; !ok {
		t.Fatalf("images missing: %v", entry)
	}
}

// TS step.reasoning keeps empty reasoning parts.
func TestReasoningPartsOfKeepsEmptyParts(t *testing.T) {
	t.Parallel()
	got := reasoningPartsOf([]types.ContentPart{types.ReasoningContent{Text: ""}, types.ReasoningContent{Text: "x"}})
	if len(got) != 2 {
		t.Fatalf("got %d parts", len(got))
	}
}

func TestMergeStreamedToolResultsKeepsArrivalOrder(t *testing.T) {
	t.Parallel()
	streamed := map[string]types.ToolResult{}
	var order []string
	ids := []string{"c9", "a1", "m5", "b2", "z0", "k7", "d4", "q3"}
	for _, id := range ids {
		order = recordStreamedToolResult(streamed, order, types.ToolResult{ToolCallID: id})
	}
	order = recordStreamedToolResult(streamed, order, types.ToolResult{ToolCallID: "a1"})
	for run := 0; run < 20; run++ {
		got := mergeStreamedToolResults(nil, streamed, order)
		if len(got) != len(ids) {
			t.Fatalf("got %d results", len(got))
		}
		for i, id := range ids {
			if got[i].ToolCallID != id {
				t.Fatalf("run %d position %d = %s want %s", run, i, got[i].ToolCallID, id)
			}
		}
	}
}

func r31ObjectModel(captured *[]types.Message, system *string) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
			*captured = opts.Prompt.Messages
			*system = opts.Prompt.System
			return &types.GenerateResult{Text: `{"name":"x"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
}

var r31ObjectSchema = schema.NewSimpleJSONSchema(map[string]interface{}{
	"type":       "object",
	"properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}},
})

// TS standardizePrompt rejects system messages in `messages` unless
// allowSystemInMessages is set; `instructions` is `system`'s alias.
func TestGenerateObject_SystemMessagesNeedOptIn(t *testing.T) {
	t.Parallel()
	var msgs []types.Message
	var system string
	model := r31ObjectModel(&msgs, &system)
	sysMsg := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be nice"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
	}
	_, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema})
	if err == nil || !strings.Contains(err.Error(), "system messages are not allowed") {
		t.Fatalf("err = %v", err)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema, AllowSystemInMessages: true}); err != nil {
		t.Fatalf("opt-in failed: %v", err)
	}
	instr := "from instructions"
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "hi", System: "old", Instructions: &instr, Schema: r31ObjectSchema}); err != nil {
		t.Fatal(err)
	}
	if system != instr {
		t.Fatalf("system = %q, want %q", system, instr)
	}
	_, err = StreamObject(context.Background(), StreamObjectOptions{Model: model, Messages: sysMsg, Schema: r31ObjectSchema})
	if err == nil || !strings.Contains(err.Error(), "system messages are not allowed") {
		t.Fatalf("stream err = %v", err)
	}
}
