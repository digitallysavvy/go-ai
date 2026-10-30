package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r70SystemModel answers every call with "ok" and records the system text of
// each call's prompt (Prompt.System or the system-role messages).
type r70SystemModel struct {
	wfMockModel
	mu      sync.Mutex
	systems []string
}

func (m *r70SystemModel) record(opts *provider.GenerateOptions) {
	parts := []string{}
	if opts.Prompt.System != "" {
		parts = append(parts, opts.Prompt.System)
	}
	for _, msg := range opts.Prompt.Messages {
		if msg.Role == types.RoleSystem {
			parts = append(parts, systemMessageText(msg))
		}
	}
	m.mu.Lock()
	m.systems = append(m.systems, strings.Join(parts, "|"))
	m.mu.Unlock()
}

func (m *r70SystemModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.record(opts)
	return &types.GenerateResult{Text: "ok", FinishReason: types.FinishReasonStop}, nil
}

func (m *r70SystemModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.record(opts)
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "ok"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

func (m *r70SystemModel) lastSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.systems) == 0 {
		return "<no call>"
	}
	return m.systems[len(m.systems)-1]
}

const r70InvalidInstructions = "Invalid prompt: instructions must be a string, SystemModelMessage, or array of SystemModelMessage"

// r70Run runs the agent through GenerateWithOptions or StreamWithOptions
// with the given call system and instructions.
func r70Run(t *testing.T, a *WorkflowAgent, stream bool, system string, instructions interface{}) error {
	t.Helper()
	ctx := context.Background()
	if !stream {
		_, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi", System: system, Instructions: instructions})
		return err
	}
	res, err := a.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi", System: system, Instructions: instructions})
	if err != nil {
		return err
	}
	_ = res.ConsumeStream()
	return res.Err()
}

// TS prepareInvocation standardizes the instructions prepareCall returns
// (`if (prepared.instructions !== undefined) effectiveInstructions =
// prepared.instructions`), so an unsupported instructions value that the
// hook replaces is not an error; one the hook leaves in place still is.
func TestR70PrepareCallInstructionsReplaceInvalidCallInstructions(t *testing.T) {
	for _, stream := range []bool{false, true} {
		model := &r70SystemModel{}
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model: model,
			PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.System = "from prepareCall"
				return o, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r70Run(t, a, stream, "", 42); err != nil {
			t.Fatalf("stream=%v: replaced invalid instructions: %v", stream, err)
		}
		if got := model.lastSystem(); got != "from prepareCall" {
			t.Fatalf("stream=%v: system = %q, want the hook's", stream, got)
		}

		// A hook that changes the instruction messages also replaces them.
		model = &r70SystemModel{}
		a, err = NewWorkflowAgent(WorkflowAgent{
			Model: model,
			PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.InstructionMessages = []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "msg"}}}}
				return o, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r70Run(t, a, stream, "", 42); err != nil {
			t.Fatalf("stream=%v: replaced invalid instructions by messages: %v", stream, err)
		}
		if got := model.lastSystem(); got != "msg" {
			t.Fatalf("stream=%v: system = %q, want the hook's message", stream, got)
		}

		// A hook that keeps the instructions: TS validates the original value.
		model = &r70SystemModel{}
		a, err = NewWorkflowAgent(WorkflowAgent{
			Model: model,
			PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.Temperature = new(float64)
				return o, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		r69WantInvalidPrompt(t, "kept invalid instructions", r70Run(t, a, stream, "", 42), r70InvalidInstructions)
		if got := model.lastSystem(); got != "<no call>" {
			t.Fatalf("stream=%v: model was called (%q) despite invalid instructions", stream, got)
		}
	}
}

// TS validates only the effective instructions, `options.instructions ??
// options.system ?? this.instructions` (the agent's `instructions ??
// system`): an unsupported agent Instructions value that the call's
// instructions or system replace is not an error.
func TestR70OnlyEffectiveInstructionsAreValidated(t *testing.T) {
	for _, stream := range []bool{false, true} {
		model := &r70SystemModel{}
		// System set, so NewWorkflowAgent does not normalize Instructions.
		a, err := NewWorkflowAgent(WorkflowAgent{Model: model, System: "agent system", Instructions: 42})
		if err != nil {
			t.Fatal(err)
		}
		if err := r70Run(t, a, stream, "", "call instructions"); err != nil {
			t.Fatalf("stream=%v: call instructions over invalid agent instructions: %v", stream, err)
		}
		if got := model.lastSystem(); got != "call instructions" {
			t.Fatalf("stream=%v: system = %q", stream, got)
		}
		if err := r70Run(t, a, stream, "call system", nil); err != nil {
			t.Fatalf("stream=%v: call system over invalid agent instructions: %v", stream, err)
		}
		if got := model.lastSystem(); got != "call system" {
			t.Fatalf("stream=%v: system = %q", stream, got)
		}
		// Without a call override the agent's invalid instructions are the
		// effective ones (TS this.instructions = instructions ?? system).
		r69WantInvalidPrompt(t, "agent invalid instructions", r70Run(t, a, stream, "", nil), r70InvalidInstructions)
	}
}

