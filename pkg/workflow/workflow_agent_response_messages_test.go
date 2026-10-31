package workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type fnModel struct {
	wfMockModel
	gen    func() *types.GenerateResult
	chunks []provider.StreamChunk
}

func (m *fnModel) DoGenerate(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
	return m.gen(), nil
}

func (m *fnModel) DoStream(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
	return testutil.NewMockTextStream(m.chunks), nil
}

// TS #22018, #22020, #22396, #22093: partial responses that end with a
// non-stop finish reason are kept in the step's response messages (generate and
// stream), text provider metadata is kept as provider options, and
// provider-executed tool calls/results survive a length stop.
func TestWorkflowPartialResponsesStayInResponseMessages(t *testing.T) {
	meta := []byte(`{"openai":{"itemId":"msg_1"}}`)
	for _, fr := range []types.FinishReason{types.FinishReasonLength, types.FinishReasonContentFilter, types.FinishReasonError, types.FinishReasonOther} {
		fr := fr
		t.Run(string(fr), func(t *testing.T) {
			m := &fnModel{
				gen: func() *types.GenerateResult {
					return &types.GenerateResult{Text: "partial", FinishReason: fr,
						Content: []types.ContentPart{types.TextContent{Text: "partial", ProviderMetadata: meta}}}
				},
				chunks: []provider.StreamChunk{
					{Type: provider.ChunkTypeTextStart, ID: "0", ProviderMetadata: meta},
					{Type: provider.ChunkTypeText, ID: "0", Text: "partial"},
					{Type: provider.ChunkTypeTextEnd, ID: "0", ProviderMetadata: meta},
					{Type: provider.ChunkTypeFinish, FinishReason: fr},
				},
			}
			a, err := NewWorkflowAgent(WorkflowAgent{Model: m})
			if err != nil {
				t.Fatal(err)
			}
			check := func(t *testing.T, steps []types.StepResult) {
				t.Helper()
				if len(steps) != 1 || len(steps[0].Response.Messages) != 1 {
					t.Fatalf("steps = %+v", steps)
				}
				msg := steps[0].Response.Messages[0]
				text, ok := msg.Content[0].(types.TextContent)
				if msg.Role != types.RoleAssistant || !ok || text.Text != "partial" {
					t.Fatalf("message = %+v", msg)
				}
				if text.ProviderOptions["openai"] == nil {
					t.Fatalf("text provider metadata dropped: %+v", text)
				}
			}
			res, err := a.GenerateWithOptions(context.Background(), WorkflowGenerateOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			check(t, res.Steps)
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ReadAll(); err != nil {
				t.Fatal(err)
			}
			check(t, st.Steps())
		})
	}
}

func TestWorkflowLengthStopKeepsProviderExecutedToolResults(t *testing.T) {
	m := &fnModel{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "p1", ToolName: "web_search", Arguments: map[string]interface{}{"q": "x"}, ProviderExecuted: true}},
		{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "p1", ToolName: "web_search", Result: "found", ProviderExecuted: true}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
	}}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: []types.Tool{{Name: "web_search", ProviderExecuted: true}}})
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
	var call, result bool
	for _, s := range st.Steps() {
		for _, msg := range s.Response.Messages {
			for _, p := range msg.Content {
				switch p.(type) {
				case types.ToolCallContent:
					call = true
				case types.ToolResultContent:
					result = true
				}
			}
		}
	}
	if !call || !result {
		t.Fatalf("provider-executed call=%v result=%v, want both", call, result)
	}
}

func TestWorkflowIncompleteStreamWithoutOutputReportsError(t *testing.T) {
	m := &fnModel{chunks: []provider.StreamChunk{}}
	a, err := NewWorkflowAgent(WorkflowAgent{Model: m})
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := st.ReadAll()
	t.Logf("readErr=%v streamErr=%v", readErr, st.Err())
	if readErr == nil && st.Err() == nil {
		t.Fatal("expected a no-output error for a stream that ended without output or finish")
	}
}

// TS #22079: streamed steps report real (non-placeholder) performance metrics.
func TestWorkflowStreamStepReportsPerformance(t *testing.T) {
	m := &fnModel{chunks: []provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "hello"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: &types.Usage{InputTokens: ptrInt64(10), OutputTokens: ptrInt64(20)}},
	}}
	a, _ := NewWorkflowAgent(WorkflowAgent{Model: m})
	st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadAll(); err != nil {
		t.Fatal(err)
	}
	steps := st.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d", len(steps))
	}
	perf := steps[0].Performance
	if perf.TimeToFirstOutputMs == nil || perf.ToolExecutionMs == nil {
		t.Fatalf("performance is a placeholder: %+v", perf)
	}
	if perf.StepTimeMs < perf.ResponseTimeMs {
		t.Fatalf("stepTimeMs %d < responseTimeMs %d", perf.StepTimeMs, perf.ResponseTimeMs)
	}
}

func ptrInt64(v int64) *int64 { return &v }

