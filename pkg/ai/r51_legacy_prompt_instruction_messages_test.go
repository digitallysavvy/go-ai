package ai

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Legacy ai.prompt records instructions given as system messages as the
// message array (TS JSON.stringify({system: event.instructions, ...}) with
// instructions: SystemModelMessage[]), each message {role, content,
// providerOptions}; a string instruction stays a string.
func TestR51LegacyPromptInstructionMessagesArray(t *testing.T) {
	instructions := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}},
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "cache me"}},
			ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"cacheControl": map[string]interface{}{"type": "ephemeral"}}}},
	}
	const wantSystem = `"system":[{"role":"system","content":"be brief"},{"role":"system","content":"cache me","providerOptions":{"anthropic":{"cacheControl":{"type":"ephemeral"}}}}]`
	for _, tc := range []struct {
		name       string
		stream     bool
		system     string
		msgs       []types.Message
		wantPrefix string
	}{
		{"generate messages", false, "", instructions, `{` + wantSystem + `,"messages":[`},
		{"stream messages", true, "", instructions, `{` + wantSystem + `,"messages":[`},
		{"generate string", false, "be terse", nil, `{"system":"be terse","messages":[`},
		{"stream string", true, "be terse", nil, `{"system":"be terse","messages":[`},
	} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		tel := &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
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
		op := "ai.generateText"
		if tc.stream {
			op = "ai.streamText"
			result, err := StreamText(context.Background(), StreamTextOptions{
				Model: model, Prompt: "Hello", System: tc.system, Telemetry: tel, InstructionMessages: tc.msgs,
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := result.ReadAll(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := GenerateText(context.Background(), GenerateTextOptions{
			Model: model, Prompt: "Hello", System: tc.system, Telemetry: tel, InstructionMessages: tc.msgs,
		}); err != nil {
			t.Fatal(err)
		}
		root := findSpanByName(rec.Ended(), op)
		if root == nil {
			t.Fatalf("%s: no %s span", tc.name, op)
		}
		v, _ := attrValueTelemetry(root, "ai.prompt")
		if !strings.HasPrefix(v, tc.wantPrefix) {
			t.Errorf("%s: ai.prompt = %s, want prefix %s", tc.name, v, tc.wantPrefix)
		}
		_ = tp.Shutdown(context.Background())
	}
}
