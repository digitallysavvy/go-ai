package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestMapProviderName(t *testing.T) {
	cases := map[string]string{
		"openai.chat":              "openai",
		"anthropic.messages":       "anthropic",
		"google.vertex.chat":       "gcp.vertex_ai",
		"google.generative-ai":     "gcp.gemini",
		"google-vertex":            "gcp.vertex_ai",
		"google":                   "gcp.gemini",
		"amazon-bedrock.converse":  "aws.bedrock",
		"azure-openai.chat":        "azure.ai.openai",
		"azure.chat":               "azure.ai.inference",
		"mistral.chat":             "mistral_ai",
		"xai.chat":                 "x_ai",
		"totally-unknown-provider": "totally-unknown-provider",
	}
	for input, want := range cases {
		if got := mapProviderName(input); got != want {
			t.Errorf("mapProviderName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMapOperationName(t *testing.T) {
	cases := map[string]string{
		"ai.generateText":   "invoke_agent",
		"ai.streamText":     "invoke_agent",
		"ai.generateObject": "invoke_agent",
		"ai.streamObject":   "invoke_agent",
		"ai.embed":          "embeddings",
		"ai.embedMany":      "embeddings",
		"ai.rerank":         "rerank",
		"ai.something.else": "ai.something.else",
	}
	for input, want := range cases {
		if got := mapOperationName(input); got != want {
			t.Errorf("mapOperationName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOpenTelemetryOnStartAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), FunctionID: "my-fn"}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		ModelProvider: "openai.chat",
		ModelID:       "gpt-5",
		Settings:      settings,
	})
	trace.SpanFromContext(ctx).End()

	// Root span name is "${mapOperationName(operationId)} ${modelId}" (TS
	// onGenerateStart et al.) — functionId never appears in the span name,
	// only in the gen_ai.agent.name attribute checked below (H3 follow-up 5).
	span := findSpan(rec, "invoke_agent gpt-5")
	if span == nil {
		t.Fatal("expected a span named 'invoke_agent gpt-5'")
	}
	if v, ok := attrValue(span, "gen_ai.operation.name"); !ok || v.(string) != "invoke_agent" {
		t.Fatalf("expected gen_ai.operation.name=invoke_agent, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.provider.name"); !ok || v.(string) != "openai" {
		t.Fatalf("expected gen_ai.provider.name=openai, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.agent.name"); !ok || v.(string) != "my-fn" {
		t.Fatalf("expected gen_ai.agent.name=my-fn, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.request.model"); !ok || v.(string) != "gpt-5" {
		t.Fatalf("expected gen_ai.request.model=gpt-5, got %v ok=%v", v, ok)
	}
}

func TestOpenTelemetryLanguageModelCallStartRequestParams(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "call-1"})
	// H5: OnLanguageModelCallStart parents the "chat" span under the
	// integration's own step span, resolved by CallID (mirrors TS's
	// state.stepContext requirement), so OnStepStart must fire first.
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 0, CallID: "call-1"})
	temp := 0.7
	maxTokens := 512
	seed := 42
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{
		Settings:        settings,
		CallID:          "call-1",
		ModelProvider:   "anthropic",
		ModelID:         "claude",
		Temperature:     &temp,
		MaxOutputTokens: &maxTokens,
		Seed:            &seed,
		StopSequences:   []string{"STOP"},
	})
	if !trace.SpanFromContext(ctx).IsRecording() {
		t.Fatal("expected the model-call ctx to carry a recording span")
	}
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "call-1", FinishReason: "stop"})

	span := findSpan(rec, "chat claude")
	if span == nil {
		t.Fatal("expected a 'chat claude' span")
	}
	if v, ok := attrValue(span, "gen_ai.request.temperature"); !ok || v.(float64) != 0.7 {
		t.Fatalf("expected gen_ai.request.temperature=0.7, got %v ok=%v", v, ok)
	}
	if v, ok := attrValue(span, "gen_ai.request.max_tokens"); !ok || v.(int64) != 512 {
		t.Fatalf("expected gen_ai.request.max_tokens=512, got %v ok=%v", v, ok)
	}
	// TS onLanguageModelCallStart records no seed on the chat span; only
	// the root span carries gen_ai.request.seed.
	if v, ok := attrValue(span, "gen_ai.request.seed"); ok {
		t.Fatalf("chat span must not carry gen_ai.request.seed, got %v", v)
	}
	if v, ok := attrValue(span, "gen_ai.response.finish_reasons"); !ok {
		t.Fatalf("expected gen_ai.response.finish_reasons to be set, ok=%v val=%v", ok, v)
	}
}