// TS #22403: a stream that closes without a finish chunk is a no-output error
// unless it carried semantic output; boundary chunks, metadata and empty
// reasoning deltas are not output. Partial output is kept without an error.
func TestWorkflowIncompleteStreamClassifiesOutputChunks(t *testing.T) {
	cases := []struct {
		name    string
		chunks  []provider.StreamChunk
		wantErr bool
	}{
		{"stream start only", []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}}, true},
		{"response metadata only", []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "r1", ModelID: "mock-model"}}}, true},
		{"bare text start", []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "t"}}, true},
		{"empty reasoning delta", []provider.StreamChunk{{Type: provider.ChunkTypeReasoningStart, ID: "r"}, {Type: provider.ChunkTypeReasoning, Text: ""}}, true},
		{"partial text", []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "t"}, {Type: provider.ChunkTypeText, Text: "partial output"}}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewWorkflowAgent(WorkflowAgent{Model: &fnModel{chunks: tc.chunks}})
			if err != nil {
				t.Fatal(err)
			}
			st, err := a.StreamWithOptions(context.Background(), WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := st.ReadAll()
			if tc.wantErr {
				if !ai.IsNoOutputGeneratedError(readErr) && !ai.IsNoOutputGeneratedError(st.Err()) {
					t.Fatalf("want NoOutputGeneratedError, got readErr=%v streamErr=%v", readErr, st.Err())
				}
				return
			}
			if readErr != nil || st.Err() != nil {
				t.Fatalf("partial output must not error: readErr=%v streamErr=%v", readErr, st.Err())
			}
			if st.Text() != "partial output" {
				t.Fatalf("text = %q", st.Text())
			}
		})
	}
}

// TS modelCallIterator ends the run after a step whose finish reason is not
// stop or tool-calls (done = !isToolExecutionAllowed): invalid local calls get
// their validation error, nothing is executed or sent for approval, and the
// model is not asked again. stream() has no step limit, so the loop used to
// run forever on a model that repeats the truncated call.
func TestWorkflowLengthStopWithLocalToolCallEndsRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		args interface{}
	}{{"unknown", "x"}, {"local", 123}, {"local", "ok"}} {
		tc := tc
		t.Run(tc.name+fmt.Sprint(tc.args), func(t *testing.T) {
			executed, generateCalls := 0, 0
			call := types.ToolCall{ID: "bad", ToolName: tc.name, Arguments: map[string]interface{}{"value": tc.args}}
			m := &fnModel{
				gen: func() *types.GenerateResult {
					generateCalls++
					return &types.GenerateResult{FinishReason: types.FinishReasonLength, ToolCalls: []types.ToolCall{call}}
				},
				chunks: []provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
				},
			}
			tools := []types.Tool{{
				Name:       "local",
				Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					executed++
					return "x", nil
				},
			}}
			a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: tools})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st, err := a.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ReadAll(); err != nil {
				t.Fatal(err)
			}
			if n := len(st.Steps()); n != 1 {
				t.Fatalf("stream steps = %d, want 1", n)
			}
			if executed != 0 {
				t.Fatalf("stream: executed=%d, want none", executed)
			}
			res, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi", StopWhen: []ai.StopCondition{ai.IsStepCount(5)}})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Steps) != 1 || generateCalls != 1 {
				t.Fatalf("generate steps = %d, model calls = %d, want 1", len(res.Steps), generateCalls)
			}
			if executed != 0 {
				t.Fatalf("executed=%d, want none", executed)
			}
		})
	}
}

// TS shouldProcessTools is false after a length step without provider results,
// so the assistant message carries no invalid call and no tool message follows.
func TestWorkflowBlockedStepAddsNoToolErrorMessage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      interface{}
		wantCalls int
	}{{"unknown", "x", 0}, {"local", 123, 0}, {"local", "ok", 1}} {
		tc := tc
		t.Run(tc.name+fmt.Sprint(tc.args), func(t *testing.T) {
			call := types.ToolCall{ID: "bad", ToolName: tc.name, Arguments: map[string]interface{}{"value": tc.args}}
			m := &fnModel{
				gen: func() *types.GenerateResult {
					return &types.GenerateResult{FinishReason: types.FinishReasonLength, ToolCalls: []types.ToolCall{call}}
				},
				chunks: []provider.StreamChunk{
					{Type: provider.ChunkTypeToolCall, ToolCall: &call},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonLength},
				},
			}
			tools := []types.Tool{{
				Name:       "local",
				Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}},
				Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
					return "x", nil
				},
			}}
			a, err := NewWorkflowAgent(WorkflowAgent{Model: m, Tools: tools})
			if err != nil {
				t.Fatal(err)
			}
			check := func(label string, msgs []types.Message) {
				t.Helper()
				for _, msg := range msgs {
					if msg.Role == types.RoleTool {
						t.Fatalf("%s: unexpected tool message %+v", label, msg)
					}
					if msg.Role == types.RoleAssistant && len(msg.ToolCalls) != tc.wantCalls {
						t.Fatalf("%s: assistant tool calls = %d, want %d", label, len(msg.ToolCalls), tc.wantCalls)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			st, err := a.StreamWithOptions(ctx, WorkflowStreamOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ReadAll(); err != nil {
				t.Fatal(err)
			}
			var streamMsgs []types.Message
			for _, step := range st.Steps() {
				streamMsgs = append(streamMsgs, step.Response.Messages...)
			}
			check("stream", streamMsgs)
			res, err := a.GenerateWithOptions(ctx, WorkflowGenerateOptions{Prompt: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			check("generate", res.ResponseMessages)
		})
	}
}
