package workflow

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// port2carryCalls records the prompt of every model call.
type port2carryCalls struct {
	mu    sync.Mutex
	calls []*provider.GenerateOptions
}

func (c *port2carryCalls) add(o *provider.GenerateOptions) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, o)
	return len(c.calls)
}

// port2carryWeatherModel is the fixture of workflow-agent-prepare-step.test.ts:
// a weather tool call, then a text answer.
func port2carryWeatherModel(calls *port2carryCalls) *testutil.MockLanguageModel {
	toolCall := types.ToolCall{ID: "weather-call", ToolName: "weather", Arguments: map[string]interface{}{"city": "London"}}
	return &testutil.MockLanguageModel{
		DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
			if calls.add(o) == 1 {
				return &types.GenerateResult{Text: "Checking the weather.", ToolCalls: []types.ToolCall{toolCall}, FinishReason: types.FinishReasonToolCalls}, nil
			}
			return &types.GenerateResult{Text: "It is 18C.", FinishReason: types.FinishReasonStop}, nil
		},
		DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
			if calls.add(o) == 1 {
				return testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: "Checking the weather."},
					{Type: provider.ChunkTypeToolCall, ToolCall: &toolCall},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
				}), nil
			}
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "It is 18C."},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func port2carryWeatherTools() []types.Tool {
	return []types.Tool{{
		Name: "weather", Description: "weather",
		Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"city": map[string]interface{}{"type": "string"}}, "required": []interface{}{"city"}},
		Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return map[string]interface{}{"temperature": 18}, nil
		},
	}}
}

// port2carrySystem is TS systemMessages(call): the instructions the model
// received, as the string system prompt followed by the leading system messages.
type port2carrySystem struct {
	Content         string
	ProviderOptions map[string]interface{}
}

func port2carrySystemMessages(o *provider.GenerateOptions) []port2carrySystem {
	var out []port2carrySystem
	if o.Prompt.System != "" {
		out = append(out, port2carrySystem{Content: o.Prompt.System})
	}
	for _, m := range o.Prompt.Messages {
		if m.Role != types.RoleSystem {
			break
		}
		var text strings.Builder
		for _, p := range m.Content {
			if tc, ok := p.(types.TextContent); ok {
				text.WriteString(tc.Text)
			}
		}
		out = append(out, port2carrySystem{Content: text.String(), ProviderOptions: m.ProviderOptions})
	}
	return out
}

func port2carrySysMsg(content string, opts map[string]interface{}) types.Message {
	return types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: content}}, ProviderOptions: opts}
}

// port2carryRunAgent runs the weather conversation through generate and
// stream with the hook returning override at step 0 only.
func port2carryRunAgent(t *testing.T, cfg WorkflowAgent, step0 func(*LanguageModelCallOptions)) (generate, stream []*provider.GenerateOptions) {
	t.Helper()
	for _, streaming := range []bool{false, true} {
		calls := &port2carryCalls{}
		c := cfg
		c.Model = port2carryWeatherModel(calls)
		c.Tools = port2carryWeatherTools()
		c.PrepareStep = func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
			if o.StepNumber == 0 && step0 != nil {
				step0(&o)
			}
			return o, nil
		}
		w, err := NewWorkflowAgent(c)
		if err != nil {
			t.Fatal(err)
		}
		if streaming {
			res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "What is the weather in London?"})
			if err != nil {
				t.Fatal(err)
			}
			if err := res.ConsumeStream(); err != nil {
				t.Fatal(err)
			}
		} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "What is the weather in London?"}); err != nil {
			t.Fatal(err)
		}
		if len(calls.calls) != 2 {
			t.Fatalf("model calls = %d, want 2", len(calls.calls))
		}
		if streaming {
			stream = calls.calls
		} else {
			generate = calls.calls
		}
	}
	return generate, stream
}

