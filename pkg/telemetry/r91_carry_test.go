package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91carryTracer(t *testing.T) (*tracetest.SpanRecorder, *sdktrace.TracerProvider) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, tp
}

func r91carryExceptionType(s sdktrace.ReadOnlySpan) (string, bool) {
	for _, ev := range s.Events() {
		if ev.Name != "exception" {
			continue
		}
		for _, kv := range ev.Attributes {
			if kv.Key == "exception.type" {
				return kv.Value.AsString(), true
			}
		}
	}
	return "", false
}

// core1-F01: the exception event's exception.type is the TS error.name
// (record-span.ts 58, legacy-open-telemetry.ts 59-65), not the Go type.
func TestR91carrySpanExceptionTypeIsTSName(t *testing.T) {
	rec, tp := r91carryTracer(t)
	tr := tp.Tracer("t")
	apiErr := providererrors.NewProviderError("p", 429, "", "slow", nil)
	cases := []struct {
		err  error
		want string
	}{
		{apiErr, "AI_APICallError"},
		{&providererrors.RetryError{Message: "r", LastError: apiErr}, "AI_RetryError"},
		{context.Canceled, "AbortError"},
		{errors.New("x"), "Error"},
	}
	for i, tc := range cases {
		_, span := tr.Start(context.Background(), fmt.Sprintf("gen%d", i))
		RecordErrorOnSpan(span, tc.err)
		span.End()
		_, legacy := tr.Start(context.Background(), fmt.Sprintf("legacy%d", i))
		legacyRecordSpanError(legacy, tc.err)
		legacy.End()
	}
	ended := rec.Ended()
	for i, tc := range cases {
		for j, s := range ended[2*i : 2*i+2] {
			got, ok := r91carryExceptionType(s)
			if !ok || got != tc.want {
				t.Fatalf("case %d span %d: exception.type = %q (ok=%v), want %q", i, j, got, ok, tc.want)
			}
		}
	}
}

// core1-F02: http.response.status_code only for the error itself or a
// RetryError's lastError (record-span.ts 51-58), not any wrapper.
func TestR91carrySpanStatusCodeNoUnwrap(t *testing.T) {
	rec, tp := r91carryTracer(t)
	tr := tp.Tracer("t")
	pe := &providererrors.ProviderError{StatusCode: 429}
	_, a := tr.Start(context.Background(), "wrapped")
	RecordErrorOnSpan(a, fmt.Errorf("x: %w", pe))
	a.End()
	_, b := tr.Start(context.Background(), "retry")
	RecordErrorOnSpan(b, &providererrors.RetryError{Message: "r", LastError: pe})
	b.End()
	_, c := tr.Start(context.Background(), "retry-wrapped")
	RecordErrorOnSpan(c, &providererrors.RetryError{Message: "r", LastError: fmt.Errorf("x: %w", pe)})
	c.End()
	ended := rec.Ended()
	if _, ok := attrValue(ended[0], "http.response.status_code"); ok {
		t.Fatal("wrapped ProviderError must not set http.response.status_code")
	}
	if v, ok := attrValue(ended[1], "http.response.status_code"); !ok || v.(int64) != 429 {
		t.Fatalf("RetryError lastError: %v %v", v, ok)
	}
	if _, ok := attrValue(ended[2], "http.response.status_code"); ok {
		t.Fatal("RetryError with a wrapped lastError must not set http.response.status_code")
	}
}

