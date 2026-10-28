package ai

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS streamText standardizes and converts the prompt, resumes approvals and
// runs prepareStep inside its async pipeline; the pipeline's catch adds an
// `error` part after `start`, so the full stream carries it and onError is
// called, as for a model call that fails to open. Go failed the result with
// no error part and no OnError call.
func TestR57StreamTextFailureBeforeFirstStreamReachesOnError(t *testing.T) {
	cases := []struct {
		name string
		opts StreamTextOptions
	}{
		{name: "prompt and messages", opts: StreamTextOptions{Prompt: "a", Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "b"}}}}}},
		{name: "missing tool result", opts: StreamTextOptions{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "1", ToolName: "t"}}},
		}}},
		{name: "PrepareStep instructions", opts: StreamTextOptions{Prompt: "a", PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{InstructionMessages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}}
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var seen []error
			opts := tc.opts
			opts.Model = &testutil.MockLanguageModel{}
			opts.OnError = func(_ context.Context, err error) { mu.Lock(); seen = append(seen, err); mu.Unlock() }
			res, err := StreamText(context.Background(), opts)
			if err != nil {
				t.Fatal(err)
			}
			var parts []string
			s := res.FullStream()
			for {
				c, err := s.Next()
				if err != nil {
					break
				}
				parts = append(parts, string(c.Type))
			}
			runErr := res.Err()
			if runErr == nil {
				t.Fatal("want the run to fail")
			}
			if have := strings.Join(parts, " "); have != "start error" {
				t.Fatalf("full stream = %q, want %q", have, "start error")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 1 || seen[0].Error() != runErr.Error() {
				t.Fatalf("OnError = %v, want once with %v", seen, runErr)
			}
		})
	}
}

// A later step that cannot be prepared (here its prompt has a tool call
// without a result, from a PrepareStep messages override) fails the run with
// an error part in the full stream, as TS streamText enqueues one when the
// next streamStep throws; OnError is called once.
func TestR57StreamTextNextStepPreparationErrorIsAnErrorPart(t *testing.T) {
	var mu sync.Mutex
	var seen []error
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "1", ToolName: "t", Arguments: map[string]interface{}{}}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls},
		}), nil
	}}
	res, err := StreamText(context.Background(), StreamTextOptions{
		Model:    model,
		Prompt:   "a",
		StopWhen: []StopCondition{IsStepCount(3)},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "r", nil
		}}},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			if o.StepNumber == 1 {
				return PrepareStepOptions{Messages: []types.Message{
					{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
					{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "9", ToolName: "t"}}},
				}}
			}
			return PrepareStepOptions{}
		},
		OnError: func(_ context.Context, err error) { mu.Lock(); seen = append(seen, err); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	var last string
	s := res.FullStream()
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		last = string(c.Type)
	}
	if res.Err() == nil {
		t.Fatal("want the run to fail")
	}
	if last != string(provider.ChunkTypeError) {
		t.Fatalf("last full-stream part = %q, want an error part", last)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("OnError calls = %d, want 1", len(seen))
	}
}
