package workflow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
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

// TS WorkflowAgent calls onStart before its abort check, so a run whose
// context is already cancelled reports its start (model, messages, contexts)
// and then onAbort. Go fired OnAbort only: core, which builds the start
// event, is never reached.
func TestWorkflowPreCancelledRunFiresOnStartThenOnAbort(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[stream], func(t *testing.T) {
			var mu sync.Mutex
			var order []string
			var start ai.OnStartEvent
			m := &wfMockModel{}
			w, err := NewWorkflowAgent(WorkflowAgent{
				Model:          m,
				RuntimeContext: map[string]interface{}{"k": "v"},
				OnStart: func(_ context.Context, e ai.OnStartEvent) {
					mu.Lock()
					order = append(order, "start")
					start = e
					mu.Unlock()
				},
				OnAbort: func(context.Context, []types.StepResult) { mu.Lock(); order = append(order, "abort"); mu.Unlock() },
				OnEnd:   func(context.Context, ai.OnFinishEvent) { mu.Lock(); order = append(order, "end"); mu.Unlock() },
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if stream {
				_, err = w.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "go"})
			} else {
				_, err = w.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "go"})
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			mu.Lock()
			defer mu.Unlock()
			if have := strings.Join(order, " "); have != "start abort" {
				t.Fatalf("callbacks = %q, want %q", have, "start abort")
			}
			if start.ModelID != m.ModelID() || len(start.Messages) != 1 || start.Messages[0].Role != types.RoleUser {
				t.Fatalf("start event = model %q messages %+v, want the model and the prompt as one user message", start.ModelID, start.Messages)
			}
			if rc, _ := start.RuntimeContext.(map[string]interface{}); rc["k"] != "v" {
				t.Fatalf("start runtime context = %v", start.RuntimeContext)
			}
		})
	}
}

// r57ChunksModel streams the given chunks on every call.
type r57ChunksModel struct {
	wfMockModel
	chunks []provider.StreamChunk
}

func (m *r57ChunksModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream(m.chunks), nil
}

// TS WorkflowAgent's onEnd `messages` is `finalMessages ?? prompt.messages`:
// a stream run whose loop throws (a model call that fails to open, a
// PrepareStep error) reports the standardized prompt messages, without the
// instructions and without the completed steps' responses. A run that ends on
// the stream's own error (an error part, or a stream with no output) keeps
// the final conversation the iterator returns.
func TestWorkflowStreamOnEndMessagesAfterThrownFailure(t *testing.T) {
	errorPart := []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "partial"},
		{Type: provider.ChunkTypeError, Err: errors.New("mid"), Text: "mid"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonError},
	}
	cases := []struct {
		name    string
		model   provider.LanguageModel
		hookErr bool
		want    string
	}{
		{name: "first step fails to open", model: &r56FailModel{failAt: 0}, want: "user"},
		{name: "second step fails to open", model: &r56FailModel{failAt: 1}, want: "user"},
		{name: "PrepareStep fails", model: &r56FailModel{failAt: -1}, hookErr: true, want: "user"},
		{name: "stream error part", model: &r57ChunksModel{chunks: errorPart}, want: "system user assistant"},
		{name: "stream without output", model: &r57ChunksModel{}, want: "system user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var ends []ai.OnFinishEvent
			cfg := WorkflowAgent{
				Model: tc.model, System: "sys", Tools: r56Tool(),
				OnEnd: func(_ context.Context, e ai.OnFinishEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() },
			}
			if tc.hookErr {
				cfg.PrepareStep = func(context.Context, LanguageModelCallOptions) (LanguageModelCallOptions, error) {
					return LanguageModelCallOptions{}, errors.New("hook")
				}
			}
			w, err := NewWorkflowAgent(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := r56Run(t, w, true); err == nil {
				t.Fatal("want the run to fail")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(ends) != 1 {
				t.Fatalf("OnEnd calls = %d, want 1", len(ends))
			}
			roles := make([]string, 0, len(ends[0].Messages))
			for _, msg := range ends[0].Messages {
				roles = append(roles, string(msg.Role))
			}
			if have := strings.Join(roles, " "); have != tc.want {
				t.Fatalf("OnEnd message roles = %q, want %q", have, tc.want)
			}
		})
	}
}
