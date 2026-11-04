package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// r87integChatSpanServer returns the server.address / server.port of the
// recorded "chat" spans (TS otel getServerAttributes, 934100cad4).
func r87integChatSpanServer(t *testing.T, rec *tracetest.SpanRecorder) (addrs []string, ports []int64) {
	t.Helper()
	for _, s := range rec.Ended() {
		if !strings.HasPrefix(s.Name(), "chat ") {
			continue
		}
		got := map[attribute.Key]attribute.Value{}
		for _, kv := range s.Attributes() {
			got[kv.Key] = kv.Value
		}
		addrs = append(addrs, got["server.address"].AsString())
		ports = append(ports, got["server.port"].AsInt64())
	}
	return addrs, ports
}

func r87integTelemetry() (*telemetry.Options, *tracetest.SpanRecorder, func()) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	opts := &telemetry.Options{
		IsEnabled:    telemetry.Bool(true),
		Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("r87integ")})},
	}
	return opts, rec, func() { _ = tp.Shutdown(context.Background()) }
}

// TS delta 30548ee7f9..3fab50aa9b, core-scope cases ported in round 87:
//   - generate-text.test.ts 15018 (request URL results and callbacks): this file;
//   - stream-text.test.ts 33641 (streamed request URL): r87_stream_requesturl_test.go;
//   - empty-text-provider-metadata.test.ts 33 (generate and stream): r87_gen_test.go;
//   - google-streamed-tool-order.test.ts 119: pkg/providers/google/r87_integ_test.go.

// TS generate-text.test.ts (934100cad4) callback block: the step-end and end
// events and the model call end event carry only the origin, no callback
// payload carries credentials / path / query / fragment, and the chat span
// gets server.address / server.port.
func TestR87integGenerateTextRequestURLCallbacksAndSpan(t *testing.T) {
	model := &testutil.MockLanguageModel{
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{
				Text: "hello", Content: []types.ContentPart{types.TextContent{Text: "hello"}},
				FinishReason: types.FinishReasonStop, RequestURL: r87genSecretURL,
			}, nil
		},
	}
	tel, rec, shutdown := r87integTelemetry()
	var mu sync.Mutex
	var payloads []interface{}
	var stepEnd OnStepFinishEvent
	var end OnFinishEvent
	var callEnd LanguageModelCallEndEvent
	record := func(v interface{}) { mu.Lock(); payloads = append(payloads, v); mu.Unlock() }
	_, err := GenerateText(context.Background(), GenerateTextOptions{
		Model: model, Prompt: "hello", ExperimentalTelemetry: tel,
		OnStepEndEvent:         func(_ context.Context, e OnStepFinishEvent) { stepEnd = e; record(e) },
		OnEndEvent:             func(_ context.Context, e OnFinishEvent) { end = e; record(e) },
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { callEnd = e; record(e) },
	})
	shutdown()
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://model.example.test:8443"
	if stepEnd.Request.URL != origin || end.Request.URL != origin {
		t.Errorf("onStepEnd / onEnd request.url = %q / %q", stepEnd.Request.URL, end.Request.URL)
	}
	if callEnd.Request == nil || callEnd.Request.URL != origin {
		t.Errorf("call end request = %+v", callEnd.Request)
	}
	b, _ := json.Marshal(payloads)
	if r87genSecretPattern.Match(b) {
		t.Errorf("callback payloads leak URL parts: %s", b)
	}
	addrs, ports := r87integChatSpanServer(t, rec)
	if len(addrs) != 1 || addrs[0] != "model.example.test" || ports[0] != 8443 {
		t.Errorf("chat span server = %v %v", addrs, ports)
	}
}

