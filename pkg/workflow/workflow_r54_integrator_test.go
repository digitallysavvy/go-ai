package workflow

import (
	"context"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r54ModelInput flattens what the model received on its first call into the
// message list TS onEnd `messages` starts with: the system prompt as a system
// message, then the prompt messages (or the simple text as a user message).
func r54ModelInput(t *testing.T, m *wfMockModel) []types.Message {
	t.Helper()
	if len(m.opts) == 0 {
		t.Fatal("model was not called")
	}
	p := m.opts[0].Prompt
	var out []types.Message
	if p.System != "" {
		out = append(out, types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: p.System}}})
	}
	for _, msg := range p.Messages {
		if msg.Role == types.RoleSystem && len(out) == 1 && p.System != "" {
			continue
		}
		out = append(out, msg)
	}
	if p.Text != "" {
		out = append(out, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: p.Text}}})
	}
	return out
}

func r54Texts(msgs []types.Message) []string {
	var out []string
	for _, m := range msgs {
		s := string(m.Role) + ":"
		for _, p := range m.Content {
			if tc, ok := p.(types.TextContent); ok {
				s += tc.Text
			}
		}
		out = append(out, s)
	}
	return out
}

// The onEnd conversation starts with exactly what the loop ran on: the
// agent's default prompt, the call prompt, PrepareCall's instructions and
// messages, and call-level instructions all show up as the model saw them.
func TestR54IntegratorOnEndBaseMatchesModelInput(t *testing.T) {
	cases := []struct {
		name  string
		agent WorkflowAgent
		gen   WorkflowGenerateOptions
		str   WorkflowStreamOptions
	}{
		{name: "call instructions", agent: WorkflowAgent{Instructions: "Sys."}, gen: WorkflowGenerateOptions{Prompt: "call prompt", Instructions: "Call sys."}, str: WorkflowStreamOptions{Prompt: "call prompt", Instructions: "Call sys."}},
		{name: "instruction messages", agent: WorkflowAgent{Instructions: []types.Message{
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Sys A."}}},
			{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Sys B."}}},
		}}, gen: WorkflowGenerateOptions{Prompt: "call prompt"}, str: WorkflowStreamOptions{Prompt: "call prompt"}},
		{name: "call prompt", agent: WorkflowAgent{Instructions: "Sys."}, gen: WorkflowGenerateOptions{Prompt: "call prompt"}, str: WorkflowStreamOptions{Prompt: "call prompt"}},
		{name: "prepareCall system and messages", agent: WorkflowAgent{
			Instructions: "Sys.",
			PrepareCall: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
				o.System = "Prepared."
				o.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "prepared message"}}}}
				return o, nil
			},
		}, gen: WorkflowGenerateOptions{Prompt: "call prompt"}, str: WorkflowStreamOptions{Prompt: "call prompt"}},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			name := tc.name + "/generate"
			if stream {
				name = tc.name + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				var mu sync.Mutex
				var got []ai.OnFinishEvent
				model := &wfMockModel{}
				cfg := tc.agent
				cfg.Model = model
				cfg.Tools = []types.Tool{{Name: "t1", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}}
				cfg.OnEnd = func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); got = append(got, e); mu.Unlock() }
				w, err := NewWorkflowAgent(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if stream {
					res, err := w.StreamWithOptions(context.Background(), tc.str)
					if err != nil {
						t.Fatal(err)
					}
					_ = res.ConsumeStream()
				} else if _, err := w.GenerateWithOptions(context.Background(), tc.gen); err != nil {
					t.Fatal(err)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(got) != 1 {
					t.Fatalf("onEnd calls = %d, want 1", len(got))
				}
				want := r54Texts(r54ModelInput(t, model))
				have := r54Texts(got[0].Messages)
				if len(have) < len(want) {
					t.Fatalf("onEnd messages %q, want them to start with the model input %q", have, want)
				}
				for i := range want {
					if have[i] != want[i] {
						t.Fatalf("onEnd messages %q, want them to start with the model input %q", have, want)
					}
				}
			})
		}
	}
}

// Combining tool messages must not write into a caller's backing array, even
// when the first message's content slice has spare capacity.
func TestR54IntegratorConversationBaseDoesNotWriteCallerArray(t *testing.T) {
	content := make([]types.ContentPart, 1, 4)
	content[0] = types.ToolResultContent{ToolCallID: "a"}
	a := types.Message{Role: types.RoleTool, Content: content}
	b := types.Message{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "b"}}}
	c := types.Message{Role: types.RoleTool, Content: content[:1]}
	_ = workflowConversationBase("", nil, []types.Message{a, b}, "")
	out := workflowConversationBase("", nil, []types.Message{c, {Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}, "")
	if len(out) != 2 || len(out[0].Content) != 1 {
		t.Fatalf("out = %+v", out)
	}
	if spare := content[:2][1]; spare != nil {
		t.Fatalf("the caller's backing array was written: %+v", spare)
	}
}