// TestOpenTelemetryProviderMetadataGatedByOptionOnly covers follow-up H2:
// TS's selectSupplementalAttributes gates ai.response.providerMetadata only
// by the providerMetadata supplemental attribute flag (a plain,
// pre-computed JSON string, not wrapped in an {output: () => ...}
// accessor), so selectAttributes' recordOutputs gate never applies to it —
// see legacy-open-telemetry.ts onLanguageModelCallEnd/onStepEnd and
// open-telemetry.ts's equivalents. Go previously also required
// Settings.RecordOutputs, dropping providerMetadata whenever a caller
// disabled output recording even with ProviderMetadata explicitly opted in.
func TestOpenTelemetryProviderMetadataGatedByOptionOnly(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, ProviderMetadata: true})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: false}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "call-pm"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 0, CallID: "call-pm"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "call-pm", ModelID: "gpt-5"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings:         settings,
		CallID:           "call-pm",
		FinishReason:     "stop",
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"cachedTokens": 5}},
	})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		Settings:         settings,
		StepNumber:       0,
		FinishReason:     "stop",
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{"cachedTokens": 5}},
	})

	chatSpan := findSpan(rec, "chat gpt-5")
	if chatSpan == nil {
		t.Fatal("expected a 'chat gpt-5' span")
	}
	if _, ok := attrValue(chatSpan, "ai.response.providerMetadata"); !ok {
		t.Error("expected ai.response.providerMetadata on the chat span even with RecordOutputs=false, since it is gated only by the ProviderMetadata option")
	}

	// Step span name is "step ${steps.length + 1}" (TS onStepStart); Go's
	// StepNumber:0 is the first step, so the span is "step 1" (H3 follow-up 5).
	stepSpan := findSpan(rec, "step 1")
	if stepSpan == nil {
		t.Fatal("expected a 'step 1' span")
	}
	if _, ok := attrValue(stepSpan, "ai.response.providerMetadata"); !ok {
		t.Error("expected ai.response.providerMetadata on the step span even with RecordOutputs=false, since it is gated only by the ProviderMetadata option")
	}
}

// TestOpenTelemetryEmbeddingUsageNotDoubleCounted covers c0a42bc: the root
// ai.embed span must not carry gen_ai.usage.input_tokens (only the
// embeddings request span does), so a trace-wide sum of that attribute
// counts the tokens once.
func TestOpenTelemetryEmbeddingUsageNotDoubleCounted(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	rootCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", Settings: settings})
	integration.OnEmbedStart(rootCtx, EmbeddingModelCallStartEvent{Settings: settings, CallID: "embed-1", ModelProvider: "openai", ModelID: "text-embedding-3"})
	integration.OnEmbedEnd(rootCtx, EmbeddingModelCallEndEvent{
		Settings: settings, CallID: "embed-1",
		Usage: types.EmbeddingUsage{InputTokens: 12},
	})
	total := int64(12)
	integration.OnEnd(rootCtx, TelemetryFinishEvent{
		Settings: settings,
		Usage:    TelemetryUsage{TotalTokens: &total},
	})

	// Root span name is "${mapOperationName(operationId)} ${modelId}"; no
	// ModelID is set on the start event here, so it's just "embeddings"
	// (H3 follow-up 5).
	rootSpan := findSpan(rec, "embeddings")
	embedSpan := findSpan(rec, "embeddings text-embedding-3")
	if rootSpan == nil || embedSpan == nil {
		t.Fatalf("expected both spans, root=%v embed=%v", rootSpan, embedSpan)
	}
	if _, ok := attrValue(rootSpan, "gen_ai.usage.input_tokens"); ok {
		t.Fatal("expected the root ai.embed span to NOT carry gen_ai.usage.input_tokens (double count)")
	}
	if v, ok := attrValue(embedSpan, "gen_ai.usage.input_tokens"); !ok || v.(int64) != 12 {
		t.Fatalf("expected the embeddings span to carry gen_ai.usage.input_tokens=12, got %v ok=%v", v, ok)
	}
}

