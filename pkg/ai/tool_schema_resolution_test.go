package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Providers read tool Parameters as a JSON Schema map; a schema.Schema value
// (the documented schema.NewSimpleJSONSchema) used to reach them unresolved
// and was sent as {}.
func TestToolSchemaParametersReachProvidersAsMaps(t *testing.T) {
	t.Parallel()
	params := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}})
	tools := []types.Tool{{Name: "search", Parameters: params}}

	var genParams, streamParams interface{}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			genParams = o.Tools[0].Parameters
			return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			streamParams = o.Tools[0].Parameters
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "x"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
	if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "x", Tools: tools}); err != nil {
		t.Fatal(err)
	}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x", Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAll(); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]interface{}{"generate": genParams, "stream": streamParams} {
		m, ok := got.(map[string]interface{})
		if !ok || m["type"] != "object" || m["properties"] == nil {
			t.Fatalf("%s: provider saw Parameters %T %#v, want the JSON Schema map", name, got, got)
		}
	}
	if _, ok := tools[0].Parameters.(*schema.SimpleJSONSchema); !ok {
		t.Fatal("caller's tool was modified")
	}
}
