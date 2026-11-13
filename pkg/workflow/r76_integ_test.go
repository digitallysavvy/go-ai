package workflow

// Round 76 integrator: carry's change-request tests (applied after the
// stream/gen fields merged) and the coordinator's defaults ruling for the
// ResolveSerializableTools ValidateInput hook.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS WorkflowAgent executeTool / createInvalidToolResult / addToolResultsToStep:
// a client tool's result carries no provider metadata (TS core copies
// toolCall.providerMetadata; WorkflowAgent does not). Both modes: the next
// prompt's tool-result part has no providerOptions, the step's tool-result
// content and ToolResults have no providerMetadata. The tool call keeps its
// metadata.
func TestR76Integ_ClientToolResultsWithoutCallMetadata(t *testing.T) {
	meta := map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "sig"}}
	tools := []types.Tool{{Name: "clientTool", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}}
	check := func(t *testing.T, mode string, prompt []types.Message, step types.StepResult) {
		t.Helper()
		tool := prompt[len(prompt)-1]
		if tool.Role != types.RoleTool || len(tool.Content) != 1 {
			t.Fatalf("%s: last prompt message = %+v", mode, tool)
		}
		if tr := tool.Content[0].(types.ToolResultContent); tr.ProviderOptions != nil || len(tr.ProviderMetadata) > 0 {
			t.Errorf("%s: prompt tool result providerOptions = %v, providerMetadata = %s; want none (TS executeTool)", mode, tr.ProviderOptions, tr.ProviderMetadata)
		}
		for _, p := range step.Content {
			switch c := p.(type) {
			case types.ToolResultContent:
				if len(c.ProviderMetadata) > 0 || c.ProviderOptions != nil {
					t.Errorf("%s: step tool-result metadata = %s / %v, want none", mode, c.ProviderMetadata, c.ProviderOptions)
				}
			case types.ToolCallContent:
				if len(c.ProviderMetadata) == 0 && c.ProviderOptions == nil {
					t.Errorf("%s: step tool-call lost its metadata", mode)
				}
			}
		}
		if len(step.ToolResults) != 1 || step.ToolResults[0].ProviderMetadata != nil {
			t.Errorf("%s: step ToolResults = %+v, want one without ProviderMetadata", mode, step.ToolResults)
		}
	}

	model, calls := r75carryScripted([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "clientTool", Arguments: map[string]interface{}{}, ProviderMetadata: meta}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
	agent, err := NewWorkflowAgent(WorkflowAgent{Model: model, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	steps := r75carryRunStream(t, agent)
	if len(*calls) != 2 || len(steps) != 2 {
		t.Fatalf("stream: calls = %d, steps = %d, want 2, 2", len(*calls), len(steps))
	}
	check(t, "stream", (*calls)[1].Prompt.Messages, steps[0])

	var gcalls []*provider.GenerateOptions
	gm := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true, DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		gcalls = append(gcalls, o)
		if len(gcalls) == 1 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "clientTool", Arguments: map[string]interface{}{}, ProviderMetadata: meta}}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}}
	gagent, err := NewWorkflowAgent(WorkflowAgent{Model: gm, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	res, err := gagent.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gcalls) != 2 {
		t.Fatalf("generate: calls = %d, want 2", len(gcalls))
	}
	check(t, "generate", gcalls[1].Prompt.Messages, res.Steps[0])
}

// r76integEOFStream yields its chunks and records "model-step-finished" when
// it reports io.EOF (the model stream ended).
type r76integEOFStream struct {
	chunks []provider.StreamChunk
	i      int
	onEnd  func()
	once   sync.Once
}

func (s *r76integEOFStream) Next() (*provider.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		s.once.Do(s.onEnd)
		return nil, io.EOF
	}
	c := &s.chunks[s.i]
	s.i++
	return c, nil
}
func (s *r76integEOFStream) Err() error   { return nil }
func (s *r76integEOFStream) Close() error { return nil }