// TestOpenTelemetryProviderExecutedToolGetsExecuteToolSpan covers 5ad6abf: a
// provider-executed tool call surfaced in OnStepEnd gets its own
// execute_tool span with gen_ai.tool.type=extension.
// TestOpenTelemetryProviderExecutedToolGetsExecuteToolSpan covers 37b75e8: a
// provider-executed tool call surfaced in the model response gets its own
// execute_tool span, parented under the chat span (TS's onLanguageModelCallEnd
// creates these — not onStepEnd, which only records step-level attributes).
func TestOpenTelemetryProviderExecutedToolGetsExecuteToolSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "call-tc"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 0, CallID: "call-tc"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "call-tc", ModelID: "gpt-5"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings:     settings,
		CallID:       "call-tc",
		FinishReason: "tool-calls",
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "tc-1", ToolName: "web_search", ProviderExecuted: true},
		},
	})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{
		Settings:     settings,
		StepNumber:   0,
		FinishReason: "tool-calls",
		ToolCalls: []types.ToolCall{
			{ID: "tc-1", ToolName: "web_search", ProviderExecuted: true},
		},
	})

	toolSpans := findSpans(rec, "execute_tool web_search")
	if len(toolSpans) != 1 {
		t.Fatalf("expected exactly one 'execute_tool web_search' span (chat-span level only, not duplicated at step end), got %d", len(toolSpans))
	}
	if v, ok := attrValue(toolSpans[0], "gen_ai.tool.type"); !ok || v.(string) != "extension" {
		t.Fatalf("expected gen_ai.tool.type=extension, got %v ok=%v", v, ok)
	}
}

func TestOpenTelemetryStepSpanNaming(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 2})
	integration.OnStepEnd(ctx, TelemetryStepEndEvent{Settings: settings, StepNumber: 2, FinishReason: "stop"})

	// StepNumber is 0-indexed; TS names the span "step ${steps.length + 1}",
	// so StepNumber:2 (the third step) is "step 3" (H3 follow-up 5).
	span := findSpan(rec, "step 3")
	if span == nil {
		t.Fatal("expected a span named 'step 3'")
	}
	if v, ok := attrValue(span, "gen_ai.operation.name"); !ok || v.(string) != "agent_step" {
		t.Fatalf("expected gen_ai.operation.name=agent_step, got %v ok=%v", v, ok)
	}
}

// TestOpenTelemetryOnStartUsesSystemNotPrompt covers a regression where
// gen_ai.system_instructions was populated from e.Prompt (the raw text-mode
// prompt) instead of e.System (the actual system instructions).
func TestOpenTelemetryOnStartUsesSystemNotPrompt(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
		Prompt:        "the user prompt text",
		System:        "Be helpful",
	})
	trace.SpanFromContext(ctx).End()

	// No ModelID is set, so the span name is just the mapped operation name.
	span := findSpan(rec, "invoke_agent")
	v, ok := attrValue(span, "gen_ai.system_instructions")
	if !ok {
		t.Fatal("expected gen_ai.system_instructions to be set")
	}
	got := v.(string)
	if got != `[{"content":"Be helpful","type":"text"}]` {
		t.Fatalf("gen_ai.system_instructions = %s, want the SemConv shape for e.System, not e.Prompt", got)
	}
}

// TestOpenTelemetryOnStartInputMessages covers gen_ai.input.messages on the
// root span, built from TelemetryStartEvent.Messages via formatInputMessages.
func TestOpenTelemetryOnStartInputMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText",
		Settings:      settings,
		Messages:      []types.Message{textMessage(types.RoleUser, "hi")},
	})
	trace.SpanFromContext(ctx).End()

	// No ModelID is set, so the span name is just the mapped operation name.
	span := findSpan(rec, "invoke_agent")
	v, ok := attrValue(span, "gen_ai.input.messages")
	if !ok {
		t.Fatal("expected gen_ai.input.messages to be set")
	}
	want := `[{"role":"user","parts":[{"content":"hi","type":"text"}]}]`
	if v.(string) != want {
		t.Fatalf("gen_ai.input.messages = %s, want %s", v, want)
	}
}