// TS generate-object.test.ts / stream-object.test.ts (934100cad4): object
// step-end / end events carry the origin and the object chat span records
// server.address / server.port (open-telemetry.ts 890).
func TestR87integObjectRequestURLCallbacksAndSpan(t *testing.T) {
	const origin = "https://model.example.test:8443"
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	for _, streaming := range []bool{false, true} {
		model := &testutil.MockLanguageModel{
			StructuredSupport: true,
			DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: `{"a":1}`, Content: []types.ContentPart{types.TextContent{Text: `{"a":1}`}}, FinishReason: types.FinishReasonStop, RequestURL: r87genSecretURL}, nil
			},
			DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				return &r87genURLStream{MockTextStream: testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: `{"a":1}`},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}), url: r87genSecretURL}, nil
			},
		}
		tel, rec, shutdown := r87integTelemetry()
		var stepReq, endReq GenerateStepRequest
		onStep := func(_ context.Context, e ObjectOnStepFinishEvent) { stepReq = e.Request }
		onEnd := func(_ context.Context, e ObjectOnFinishEvent) { endReq = e.Request }
		var err error
		if streaming {
			_, err = StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "x", Schema: sch, ExperimentalTelemetry: tel, OnStepEnd: onStep, OnEnd: onEnd})
		} else {
			_, err = GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "x", Schema: sch, ExperimentalTelemetry: tel, OnStepEnd: onStep, OnEnd: onEnd})
		}
		shutdown()
		if err != nil {
			t.Fatalf("streaming=%v: %v", streaming, err)
		}
		if stepReq.URL != origin || endReq.URL != origin {
			t.Errorf("streaming=%v: step / end request.url = %q / %q", streaming, stepReq.URL, endReq.URL)
		}
		addrs, ports := r87integChatSpanServer(t, rec)
		if len(addrs) != 1 || addrs[0] != "model.example.test" || ports[0] != 8443 {
			t.Errorf("streaming=%v: chat span server = %v %v", streaming, addrs, ports)
		}
	}
}

// End to end through the OpenAI Chat provider (TS openai-chat-language-model
// reports request.url for generate and stream): GenerateText and StreamText
// results, the call end event and the chat span carry the origin of the
// configured base URL, never its credentials or path. This also proves the
// WarningsStream wrapper forwards RequestURL().
func TestR87integOpenAIChatRequestOriginEndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"hi"},"finish_reason":null}]}` + "\n\n" +
				`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}` + "\n\n" +
				"data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()
	origin := srv.URL
	base := strings.Replace(srv.URL, "http://", "http://user:password@", 1) + "/private/v1"
	model, err := openai.New(openai.Config{APIKey: "k", BaseURL: base}).ChatModel("m")
	if err != nil {
		t.Fatal(err)
	}

	for _, streaming := range []bool{false, true} {
		tel, rec, shutdown := r87integTelemetry()
		var callEnd LanguageModelCallEndEvent
		var stepURL, endURL string
		onCall := func(_ context.Context, e LanguageModelCallEndEvent) { callEnd = e }
		onStep := func(_ context.Context, e OnStepFinishEvent) { stepURL = e.Request.URL }
		onEnd := func(_ context.Context, e OnFinishEvent) { endURL = e.Request.URL }
		var resultURL string
		if streaming {
			r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hello", ExperimentalTelemetry: tel, OnLanguageModelCallEnd: onCall, OnStepFinishEvent: onStep, OnEndEvent: onEnd})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.ReadAll(); err != nil {
				t.Fatal(err)
			}
			resultURL = r.FinalStep().Request.URL
		} else {
			r, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hello", ExperimentalTelemetry: tel, OnLanguageModelCallEnd: onCall, OnStepEndEvent: onStep, OnEndEvent: onEnd})
			if err != nil {
				t.Fatal(err)
			}
			resultURL = r.Request.URL
		}
		shutdown()
		if resultURL != origin || stepURL != origin || endURL != origin {
			t.Errorf("streaming=%v: result / step / end url = %q / %q / %q, want %q", streaming, resultURL, stepURL, endURL, origin)
		}
		if callEnd.Request == nil || callEnd.Request.URL != origin {
			t.Errorf("streaming=%v: call end request = %+v", streaming, callEnd.Request)
		}
		addrs, ports := r87integChatSpanServer(t, rec)
		if len(addrs) != 1 || addrs[0] != "127.0.0.1" || ports[0] == 0 {
			t.Errorf("streaming=%v: chat span server = %v %v", streaming, addrs, ports)
		}
	}
}

// Survivor test for stream ba77a80b: the approval failure ends the stream at
// the tool-call part (TS resolveToolApproval throws inside the transform), so
// model parts after the call are not forwarded.
func TestR87integApprovalErrorStopsAtToolCallPart(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeText, ID: "1", Text: "after"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}), nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x",
		Tools: []types.Tool{{Name: "t", Parameters: map[string]interface{}{"type": "object"},
			ContextSchema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}, "required": []interface{}{"a"}}),
			ToolApproval: types.ToolNeedsApprovalFunc(func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool { return true }),
			Execute:      func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return 1, nil }}},
		ToolsContext: map[string]interface{}{"t": map[string]interface{}{"a": 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for c := range r.Chunks() {
		parts = append(parts, string(c.Type))
	}
	if want := []string{"start", "start-step", "tool-call"}; strings.Join(parts, ",") != strings.Join(want, ",") {
		t.Errorf("full stream = %v, want %v", parts, want)
	}
	if r.Err() == nil {
		t.Error("want the tool context error")
	}
}
