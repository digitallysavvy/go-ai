package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 92 gen tests (TS 2ebc78b654).

// create-agent-ui-stream.test.ts 410 'should reject many malformed parts
// without running the agent' (11323f9894): 2500 malformed parts fail
// validation with one TypeValidationError whose message carries the value
// once, and the model is never called.
func TestR92genAgentUIStreamRejectsManyMalformedParts(t *testing.T) {
	streamCalls := 0
	model := &functionalAgentLanguageModel{doStream: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		streamCalls++
		return nil, errors.New("not called")
	}}
	parts := make([]map[string]interface{}, 2500)
	for i := range parts {
		parts[i] = map[string]interface{}{}
	}
	raw, err := json.Marshal([]map[string]interface{}{{"id": "user-1", "role": "user", "parts": parts}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = CreateAgentUIStreamResponseFromUIMessages(context.Background(), NewToolLoopAgent(AgentConfig{Model: model}), CreateAgentUIStreamFromUIMessagesOptions{UIMessages: json.RawMessage(raw)})
	var validationErr *providererrors.TypeValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("err = %v (%T), want a TypeValidationError", err, err)
	}
	if n := len(err.Error()); n >= len(raw)+1000 {
		t.Fatalf("error message is %d bytes, want under %d", n, len(raw)+1000)
	}
	if strings.Count(err.Error(), `"user-1"`) > 1 {
		t.Fatal("the value appears more than once in the message")
	}
	if streamCalls != 0 {
		t.Fatalf("doStream calls = %d, want 0", streamCalls)
	}
}

// The legacy loop's root ai.prompt for a string prompt (Execute,
// GenerateAgent) is TS standardizePrompt's [{role: 'user', content: prompt}]
// (legacy-open-telemetry.ts onGenerateStart; GenerateText r91 core1-R02); a
// messages call keeps its parts, and a nested run does not inherit the
// prompt.
func TestR92genLoopRootPromptString(t *testing.T) {
	run := func(t *testing.T, call func(a *ToolLoopAgent) error) []string {
		t.Helper()
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		tel := &ai.TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{
			telemetry.NewLegacyOpenTelemetry(telemetry.LegacyOpenTelemetryOptions{Tracer: tp.Tracer("t")}),
		}}
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
		}}
		if err := call(NewToolLoopAgent(AgentConfig{Model: model, Telemetry: tel})); err != nil {
			t.Fatal(err)
		}
		var prompts []string
		for _, s := range rec.Ended() {
			if s.Name() != "ai.generateText" {
				continue
			}
			for _, kv := range s.Attributes() {
				if kv.Key == "ai.prompt" {
					prompts = append(prompts, kv.Value.AsString())
				}
			}
		}
		return prompts
	}
	const want = `{"messages":[{"role":"user","content":"Hello"}]}`
	if got := run(t, func(a *ToolLoopAgent) error { _, err := a.Execute(context.Background(), "Hello"); return err }); len(got) != 1 || got[0] != want {
		t.Errorf("Execute: ai.prompt = %v, want %s", got, want)
	}
	if got := run(t, func(a *ToolLoopAgent) error {
		_, err := a.GenerateAgent(context.Background(), AgentGenerateOptions{Prompt: "Hello"})
		return err
	}); len(got) != 1 || got[0] != want {
		t.Errorf("GenerateAgent: ai.prompt = %v, want %s", got, want)
	}
	const parts = `{"messages":[{"role":"user","content":[{"type":"text","text":"Hello"}]}]}`
	if got := run(t, func(a *ToolLoopAgent) error {
		_, err := a.ExecuteWithMessages(context.Background(), []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}}})
		return err
	}); len(got) != 1 || got[0] != parts {
		t.Errorf("ExecuteWithMessages: ai.prompt = %v, want %s", got, parts)
	}
	// A run nested in the call's ctx (a subagent inside a tool) does not
	// see the prompt; replaced messages do not keep it.
	var leaked bool
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(ctx context.Context, _ *provider.GenerateOptions) (*types.GenerateResult, error) {
		leaked = ctx.Value(loopPromptStringKey{}) != nil
		return &types.GenerateResult{Text: "hi", FinishReason: types.FinishReasonStop}, nil
	}}
	if _, err := NewToolLoopAgent(AgentConfig{Model: model}).Execute(context.Background(), "Hello"); err != nil || leaked {
		t.Fatalf("err = %v, prompt string visible to the model call: %v", err, leaked)
	}
	ctx := withLoopPromptString(context.Background(), "Hello")
	if p := loopPromptStringFor(ctx, []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "other"}}}}); p != nil {
		t.Fatal("replaced messages kept the prompt string")
	}
}