// TS workflow-agent-prepare-step.test.ts, describe 'instructions': one
// prepareStep result at step 0 sets the instructions of both steps, in
// generate and in stream.
func TestPort2carryPrepareStepInstructions(t *testing.T) {
	stepOpts := map[string]interface{}{"test": map[string]interface{}{"value": "step"}}
	step1Opts := map[string]interface{}{"test": map[string]interface{}{"value": "step 1"}}
	initialMsgs := []types.Message{
		port2carrySysMsg("Initial instructions 1", map[string]interface{}{"test": map[string]interface{}{"value": "initial 1"}}),
		port2carrySysMsg("Initial instructions 2", nil),
	}
	for _, tc := range []struct {
		name         string
		instructions interface{}
		step0        func(*LanguageModelCallOptions)
		want         []port2carrySystem
	}{
		{
			name:         "applies string instructions returned at step 0",
			instructions: "Initial instructions",
			step0:        func(o *LanguageModelCallOptions) { o.System = "Step instructions" },
			want:         []port2carrySystem{{Content: "Step instructions"}},
		},
		{
			name:         "applies system message instructions with providerOptions returned at step 0",
			instructions: "Initial instructions",
			step0: func(o *LanguageModelCallOptions) {
				o.InstructionMessages = []types.Message{port2carrySysMsg("Step instructions", stepOpts)}
			},
			want: []port2carrySystem{{Content: "Step instructions", ProviderOptions: stepOpts}},
		},
		{
			name:         "applies array instructions returned at step 0",
			instructions: "Initial instructions",
			step0: func(o *LanguageModelCallOptions) {
				o.InstructionMessages = []types.Message{port2carrySysMsg("Step instructions 1", step1Opts), port2carrySysMsg("Step instructions 2", nil)}
			},
			want: []port2carrySystem{{Content: "Step instructions 1", ProviderOptions: step1Opts}, {Content: "Step instructions 2"}},
		},
		{
			name:         "replaces initial instructions that are an array of messages",
			instructions: initialMsgs,
			step0: func(o *LanguageModelCallOptions) {
				o.InstructionMessages = []types.Message{port2carrySysMsg("Step instructions 1", nil), port2carrySysMsg("Step instructions 2", nil)}
			},
			want: []port2carrySystem{{Content: "Step instructions 1"}, {Content: "Step instructions 2"}},
		},
		{
			// TS `system` is the older name of `instructions`; Go has one
			// System field for both.
			name:         "applies system returned at step 0",
			instructions: "Initial instructions",
			step0:        func(o *LanguageModelCallOptions) { o.System = "Step instructions" },
			want:         []port2carrySystem{{Content: "Step instructions"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generate, stream := port2carryRunAgent(t, WorkflowAgent{Instructions: tc.instructions}, tc.step0)
			for name, calls := range map[string][]*provider.GenerateOptions{"generate": generate, "stream": stream} {
				for i, call := range calls {
					if got := port2carrySystemMessages(call); !reflect.DeepEqual(got, tc.want) {
						t.Errorf("%s call %d system = %+v, want %+v", name, i, got, tc.want)
					}
				}
			}
		})
	}
}

// TS 'passes the instructions in effect to prepareStep like ToolLoopAgent':
// step 0 sees the initial instructions, step 1 the instructions step 0 set.
func TestPort2carryPrepareStepSeesInstructionsInEffect(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		var mu sync.Mutex
		var seen []string
		calls := &port2carryCalls{}
		w, err := NewWorkflowAgent(WorkflowAgent{
			Model: port2carryWeatherModel(calls), Tools: port2carryWeatherTools(), Instructions: "Initial instructions",
			PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				mu.Lock()
				seen = append(seen, o.System)
				mu.Unlock()
				if o.StepNumber == 0 {
					o.System = "Step instructions"
				}
				return o, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := runWorkflow(t, w, streaming, WorkflowGenerateOptions{Prompt: "go"}, WorkflowStreamOptions{Prompt: "go"}); err != nil {
			t.Fatal(err)
		}
		if want := []string{"Initial instructions", "Step instructions"}; !reflect.DeepEqual(seen, want) {
			t.Errorf("streaming=%v instructions seen by prepareStep = %v, want %v", streaming, seen, want)
		}
	}
}

// TS 'merges providerOptions returned at step 0, like ToolLoopAgent': the
// step's options merge into the agent's for that step only.
func TestPort2carryPrepareStepMergesProviderOptions(t *testing.T) {
	agentOpts := map[string]interface{}{
		"mock":      map[string]interface{}{"fromAgent": "agent", "shared": "agent"},
		"agentOnly": map[string]interface{}{"value": "agent"},
	}
	want := []map[string]interface{}{
		{
			"mock":      map[string]interface{}{"fromAgent": "agent", "fromStep": "step", "shared": "step"},
			"agentOnly": map[string]interface{}{"value": "agent"},
		},
		agentOpts,
	}
	generate, stream := port2carryRunAgent(t, WorkflowAgent{ProviderOptions: agentOpts}, func(o *LanguageModelCallOptions) {
		o.ProviderOptions = map[string]interface{}{"mock": map[string]interface{}{"fromStep": "step", "shared": "step"}}
	})
	for name, calls := range map[string][]*provider.GenerateOptions{"generate": generate, "stream": stream} {
		for i, call := range calls {
			if !reflect.DeepEqual(call.ProviderOptions, want[i]) {
				t.Errorf("%s call %d providerOptions = %+v, want %+v", name, i, call.ProviderOptions, want[i])
			}
		}
	}
}

// TS 'keeps the agent user-agent when prepareStep returns a user-agent': the
// step's user-agent is kept and the agent suffix is added; the agent's own
// headers stay. Same case as TestWorkflowPrepareStepHeadersMergeOverCallHeaders
// "step 0 user-agent"; this one reads the real headers.
func TestPort2carryPrepareStepUserAgentKeepsAgentSuffix(t *testing.T) {
	generate, stream := port2carryRunAgent(t, WorkflowAgent{Headers: map[string]string{"x-agent": "agent"}}, func(o *LanguageModelCallOptions) {
		o.Headers = map[string]string{"user-agent": "custom"}
	})
	for name, calls := range map[string][]*provider.GenerateOptions{"generate": generate, "stream": stream} {
		h := calls[0].Headers
		ua := h["user-agent"]
		if !strings.Contains(ua, "custom") || !strings.Contains(ua, "ai-sdk-agent/workflow") || h["x-agent"] != "agent" {
			t.Errorf("%s step 0 headers = %v, want custom user-agent with the workflow suffix and x-agent", name, h)
		}
	}
}
