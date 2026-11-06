package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS's model-call iterator keeps one conversationPrompt: a prepareStep
// messages or system override replaces it for the rest of the run, and onEnd
// `messages` is that final conversation. Go rebuilt onEnd messages from the
// pre-loop prompt, so a PrepareStep override was missing from them.
func TestWorkflowOnEndMessagesFollowPrepareStepOverride(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var got []ai.OnFinishEvent
			m := &scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:        m,
				Instructions: "Sys.",
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					if o.StepNumber == 0 {
						o.System = "Step sys."
						o.Messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "replaced"}}}}
					}
					return o, nil
				},
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); got = append(got, e); mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "orig"})
				if err != nil {
					t.Fatal(err)
				}
				_ = res.ConsumeStream()
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "orig"}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(got) != 1 {
				t.Fatalf("onEnd calls = %d, want 1", len(got))
			}
			var roles []string
			for _, msg := range got[0].Messages {
				r := string(msg.Role)
				for _, p := range msg.Content {
					if tc, ok := p.(types.TextContent); ok {
						r += "(" + tc.Text + ")"
					}
				}
				roles = append(roles, r)
			}
			if have, want := strings.Join(roles, " "), "system(Step sys.) user(replaced) assistant tool assistant(done)"; have != want {
				t.Fatalf("onEnd messages = %q, want %q", have, want)
			}
		})
	}
}

// r55OptsModel records each call's model tag, temperature and system.
type r55OptsModel struct {
	scriptedModel
	tag string
	log *[]string
	mu  *sync.Mutex
}

func (m *r55OptsModel) note(o *provider.GenerateOptions) {
	temp := "nil"
	if o.Temperature != nil {
		temp = fmt.Sprint(*o.Temperature)
	}
	m.mu.Lock()
	*m.log = append(*m.log, fmt.Sprintf("%s temp=%s sys=%s", m.tag, temp, o.Prompt.System))
	m.mu.Unlock()
}

func (m *r55OptsModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.note(o)
	return m.scriptedModel.DoGenerate(ctx, o)
}

func (m *r55OptsModel) DoStream(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	m.note(o)
	return m.scriptedModel.DoStream(ctx, o)
}

// TS's iterator keeps prepareStep's model, instructions and generation
// settings for the following steps (currentModel, conversationPrompt[0],
// currentGenerationSettings). Go reset them each step (generate) or kept only
// the model and instructions (stream).
func TestWorkflowPrepareStepOverridesPersistToLaterSteps(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "generate"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var log []string
			calls := []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}
			a := &r55OptsModel{scriptedModel: scriptedModel{calls: calls}, tag: "A", log: &log, mu: &mu}
			b := &r55OptsModel{scriptedModel: scriptedModel{calls: calls}, tag: "B", log: &log, mu: &mu}
			var hookTemps []string
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model: a, Instructions: "Sys.",
				Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "r", nil
				}}},
				PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					temp := "nil"
					if o.Temperature != nil {
						temp = fmt.Sprint(*o.Temperature)
					}
					mu.Lock()
					hookTemps = append(hookTemps, temp)
					mu.Unlock()
					if o.StepNumber == 0 {
						tt := 0.5
						o.Temperature, o.Model, o.System = &tt, b, "Step sys."
					}
					return o, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if stream {
				res, err := w.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "orig"})
				if err != nil {
					t.Fatal(err)
				}
				_ = res.ConsumeStream()
			} else if _, err := w.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "orig"}); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if have, want := strings.Join(log, " | "), "B temp=0.5 sys=Step sys. | B temp=0.5 sys=Step sys."; have != want {
				t.Fatalf("model calls = %q, want %q", have, want)
			}
			if have, want := strings.Join(hookTemps, ","), "nil,0.5"; have != want {
				t.Fatalf("hook temperatures = %q, want %q", have, want)
			}
		})
	}
}
