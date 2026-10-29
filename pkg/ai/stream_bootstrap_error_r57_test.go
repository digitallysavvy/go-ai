package ai

import (
	"context"
	"errors"
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
		// r57 integrator: the remaining pre-stream sites.
		{name: "approval resume", opts: StreamTextOptions{
			Tools:                          []types.Tool{{Name: "tool1"}},
			ExperimentalToolApprovalSecret: []byte("secret"),
			Messages:                       approvalHistory("tool1", map[string]interface{}{"value": "v"}, "forged-signature", true, ""),
		}},
		{name: "PrepareStep maxOutputTokens", opts: StreamTextOptions{Prompt: "a", PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
			zero := 0
			return PrepareStepOptions{MaxOutputTokens: &zero}
		}}},
		{name: "output response format", opts: StreamTextOptions{Prompt: "a", Output: r57FailingFormatOutput{}}},
		{name: "step prompt conversion", opts: StreamTextOptions{Prompt: "a", PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions {
			return PrepareStepOptions{Messages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "s"}}}}}
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
	zero := 0
	cases := map[string]PrepareStepOptions{
		"missing tool result": {Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "b"}}},
			{Role: types.RoleAssistant, Content: []types.ContentPart{types.ToolCallContent{ToolCallID: "9", ToolName: "t"}}},
		}},
		// r57 integrator: the other next-step preparation sites.
		"instructions":      {InstructionMessages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "x"}}}}},
		"maxOutputTokens":   {MaxOutputTokens: &zero},
		"prompt conversion": {Messages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "s"}}}}},
	}
	for name, step1 := range cases {
		t.Run(name, func(t *testing.T) { r57NextStepPreparationError(t, step1) })
	}
}

func r57NextStepPreparationError(t *testing.T, step1 PrepareStepOptions) {
	t.Helper()
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
				return step1
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

// r57FailingFormatOutput is an Output whose response format cannot be built.
type r57FailingFormatOutput struct{}

func (r57FailingFormatOutput) ResponseFormat(context.Context) (*provider.ResponseFormat, error) {
	return nil, errors.New("no format")
}

func (r57FailingFormatOutput) parseCompleteOutput(context.Context, ParseCompleteOutputOptions) (interface{}, error) {
	return nil, nil
}

func (r57FailingFormatOutput) parsePartialOutput(context.Context, ParsePartialOutputOptions) (interface{}, bool, error) {
	return nil, false, nil
}