// TS model-call-iterator.test.ts "replays tool input callbacks in order
// before advancing the agent step": the stream-mode model step records the
// tool input lifecycle and the iterator replays it after the step returned,
// with the tool's context (invokeToolInputLifecycleCallbacks).
func TestR76Integ_StreamReplaysToolInputCallbacksAfterModelStep(t *testing.T) {
	var mu sync.Mutex
	var log []string
	add := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	n := 0
	m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
	m.DoStreamFunc = func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		n++
		if n == 1 {
			return &r76integEOFStream{onEnd: func() { add("model-step-finished") }, chunks: []provider.StreamChunk{
				{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "search"}},
				{Type: provider.ChunkTypeToolInputDelta, ID: "call-1", Text: `{"query":"docs"}`},
				{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "search", Arguments: map[string]interface{}{"query": "docs"}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
			}}, nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "x"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
	}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, ToolsContext: map[string]interface{}{"search": map[string]interface{}{"requestId": "request-1"}}, Tools: []types.Tool{{
		Name: "search", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string"}}},
		OnInputStart: func(_ context.Context, o types.OnInputStartOptions) error {
			add(fmt.Sprintf("start:%v", o.Context.(map[string]interface{})["requestId"]))
			return nil
		},
		OnInputDelta: func(_ context.Context, o types.OnInputDeltaOptions) error {
			add("delta:" + o.InputTextDelta)
			return nil
		},
		OnInputAvailable: func(_ context.Context, o types.OnInputAvailableOptions) error {
			b, _ := json.Marshal(o.Input)
			add("available:" + string(b))
			return nil
		},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "ok", nil
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "search"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	want := []string{"model-step-finished", "start:request-1", `delta:{"query":"docs"}`, `available:{"query":"docs"}`}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(log, want) {
		t.Errorf("callback order = %v, want %v", log, want)
	}
}

// r76integRoles lists each message's role, and for an assistant message the
// types of its parts.
func r76integRoles(msgs []types.Message) string {
	var out []string
	for _, m := range msgs {
		s := string(m.Role)
		if m.Role == types.RoleAssistant {
			var parts []string
			for _, p := range m.Content {
				parts = append(parts, p.ContentType())
			}
			s += "[" + strings.Join(parts, ",") + "]"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// TS workflow-agent.test.ts "tool execution with messages": a tool's execute
// gets the iterator's conversation prompt (system instructions first, then
// the step's assistant message), and onToolExecutionStart gets it without the
// trailing assistant message (getToolCallbackMessages). The second round sees
// the first round's tool message. Both modes.
func TestR76Integ_ToolMessagesAreConversationPrompt(t *testing.T) {
	for _, mode := range []string{"stream", "generate"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var execs, starts []string
			call := 0
			m := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
			m.DoStreamFunc = func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
				call++
				if call <= 2 {
					id := fmt.Sprintf("search-%d", call)
					return testutil.NewMockTextStream([]provider.StreamChunk{
						{Type: provider.ChunkTypeText, Text: "let me search"},
						{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "searchTool", Arguments: map[string]interface{}{"query": "cats"}}},
						{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
					}), nil
				}
				return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: "done"}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
			}
			m.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
				call++
				if call <= 2 {
					return &types.GenerateResult{Text: "let me search", ToolCalls: []types.ToolCall{{ID: fmt.Sprintf("search-%d", call), ToolName: "searchTool", Arguments: map[string]interface{}{"query": "cats"}}}, FinishReason: types.FinishReasonToolCalls}, nil
				}
				return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
			}
			a, err := NewWorkflowAgent(WorkflowAgent{
				Model: m, Instructions: "sys",
				Tools: []types.Tool{{Name: "searchTool", Execute: func(_ context.Context, _ map[string]interface{}, o types.ToolExecutionOptions) (interface{}, error) {
					mu.Lock()
					execs = append(execs, r76integRoles(o.Messages))
					mu.Unlock()
					return map[string]interface{}{"found": true}, nil
				}}},
				OnToolExecutionStart: func(_ context.Context, e ai.OnToolCallStartEvent) {
					mu.Lock()
					starts = append(starts, r76integRoles(e.Messages))
					mu.Unlock()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			r76carryRun(t, a, mode)
			wantExec := []string{"system user assistant[text,tool-call]", "system user assistant[text,tool-call] tool assistant[text,tool-call]"}
			wantStart := []string{"system user", "system user assistant[text,tool-call] tool"}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(execs, wantExec) {
				t.Errorf("execute messages = %q, want %q", execs, wantExec)
			}
			if !reflect.DeepEqual(starts, wantStart) {
				t.Errorf("start messages = %q, want %q", starts, wantStart)
			}
		})
	}
}

// Owner ruling 2026-11-01 (decision 1, option c): a tool rebuilt by
// ResolveSerializableTools validates its input with the Ajv-worded validator
// (TS resolveSerializableTools createValidatedInputSchema + formatAjvError),
// so an invalid input surfaces those issues in the tool error, through
// GenerateText and StreamText.
func TestR76Integ_ResolvedToolInputAjvWording(t *testing.T) {
	tools := ResolveSerializableTools(map[string]SerializableToolDef{"weather": {InputSchema: map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"city": map[string]interface{}{"type": "string"}},
		"required":             []interface{}{"city"},
		"additionalProperties": false,
	}}})
	executed := false
	tools[0].Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		executed = true
		return "rain", nil
	}
	const issues = `data is missing required property "city", data has unexpected property "town"`
	call := types.ToolCall{ID: "call-1", ToolName: "weather", Arguments: map[string]interface{}{"town": "London"}}
	check := func(mode string, steps []types.StepResult) {
		t.Helper()
		if len(steps) == 0 {
			t.Fatalf("%s: no steps", mode)
		}
		var te *types.ToolErrorContent
		for _, p := range steps[0].Content {
			if c, ok := p.(types.ToolErrorContent); ok {
				te = &c
			}
		}
		if te == nil || !strings.Contains(te.ErrorText, issues) || !strings.Contains(te.ErrorText, "Invalid input for tool weather") {
			t.Errorf("%s: tool error = %+v, want the Ajv issues %q", mode, te, issues)
		}
	}
	gm := &testutil.MockLanguageModel{ProviderName: "mock", ModelName: "m", ToolSupport: true}
	n := 0
	gm.DoGenerateFunc = func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		n++
		if n == 1 {
			return &types.GenerateResult{ToolCalls: []types.ToolCall{call}, FinishReason: types.FinishReasonToolCalls}, nil
		}
		return &types.GenerateResult{Text: "done", FinishReason: types.FinishReasonStop}, nil
	}
	gres, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: gm, Prompt: "weather?", Tools: tools, StopWhen: []ai.StopCondition{ai.IsStepCount(2)}})
	if err != nil {
		t.Fatal(err)
	}
	check("generate", gres.Steps)
	sm, _ := r75carryScripted([]provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &call},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
	})
	sres, err := ai.StreamText(context.Background(), ai.StreamTextOptions{Model: sm, Prompt: "weather?", Tools: tools, StopWhen: []ai.StopCondition{ai.IsStepCount(2)}})
	if err != nil {
		t.Fatal(err)
	}
	check("stream", sres.Steps())
	if executed {
		t.Error("the tool ran with invalid input")
	}
}

