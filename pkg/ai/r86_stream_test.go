package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// TS stream-language-model-call.ts converts each step's own messages
// (prepareStep's messages included), so the "image" part deprecation follows
// them. Recorded with streamText under Node v24 (packages/ai dist at
// 30548ee7f9): step 1 logs one batch of one warning (the initial prompt);
// step 2, whose prepareStep appends a user message with two image parts,
// logs that batch again and then a batch of two.
func TestR86streamImageDeprecationFollowsStepMessages(t *testing.T) {
	var batches []int
	SetLogWarnings(func(o LogWarningsOptions) {
		if len(o.Warnings) > 0 && o.Warnings[0].Setting == `"image" content part` {
			batches = append(batches, len(o.Warnings))
		}
	})
	defer SetLogWarnings(nil)

	calls := 0
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		batches = append(batches, -calls)
		if calls == 1 {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
			}), nil
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeText, ID: "1", Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
		}), nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.ImageContent{Image: []byte{1, 2}, MimeType: "image/png"},
		}}},
		Tools: []types.Tool{{Name: "t", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return 1, nil
		}}},
		StopWhen: []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, step PrepareStepOptions) PrepareStepOptions {
			if step.StepNumber != 1 {
				return PrepareStepOptions{}
			}
			return PrepareStepOptions{Messages: append(append([]types.Message(nil), step.Messages...), types.Message{
				Role: types.RoleUser, Content: []types.ContentPart{
					types.ImageContent{Image: []byte{3}, MimeType: "image/png"},
					types.ImageContent{Image: []byte{4}, MimeType: "image/png"},
				}})}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ReadAll(); err != nil {
		t.Fatal(err)
	}
	want := []int{1, -1, 1, 2, -2}
	if len(batches) != len(want) {
		t.Fatalf("log sequence = %v, want %v", batches, want)
	}
	for i := range want {
		if batches[i] != want[i] {
			t.Fatalf("log sequence = %v, want %v", batches, want)
		}
	}
}

