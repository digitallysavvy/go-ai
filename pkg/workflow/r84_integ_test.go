package workflow

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

type r84integCity struct {
	City string `json:"city"`
}

func r84integChatToolDefs(t *testing.T, mode string, tools []types.Tool) (string, bool) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
		},
	}
	w, err := NewWorkflowAgent(WorkflowAgent{
		Model: model,
		Tools: tools,
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("g")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mode == "stream" {
		res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
	} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Ended() {
		if s.Name() == "chat mock-model" {
			return r76carryAttr(s, "gen_ai.tool.definitions")
		}
	}
	t.Fatal("no chat span")
	return "", false
}

// Carry r84 open item: TS WorkflowAgent dispatches onLanguageModelCallStart
// with `Object.values(serializeToolSet(effectiveTools)).map(t => ({...t}))`
// (model-call-iterator.ts 367), so the chat span's gen_ai.tool.definitions
// holds the serialized definitions in TS key order, without names, and "[]"
// for a step without tools.
func TestR84integWorkflowChatSpanSerializedToolDefinitions(t *testing.T) {
	noop := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "ok", nil }
	strict := true
	tools := []types.Tool{
		{Name: "weather", Title: "Weather", Description: "Get weather", Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r84integCity{})), Strict: &strict, Execute: noop,
			InputExamples: []types.ToolInputExample{{Input: map[string]interface{}{"city": "Paris"}}}},
		{Name: "ping", Execute: noop, OnInputStart: func(context.Context, types.OnInputStartOptions) error { return nil }},
		{Name: "web_search", Type: "provider", ProviderID: "openai.web_search", ProviderExecuted: true, ProviderArgs: map[string]interface{}{"k": "v"}},
	}
	want := `[{"title":"Weather","description":"Get weather","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"city":{"type":"string"}},"additionalProperties":false},"strict":true,"inputExamples":[{"input":{"city":"Paris"}}],"hasOwnValidator":true},` +
		`{"inputSchema":{"type":"object","properties":{},"additionalProperties":false},"hasOnInputStart":true},` +
		`{"inputSchema":{"type":"object","properties":{},"additionalProperties":false},"type":"provider","isProviderExecuted":true,"id":"openai.web_search","args":{"k":"v"}}]`
	for _, mode := range []string{"stream", "generate"} {
		got, ok := r84integChatToolDefs(t, mode, tools)
		if !ok || got != want {
			t.Errorf("%s: gen_ai.tool.definitions =\n%s\nwant\n%s", mode, got, want)
		}
		if got, ok := r84integChatToolDefs(t, mode, nil); !ok || got != "[]" {
			t.Errorf("%s without tools: gen_ai.tool.definitions = %q (%v), want []", mode, got, ok)
		}
	}
}
