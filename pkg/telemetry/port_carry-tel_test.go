package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
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

// TS packages/otel open-telemetry.test.ts 'steering interrupt' (e4039889ba):
// a model call started while the previous one is still open ends the
// previous chat span with an error status.
func TestPortcarrytelSteeringInterruptEndsCancelledModelCallSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	set := &Settings{Integrations: []TelemetryIntegration{NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("t")})}}
	id := t.Name()
	ctx := FireOnStart(context.Background(), TelemetryStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", ModelProvider: "openai.chat"})
	ctx = FireOnStepStart(ctx, TelemetryStepStartEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
	start := LanguageModelCallStartEvent{Settings: set, CallID: id, ModelID: "m", ModelProvider: "openai.chat"}
	FireOnLanguageModelCallStart(ctx, start)
	// The interrupt cancels the call: no end event, a second call starts.
	ctx2 := FireOnLanguageModelCallStart(ctx, start)
	FireOnLanguageModelCallEnd(ctx2, LanguageModelCallEndEvent{Settings: set, CallID: id, ModelID: "m", ModelProvider: "openai.chat", FinishReason: "stop"})
	FireOnStepEnd(ctx, TelemetryStepEndEvent{Settings: set, CallID: id, OperationType: "ai.generateText"})
	FireOnEnd(ctx, TelemetryFinishEvent{Settings: set, CallID: id, OperationType: "ai.generateText", ModelID: "m", FinishReason: "stop"})

	var chats []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "chat m" {
			chats = append(chats, s)
		}
	}
	if len(chats) != 2 {
		t.Fatalf("ended chat spans = %d, want 2", len(chats))
	}
	if chats[0].Status().Code != codes.Error {
		t.Errorf("cancelled chat span status = %v, want Error", chats[0].Status().Code)
	}
	if chats[1].Status().Code == codes.Error {
		t.Errorf("second chat span has an error status")
	}
	for _, s := range rec.Started() {
		if s.EndTime().IsZero() {
			t.Errorf("span %s never ended", s.Name())
		}
	}
}

// TS fab74785e6 (redaction keeps a stream a subscriber replaced): Go events
// are values handed to subscribers, with no stream to forward. The matching
// guarantee here is that redaction copies only along a changed path and
// leaves a body that holds no credential as the very same value, with the
// caller's event untouched.
func TestPortcarrytelRedactionKeepsBodyWithoutCredentials(t *testing.T) {
	type resp struct {
		Body interface{}
	}
	type event struct {
		Response *resp
		Other    string
	}
	body := map[string]interface{}{"id": "r1"}
	in := event{Response: &resp{Body: body}, Other: "x"}
	out := redactCredentials(in)
	if out.Response != in.Response {
		t.Errorf("response copied although nothing was redacted")
	}
	got, _ := out.Response.Body.(map[string]interface{})
	got["id"] = "replaced"
	if body["id"] != "replaced" {
		t.Errorf("body is not the caller's value")
	}
}

type portcarrytelBodyEvent struct {
	Request  *portcarrytelBody
	Response *portcarrytelBody
}

type portcarrytelBody struct {
	Body interface{}
}

func portcarrytelRedactBody(t *testing.T, body interface{}) interface{} {
	t.Helper()
	return redactCredentials(portcarrytelBodyEvent{Request: &portcarrytelBody{Body: body}}).Request.Body
}

// TS b0fccbda62 (provg CR): the Interactions wire tool in a request body and
// Google MCP tools in Tools lose header values; header-less ones are as given.
func TestPortcarrytelGoogleMCPWireAndToolsRedacted(t *testing.T) {
	body := map[string]interface{}{"tools": []interface{}{
		map[string]interface{}{"type": "mcp_server", "headers": map[string]interface{}{"A": "secret", "B": "secret"}},
		map[string]interface{}{"type": "google_search"},
	}}
	before := r88carryJSON(t, body)
	got := r88carryJSON(t, portcarrytelRedactBody(t, body))
	if strings.Contains(got, "secret") || strings.Count(got, "[redacted]") != 2 || !strings.Contains(got, "google_search") {
		t.Errorf("got %s", got)
	}
	if r88carryJSON(t, body) != before {
		t.Errorf("body mutated")
	}
	plain := map[string]interface{}{"tools": []interface{}{map[string]interface{}{"type": "mcp_server", "url": "u"}}}
	if out := portcarrytelRedactBody(t, plain); r88carryJSON(t, out) != r88carryJSON(t, plain) {
		t.Errorf("header-less wire tool changed: %v", out)
	}
	tools := []types.Tool{{Type: types.ToolTypeProviderDefined, Name: "c", ProviderID: "google.mcp_server",
		ProviderArgs: map[string]interface{}{"name": "m", "headers": map[string]string{"Authorization": "Bearer secret"}}}}
	redacted := redactCredentials(TelemetryStepStartEvent{StepTools: tools}).StepTools
	if strings.Contains(r88carryJSON(t, redacted[0].ProviderArgs), "secret") {
		t.Errorf("tool args leak: %v", redacted[0].ProviderArgs)
	}
	if tools[0].ProviderArgs["headers"].(map[string]string)["Authorization"] != "Bearer secret" {
		t.Errorf("tool args mutated")
	}
}

