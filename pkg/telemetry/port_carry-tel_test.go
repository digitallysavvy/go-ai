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

// TS packages/otel gen-ai-format-messages.test.ts formatOutputContent
// (71410a4755): parts keep the order the model produced them in.
func TestPortcarrytelFormatOutputContentKeepsOrder(t *testing.T) {
	content := []types.ContentPart{
		types.TextContent{Text: "Summary of earlier work."},
		types.ReasoningContent{Text: "I should answer."},
		types.TextContent{Text: "Looking it up."},
		types.ToolCallContent{ToolCallID: "c1", ToolName: "web_search", Input: `{"q":"x"}`},
		types.ToolResultContent{ToolCallID: "c1", ToolName: "web_search", Result: "found"},
		types.TextContent{Text: "Done."},
		types.TextContent{Text: ""},
		types.ReasoningContent{Text: ""},
	}
	got := r88carryJSON(t, formatOutputContent(content, "stop"))
	want := `[{"role":"assistant","parts":[{"type":"text","content":"Summary of earlier work."},{"type":"reasoning","content":"I should answer."},{"type":"text","content":"Looking it up."},{"type":"tool_call","id":"c1","name":"web_search","arguments":{"q":"x"}},{"type":"tool_call_response","id":"c1","response":"found"},{"type":"text","content":"Done."},{"type":"reasoning","content":""}],"finish_reason":"stop"}]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestPortcarrytelFormatOutputContentExtraParts(t *testing.T) {
	content := []types.ContentPart{
		types.GeneratedFileContent{MediaType: "image/png", Data: []byte{1, 2, 3}},
		types.ReasoningFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/r.png"}},
		types.CustomContent{Kind: "openai.compaction"},
		types.ToolErrorContent{ToolCallID: "c1", ToolName: "t", Error: "boom"},
		types.ToolErrorContent{ToolCallID: "c2", ToolName: "t", Error: map[string]interface{}{"code": 1}, ProviderExecuted: true},
		types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1", Reason: "why"},
		types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: true},
		types.SourceContent{},
	}
	got := r88carryJSON(t, formatOutputContent(content, "tool-calls"))
	want := `[{"role":"assistant","parts":[{"type":"blob","modality":"image","mime_type":"image/png","content":"AQID"},{"type":"uri","modality":"image","mime_type":"image/png","uri":"https://example.com/r.png"},{"type":"custom","kind":"openai.compaction"},{"type":"tool_call_response","id":"c1","response":"boom"},{"type":"tool_call_response","id":"c2","response":{"code":1}},{"type":"tool_approval_request","approval_id":"a1","tool_call_id":"c1","reason":"why"},{"type":"tool_approval_response","approval_id":"a1","approved":true}],"finish_reason":"tool_call"}]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// Input messages map file, reasoning-file, custom and approval parts too.
func TestPortcarrytelInputMessagesMapExtraParts(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningFileContent{MediaType: "image/png", Data: []byte{1, 2, 3}},
			types.CustomContent{Kind: "x"},
			types.ToolApprovalRequestContent{ApprovalID: "a1", ToolCallID: "c1"},
		}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolApprovalResponseContent{ApprovalID: "a1", Approved: false, Reason: "no"}}},
	}
	got := r88carryJSON(t, formatModelMessages("", msgs))
	want := `[{"role":"assistant","parts":[{"type":"blob","modality":"image","mime_type":"image/png","content":"AQID"},{"type":"custom","kind":"x"},{"type":"tool_approval_request","approval_id":"a1","tool_call_id":"c1"}]},{"role":"tool","parts":[{"type":"tool_approval_response","approval_id":"a1","approved":false,"reason":"no"}]}]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// The root span's gen_ai.output.messages follows the all-steps content order
// (TS open-telemetry.test.ts 'records step output parts the same way...').
func TestPortcarrytelRootSpanOutputUsesContentOrder(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	set := &Settings{Integrations: []TelemetryIntegration{NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})}}
	id := t.Name()
	ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "openai.chat"})
	FireOnEnd(ctx, TelemetryFinishEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", FinishReason: "stop",
		Text: "final",
		Content: []types.ContentPart{
			types.TextContent{Text: "draft"},
			types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{}`},
			types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Result: "ok"},
			types.TextContent{Text: "final"},
		}})
	_ = tp.Shutdown(context.Background())
	var got string
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "gen_ai.output.messages" {
				got = kv.Value.String()
			}
		}
	}
	want := `[{"role":"assistant","parts":[{"type":"text","content":"draft"},{"type":"tool_call","id":"c1","name":"t","arguments":{}},{"type":"tool_call_response","id":"c1","response":"ok"},{"type":"text","content":"final"}],"finish_reason":"stop"}]`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
