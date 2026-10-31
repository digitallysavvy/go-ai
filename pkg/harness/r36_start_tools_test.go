package harness

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TS onStart / onStepStart events carry `tools` (the merged tool set) and
// `activeTools` (the names the runtime may invoke this turn).
func TestRunPrompt_StartEventsCarryToolsAndActiveTools(t *testing.T) {
	exec := func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "done", nil
	}
	tools := map[string]types.Tool{
		"b": {Name: "b", Description: "inactive", Parameters: map[string]any{"type": "object"}, Execute: exec},
		"a": {Name: "a", Description: "active", Parameters: map[string]any{"type": "object"}, Execute: exec},
	}
	active := map[string]types.Tool{"a": tools["a"]}
	session := &slowToolTurnSession{toolName: "a"}

	var mu sync.Mutex
	var start ai.OnStartEvent
	var stepStart ai.OnStepStartEvent
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: active,
		SandboxSession: testSandbox(), Model: "harness-model",
		Callbacks: Callbacks{
			OnStart:     func(_ context.Context, e ai.OnStartEvent) { mu.Lock(); start = e; mu.Unlock() },
			OnStepStart: func(_ context.Context, e ai.OnStepStartEvent) { mu.Lock(); stepStart = e; mu.Unlock() },
		},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done

	mu.Lock()
	defer mu.Unlock()
	names := func(ts []types.Tool) []string {
		var n []string
		for _, tool := range ts {
			n = append(n, tool.Name)
		}
		return n
	}
	if got := names(start.Tools); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("OnStart tools = %v, want [a b]", got)
	}
	if !reflect.DeepEqual(start.ActiveTools, []string{"a"}) {
		t.Fatalf("OnStart activeTools = %v, want [a]", start.ActiveTools)
	}
	if got := names(stepStart.Tools); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("OnStepStart tools = %v, want [a b]", got)
	}
	if !reflect.DeepEqual(stepStart.ActiveTools, []string{"a"}) {
		t.Fatalf("OnStepStart activeTools = %v, want [a]", stepStart.ActiveTools)
	}
}

// TS onLanguageModelCallStart lists the tools as {name, description?,
// inputSchema?}; the schema must reach the event (and so
// gen_ai.tool.definitions), not just the name.
func TestRunPrompt_LanguageModelCallStartToolsCarryInputSchema(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}
	tools := map[string]types.Tool{"a": {Name: "a", Parameters: schema,
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		}}}
	session := &slowToolTurnSession{toolName: "a"}
	var mu sync.Mutex
	var got []types.Tool
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		ToolSpecs:      []ToolSpec{{Name: "a", Description: "d", InputSchema: schema}},
		SandboxSession: testSandbox(), Model: "harness-model",
		Callbacks: Callbacks{OnLanguageModelCallStart: func(_ context.Context, e ai.LanguageModelCallStartEvent) {
			mu.Lock()
			got = e.Tools
			mu.Unlock()
		}},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Name != "a" || got[0].Description != "d" || !reflect.DeepEqual(got[0].Parameters, schema) {
		t.Fatalf("model call start tools = %+v", got)
	}
}

// TS onToolExecutionStart / onToolExecutionEnd events carry `messages` (the
// turn's prompt as a user message).
func TestRunPrompt_ToolExecutionEventsCarryMessages(t *testing.T) {
	tool := types.Tool{Name: "a", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		}}
	tools := map[string]types.Tool{"a": tool}
	session := &slowToolTurnSession{toolName: "a"}
	var mu sync.Mutex
	var startMsgs, endMsgs []types.Message
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), Model: "harness-model",
		Callbacks: Callbacks{
			OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) { mu.Lock(); startMsgs = e.Messages; mu.Unlock() },
			OnToolExecutionEnd:   func(_ context.Context, e ai.OnToolCallFinishEvent) { mu.Lock(); endMsgs = e.Messages; mu.Unlock() },
		},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done
	mu.Lock()
	defer mu.Unlock()
	if len(startMsgs) != 1 || len(endMsgs) != 1 {
		t.Fatalf("messages start=%v end=%v, want the prompt as one user message", startMsgs, endMsgs)
	}
}

// TS publishes `maxRetries: 0` on the harness start event; the legacy OTel
// integration records it as ai.settings.maxRetries on the root span.
func TestRunPrompt_LegacyTelemetryRootSpanRecordsZeroMaxRetries(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tools := map[string]types.Tool{"a": {Name: "a", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		}}}
	session := &slowToolTurnSession{toolName: "a"}
	out := runPrompt(context.Background(), runPromptInput{
		Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
		Prompt: TextPrompt("go"), Tools: tools, ActiveTools: tools,
		SandboxSession: testSandbox(), Model: "harness-model",
		Telemetry: &telemetry.Options{
			IsEnabled:    telemetry.Bool(true),
			Integrations: []telemetry.TelemetryIntegration{telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("legacy")})},
		},
	})
	stream := out.Result.Stream()
	for {
		if _, err := stream.Next(); err != nil {
			break
		}
	}
	<-out.Done
	roots := spansNamed(rec, "ai.harness")
	if len(roots) != 1 {
		t.Fatalf("ai.harness spans = %d", len(roots))
	}
	for _, kv := range roots[0].Attributes() {
		if string(kv.Key) == "ai.settings.maxRetries" {
			if kv.Value.AsInt64() != 0 {
				t.Fatalf("ai.settings.maxRetries = %v", kv.Value)
			}
			return
		}
	}
	t.Fatalf("root span has no ai.settings.maxRetries: %v", roots[0].Attributes())
}

// TS lifecycle events carry the prompt as one user message whose content is
// promptToText(prompt): the text parts of a structured message, joined.
func TestRunPrompt_LifecycleMessagesArePromptText(t *testing.T) {
	tools := map[string]types.Tool{"a": {Name: "a", Parameters: map[string]any{"type": "object"},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "done", nil
		}}}
	cases := []struct {
		name   string
		prompt Prompt
		want   string
	}{
		{"structured", Prompt{Message: &types.Message{Role: types.RoleUser, Content: []types.ContentPart{
			types.TextContent{Text: "look "}, types.ImageContent{Image: []byte("x"), MimeType: "image/png"}, types.TextContent{Text: "here"},
		}}}, "look here"},
		{"empty text", TextPrompt(""), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			session := &slowToolTurnSession{toolName: "a"}
			var mu sync.Mutex
			var got []types.Message
			out := runPrompt(context.Background(), runPromptInput{
				Harness: &mockHarnessAdapter{id: "mock", session: session}, Session: session,
				Prompt: tc.prompt, Tools: tools, ActiveTools: tools,
				SandboxSession: testSandbox(), Model: "harness-model",
				Callbacks: Callbacks{OnStart: func(_ context.Context, e ai.OnStartEvent) { mu.Lock(); got = e.Messages; mu.Unlock() }},
			})
			stream := out.Result.Stream()
			for {
				if _, err := stream.Next(); err != nil {
					break
				}
			}
			<-out.Done
			mu.Lock()
			defer mu.Unlock()
			if len(got) != 1 || got[0].Role != types.RoleUser || len(got[0].Content) != 1 {
				t.Fatalf("messages = %+v, want one user message with one text part", got)
			}
			if txt, ok := got[0].Content[0].(types.TextContent); !ok || txt.Text != tc.want {
				t.Fatalf("content = %#v, want text %q", got[0].Content[0], tc.want)
			}
		})
	}
}
