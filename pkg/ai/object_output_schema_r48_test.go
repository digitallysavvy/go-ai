package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS onStart (and the telemetry ai.schema attribute) carries
// outputStrategy.jsonSchema(): the enum and array wrappers the model is asked
// for, not the caller's item schema (or nothing, for enum).
func TestObjectStartSchemaIsOutputStrategySchema(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m", StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"result":"b"}`, FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, errors.New("no stream")
		},
	}
	var enumStart ObjectOnStartEvent
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "p", OutputMode: ObjectModeEnum, EnumValues: []string{"a", "b"},
		ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true)},
		OnStart:               func(_ context.Context, e ObjectOnStartEvent) { enumStart = e },
	}); err != nil {
		t.Fatal(err)
	}
	want := `{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"result":{"enum":["a","b"],"type":"string"}},"required":["result"],"type":"object"}`
	if got, _ := json.Marshal(enumStart.Schema); string(got) != want {
		t.Errorf("enum onStart schema = %s, want %s", got, want)
	}
	root := spanByOperationName(spanRecorder.Ended(), "ai.generateObject", "ai.generateObject")
	if root == nil {
		t.Fatal("no root span")
	}
	if v, ok := attrValueTelemetry(root, "ai.schema"); !ok || v != want {
		t.Errorf("enum ai.schema = %v (ok=%v), want %s", v, ok, want)
	}

	var arrayStart ObjectOnStartEvent
	item := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "string"})
	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "p", OutputMode: ObjectModeArray, Schema: item,
		OnStart: func(_ context.Context, e ObjectOnStartEvent) { arrayStart = e },
	})
	wantArray := `{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"elements":{"items":{"type":"string"},"type":"array"}},"required":["elements"],"type":"object"}`
	if got, _ := json.Marshal(arrayStart.Schema); string(got) != wantArray {
		t.Errorf("array onStart schema = %s, want %s", got, wantArray)
	}
}
