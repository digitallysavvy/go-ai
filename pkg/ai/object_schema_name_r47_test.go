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

// TS 0f3a85f92d filterTelemetryEvent (8da48db187) clears schemaName and
// schemaDescription before any integration sees the start event when inputs
// are hidden, so ai.schema.name / ai.schema.description are recorded only
// with inputs on (ai.schema is always an input).
func TestObjectRootSchemaNameRecordedWithoutInputs(t *testing.T) {
	for _, recordInputs := range []bool{false, true} {
		spanRecorder, cleanup := setupTelemetryTest(t)
		model := &testutil.MockLanguageModel{
			ProviderName: "p", ModelName: "m", StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{"name":"x"}`, FinishReason: types.FinishReasonStop}, nil
			},
		}
		sch := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
		settings := &telemetry.Options{IsEnabled: telemetry.Bool(true), RecordInputs: telemetry.Bool(recordInputs)}
		if _, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: model, Prompt: "p", Schema: sch, SchemaName: "Person", SchemaDescription: "A person",
			ExperimentalTelemetry: settings,
		}); err != nil {
			cleanup()
			t.Fatal(err)
		}
		root := spanByOperationName(spanRecorder.Ended(), "ai.generateObject", "ai.generateObject")
		cleanup()
		if root == nil {
			t.Fatal("no root span")
		}
		name, nameOK := attrValueTelemetry(root, "ai.schema.name")
		desc, descOK := attrValueTelemetry(root, "ai.schema.description")
		_, schemaOK := attrValueTelemetry(root, "ai.schema")
		if recordInputs {
			if !nameOK || name != "Person" || !descOK || desc != "A person" || !schemaOK {
				t.Errorf("inputs on: ai.schema.name = %v (%v), ai.schema.description = %v (%v), ai.schema present %v", name, nameOK, desc, descOK, schemaOK)
			}
			continue
		}
		if nameOK || descOK || schemaOK {
			t.Errorf("inputs off: ai.schema.name present %v, ai.schema.description present %v, ai.schema present %v", nameOK, descOK, schemaOK)
		}
	}
}
