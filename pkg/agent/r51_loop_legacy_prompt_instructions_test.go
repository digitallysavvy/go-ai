package agent

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// The legacy ToolLoopAgent root records AgentConfig.InstructionMessages as
// its instructions, like GenerateText (TS ToolLoopAgent runs generateText
// with instructions): legacy ai.prompt's system is the message array and the
// event carries the messages, not just the System string.
func TestR51LoopRootInstructionMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tel := &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
	}}
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		},
	}
	a := NewToolLoopAgent(AgentConfig{
		Model:     model,
		Telemetry: tel,
		InstructionMessages: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "be brief"}}},
		},
	})
	if _, err := a.Execute(context.Background(), "Hello"); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range rec.Ended() {
		if s.Name() != "ai.generateText" {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.prompt" {
				found = true
				if v := kv.Value.AsString(); !strings.HasPrefix(v, `{"system":[{"role":"system","content":"be brief"}],"messages":[`) {
					t.Errorf("ai.prompt = %s", v)
				}
			}
		}
	}
	if !found {
		t.Fatal("no ai.generateText root with ai.prompt")
	}
	if e := a.loopStartEvent("c", nil, nil); len(e.InstructionMessages) != 1 {
		t.Errorf("start event InstructionMessages = %v", e.InstructionMessages)
	}
	if e := a.loopStartEvent("c", &ai.TelemetrySettings{RecordInputs: telemetry.Bool(false)}, nil); e.InstructionMessages != nil {
		t.Errorf("RecordInputs false: InstructionMessages = %v", e.InstructionMessages)
	}
}