// TestOpenTelemetryLanguageModelCallOutputMessagesSemConvShape covers
// gen_ai.output.messages on the chat span using the SemConv per-part shape
// instead of a raw JSON dump of the internal content-part structs.
func TestOpenTelemetryLanguageModelCallOutputMessagesSemConvShape(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true), RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "call-2"})
	// H5: OnLanguageModelCallStart parents the "chat" span under the
	// integration's own step span, resolved by CallID.
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", StepNumber: 0, CallID: "call-2"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "call-2", ModelID: "gpt-5"})
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings:     settings,
		CallID:       "call-2",
		FinishReason: "stop",
		Content:      []types.ContentPart{types.TextContent{Text: "hello"}},
	})

	span := findSpan(rec, "chat gpt-5")
	v, ok := attrValue(span, "gen_ai.output.messages")
	if !ok {
		t.Fatal("expected gen_ai.output.messages to be set")
	}
	want := `[{"role":"assistant","parts":[{"content":"hello","type":"text"}],"finish_reason":"stop"}]`
	if v.(string) != want {
		t.Fatalf("gen_ai.output.messages = %s, want %s", v, want)
	}
}

// TestOpenTelemetryStepToolChoiceOptIn covers 152c67c/18651f6:
// ai.prompt.toolChoice on the step span, gated by OpenTelemetryOptions.ToolChoice.
func TestOpenTelemetryStepToolChoiceOptIn(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer, ToolChoice: true})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnStepStart(ctx, TelemetryStepStartEvent{
		Settings:   settings,
		StepNumber: 1,
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	})

	span := findSpan(rec, "step 2")
	v, ok := attrValue(span, "ai.prompt.toolChoice")
	if !ok {
		t.Fatal("expected ai.prompt.toolChoice to be set when OpenTelemetryOptions.ToolChoice is true")
	}
	if v.(string) != `{"type":"required"}` {
		t.Fatalf("ai.prompt.toolChoice = %s, want {\"type\":\"required\"}", v)
	}
}

// TestOpenTelemetryStepToolChoiceOptedOut confirms ai.prompt.toolChoice is
// omitted by default (matching TS's opt-in gate).
func TestOpenTelemetryStepToolChoiceOptedOut(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("genai-test")

	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings})
	integration.OnStepStart(ctx, TelemetryStepStartEvent{
		Settings:   settings,
		StepNumber: 1,
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	})

	span := findSpan(rec, "step 2")
	if span == nil {
		t.Fatal("expected a span named 'step 2'")
	}
	if _, ok := attrValue(span, "ai.prompt.toolChoice"); ok {
		t.Fatal("expected ai.prompt.toolChoice to be omitted when OpenTelemetryOptions.ToolChoice is unset")
	}
}

// TestOpenTelemetrySpanStatusErrorOnErrorFinishReason ports TS 51e1763d2b
// (#21915): the "chat" (language-model-call), step, and root spans must get
// SpanStatusCode.ERROR when the corresponding finish event's FinishReason is
// "error", and otherwise keep the default Unset status.
func TestOpenTelemetrySpanStatusErrorOnErrorFinishReason(t *testing.T) {
	for _, finishReason := range []string{"error", "stop"} {
		t.Run(finishReason, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
			tracer := tp.Tracer("genai-finish-status-test")

			integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tracer})
			settings := &Settings{IsEnabled: Bool(true)}

			ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
				OperationType: "ai.generateText", ModelID: "gpt-5", Settings: settings, CallID: "call-1",
			})
			ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{
				Settings: settings, OperationType: "ai.generateText", StepNumber: 0, CallID: "call-1",
			})
			ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{
				Settings: settings, CallID: "call-1", ModelID: "claude",
			})
			integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
				Settings: settings, CallID: "call-1", FinishReason: finishReason,
			})
			integration.OnStepEnd(ctx, TelemetryStepEndEvent{
				Settings: settings, StepNumber: 0, CallID: "call-1", FinishReason: finishReason,
			})
			integration.OnEnd(ctx, TelemetryFinishEvent{
				Settings: settings, CallID: "call-1", FinishReason: finishReason,
			})

			wantCode := codes.Unset
			if finishReason == "error" {
				wantCode = codes.Error
			}
			for _, name := range []string{"chat claude", "step 1", "invoke_agent gpt-5"} {
				span := findSpan(rec, name)
				if span == nil {
					t.Fatalf("expected a %q span", name)
				}
				if got := span.Status().Code; got != wantCode {
					t.Errorf("%s span status = %v, want %v", name, got, wantCode)
				}
			}
		})
	}
}

