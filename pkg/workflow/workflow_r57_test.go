package workflow

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r57UAModel records whether each model call's user-agent carries the
// WorkflowAgent suffix, and its x-h header.
type r57UAModel struct {
	scriptedModel
	mu    sync.Mutex
	calls []string
}

func (m *r57UAModel) note(o *provider.GenerateOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tag := "-"
	if strings.Contains(o.Headers["user-agent"], "ai-sdk-agent/workflow") {
		tag = "ua"
	}
	m.calls = append(m.calls, tag+"/"+o.Headers["x-h"])
}

func (m *r57UAModel) DoGenerate(ctx context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.note(o)
	return m.scriptedModel.DoGenerate(ctx, o)
}

func (m *r57UAModel) DoStream(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
	m.note(o)
	return m.scriptedModel.DoStream(ctx, o)
}

// TS appends the ai-sdk-agent/workflow user-agent suffix to the call's
// generation settings before the loop; a prepareStep that returns headers
// replaces them (mergePrepareStepGenerationSettings), so the suffix is gone
// from that step on. A hook that leaves the headers alone keeps it.
func TestWorkflowPrepareStepHeadersOverrideDropsUserAgentSuffix(t *testing.T) {
	cases := []struct {
		name     string
		override int // step whose hook replaces the headers; -1 for none
		inPlace  bool
		want     string
	}{
		{name: "no override", override: -1, want: "ua/ ua/"},
		{name: "step 0 override", override: 0, want: "-/1 -/1"},
		{name: "step 1 override", override: 1, want: "ua/ -/1"},
		{name: "step 0 in-place edit", override: 0, inPlace: true, want: "-/1 -/1"},
	}
	for _, tc := range cases {
		for _, stream := range []bool{false, true} {
			t.Run(tc.name+"/"+map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
				m := &r57UAModel{scriptedModel: scriptedModel{calls: []types.ToolCall{{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}}}}
				w, err := NewWorkflowAgent(WorkflowAgent{
					Model: m, Tools: r56Tool(), Headers: map[string]string{"x-a": "a"},
					PrepareStep: func(_ context.Context, o LanguageModelCallOptions) (LanguageModelCallOptions, error) {
						if o.StepNumber == tc.override {
							if tc.inPlace {
								o.Headers["x-h"] = "1"
							} else {
								o.Headers = map[string]string{"x-h": "1"}
							}
						}
						return o, nil
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := r56Run(t, w, stream); err != nil {
					t.Fatal(err)
				}
				m.mu.Lock()
				defer m.mu.Unlock()
				if have := strings.Join(m.calls, " "); have != tc.want {
					t.Fatalf("model calls = %q, want %q", have, tc.want)
				}
			})
		}
	}
}