// core2-05 (+core1-R01): the legacy step ai.prompt.messages is TS
// stringifyForTelemetry of the V4 prompt. Expected text from a node run of
// TS 2ebc78b654 convertToLanguageModelPrompt + stringifyForTelemetry on the
// same prompt.
func TestR91carryLegacyStepPromptMessagesV4Shape(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "hi"}, types.TextContent{Text: ""},
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, Data: png}, Data: png, MimeType: "image/png", MediaType: "image/png", Filename: "a.png"},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{
			types.ReasoningContent{Text: "think", Signature: "sig"}, types.TextContent{Text: ""}, types.TextContent{Text: "ok"},
			types.ToolCallContent{ToolCallID: "c1", ToolName: "t", Input: `{"b":1,"a":2}`},
		}, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{"b": 1, "a": 2}}}},
		{Role: types.RoleTool, Content: []types.ContentPart{
			types.ToolResultContent{ToolCallID: "c1", ToolName: "t", Output: &types.ToolResultOutput{Type: types.ToolResultOutputJSON, Value: map[string]interface{}{"x": 1}}},
		}, ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"a": 1}}},
		{Role: types.RoleTool, Content: []types.ContentPart{}, ProviderOptions: map[string]interface{}{"p": map[string]interface{}{"b": 2}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "again"}}},
	}
	want := `[{"role":"system","content":"be nice"},{"role":"user","content":[{"type":"text","text":"hi"},{"type":"file","mediaType":"image/png","filename":"a.png","data":"iVBORw0KGgo="}]},{"role":"assistant","content":[{"type":"reasoning","text":"think"},{"type":"text","text":"ok"},{"type":"tool-call","toolCallId":"c1","toolName":"t","input":{"b":1,"a":2}}]},{"role":"tool","content":[{"type":"tool-result","toolCallId":"c1","toolName":"t","output":{"type":"json","value":{"x":1}},"providerOptions":{"p":{"a":1}}}],"providerOptions":{"p":{"b":2}}},{"role":"user","content":[{"type":"text","text":"again"}]}]`

	rec, tp := r91carryTracer(t)
	lot := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := lot.OnStart(context.Background(), TelemetryStartEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	sctx := lot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings, System: "be nice", PromptMessages: msgs})
	lot.OnStepEnd(sctx, TelemetryStepEndEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	lot.OnEnd(ctx, TelemetryFinishEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	got, ok := attrValue(findSpan(rec, "ai.generateText.doGenerate"), "ai.prompt.messages")
	if !ok || got.(string) != want {
		t.Fatalf("ai.prompt.messages =\n%v\nwant\n%s", got, want)
	}

	// Instruction messages lead as one system message each.
	text, _ := legacyPromptMessagesJSON("ignored", []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "a"}}, ProviderOptions: map[string]interface{}{"x": map[string]interface{}{"y": true}}}}, msgs[4:])
	if text != `[{"role":"system","content":"a","providerOptions":{"x":{"y":true}}},{"role":"user","content":[{"type":"text","text":"again"}]}]` {
		t.Fatalf("instruction messages: %s", text)
	}
}

// core1-R02 / core1-R01 root: a string prompt is recorded as TS
// standardizePrompt keeps it, and messages carry no Go-only keys
// (legacy-open-telemetry.ts 373-379).
func TestR91carryLegacyRootPromptShape(t *testing.T) {
	hi := "hi"
	if got := legacyPromptJSON(false, "sys", "", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}, nil, &hi); got != `{"system":"sys","messages":[{"role":"user","content":"hi"}]}` {
		t.Fatalf("string prompt: %s", got)
	}
	msgs := []types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "x"}}, ToolCalls: []types.ToolCall{{ID: "c"}}, Name: "n"}}
	if got := legacyPromptJSON(false, "", "", msgs, nil, nil); got != `{"messages":[{"role":"assistant","content":[{"type":"text","text":"x"}]}]}` {
		t.Fatalf("messages: %s", got)
	}
	// Hidden inputs drop the prompt string too.
	ev := FilterEvent(TelemetryStartEvent{PromptString: &hi}, &Options{RecordInputs: Bool(false)})
	if ev.PromptString != nil {
		t.Fatal("PromptString not hidden")
	}
}

// core1-R03: ai.response.object keeps the model's key order on the step
// and root spans (TS JSON.stringify(JSON.parse(objectText)) /
// JSON.stringify(object)).
func TestR91carryLegacyResponseObjectKeyOrder(t *testing.T) {
	rec, tp := r91carryTracer(t)
	lot := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := lot.OnStart(context.Background(), TelemetryStartEvent{CallID: "o", OperationType: "ai.generateObject", Settings: settings})
	sctx := lot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "o", OperationType: "ai.generateObject", Settings: settings})
	lot.OnStepEnd(sctx, TelemetryStepEndEvent{CallID: "o", OperationType: "ai.generateObject", Settings: settings, Text: `{"b":1,"a":{"z":2,"y":3}}`})
	lot.OnEnd(ctx, TelemetryFinishEvent{CallID: "o", OperationType: "ai.generateObject", Settings: settings,
		Object: map[string]interface{}{"b": 1, "a": map[string]interface{}{"z": 2, "y": 3}}})
	want := `{"b":1,"a":{"z":2,"y":3}}`
	if v, _ := attrValue(findSpan(rec, "ai.generateObject.doGenerate"), "ai.response.object"); v != want {
		t.Fatalf("step ai.response.object = %v", v)
	}
	if v, _ := attrValue(findSpan(rec, "ai.generateObject"), "ai.response.object"); v != want {
		t.Fatalf("root ai.response.object = %v", v)
	}
}

