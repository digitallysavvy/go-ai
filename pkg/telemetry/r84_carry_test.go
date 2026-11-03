package telemetry

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// r84carryToolDefinitions runs one GenAI chat span with tools and returns its
// gen_ai.tool.definitions at the end of the call.
func r84carryToolDefinitions(t *testing.T, provider string, tools []types.Tool, content []types.ContentPart) string {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: Bool(true), RecordOutputs: Bool(true)}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "r84", ModelID: "m", ModelProvider: provider})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "r84", ModelProvider: provider})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "r84", ModelID: "m", ModelProvider: provider, Tools: tools})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "r84", FinishReason: "stop", Content: content})
	span := findSpan(rec, "chat m")
	if span == nil {
		t.Fatal("no chat span")
	}
	v, _ := attrValue(span, "gen_ai.tool.definitions")
	s, _ := v.(string)
	return s
}

// TS prepareTools: provider tools are {type, name, id, args}; a
// provider-executed tool seen only in the response is appended as
// {type: "extension", name} (open-telemetry.ts 958).
func TestR84carryToolDefinitionsProviderAndExtensionOrder(t *testing.T) {
	got := r84carryToolDefinitions(t, "openai.responses", []types.Tool{
		{Name: "web_search", Type: types.ToolTypeProviderDefined, ProviderID: "openai.web_search", ProviderArgs: map[string]interface{}{"k": "v"}},
	}, []types.ContentPart{types.ToolCallContent{ToolCallID: "c1", ToolName: "code_interpreter", ProviderExecuted: true}})
	const want = `[{"type":"provider","name":"web_search","id":"openai.web_search","args":{"k":"v"}},{"type":"extension","name":"code_interpreter"}]`
	if got != want {
		t.Fatalf("gen_ai.tool.definitions =\n%s\nwant\n%s", got, want)
	}
}

// TS harness turn telemetry (turn-telemetry.ts 124-136) lists tool specs as
// {type: "function", name, description?, inputSchema?}: description before
// the schema, and no inputSchema for a tool without one.
func TestR84carryHarnessToolDefinitions(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"q":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	got := r84carryToolDefinitions(t, "harness:mock", []types.Tool{
		{Name: "search", Description: "find", Parameters: ordered},
		{Name: "ping"},
	}, nil)
	const want = `[{"type":"function","name":"search","description":"find","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}},{"type":"function","name":"ping"}]`
	if got != want {
		t.Fatalf("gen_ai.tool.definitions =\n%s\nwant\n%s", got, want)
	}
}

// A schema value that is no schema (typed nil) gets the empty object schema
// for a model tool, as the Go converters send it, without panicking.
func TestR84carryToolDefinitionsNilSchemaValues(t *testing.T) {
	var nilJSON *schema.SimpleJSONSchema
	got := r84carryToolDefinitions(t, "openai.chat", []types.Tool{
		{Name: "a", Parameters: nilJSON},
		{Name: "b", Parameters: &schema.SimpleJSONSchema{}},
	}, nil)
	const want = `[{"type":"function","name":"a","inputSchema":{"properties":{},"type":"object"}},{"type":"function","name":"b","inputSchema":{"properties":{},"type":"object"}}]`
	if got != want {
		t.Fatalf("gen_ai.tool.definitions =\n%s\nwant\n%s", got, want)
	}
}