// TS onGenerateStart / onObjectOperationStart put the request settings on the
// root span (open-telemetry.test.ts "creates root span with correct
// attributes"), and the object root declares gen_ai.output.type=json.
func TestOpenTelemetryOnStartRequestSettingsOnRootSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test"), Schema: true})
	settings := &Settings{IsEnabled: Bool(true)}

	maxTokens, topK, seed := 100, 5, 7
	temperature, topP := 0.7, 0.9
	start := TelemetryStartEvent{
		OperationType:   "ai.generateText",
		ModelProvider:   "openai.chat",
		ModelID:         "gpt-text",
		Settings:        settings,
		MaxOutputTokens: &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		TopK:            &topK,
		Seed:            &seed,
		StopSequences:   []string{"END"},
	}
	trace.SpanFromContext(integration.OnStart(context.Background(), start)).End()

	span := findSpan(rec, "invoke_agent gpt-text")
	if span == nil {
		t.Fatal("missing text root span")
	}
	for key, want := range map[string]interface{}{
		"gen_ai.request.max_tokens":  int64(100),
		"gen_ai.request.temperature": 0.7,
		"gen_ai.request.top_p":       0.9,
		"gen_ai.request.top_k":       int64(5),
		"gen_ai.request.seed":        int64(7),
	} {
		if v, ok := attrValue(span, key); !ok || v != want {
			t.Errorf("%s = %v (ok=%v), want %v", key, v, ok, want)
		}
	}
	if _, ok := attrValue(span, "gen_ai.request.stop_sequences"); !ok {
		t.Error("generateText root must carry gen_ai.request.stop_sequences")
	}
	if _, ok := attrValue(span, "gen_ai.output.type"); ok {
		t.Error("text root must not carry gen_ai.output.type")
	}

	start.OperationType = "ai.generateObject"
	start.ModelID = "gpt-object"
	start.SettingsOutput = "object"
	trace.SpanFromContext(integration.OnStart(context.Background(), start)).End()
	obj := findSpan(rec, "invoke_agent gpt-object")
	if obj == nil {
		t.Fatal("missing object root span")
	}
	if v, ok := attrValue(obj, "gen_ai.output.type"); !ok || v != "json" {
		t.Errorf("gen_ai.output.type = %v (ok=%v), want json", v, ok)
	}
	if _, ok := attrValue(obj, "gen_ai.request.stop_sequences"); ok {
		t.Error("object root must not carry gen_ai.request.stop_sequences (TS settings omit it)")
	}
	if v, ok := attrValue(obj, "ai.settings.output"); !ok || v != "object" {
		t.Errorf("ai.settings.output = %v (ok=%v), want object", v, ok)
	}
	if v, ok := attrValue(obj, "gen_ai.request.max_tokens"); !ok || v != int64(100) {
		t.Errorf("object root max_tokens = %v (ok=%v)", v, ok)
	}
}

// TS open-telemetry.test.ts "sets GenAI client performance attributes on the
// chat span": time_per_output_chunk is the average gap in seconds (67ms -> 0.067).
func TestOpenTelemetryLanguageModelCallEndPerformanceAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test")})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "perf-1", ModelID: "m"})
	ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "perf-1"})
	ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "perf-1", ModelID: "m"})
	first := int64(345)
	integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{
		Settings:     settings,
		CallID:       "perf-1",
		FinishReason: "stop",
		Performance: LanguageModelCallPerformance{
			ResponseTimeMs:            1234,
			TimeToFirstOutputMs:       &first,
			TimeBetweenOutputChunksMs: &types.OutputChunkTimingStats{Min: 10, P10: 20, Median: 50, Avg: 67, P90: 90, Max: 100},
		},
	})

	span := findSpan(rec, "chat m")
	if span == nil {
		t.Fatal("missing chat span")
	}
	for key, want := range map[string]float64{
		"gen_ai.client.operation.duration":              1.234,
		"gen_ai.client.operation.time_to_first_chunk":   0.345,
		"gen_ai.client.operation.time_per_output_chunk": 0.067,
	} {
		v, ok := attrValue(span, key)
		if !ok || math.Abs(v.(float64)-want) > 1e-9 {
			t.Errorf("%s = %v (ok=%v), want %v", key, v, ok, want)
		}
	}
}

