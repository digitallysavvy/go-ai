package agent

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS generateText (934100cad4), which ToolLoopAgent.generate runs: the
// agent loop's step-end / end events and the model call end event carry only
// the origin of the provider-reported URL, and the legacy loop's chat span
// records server.address / server.port.
func TestR87integAgentRequestURLCallbacksAndSpan(t *testing.T) {
	model := &functionalAgentLanguageModel{
		modelID: "m",
		doGenerate: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop,
				RequestURL: "https://user:password@model.example.test:8443/private?key=secret#fragment"}, nil
		},
	}
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	var mu sync.Mutex
	var payloads []interface{}
	var callEnd ai.LanguageModelCallEndEvent
	var stepURL, endURL string
	record := func(v interface{}) { mu.Lock(); payloads = append(payloads, v); mu.Unlock() }
	_, err := NewToolLoopAgent(AgentConfig{
		Model: model,
		Telemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewOpenTelemetry(telemetry.OpenTelemetryOptions{Tracer: tp.Tracer("r87integ")})}},
		OnLanguageModelCallEnd: func(_ context.Context, e ai.LanguageModelCallEndEvent) { callEnd = e; record(e) },
		OnStepEndEvent:         func(_ context.Context, e ai.OnStepFinishEvent) { stepURL = e.Request.URL; record(e) },
		OnEndEvent:             func(_ context.Context, e ai.OnFinishEvent) { endURL = e.Request.URL; record(e) },
	}).GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "test"})
	_ = tp.Shutdown(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://model.example.test:8443"
	if callEnd.Request == nil || callEnd.Request.URL != origin {
		t.Errorf("call end request = %+v", callEnd.Request)
	}
	if stepURL != origin || endURL != origin {
		t.Errorf("step / end request url = %q / %q", stepURL, endURL)
	}
	b, _ := json.Marshal(payloads)
	if regexp.MustCompile(`password|private|secret|fragment`).Match(b) {
		t.Errorf("callback payloads leak URL parts: %s", b)
	}
	found := false
	for _, s := range rec.Ended() {
		if !strings.HasPrefix(s.Name(), "chat ") {
			continue
		}
		got := map[attribute.Key]attribute.Value{}
		for _, kv := range s.Attributes() {
			got[kv.Key] = kv.Value
		}
		found = got["server.address"].AsString() == "model.example.test" && got["server.port"].AsInt64() == 8443
	}
	if !found {
		t.Error("agent chat span has no server.address / server.port")
	}
}