// core1-F03: the legacy tool span has no Go-only ai.toolCall.durationMs.
func TestR91carryLegacyToolSpanNoDuration(t *testing.T) {
	rec, tp := r91carryTracer(t)
	lot := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	ctx := lot.OnStart(context.Background(), TelemetryStartEvent{CallID: "x", OperationType: "ai.generateText", Settings: settings})
	sctx := lot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "x", OperationType: "ai.generateText", Settings: settings})
	tctx := lot.OnToolExecutionStart(sctx, TelemetryToolCallStartEvent{CallID: "x", ToolCallID: "t1", ToolName: "tool", Settings: settings})
	lot.OnToolExecutionEnd(tctx, TelemetryToolCallFinishEvent{CallID: "x", ToolCallID: "t1", ToolName: "tool", Settings: settings, DurationMs: 5, Result: "ok"})
	lot.OnStepEnd(sctx, TelemetryStepEndEvent{CallID: "x", OperationType: "ai.generateText", Settings: settings})
	lot.OnEnd(ctx, TelemetryFinishEvent{CallID: "x", OperationType: "ai.generateText", Settings: settings})
	for _, s := range rec.Ended() {
		if _, ok := attrValue(s, "ai.toolCall.durationMs"); ok {
			t.Fatalf("span %s has ai.toolCall.durationMs", s.Name())
		}
	}
	if findSpan(rec, "ai.toolCall") == nil {
		t.Fatal("no tool span")
	}
}

// core1-05 (carry fields): the speech / transcription event fields follow TS
// filterTelemetryEvent: instructions and providerOptions are inputs,
// segments and words are outputs, and the response body is always dropped.
func TestR91carrySpeechTranscriptionFieldsFiltered(t *testing.T) {
	start := FilterEvent(TelemetryStartEvent{
		OperationType: "ai.generateSpeech", Text: "say", Voice: "alloy", Instructions: "calm",
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"x": 1}},
	}, &Options{RecordInputs: Bool(false)})
	if start.Instructions != "" || start.Text != "" || len(start.ProviderOptions) != 0 || start.ProviderOptions == nil || start.Voice != "alloy" {
		t.Fatalf("start = %+v", start)
	}
	fin := FilterEvent(TelemetryFinishEvent{
		OperationType: "ai.transcribe", Text: "t", Language: "en",
		Segments: []TelemetryTranscriptTiming{{Text: "s"}}, Words: []TelemetryTranscriptTiming{{Text: "w"}},
		Response: &TelemetryAudioResponse{ModelID: "m", Body: "raw"},
	}, &Options{RecordOutputs: Bool(false)})
	if fin.Text != "" || len(fin.Segments) != 0 || len(fin.Words) != 0 || fin.Response == nil || fin.Response.Body != nil || fin.Response.ModelID != "m" || fin.Language != "en" {
		t.Fatalf("finish = %+v", fin)
	}
}

func r91carryCredentialTools(sample string) []types.Tool {
	policy := func() map[string]interface{} {
		return map[string]interface{}{"type": "allowlist", "allowedDomains": []interface{}{"example.com"},
			"domainSecrets": []interface{}{map[string]interface{}{"domain": "example.com", "name": "EXAMPLE", "value": sample}}}
	}
	return []types.Tool{
		{Name: "remote", Type: "provider", ProviderID: "openai.mcp", ProviderArgs: map[string]interface{}{
			"serverLabel": "example", "serverUrl": "https://example.com/mcp", "authorization": sample,
			"headers": map[string]interface{}{"X-Example": sample}}},
		{Name: "python", Type: "provider", ProviderID: "openai.code_interpreter", ProviderArgs: map[string]interface{}{"container": map[string]interface{}{"networkPolicy": policy()}}},
		{Name: "terminal", Type: "provider", ProviderID: "openai.shell", ProviderArgs: map[string]interface{}{"environment": map[string]interface{}{"type": "containerAuto", "networkPolicy": policy()}}},
	}
}

