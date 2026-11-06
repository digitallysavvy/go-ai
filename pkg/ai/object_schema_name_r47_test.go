package ai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS onObjectOperationStart: ai.schema is an input, but ai.schema.name and
// ai.schema.description are plain values recorded with inputs off too.
func TestObjectRootSchemaNameRecordedWithoutInputs(t *testing.T) {
	spanRecorder, cleanup := setupTelemetryTest(t)
	defer cleanup()
	model := &testutil.MockLanguageModel{
		ProviderName: "p", ModelName: "m", StructuredSupport: true,
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: `{"name":"x"}`, FinishReason: types.FinishReasonStop}, nil
		},
	}
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	settings := &telemetry.Options{IsEnabled: telemetry.Bool(true), RecordInputs: telemetry.Bool(false)}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
		Model: model, Prompt: "p", Schema: sch, SchemaName: "Person", SchemaDescription: "A person",
		ExperimentalTelemetry: settings,
	}); err != nil {
		t.Fatal(err)
	}
	root := spanByOperationName(spanRecorder.Ended(), "ai.generateObject", "ai.generateObject")
	if root == nil {
		t.Fatal("no root span")
	}
	if v, ok := attrValueTelemetry(root, "ai.schema.name"); !ok || v != "Person" {
		t.Errorf("ai.schema.name = %v (ok=%v)", v, ok)
	}
	if v, ok := attrValueTelemetry(root, "ai.schema.description"); !ok || v != "A person" {
		t.Errorf("ai.schema.description = %v (ok=%v)", v, ok)
	}
	if _, ok := attrValueTelemetry(root, "ai.schema"); ok {
		t.Error("ai.schema is an input and must stay off")
	}
}