// TS stringifies the prepared tools for gen_ai.tool.definitions (function
// tools keep type and inputSchema, provider tools id and args) and gates the
// attribute on recordInputs.
func TestOpenTelemetryToolDefinitionsShapeAndInputGate(t *testing.T) {
	strict := true
	tools := []types.Tool{
		{Name: "get_weather", Description: "Get weather", Parameters: map[string]interface{}{"type": "object"}, Strict: &strict},
		{Name: "web_search", Type: "provider", ProviderID: "openai.web_search", ProviderArgs: map[string]interface{}{"k": "v"}},
	}
	run := func(recordInputs bool) *tracetest.SpanRecorder {
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
		integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test")})
		settings := &Settings{IsEnabled: Bool(true), RecordInputs: recordInputs, RecordOutputs: true}
		ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "td", ModelID: "m"})
		ctx = integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "td"})
		ctx = integration.OnLanguageModelCallStart(ctx, LanguageModelCallStartEvent{Settings: settings, CallID: "td", ModelID: "m", Tools: tools})
		integration.OnLanguageModelCallEnd(ctx, LanguageModelCallEndEvent{Settings: settings, CallID: "td", FinishReason: "stop"})
		return rec
	}

	span := findSpan(run(true), "chat m")
	if span == nil {
		t.Fatal("missing chat span")
	}
	raw, ok := attrValue(span, "gen_ai.tool.definitions")
	if !ok {
		t.Fatal("missing gen_ai.tool.definitions")
	}
	var defs []map[string]interface{}
	if err := json.Unmarshal([]byte(raw.(string)), &defs); err != nil {
		t.Fatal(err)
	}
	if len(defs) != 2 ||
		defs[0]["type"] != "function" || defs[0]["name"] != "get_weather" || defs[0]["description"] != "Get weather" ||
		defs[0]["inputSchema"] == nil || defs[0]["strict"] != true ||
		defs[1]["type"] != "provider" || defs[1]["id"] != "openai.web_search" || defs[1]["args"] == nil {
		t.Fatalf("definitions = %v", defs)
	}

	if span := findSpan(run(false), "chat m"); span != nil {
		if _, ok := attrValue(span, "gen_ai.tool.definitions"); ok {
			t.Error("tool definitions are inputs: must be omitted when recordInputs is false")
		}
	}
}

// TS onGenerateEnd records finish reasons, usage and gen_ai.output.messages
// on the root span (open-telemetry.test.ts "sets total usage and output on
// root span"); the step span only gets the supplemental attributes.
func TestOpenTelemetryRootAndStepSpanEndAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test"), Usage: true})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateText", Settings: settings, CallID: "end-1", ModelID: "m", ModelProvider: "openai.chat"})
	stepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "end-1", ModelID: "m", ModelProvider: "openai.chat"})
	usage := TelemetryUsage{
		InputTokens: int64p(10), OutputTokens: int64p(20), CacheReadInputTokens: int64p(3), CacheCreationInputTokens: int64p(4),
		NoCacheInputTokens: int64p(7), OutputTextTokens: int64p(15), ReasoningTokens: int64p(5),
	}
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{Settings: settings, OperationType: "ai.generateText", CallID: "end-1", FinishReason: "stop", Usage: usage})
	integration.OnEnd(ctx, TelemetryFinishEvent{
		CallID: "end-1", OperationType: "ai.generateText", Settings: settings, FinishReason: "tool-calls", Usage: usage,
		Text:           "Hello world",
		ReasoningParts: []types.ReasoningContent{{Text: "think"}},
		ToolCalls:      []types.ToolCall{{ID: "tc1", ToolName: "weather", Arguments: map[string]interface{}{"city": "Berlin"}}},
		ToolResults: []types.ToolResult{
			{ToolCallID: "tc1", ToolName: "weather", Result: map[string]interface{}{"temp": 20}},
			{ToolCallID: "tc2", ToolName: "failing", Error: context.Canceled},
		},
	})

	step := findSpan(rec, "step 1")
	if step == nil {
		t.Fatal("missing step span")
	}
	for _, key := range []string{"gen_ai.response.finish_reasons", "gen_ai.usage.input_tokens", "gen_ai.usage.output_tokens", "gen_ai.provider.name", "gen_ai.request.model"} {
		if _, ok := attrValue(step, key); ok {
			t.Errorf("step span must not carry %s (TS keeps it on the chat span and root)", key)
		}
	}
	if v, ok := attrValue(step, "ai.usage.inputTokenDetails.noCacheTokens"); !ok || v != int64(7) {
		t.Errorf("step noCacheTokens = %v (ok=%v), want 7", v, ok)
	}

	root := findSpan(rec, "invoke_agent m")
	if root == nil {
		t.Fatal("missing root span")
	}
	for key, want := range map[string]int64{
		"gen_ai.usage.input_tokens":                   10,
		"gen_ai.usage.output_tokens":                  20,
		"gen_ai.usage.cache_read.input_tokens":        3,
		"gen_ai.usage.cache_creation.input_tokens":    4,
		"ai.usage.inputTokenDetails.noCacheTokens":    7,
		"ai.usage.outputTokenDetails.textTokens":      15,
		"ai.usage.outputTokenDetails.reasoningTokens": 5,
	} {
		if v, ok := attrValue(root, key); !ok || v != want {
			t.Errorf("root %s = %v (ok=%v), want %d", key, v, ok, want)
		}
	}
	if _, ok := attrValue(root, "ai.usage.totalTokens"); ok {
		t.Error("ai.usage.totalTokens is not a TS supplemental usage attribute")
	}
	raw, ok := attrValue(root, "gen_ai.output.messages")
	if !ok {
		t.Fatal("root span is missing gen_ai.output.messages")
	}
	var msgs []struct {
		Role         string                   `json:"role"`
		FinishReason string                   `json:"finish_reason"`
		Parts        []map[string]interface{} `json:"parts"`
	}
	if err := json.Unmarshal([]byte(raw.(string)), &msgs); err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "assistant" || msgs[0].FinishReason != "tool_call" {
		t.Fatalf("messages = %+v", msgs)
	}
	var kinds []string
	for _, p := range msgs[0].Parts {
		kinds = append(kinds, p["type"].(string))
	}
	if got := strings.Join(kinds, ","); got != "reasoning,text,tool_call,tool_call_response" {
		t.Errorf("part order = %s, want reasoning,text,tool_call,tool_call_response (failed tool results are not tool results)", got)
	}
}

