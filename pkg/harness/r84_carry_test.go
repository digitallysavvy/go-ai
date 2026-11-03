package harness

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// TS telemetry-integration.test.ts 648 "exports normalized OpenTelemetry
// spans from HarnessAgent turns": the chat span's gen_ai.tool.definitions is
// turn telemetry's languageModelTools, JSON.stringify of {type: 'function',
// name, description, inputSchema} (turn-telemetry.ts 124-136), with the
// tool's schema in its own key order. A tool without a schema has no
// inputSchema.
func TestR84carryHarnessChatSpanToolDefinitions(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			stop := FinishReason{Unified: FinishReasonStop}
			return []StreamPart{
				&StreamStartPart{ModelID: "otel-model"},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "final"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(5, 2)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(5, 2)},
			}
		},
	})
	echoSchema, err := schema.NewOrderedSchema([]byte(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness, Model: "requested-model", Instructions: "Answer concisely.",
		UserTools: map[string]types.Tool{
			"echo": {Name: "echo", Description: "Echo a value.", Parameters: echoSchema},
			"ping": {Name: "ping"},
		},
		Telemetry: &telemetry.Options{
			IsEnabled: telemetry.Bool(true), RecordInputs: telemetry.Bool(true), RecordOutputs: telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{
				telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "go", HarnessSession: session}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var defs string
	for _, s := range rec.Ended() {
		if s.Name() != "chat otel-model" {
			continue
		}
		for _, kv := range s.Attributes() {
			if kv.Key == "gen_ai.tool.definitions" {
				defs = kv.Value.AsString()
			}
		}
	}
	const want = `[{"type":"function","name":"echo","description":"Echo a value.","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}},{"type":"function","name":"ping"}]`
	if defs != want {
		t.Fatalf("gen_ai.tool.definitions =\n%s\nwant\n%s", defs, want)
	}
}