// r86streamParallelToolsRun runs one step with a slow and a fast tool call
// and returns the full stream's tool parts, the step content's tool parts and
// the tool message's result order.
func r86streamParallelToolsRun(t *testing.T, ctx context.Context, concurrency *int, fastDone func()) ([]string, []string, []string) {
	t.Helper()
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "a", ToolName: "slow", RawArguments: "{}"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "b", ToolName: "fast", RawArguments: "{}"}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonToolCalls, Usage: r72streamTestUsage},
		}), nil
	}}
	release := make(chan struct{})
	r, err := StreamText(ctx, StreamTextOptions{Model: model, Prompt: "x", ToolCallConcurrency: concurrency, Tools: []types.Tool{
		{Name: "slow", Execute: func(execCtx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
			select {
			case <-release:
			case <-time.After(300 * time.Millisecond):
			}
			return "S", nil
		}},
		{Name: "fast", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			if fastDone != nil {
				go func() {
					time.Sleep(50 * time.Millisecond)
					fastDone()
					time.Sleep(50 * time.Millisecond)
					close(release)
				}()
			} else {
				go func() { time.Sleep(50 * time.Millisecond); close(release) }()
			}
			return "F", nil
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	fs := r.FullStream()
	for {
		c, err := fs.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeToolCall:
			parts = append(parts, "tool-call:"+c.ToolCall.ID)
		case provider.ChunkTypeToolResult:
			parts = append(parts, "tool-result:"+c.ToolResult.ToolCallID)
		case provider.ChunkTypeAbort:
			parts = append(parts, "abort")
		}
	}
	var content, messages []string
	if steps := r.Steps(); len(steps) > 0 {
		for _, p := range steps[0].Content {
			switch v := p.(type) {
			case types.ToolCallContent:
				content = append(content, "tool-call:"+v.ToolCallID)
			case types.ToolResultContent:
				content = append(content, "tool-result:"+v.ToolCallID)
			}
		}
	}
	for _, m := range r.ResponseMessages() {
		if m.Role != types.RoleTool {
			continue
		}
		for _, p := range m.Content {
			if v, ok := p.(types.ToolResultContent); ok {
				messages = append(messages, v.ToolCallID)
			}
		}
	}
	return parts, content, messages
}

// TS executeToolsFromStream enqueues each tool result as its execution
// settles. Recorded with streamText under Node v24 (30548ee7f9): with the
// default concurrency the fast call's result streams first (full stream and
// step content), the tool message keeps call order; with
// toolCallConcurrency 1 the calls run, and stream, in call order.
func TestR86streamParallelToolResultsInCompletionOrder(t *testing.T) {
	parts, content, messages := r86streamParallelToolsRun(t, context.Background(), nil, nil)
	r86streamWantStrings(t, "parts", parts, "tool-call:a", "tool-call:b", "tool-result:b", "tool-result:a")
	r86streamWantStrings(t, "content", content, "tool-call:a", "tool-call:b", "tool-result:b", "tool-result:a")
	r86streamWantStrings(t, "tool message", messages, "a", "b")

	one := 1
	parts, content, messages = r86streamParallelToolsRun(t, context.Background(), &one, nil)
	r86streamWantStrings(t, "sequential parts", parts, "tool-call:a", "tool-call:b", "tool-result:a", "tool-result:b")
	r86streamWantStrings(t, "sequential content", content, "tool-call:a", "tool-call:b", "tool-result:a", "tool-result:b")
	r86streamWantStrings(t, "sequential tool message", messages, "a", "b")
}

// A result that settled before the abort is streamed; the slow call's later
// result is not (TS recorded: start start-step tool-call:a tool-call:b
// tool-result:b abort).
func TestR86streamToolResultBeforeAbortIsStreamed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parts, _, _ := r86streamParallelToolsRun(t, ctx, nil, cancel)
	r86streamWantStrings(t, "parts", parts, "tool-call:a", "tool-call:b", "tool-result:b", "abort")
}

func r86streamWantStrings(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// TS keeps a part's own `rawInput: null`: safeValidateUIMessages returns it
// unchanged and convertToModelMessages logs the rawInput deprecation (Node
// recorded, 30548ee7f9).
func TestR86streamToolPartRawInputNull(t *testing.T) {
	const part = `{"type":"tool-x","toolCallId":"c","state":"output-error","rawInput":null,"errorText":"e"}`
	var msg UIMessage
	if err := json.Unmarshal([]byte(`{"id":"a","role":"assistant","parts":[`+part+`]}`), &msg); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":"a","role":"assistant","parts":[` + part + `]}`; string(b) != want {
		t.Errorf("round trip = %s\nwant %s", b, want)
	}
	var settings []string
	SetLogWarnings(func(o LogWarningsOptions) {
		for _, w := range o.Warnings {
			settings = append(settings, w.Setting)
		}
	})
	defer SetLogWarnings(nil)
	if _, err := ConvertToModelMessages(context.Background(), []UIMessage{msg}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(settings, ",") != "rawInput in output-error UI message parts" {
		t.Errorf("warnings = %v", settings)
	}
}

// TS step toolResults come from the step content: a provider-executed result
// streamed during the model call precedes a local result that settled after
// the stream (Node recorded, 30548ee7f9: content tool-call:c1 tool-call:c2
// tool-result:c2 tool-result:c1; step, onStepEnd, onEnd and result
// toolResults c2,c1).
func TestR86streamToolResultsFollowContentOrder(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c1", ToolName: "exec", RawArguments: "{}"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "c2", ToolName: "web", RawArguments: "{}", ProviderExecuted: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "c2", ToolName: "web", Result: map[string]interface{}{"r": 2}, ProviderExecuted: true}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: r72streamTestUsage},
		}), nil
	}}
	ids := func(rs []types.ToolResult) []string {
		var s []string
		for _, r := range rs {
			s = append(s, r.ToolCallID)
		}
		return s
	}
	var stepEnd, end []string
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "x", Tools: []types.Tool{
		{Name: "exec", Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "E", nil
		}},
		{Name: "web", Type: types.ToolTypeProviderDefined, ProviderID: "m.web", ProviderExecuted: true},
	},
		OnStepEndEvent: func(_ context.Context, e OnStepFinishEvent) { stepEnd = ids(e.ToolResults) },
		OnEndEvent:     func(_ context.Context, e OnFinishEvent) { end = ids(e.ToolResults) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	r86streamWantStrings(t, "step toolResults", ids(r.Steps()[0].ToolResults), "c2", "c1")
	r86streamWantStrings(t, "result toolResults", ids(r.ToolResults()), "c2", "c1")
	r86streamWantStrings(t, "onStepEnd toolResults", stepEnd, "c2", "c1")
	r86streamWantStrings(t, "onEnd toolResults", end, "c2", "c1")
}