// TS onRerankOperationEnd records nothing and onEmbedOperationEnd only the
// embedding values: no finish reason and no usage on those roots.
func TestOpenTelemetryEmbedAndRerankRootEnd(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test"), Embedding: true})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	embedCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.embed", Settings: settings, CallID: "e1", ModelID: "emb"})
	integration.OnEnd(embedCtx, TelemetryFinishEvent{CallID: "e1", OperationType: "ai.embed", Settings: settings, FinishReason: "stop", Usage: TelemetryUsage{TotalTokens: int64p(5)}, Embedding: []float64{0.1, 0.2}})
	embed := findSpan(rec, "embeddings emb")
	if embed == nil {
		t.Fatal("missing embed root span")
	}
	if v, ok := attrValue(embed, "ai.embedding"); !ok || v != "[0.1,0.2]" {
		t.Errorf("ai.embedding = %v (ok=%v)", v, ok)
	}
	for _, key := range []string{"gen_ai.response.finish_reasons", "gen_ai.usage.input_tokens"} {
		if _, ok := attrValue(embed, key); ok {
			t.Errorf("embed root must not carry %s", key)
		}
	}

	rerankCtx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.rerank", Settings: settings, CallID: "r1", ModelID: "rr"})
	integration.OnEnd(rerankCtx, TelemetryFinishEvent{CallID: "r1", OperationType: "ai.rerank", Settings: settings, FinishReason: "stop"})
	rerank := findSpan(rec, "rerank rr")
	if rerank == nil {
		t.Fatal("missing rerank root span")
	}
	if _, ok := attrValue(rerank, "gen_ai.response.finish_reasons"); ok {
		t.Error("rerank root must not carry finish reasons")
	}
}

func TestOpenTelemetryObjectRootEndOutputMessages(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test")})
	settings := &Settings{IsEnabled: Bool(true), RecordInputs: true, RecordOutputs: true}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{OperationType: "ai.generateObject", Settings: settings, CallID: "o1", ModelID: "m"})
	integration.OnEnd(ctx, TelemetryFinishEvent{
		CallID: "o1", OperationType: "ai.generateObject", Settings: settings, FinishReason: "stop",
		Usage:  TelemetryUsage{InputTokens: int64p(3), CacheCreationInputTokens: int64p(9)},
		Object: map[string]interface{}{"name": "Ann"},
	})
	root := findSpan(rec, "invoke_agent m")
	if root == nil {
		t.Fatal("missing root span")
	}
	raw, ok := attrValue(root, "gen_ai.output.messages")
	if !ok || raw != `[{"role":"assistant","parts":[{"content":"{\"name\":\"Ann\"}","type":"text"}],"finish_reason":"stop"}]` {
		t.Errorf("gen_ai.output.messages = %v (ok=%v)", raw, ok)
	}
	if _, ok := attrValue(root, "gen_ai.usage.cache_creation.input_tokens"); ok {
		t.Error("object root must not carry cache_creation tokens (TS omits them)")
	}
}

