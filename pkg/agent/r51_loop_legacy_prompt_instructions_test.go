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

// The legacy loop's step and chat telemetry carry the step's instruction
// messages too: the GenAI chat span records them as
// gen_ai.system_instructions (TS languageModelCallStart instructions), and
// the legacy step span's ai.prompt.messages starts with them, as the step's
// prompt does.
func TestR51LoopStepTelemetryInstructionMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tel := &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
		telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")}),
		telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
	}}
	model := &testutil.MockLanguageModel{
		ModelName: "m",
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
	var sawChat, sawStep bool
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			switch {
			case s.Name() == "chat m" && kv.Key == "gen_ai.system_instructions":
				sawChat = true
				if v := kv.Value.AsString(); v != `[{"type":"text","content":"be brief"}]` {
					t.Errorf("chat gen_ai.system_instructions = %s", v)
				}
			case s.Name() == "ai.generateText.doGenerate" && kv.Key == "ai.prompt.messages":
				sawStep = true
				if v := kv.Value.AsString(); !strings.HasPrefix(v, `[{"role":"system","content":[{"type":"text","text":"be brief"`) {
					t.Errorf("step ai.prompt.messages = %s", v)
				}
			}
		}
	}
	if !sawChat || !sawStep {
		t.Fatalf("chat system instructions seen = %v, step prompt messages seen = %v", sawChat, sawStep)
	}
}
