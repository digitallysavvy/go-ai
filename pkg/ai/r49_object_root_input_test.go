package ai

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS onObjectOperationStart records gen_ai.input.messages as
// formatModelMessages({prompt, messages}): a string prompt is the user message.
func TestR49ObjectRootInputMessagesIncludePrompt(t *testing.T) {
	answerSchema := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	for _, stream := range []bool{false, true} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		integration := telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")})
		model := &testutil.MockLanguageModel{
			ProviderName: "openai.chat", ModelName: "gpt-4",
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{}`, FinishReason: types.FinishReasonStop}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: `{}`},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), nil
			},
		}
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{integration}}
		var err error
		if stream {
			_, err = StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "Hello", Schema: answerSchema, Telemetry: tel})
		} else {
			_, err = GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "Hello", Schema: answerSchema, Telemetry: tel})
		}
		if err != nil {
			t.Fatal(err)
		}
		var got interface{}
		for _, s := range rec.Ended() {
			if s.Name() != "invoke_agent gpt-4" {
				continue
			}
			for _, kv := range s.Attributes() {
				if kv.Key == "gen_ai.input.messages" {
					got = kv.Value.AsInterface()
				}
			}
		}
		if want := `[{"role":"user","parts":[{"type":"text","content":"Hello"}]}]`; got != want {
			t.Errorf("stream=%v: root gen_ai.input.messages = %v, want %s", stream, got, want)
		}
		_ = tp.Shutdown(context.Background())
	}
}