// Coordinator ruling (r76): the ValidateInput hook ResolveSerializableTools
// sets matches TS Ajv (no useDefaults): a missing required property with a
// schema default fails, and a valid input comes back unchanged (an optional
// property's default is not filled). The exported
// ValidateSerializableToolInput keeps its v0.5.2 behavior: it fills the
// defaults before validating and returns the defaulted value.
func TestR76Integ_ResolvedToolValidateInputWithoutDefaults(t *testing.T) {
	def := SerializableToolDef{InputSchema: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"city":  map[string]interface{}{"type": "string", "default": "Paris"},
			"units": map[string]interface{}{"type": "string", "default": "metric"},
		},
		"required": []interface{}{"city"},
	}}
	tools := ResolveSerializableTools(map[string]SerializableToolDef{"weather": def})
	if len(tools) != 1 || tools[0].ValidateInput == nil {
		t.Fatalf("tools = %+v, want one with ValidateInput", tools)
	}
	validate := tools[0].ValidateInput

	if _, err := validate(map[string]interface{}{}); err == nil || err.Error() != `data is missing required property "city"` {
		t.Errorf("hook err = %v, want the bare Ajv issue (no defaults filled)", err)
	}
	in := map[string]interface{}{"city": "London"}
	out, err := validate(in)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, map[string]interface{}{"city": "London"}) {
		t.Errorf("hook value = %v, want the input unchanged", out)
	}
	if len(in) != 1 {
		t.Errorf("hook changed its input: %v", in)
	}

	got, err := ValidateSerializableToolInput(def, map[string]interface{}{})
	if err != nil {
		t.Fatalf("ValidateSerializableToolInput err = %v, want the defaults to satisfy required", err)
	}
	if !reflect.DeepEqual(got, map[string]interface{}{"city": "Paris", "units": "metric"}) {
		t.Errorf("ValidateSerializableToolInput = %v, want the defaulted value", got)
	}
	if _, err := ValidateSerializableToolInput(def, map[string]interface{}{"city": 1}); err == nil || !strings.HasPrefix(err.Error(), "workflow: invalid input for tool: ") {
		t.Errorf("ValidateSerializableToolInput err = %v, want the v0.5.2 wrapping", err)
	}
}

// TS resolveSerializableTools gives provider tools and tools with their own
// validator jsonSchema() without a validate function: their input passes
// unvalidated (a hook that returns it as is).
func TestR76Integ_ResolvedProviderAndOwnValidatorToolsPassThrough(t *testing.T) {
	strict := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}}, "required": []interface{}{"q"}, "additionalProperties": false}
	tools := ResolveSerializableTools(map[string]SerializableToolDef{
		"own":  {InputSchema: strict, HasOwnValidator: true},
		"prov": {Type: "provider", ID: "openai.web_search", InputSchema: strict},
	})
	for _, tool := range tools {
		if tool.ValidateInput == nil {
			t.Fatalf("%s: no ValidateInput", tool.Name)
		}
		in := map[string]interface{}{"x": 1}
		out, err := tool.ValidateInput(in)
		if err != nil || !reflect.DeepEqual(out, in) {
			t.Errorf("%s: ValidateInput = %v, %v; want the input unchanged", tool.Name, out, err)
		}
	}
}