// TS reuses baseSupplementalAttributes (runtime context, headers) on the step
// span but not on tool spans.
func TestOpenTelemetryStepSpanReusesRootSupplementalAttributes(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai-test"), RuntimeContext: true, Headers: true})
	settings := &Settings{IsEnabled: Bool(true)}

	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		OperationType: "ai.generateText", Settings: settings, CallID: "sup", ModelID: "m",
		RuntimeContext: map[string]interface{}{"user": "u1"}, Headers: map[string]string{"x-id": "7"},
	})
	stepCtx := integration.OnStepStart(ctx, TelemetryStepStartEvent{Settings: settings, OperationType: "ai.generateText", CallID: "sup"})
	toolCtx := integration.OnToolExecutionStart(stepCtx, TelemetryToolCallStartEvent{Settings: settings, CallID: "sup", ToolCallID: "t1", ToolName: "tool"})
	integration.OnToolExecutionEnd(toolCtx, TelemetryToolCallFinishEvent{Settings: settings, CallID: "sup", ToolCallID: "t1", ToolName: "tool"})
	integration.OnStepEnd(stepCtx, TelemetryStepEndEvent{Settings: settings, OperationType: "ai.generateText", CallID: "sup", FinishReason: "stop"})

	step := findSpan(rec, "step 1")
	tool := findSpan(rec, "execute_tool tool")
	if step == nil || tool == nil {
		t.Fatal("missing step or tool span")
	}
	if v, ok := attrValue(step, "ai.settings.context.user"); !ok || v != "u1" {
		t.Errorf("step runtime context = %v (ok=%v)", v, ok)
	}
	if v, ok := attrValue(step, "ai.request.headers.x-id"); !ok || v != "7" {
		t.Errorf("step header = %v (ok=%v)", v, ok)
	}
	if _, ok := attrValue(tool, "ai.settings.context.user"); ok {
		t.Error("tool span must not carry the runtime context (TS tool spans have none)")
	}
}

// TS onEmbedOperationStart / onRerankOperationStart put the embedding and
// reranking inputs on the root span too (opt-in, input-gated), and the
// rerank span stringifies object documents as well as text ones.
func TestOpenTelemetryRootSpansCarryEmbeddingAndRerankingInputs(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	integration := NewOpenTelemetry(OpenTelemetryOptions{Tracer: tp.Tracer("genai"), Embedding: true, Reranking: true})
	settings := &Settings{RecordInputs: true}

	trace.SpanFromContext(integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "e1", OperationType: "ai.embed", ModelID: "emb", Settings: settings, Prompt: "hello",
	})).End()
	trace.SpanFromContext(integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "e2", OperationType: "ai.embedMany", ModelID: "emb-many", Settings: settings, Values: []string{"a", "b"},
	})).End()
	docs := []map[string]interface{}{{"id": 1}}
	ctx := integration.OnStart(context.Background(), TelemetryStartEvent{
		CallID: "r1", OperationType: "ai.rerank", ModelID: "rr", Settings: settings, Documents: docs,
	})
	integration.OnRerankStart(ctx, RerankingModelCallStartEvent{CallID: "r1", OperationID: "ai.rerank.doRerank", ModelID: "rr", Settings: settings, Documents: docs})
	integration.OnRerankEnd(ctx, RerankingModelCallEndEvent{CallID: "r1", OperationID: "ai.rerank.doRerank", Settings: settings})
	trace.SpanFromContext(ctx).End()

	if v, _ := attrValue(findSpan(rec, "embeddings emb"), "ai.value"); v != `"hello"` {
		t.Errorf("embed root ai.value = %v", v)
	}
	if v, _ := attrValue(findSpan(rec, "embeddings emb-many"), "ai.values"); fmt.Sprint(v) != `["a" "b"]` {
		t.Errorf("embedMany root ai.values = %v", v)
	}
	for _, span := range findSpans(rec, "rerank rr") {
		if v, _ := attrValue(span, "ai.documents"); fmt.Sprint(v) != `[{"id":1}]` {
			t.Errorf("%s ai.documents = %v", span.Name(), v)
		}
	}
	if n := len(findSpans(rec, "rerank rr")); n != 2 {
		t.Fatalf("rerank spans = %d, want root and nested", n)
	}
}
