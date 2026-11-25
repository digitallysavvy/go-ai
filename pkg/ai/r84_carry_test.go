package ai

import (
	"context"
	"reflect"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r84carryTool1Schema is the JSON Schema of TS z.object({value: z.string()})
// in zod's key order.
const r84carryTool1Schema = `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`

// TS legacy-open-telemetry.test.ts 1419 "should record successful tool call"
// (and the streamText twin 3605): ai.prompt.tools holds
// JSON.stringify(tool) of each prepared tool, {"type":"function","name",
// "inputSchema",...} in that key order. The GenAI chat span's
// gen_ai.tool.definitions stringifies the same prepared tools
// (open-telemetry.ts 906).
func TestR84carryPromptToolsArePreparedTools(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(r84carryTool1Schema))
	if err != nil {
		t.Fatal(err)
	}
	type tool2Input struct {
		City string `json:"city"`
		Unit string `json:"unit,omitempty"`
	}
	strict := true
	tools := []types.Tool{
		{Name: "tool1", Parameters: ordered,
			Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				return "result1", nil
			}},
		{Name: "tool2", Description: "weather", Strict: &strict,
			InputExamples:   []types.ToolInputExample{{Input: map[string]interface{}{"city": "Paris"}}},
			ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"x": 1}},
			Parameters:      schema.NewSimpleStructSchema(reflect.TypeOf(tool2Input{}))},
	}
	const wantTool1 = `{"type":"function","name":"tool1","inputSchema":` + r84carryTool1Schema + `}`
	tool2Schema, _, _ := schema.MarshalSchema(tools[1].Parameters)
	wantTool2 := `{"type":"function","name":"tool2","inputSchema":` + string(tool2Schema) +
		`,"description":"weather","inputExamples":[{"input":{"city":"Paris"}}],"providerOptions":{"openai":{"x":1}},"strict":true}`
	for _, stream := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
		}}
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "hi"},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		stepSpan := "ai.generateText.doGenerate"
		if stream {
			stepSpan = "ai.streamText.doStream"
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input", Tools: tools, Telemetry: tel})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "test-input", Tools: tools, Telemetry: tel}); err != nil {
			t.Fatal(err)
		}
		span := findSpanByName(rec.Ended(), stepSpan)
		if span == nil {
			t.Fatalf("no %s span", stepSpan)
		}
		var got []string
		for _, kv := range span.Attributes() {
			if kv.Key == "ai.prompt.tools" {
				got = kv.Value.AsStringSlice()
			}
		}
		if !reflect.DeepEqual(got, []string{wantTool1, wantTool2}) {
			t.Errorf("stream=%v ai.prompt.tools =\n%q\nwant\n%q", stream, got, []string{wantTool1, wantTool2})
		}
		chat := findSpanByName(rec.Ended(), "chat mock-model")
		if chat == nil {
			t.Fatalf("stream=%v: no chat span", stream)
		}
		defs, _ := attrValueTelemetry(chat, "gen_ai.tool.definitions")
		if want := "[" + wantTool1 + "," + wantTool2 + "]"; defs != want {
			t.Errorf("stream=%v gen_ai.tool.definitions =\n%s\nwant\n%s", stream, defs, want)
		}
	}
}
