package workflow

import (
	"context"
	"reflect"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r86carryStreamTextChatToolDefs is the chat span's gen_ai.tool.definitions
// of a plain StreamText call with the same tools (the TS test's baseline).
func r86carryStreamTextChatToolDefs(t *testing.T, tools []types.Tool) (string, bool) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	res, err := ai.StreamText(context.Background(), ai.StreamTextOptions{
		Model:  &testutil.MockLanguageModel{},
		Prompt: "hi",
		Tools:  tools,
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("g")}),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res.ReadAll(); err != nil {
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

// r86carryWorkflowChatToolDefs is the chat span's gen_ai.tool.definitions of
// a WorkflowAgent run in the given mode.
func r86carryWorkflowChatToolDefs(t *testing.T, mode string, tools []types.Tool) (string, bool) {
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

type r86carryCity struct {
	City string `json:"city"`
}

// TS 4d6d830c14 (model-call-iterator.ts 390): WorkflowAgent's model-call
// start event carries toLanguageModelToolDefinitions(serializeToolSet(...)),
// the definitions prepareTools sends to the model, so the chat span's
// gen_ai.tool.definitions equals streamText's
// (workflow-agent-telemetry.integration.test.ts "reports a multi-step run as
// one span tree like streamText"). A step without tools records none
// (toLanguageModelToolDefinitions returns undefined).
func TestR86carryWorkflowChatSpanToolDefinitionsLikeStreamText(t *testing.T) {
	noop := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}
	strict := true
	tools := []types.Tool{
		{Name: "weather", Title: "Weather", Description: "Get weather", Parameters: schema.NewSimpleStructSchema(reflect.TypeOf(r86carryCity{})), Strict: &strict, Execute: noop,
			InputExamples: []types.ToolInputExample{{Input: map[string]interface{}{"city": "Paris"}}}},
		{Name: "ping", Execute: noop, OnInputStart: func(context.Context, types.OnInputStartOptions) error { return nil }},
		{Name: "web_search", Type: "provider", ProviderID: "openai.web_search", ProviderExecuted: true, ProviderArgs: map[string]interface{}{"k": "v"}},
	}
	want := `[{"type":"function","name":"weather","inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"city":{"type":"string"}},"additionalProperties":false},"description":"Get weather","inputExamples":[{"input":{"city":"Paris"}}],"strict":true},` +
		`{"type":"function","name":"ping","inputSchema":{"type":"object","properties":{},"additionalProperties":false}},` +
		`{"type":"provider","name":"web_search","id":"openai.web_search","args":{"k":"v"}}]`
	baseline, ok := r86carryStreamTextChatToolDefs(t, tools)
	if !ok || baseline != want {
		t.Fatalf("streamText baseline gen_ai.tool.definitions =\n%s\nwant\n%s", baseline, want)
	}
	for _, mode := range []string{"stream", "generate"} {
		if got, ok := r86carryWorkflowChatToolDefs(t, mode, tools); !ok || got != baseline {
			t.Errorf("%s: gen_ai.tool.definitions =\n%s\nwant (streamText)\n%s", mode, got, baseline)
		}
		if got, ok := r86carryWorkflowChatToolDefs(t, mode, nil); ok {
			t.Errorf("%s without tools: gen_ai.tool.definitions = %q, want none", mode, got)
		}
	}
}

// TS workflow-chat-transport.ts reads the body with parseJsonEventStream,
// which skips only an exact "[DONE]" and parses any other data, an empty one
// included, as JSON: a failed parse ends the attempt (`throw chunk.error`).
// Go trimmed the data and skipped empty and padded "[DONE]" events.
func TestR86carryTransportEventDataNotTrimmed(t *testing.T) {
	for name, tc := range map[string]struct {
		body    string
		wantErr bool
		read    int
	}{
		"done":                {body: "data: [DONE]\n\n", read: 0},
		"empty data":          {body: "data:\n\ndata: {\"type\":\"finish\"}\n\n", wantErr: true},
		"padded done":         {body: "data:  [DONE] \n\n", wantErr: true},
		"json with spaces":    {body: "data:  {\"type\":\"finish\"} \n\n", read: 1},
		"json then done":      {body: "data: {\"type\":\"start\"}\n\ndata: [DONE]\n\n", read: 1},
		"no-break space json": {body: "data: \u00a0{\"type\":\"finish\"}\n\n", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			out := make(chan ai.UIMessageChunk, 16)
			tr := &WorkflowChatTransport{}
			read, _, err := tr.pumpChunkStream(context.Background(), strings.NewReader(tc.body), newUIStreamNormalizer(), nil, out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && read != tc.read {
				t.Fatalf("read = %d, want %d", read, tc.read)
			}
		})
	}
}