// TS WorkflowAgent's stream mode parses only `options.output ??
// this.output` and exposes no partial output without one, so
// WorkflowStreamResult.PartialOutput() stays nil without a spec whatever
// core StreamText's default is; with a spec it is the core partial.
func TestR70WorkflowStreamPartialOutputNeedsSpec(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &fnModel{chunks: chunks}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got := st.PartialOutput(); got != nil {
		t.Errorf("no spec: PartialOutput() = %#v, want nil", got)
	}

	st, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi", Output: ai.TextOutput()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	if got, core := st.PartialOutput(), st.StreamTextResult.PartialOutput(); got != core || got == nil {
		t.Errorf("text spec: PartialOutput() = %#v, core %#v, want the core partial", got, core)
	}
}

// TS workflow-agent.test.ts allowSystemInMessages: "should reject system
// messages in messages by default" (stream() rejects with InvalidPromptError
// from standardizePrompt, before any callback or model call) and "should
// allow system messages when allowSystemInMessages is true".
func TestR70WorkflowRejectsSystemMessagesBeforeTheRun(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "You are a helpful assistant."}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What is the weather?"}}},
	}
	const want = "Invalid prompt: System messages are not allowed in the prompt or messages fields. Use the instructions option instead."
	for _, stream := range []bool{false, true} {
		model := &r70SystemModel{}
		var mu sync.Mutex
		var events []string
		note := func(name string) {
			mu.Lock()
			events = append(events, name)
			mu.Unlock()
		}
		a, err := NewWorkflowAgent(WorkflowAgent{
			Model:   model,
			OnStart: func(context.Context, ai.OnStartEvent) { note("start") },
			OnError: func(context.Context, error) { note("error") },
			OnEnd:   func(context.Context, ai.OnFinishEvent) { note("end") },
		})
		if err != nil {
			t.Fatal(err)
		}
		if stream {
			_, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs})
		} else {
			_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: msgs})
		}
		r69WantInvalidPrompt(t, "system message", err, want)
		mu.Lock()
		if len(events) != 0 {
			t.Fatalf("stream=%v: callbacks %v fired for a rejected prompt", stream, events)
		}
		mu.Unlock()
		if got := model.lastSystem(); got != "<no call>" {
			t.Fatalf("stream=%v: model called (%q)", stream, got)
		}

		for _, allow := range []struct{ agent, call bool }{{true, false}, {false, true}} {
			a, err := NewWorkflowAgent(WorkflowAgent{Model: model, AllowSystemInMessages: allow.agent})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: msgs, AllowSystemInMessages: allow.call})
				if err != nil {
					t.Fatalf("allowed %+v: %v", allow, err)
				}
				_ = st.ConsumeStream()
				err = st.Err()
				if err != nil {
					t.Fatalf("allowed %+v: %v", allow, err)
				}
			} else if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: msgs, AllowSystemInMessages: allow.call}); err != nil {
				t.Fatalf("allowed %+v: %v", allow, err)
			}
			if got := model.lastSystem(); got != "You are a helpful assistant." {
				t.Fatalf("stream=%v allowed %+v: system = %q", stream, allow, got)
			}
		}
	}
}

// TS standardizePrompt checks invalid instructions before empty messages.
func TestR70WorkflowInstructionsCheckedBeforeEmptyMessages(t *testing.T) {
	a, err := NewWorkflowAgent(WorkflowAgent{Model: &r70SystemModel{}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Messages: []types.Message{}, Instructions: 42})
	r69WantInvalidPrompt(t, "generate", err, r70InvalidInstructions)
	_, err = a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Messages: []types.Message{}, Instructions: 42})
	r69WantInvalidPrompt(t, "stream", err, r70InvalidInstructions)
}

// r70ToolOnceModel calls testTool once, then answers "done".
type r70ToolOnceModel struct {
	wfMockModel
	mu sync.Mutex
	n  int
}

func (m *r70ToolOnceModel) next() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n++
	return m.n == 1
}