// TS 83026e8e0f gateway-byok-telemetry / redact-gateway-request-credentials
// tests: BYOK values are redacted in single and batch bodies, JSON text
// included; names, shape and unrelated options stay.
func TestPortcarrytelGatewayByokRedacted(t *testing.T) {
	body := map[string]interface{}{"prompt": "p", "providerOptions": map[string]interface{}{
		"gateway": map[string]interface{}{"order": []interface{}{"a"}, "byok": map[string]interface{}{
			"anthropic": []interface{}{map[string]interface{}{"apiKey": "secret"}},
			"bedrock":   []interface{}{map[string]interface{}{"accessKeyId": "secret"}}}},
		"openai": map[string]interface{}{"x": "keep"}}}
	before := r88carryJSON(t, body)
	got := r88carryJSON(t, portcarrytelRedactBody(t, body))
	want := `{"prompt":"p","providerOptions":{"gateway":{"byok":{"anthropic":[{"apiKey":"[redacted]"}],"bedrock":[{"accessKeyId":"[redacted]"}]},"order":["a"]},"openai":{"x":"keep"}}}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if r88carryJSON(t, body) != before {
		t.Errorf("body mutated")
	}

	batch := map[string]interface{}{"requests": []interface{}{
		map[string]interface{}{"id": "1", "options": map[string]interface{}{"providerOptions": map[string]interface{}{
			"gateway": map[string]interface{}{"byok": map[string]interface{}{"openai": []interface{}{map[string]interface{}{"apiKey": "secret"}}}}}}},
		"x"}}
	got = r88carryJSON(t, portcarrytelRedactBody(t, batch))
	if strings.Contains(got, "secret") || !strings.Contains(got, `"x"`) || !strings.Contains(got, `"openai":[{"apiKey":"[redacted]"}]`) {
		t.Errorf("batch = %s", got)
	}

	text := `{"providerOptions":{"gateway":{"byok":{"a":"secret"}}}}`
	out, ok := portcarrytelRedactBody(t, text).(string)
	if !ok || strings.Contains(out, "secret") || !strings.Contains(out, "[redacted]") {
		t.Errorf("text body = %v", out)
	}

	none := map[string]interface{}{"providerOptions": map[string]interface{}{"gateway": map[string]interface{}{"order": []interface{}{"a"}}}}
	if out := portcarrytelRedactBody(t, none).(map[string]interface{}); r88carryJSON(t, out) != r88carryJSON(t, none) {
		t.Errorf("byok-free body changed")
	}
}

// TS 797dc0254e: part-level provider options never reach extract telemetry,
// whether or not the file is a location.
func TestPortcarrytelExtractStateStripsPartProviderOptions(t *testing.T) {
	opts := map[string]interface{}{"anthropic": map[string]interface{}{"title": "private"}}
	in := []portcarrytelStatePart{
		{Type: "file", File: &types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, ProviderOptions: opts}},
		{Type: "file", File: &types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x/a?sig=secret"}, ProviderOptions: opts}},
	}
	out := redactFileLocations(in).([]portcarrytelStatePart)
	for i, p := range out {
		if p.File.ProviderOptions != nil {
			t.Errorf("part %d keeps provider options", i)
		}
	}
	if out[0].File.FileData.Type != types.FileDataTypeData || out[0].File.FileData.DataString == "[redacted]" {
		t.Errorf("inline data part redacted: %+v", out[0].File.FileData)
	}
	if out[1].File.FileData.DataString != "[redacted]" {
		t.Errorf("url part not redacted")
	}
	if in[0].File.ProviderOptions == nil || in[1].File.ProviderOptions == nil {
		t.Errorf("input modified")
	}
}
