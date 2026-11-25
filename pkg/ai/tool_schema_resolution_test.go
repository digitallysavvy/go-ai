package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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

// A tool that returns raw JSON text hands the model that text in its source key
// order (TS JSON.stringify of the parsed value keeps insertion order).
func TestToolResultRawJSONKeepsKeyOrderInModelOutput(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"zebra":1,"apple":{"y":[1,2],"b":true},"mango":"<x>"}`)
	out := toolResultModelOutput(raw)
	if out.Type != types.ToolResultOutputJSON {
		t.Fatalf("type = %v", out.Type)
	}
	b, err := jsonstr.Marshal(out.Value)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"zebra":1,"apple":{"y":[1,2],"b":true},"mango":"<x>"}` {
		t.Fatalf("model-visible JSON = %s", b)
	}
	// A plain map still round-trips to sorted keys (deterministic).
	m := toolResultModelOutput(map[string]interface{}{"b": 1, "a": 2})
	if _, ok := m.Value.(map[string]interface{}); !ok {
		t.Fatalf("map result Value = %T", m.Value)
	}
}