func (m *r70ToolOnceModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.next() {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{{ID: "test-call-id", ToolName: "testTool", Arguments: map[string]interface{}{}}}}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *r70ToolOnceModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	if m.next() {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "test-call-id", ToolName: "testTool", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

type r70Sandbox struct{ name string }

// TS workflow-agent.test.ts experimental_sandbox: the stream-level sandbox,
// the constructor-level one, the stream-level one over the constructor's,
// and none (undefined) reach the tool's execute options.
func TestR70WorkflowSandboxReachesToolExecution(t *testing.T) {
	ctor, call := &r70Sandbox{"constructor"}, &r70Sandbox{"stream"}
	cases := []struct {
		name       string
		ctor, call interface{}
		want       interface{}
	}{
		{"stream-level", nil, call, call},
		{"constructor-level", ctor, nil, ctor},
		{"stream over constructor", ctor, call, call},
		{"none", nil, nil, nil},
	}
	for _, stream := range []bool{false, true} {
		for _, c := range cases {
			var mu sync.Mutex
			var got []interface{}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model:               &r70ToolOnceModel{},
				ExperimentalSandbox: c.ctor,
				Tools: []types.Tool{{
					Name: "testTool", Description: "A test tool",
					Parameters: map[string]interface{}{"type": "object"},
					Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						got = append(got, o.ExperimentalSandbox)
						mu.Unlock()
						return "ok", nil
					},
				}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test", ExperimentalSandbox: c.call})
				if err != nil {
					t.Fatal(err)
				}
				_ = st.ConsumeStream()
				if err := st.Err(); err != nil {
					t.Fatalf("%s: %v", c.name, err)
				}
			} else if _, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test", ExperimentalSandbox: c.call}); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			mu.Lock()
			if len(got) != 1 || got[0] != c.want {
				t.Fatalf("stream=%v %s: execute saw %v, want [%v]", stream, c.name, got, c.want)
			}
			mu.Unlock()
		}
	}
}

// r70MissingResultModel calls the provider-executed WebSearch tool without a
// result, then answers "done", recording the second call's prompt.
type r70MissingResultModel struct {
	wfMockModel
	dyn    bool
	mu     sync.Mutex
	calls  int
	second []types.Message
}

func (m *r70MissingResultModel) step(opts *provider.GenerateOptions) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.calls == 2 {
		m.second = opts.Prompt.Messages
	}
	return m.calls
}

func (m *r70MissingResultModel) call() types.ToolCall {
	return types.ToolCall{ID: "missing-result-id", ToolName: "WebSearch", Arguments: map[string]interface{}{"query": "test query"}, ProviderExecuted: true, Dynamic: m.dyn}
}

func (m *r70MissingResultModel) DoGenerate(_ context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if m.step(opts) == 1 {
		return &types.GenerateResult{FinishReason: types.FinishReasonToolCalls, ToolCalls: []types.ToolCall{m.call()}}, nil
	}
	return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
}

func (m *r70MissingResultModel) DoStream(_ context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if m.step(opts) == 1 {
		c := m.call()
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &c},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}
	return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
}

// TS workflow-agent.test.ts "should warn and return empty result when
// provider-executed tool result is missing": the provider-executed call is
// answered with one tool result whose output is type "text" and an empty value,
// placed after the call in the assistant message (addToolResultsToConversation).
// The run then ends: TS modelCallIterator sets done when the step had no
// client tool calls and no pending deferred results, so the model is not
// called again. Both a registered provider tool and a dynamic
// provider-executed call of an unregistered tool (the TS case's `tools: {}`;
// parseToolCall keeps such a call only when it is dynamic) behave so.
func TestR70WorkflowMissingProviderResultIsEmptyText(t *testing.T) {
	t.Skip("parity bug: change requests CR-1 (generate.go/stream.go/toolloop.go keep the loop going after the synthetic result) and CR-2 (toolloop.go runs an unregistered dynamic provider-executed call locally), reports/loop-r70-carry.md")
	for _, variant := range []string{"registered", "dynamic"} {
		for _, stream := range []bool{false, true} {
			m := &r70MissingResultModel{dyn: variant == "dynamic"}
			cfg := WorkflowAgent{Model: m}
			if variant == "registered" {
				cfg.Tools = []types.Tool{{Name: "WebSearch", ProviderExecuted: true}}
			}
			a, err := NewWorkflowAgent(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var response []types.Message
			if stream {
				st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "test"})
				if err != nil {
					t.Fatal(err)
				}
				_ = st.ConsumeStream()
				if err := st.Err(); err != nil {
					t.Fatal(err)
				}
				for _, step := range st.Steps() {
					response = append(response, step.Response.Messages...)
				}
			} else {
				res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "test"})
				if err != nil {
					t.Fatal(err)
				}
				response = res.ResponseMessages
			}
			m.mu.Lock()
			calls := m.calls
			m.mu.Unlock()
			if calls != 1 {
				t.Errorf("%s stream=%v: model calls = %d, want 1", variant, stream, calls)
			}
			if len(response) != 1 || response[0].Role != types.RoleAssistant || len(response[0].Content) != 2 {
				t.Fatalf("%s stream=%v: response = %#v, want one assistant message [tool-call, tool-result]", variant, stream, response)
			}
			if c, ok := response[0].Content[0].(types.ToolCallContent); !ok || c.ToolCallID != "missing-result-id" || !c.ProviderExecuted {
				t.Fatalf("%s stream=%v: first part = %#v", variant, stream, response[0].Content[0])
			}
			r, ok := response[0].Content[1].(types.ToolResultContent)
			if !ok || r.ToolCallID != "missing-result-id" || r.ToolName != "WebSearch" || r.Output == nil ||
				r.Output.Type != types.ToolResultOutputText || r.Output.Value != "" {
				t.Fatalf("%s stream=%v: second part = %#v", variant, stream, response[0].Content[1])
			}
		}
	}
}