// TS a41a6cf7b3 (security): both integrations redact OpenAI provider tool
// credentials in their tool definition attributes themselves, also for an
// event handed to them without the Fire* dispatch, and never touch the
// caller's tools.
func TestR91carryIntegrationsRedactProviderToolCredentials(t *testing.T) {
	const sample = "sample-value"
	tools := r91carryCredentialTools(sample)
	settings := &Settings{IsEnabled: Bool(true)}

	rec, tp := r91carryTracer(t)
	lot := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("l")})
	ctx := lot.OnStart(context.Background(), TelemetryStartEvent{CallID: "a", OperationType: "ai.generateText", Settings: settings})
	sctx := lot.OnStepStart(ctx, TelemetryStepStartEvent{CallID: "a", OperationType: "ai.generateText", Settings: settings, StepTools: tools})
	lot.OnStepEnd(sctx, TelemetryStepEndEvent{CallID: "a", OperationType: "ai.generateText", Settings: settings})
	lot.OnEnd(ctx, TelemetryFinishEvent{CallID: "a", OperationType: "ai.generateText", Settings: settings})

	ot := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("g")})
	gctx := ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "b", OperationType: "ai.generateText", Settings: settings, ModelProvider: "openai", ModelID: "m"})
	gsctx := ot.OnStepStart(gctx, TelemetryStepStartEvent{CallID: "b", OperationType: "ai.generateText", Settings: settings})
	cctx := ot.OnLanguageModelCallStart(gsctx, LanguageModelCallStartEvent{CallID: "b", Settings: settings, ModelProvider: "openai", ModelID: "m", Tools: tools})
	ot.OnLanguageModelCallEnd(cctx, LanguageModelCallEndEvent{CallID: "b", Settings: settings})
	ot.OnStepEnd(gsctx, TelemetryStepEndEvent{CallID: "b", OperationType: "ai.generateText", Settings: settings})
	ot.OnEnd(gctx, TelemetryFinishEvent{CallID: "b", OperationType: "ai.generateText", Settings: settings})

	// Prebuilt definitions (the WorkflowAgent form) are redacted too.
	var prebuilt []*jsonorder.Object
	for _, tool := range tools {
		prebuilt = append(prebuilt, languageModelToolDefinitionUnredacted(tool))
	}
	gctx = ot.OnStart(context.Background(), TelemetryStartEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings, ModelProvider: "openai", ModelID: "m"})
	gsctx = ot.OnStepStart(gctx, TelemetryStepStartEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	cctx = ot.OnLanguageModelCallStart(gsctx, LanguageModelCallStartEvent{CallID: "c", Settings: settings, ModelProvider: "openai", ModelID: "m", Tools: prebuilt})
	ot.OnLanguageModelCallEnd(cctx, LanguageModelCallEndEvent{CallID: "c", Settings: settings})
	ot.OnStepEnd(gsctx, TelemetryStepEndEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	ot.OnEnd(gctx, TelemetryFinishEvent{CallID: "c", OperationType: "ai.generateText", Settings: settings})
	if !strings.Contains(prebuilt[0].String(), sample) {
		t.Fatal("caller definitions modified")
	}

	var found []string
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if kv.Key == "ai.prompt.tools" || kv.Key == "gen_ai.tool.definitions" {
				found = append(found, fmt.Sprint(kv.Value.AsInterface()))
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("tool definition attributes = %d, want 3: %v", len(found), found)
	}
	for _, v := range found {
		if strings.Contains(v, sample) || !strings.Contains(v, "[redacted]") || !strings.Contains(v, "X-Example") || !strings.Contains(v, "example.com") {
			t.Fatalf("definitions not redacted as TS: %s", v)
		}
	}
	if fmt.Sprint(tools) != fmt.Sprint(r91carryCredentialTools(sample)) {
		t.Fatal("caller tools modified")
	}
}

// languageModelToolDefinitionUnredacted is the {type, name, id, args}
// definition with the real args, as a caller could prebuild it.
func languageModelToolDefinitionUnredacted(tool types.Tool) *jsonorder.Object {
	def := jsonorder.New()
	def.Set("type", "provider")
	def.Set("name", tool.Name)
	def.Set("id", tool.ProviderID)
	def.Set("args", tool.ProviderArgs)
	return def
}

// cr-r91-provc-to-carry: TS onAudioOperationStart records no ai.settings.*,
// so a speech or transcription start event's MaxRetries stays off the span.
func TestR91carryLegacyAudioSpanNoSettings(t *testing.T) {
	rec, tp := r91carryTracer(t)
	lot := NewLegacyOpenTelemetry(LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")})
	settings := &Settings{IsEnabled: Bool(true)}
	retries := 2
	temp := 0.5
	for i, op := range []string{"ai.generateSpeech", "ai.transcribe", "ai.streamTranscribe"} {
		id := fmt.Sprint("a", i)
		ctx := lot.OnStart(context.Background(), TelemetryStartEvent{CallID: id, OperationType: op, Settings: settings, ModelProvider: "p", ModelID: "m", MaxRetries: &retries, Temperature: &temp})
		lot.OnEnd(ctx, TelemetryFinishEvent{CallID: id, OperationType: op, Settings: settings})
	}
	for _, s := range rec.Ended() {
		for _, kv := range s.Attributes() {
			if strings.HasPrefix(string(kv.Key), "ai.settings.") {
				t.Fatalf("span %s has %s", s.Name(), kv.Key)
			}
		}
		if v, _ := attrValue(s, "ai.model.id"); v != "m" {
			t.Fatalf("span %s lost ai.model.id", s.Name())
		}
	}
	if len(rec.Ended()) != 3 {
		t.Fatalf("spans = %d", len(rec.Ended()))
	}
}
