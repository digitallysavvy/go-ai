package harness

import (
	"context"
	"sync"
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

// r84carryEventRecorder records the start, step start and model call start
// telemetry events of harness turns.
type r84carryEventRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu         sync.Mutex
	starts     []telemetry.TelemetryStartEvent
	stepStarts []telemetry.TelemetryStepStartEvent
	callStarts []telemetry.LanguageModelCallStartEvent
	ends       []telemetry.TelemetryFinishEvent
}

func (r *r84carryEventRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	r.ends = append(r.ends, e)
	r.mu.Unlock()
}

func (r *r84carryEventRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	r.starts = append(r.starts, e)
	r.mu.Unlock()
	return ctx
}

func (r *r84carryEventRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	r.stepStarts = append(r.stepStarts, e)
	r.mu.Unlock()
	return ctx
}

func (r *r84carryEventRecorder) OnLanguageModelCallStart(ctx context.Context, e telemetry.LanguageModelCallStartEvent) context.Context {
	r.mu.Lock()
	r.callStarts = append(r.callStarts, e)
	r.mu.Unlock()
	return ctx
}

// TS telemetry-integration.test.ts 402 "reports fresh turn settings without
// exposing skills": each turn's telemetry events carry the model,
// instructions and tools PrepareCall resolved for that turn, and the chat
// span's tool definitions are that turn's tools.
func TestR84carryHarnessFreshTurnSettingsInTelemetry(t *testing.T) {
	stop := FinishReason{Unified: FinishReasonStop}
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "done"}, &TextEndPart{ID: "t1"},
				&FinishStepPart{FinishReason: stop, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: stop, TotalUsage: stringUsage(1, 1)},
			}
		},
	})
	valueSchema := `{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`
	mk := func(name, description string) types.Tool {
		s, err := schema.NewOrderedSchema([]byte(valueSchema))
		if err != nil {
			t.Fatal(err)
		}
		return types.Tool{Name: name, Description: description, Parameters: s}
	}
	echo, reverse := mk("echo", "Echo a value."), mk("reverse", "Reverse a value.")
	rec := &r84carryEventRecorder{}
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	defer func() { _ = tp.Shutdown(context.Background()) }()
	a, err := NewAgent(AgentSettings{
		Harness: mock.harness,
		Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{
			rec, telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("genai")}),
		}},
		PrepareCall: func(_ context.Context, o PrepareCallOptions) (PrepareCallResult, error) {
			tenant, _ := o.CallOptions.(string)
			tools := map[string]types.Tool{"echo": echo}
			if tenant == "beta" {
				tools = map[string]types.Tool{"reverse": reverse}
			}
			return PrepareCallResult{
				Model:        "model-" + tenant,
				Skills:       []Skill{{Name: tenant + "-skill", Description: tenant + " skill", Content: tenant + " skill instructions"}},
				Instructions: tenant + " instructions", HasInstructions: true,
				Tools:  tools,
				Prompt: o.Prompt,
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(context.Background(), CreateSessionOptions{SandboxSession: testSandbox()})
	if err != nil {
		t.Fatal(err)
	}
	for _, tenant := range []string{"alpha", "beta"} {
		if _, err := a.Generate(context.Background(), agent.AgentGenerateOptions{Prompt: "turn", HarnessSession: session, CallOptions: tenant}); err != nil {
			t.Fatalf("Generate %s: %v", tenant, err)
		}
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.starts) != 2 || len(rec.stepStarts) != 2 || len(rec.callStarts) != 2 {
		t.Fatalf("events: %d starts, %d step starts, %d model call starts", len(rec.starts), len(rec.stepStarts), len(rec.callStarts))
	}
	toolNames := func(v interface{}) []string {
		var names []string
		switch ts := v.(type) {
		case []types.Tool:
			for _, tool := range ts {
				names = append(names, tool.Name)
			}
		case map[string]types.Tool:
			for name := range ts {
				names = append(names, name)
			}
		}
		return names
	}
	for i, tenant := range []string{"alpha", "beta"} {
		wantTool := map[string]string{"alpha": "echo", "beta": "reverse"}[tenant]
		start, step, call := rec.starts[i], rec.stepStarts[i], rec.callStarts[i]
		if start.ModelID != "model-"+tenant || step.ModelID != "model-"+tenant || call.ModelID != "model-"+tenant {
			t.Errorf("%s: model ids %q %q %q", tenant, start.ModelID, step.ModelID, call.ModelID)
		}
		if call.System != tenant+" instructions" {
			t.Errorf("%s: model call instructions %q", tenant, call.System)
		}
		if got := toolNames(call.Tools); len(got) != 1 || got[0] != wantTool {
			t.Errorf("%s: model call tools %v", tenant, got)
		}
	}
	var defs []string
	for _, s := range spans.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "gen_ai.tool.definitions" {
				defs = append(defs, s.Name()+" "+kv.Value.AsString())
			}
		}
	}
	want := []string{
		`chat model-alpha [{"type":"function","name":"echo","description":"Echo a value.","inputSchema":` + valueSchema + `}]`,
		`chat model-beta [{"type":"function","name":"reverse","description":"Reverse a value.","inputSchema":` + valueSchema + `}]`,
	}
	if len(defs) != 2 || defs[0] != want[0] || defs[1] != want[1] {
		t.Fatalf("tool definitions =\n%v\nwant\n%v", defs, want)
	}
}

// TS telemetry-integration.test.ts 595 "uses final-step text and reasoning
// on the telemetry end event": the end event's text and reasoning are the
// final step's, not the whole turn's.
func TestR84carryHarnessTelemetryEndUsesFinalStep(t *testing.T) {
	mock := newMockHarness(mockHarnessOptions{
		script: func(func(string, interface{})) []StreamPart {
			return []StreamPart{
				&StreamStartPart{},
				&TextStartPart{ID: "t1"}, &TextDeltaPart{ID: "t1", Delta: "draft"}, &TextEndPart{ID: "t1"},
				&ReasoningStartPart{ID: "r1"}, &ReasoningDeltaPart{ID: "r1", Delta: "first thought"}, &ReasoningEndPart{ID: "r1"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonToolCalls, Raw: "tool_calls"}, Usage: stringUsage(1, 1)},
				&TextStartPart{ID: "t2"}, &TextDeltaPart{ID: "t2", Delta: "final"}, &TextEndPart{ID: "t2"},
				&ReasoningStartPart{ID: "r2"}, &ReasoningDeltaPart{ID: "r2", Delta: "final thought"}, &ReasoningEndPart{ID: "r2"},
				&FinishStepPart{FinishReason: FinishReason{Unified: FinishReasonStop, Raw: "stop"}, Usage: stringUsage(1, 1)},
				&FinishPart{FinishReason: FinishReason{Unified: FinishReasonStop, Raw: "stop"}, TotalUsage: stringUsage(2, 2)},
			}
		},
	})
	rec := &r84carryEventRecorder{}
	a, err := NewAgent(AgentSettings{Harness: mock.harness, Telemetry: &telemetry.Options{Integrations: []telemetry.TelemetryIntegration{rec}}})
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
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.ends) != 1 {
		t.Fatalf("end events = %d", len(rec.ends))
	}
	end := rec.ends[0]
	if end.Text != "final" {
		t.Errorf("end text = %q, want final", end.Text)
	}
	if len(end.ReasoningParts) != 1 || end.ReasoningParts[0].Text != "final thought" || end.Reasoning != "final thought" {
		t.Errorf("end reasoning = %q %+v, want the final step's", end.Reasoning, end.ReasoningParts)
	}
}
