package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS packages/otel redact-tool-definition.test.ts and
// tool-credential-redaction.test.ts (b0fccbda62): Google MCP server header
// values are redacted, names and other args kept, input not mutated.
func TestPortcarrytelGoogleMCPArgsRedacted(t *testing.T) {
	args := map[string]interface{}{
		"name": "local-mcp", "url": "https://example.com/mcp", "allowedTools": []interface{}{"search"},
		"headers": map[string]interface{}{"Authorization": "Bearer mcp-private-token", "X-Custom-Key": "mcp-private-key"},
	}
	before := r88carryJSON(t, args)
	got := r88carryJSON(t, redactProviderToolArgs("google.mcp_server", args))
	want := `{"allowedTools":["search"],"headers":{"Authorization":"[redacted]","X-Custom-Key":"[redacted]"},"name":"local-mcp","url":"https://example.com/mcp"}`
	if got != want {
		t.Errorf("got %s", got)
	}
	if r88carryJSON(t, args) != before {
		t.Errorf("args mutated")
	}
	plain := map[string]interface{}{"url": "https://example.com/mcp"}
	if out, changed := redactProviderToolArgsChanged("google.mcp_server", plain); changed || len(out) != 1 {
		t.Errorf("credential-free args changed: %v", out)
	}
}

func TestPortcarrytelGoogleMCPToolDefinitionSpansRedacted(t *testing.T) {
	tool := types.Tool{Type: types.ToolTypeProviderDefined, Name: "connector", ProviderID: "google.mcp_server",
		ProviderArgs: map[string]interface{}{"name": "local-mcp",
			"headers": map[string]interface{}{"Authorization": "Bearer mcp-private-token"}}}
	for _, legacy := range []bool{true, false} {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		var integ TelemetryIntegration
		if legacy {
			integ = NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
		} else {
			integ = NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})
		}
		set := &Settings{Integrations: []TelemetryIntegration{integ}}
		id := t.Name()
		ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "google.interactions"})
		ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", StepTools: []types.Tool{tool}})
		ctx = FireOnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: set, CallID: id, ModelID: "m", ModelProvider: "google.interactions", Tools: []types.Tool{tool}})
		FireOnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: set, CallID: id, ModelID: "m", ModelProvider: "google.interactions", FinishReason: "stop"})
		FireOnStepEnd(ctx, TelemetryStepEndEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
		FireOnEnd(ctx, TelemetryFinishEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
		_ = tp.Shutdown(context.Background())
		var found bool
		for _, s := range rec.Ended() {
			for _, kv := range s.Attributes() {
				v := kv.Value.String()
				if strings.Contains(v, "mcp-private") {
					t.Errorf("legacy=%v %s leaks: %s", legacy, kv.Key, v)
				}
				if (kv.Key == "ai.prompt.tools" || kv.Key == "gen_ai.tool.definitions") &&
					strings.Contains(v, "[redacted]") && strings.Contains(v, "google.mcp_server") && strings.Contains(v, "local-mcp") {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("legacy=%v: no redacted definition", legacy)
		}
	}
	if tool.ProviderArgs["headers"].(map[string]interface{})["Authorization"] != "Bearer mcp-private-token" {
		t.Errorf("provider args mutated")
	}
}
