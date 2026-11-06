package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// GenerateText and StreamText give the GenAI chat span the step's
// caller-facing messages and instruction messages (TS
// languageModelCallStartEvent.messages/instructions): the instructions are
// gen_ai.system_instructions, not system messages in gen_ai.input.messages,
// and a PrepareStep temperature does not replace the root call's.
func TestR50GenAIChatSpanStepMessagesAndInstructions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}}
		model := &testutil.MockLanguageModel{
			ProviderName: "openai.chat", ModelName: "gpt-4",
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
		instructions := []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}}}
		rootTemp, stepTemp := 0.1, 0.8
		prepare := func(_ context.Context, s PrepareStepOptions) PrepareStepOptions {
			s.Temperature = &stepTemp
			return s
		}
		if stream {
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "Hello", Telemetry: tel, InstructionMessages: instructions,
				Temperature: &rootTemp, PrepareStep: prepare,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "Hello", Telemetry: tel, InstructionMessages: instructions,
			Temperature: &rootTemp, PrepareStep: prepare,
		}); err != nil {
			t.Fatal(err)
		}
		span := findSpanByName(rec.Ended(), "chat gpt-4")
		if span == nil {
			t.Fatalf("stream=%v: no chat span", stream)
		}
		if v, _ := attrValueTelemetry(span, "gen_ai.input.messages"); v != `[{"role":"user","parts":[{"type":"text","content":"Hello"}]}]` {
			t.Errorf("stream=%v: gen_ai.input.messages = %s", stream, v)
		}
		if v, _ := attrValueTelemetry(span, "gen_ai.system_instructions"); v != `[{"type":"text","content":"be brief"}]` {
			t.Errorf("stream=%v: gen_ai.system_instructions = %q", stream, v)
		}
		root := findSpanByName(rec.Ended(), "invoke_agent gpt-4")
		if v, _ := attrValueTelemetry(root, "gen_ai.system_instructions"); v != `[{"type":"text","content":"be brief"}]` {
			t.Errorf("stream=%v: root gen_ai.system_instructions = %q", stream, v)
		}
		temp := -1.0
		for _, kv := range span.Attributes() {
			if kv.Key == "gen_ai.request.temperature" {
				temp = kv.Value.AsFloat64()
			}
		}
		if temp != 0.1 {
			t.Errorf("stream=%v: chat temperature = %v, want the root's 0.1", stream, temp)
		}
		_ = tp.Shutdown(context.Background())
	}
}
