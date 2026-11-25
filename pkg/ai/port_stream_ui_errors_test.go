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

func portstreamBool(b bool) *bool { return &b }

// portstreamUIChunks converts one provider chunk with ToUIMessageChunk.
func portstreamUIChunk(t *testing.T, part provider.StreamChunk, opts UIMessageStreamResultOptions) UIMessageChunk {
	t.Helper()
	chunk, ok := ToUIMessageChunk(part, opts)
	if !ok {
		t.Fatalf("no UI chunk for %+v", part)
	}
	return chunk
}

// TS 1a94af38c7 toUIMessageChunk "masks error parts by default" and "masks
// error parts when redactToolErrors is false".
func TestPortStreamUIChunkMasksStreamErrors(t *testing.T) {
	t.Parallel()
	part := provider.StreamChunk{Type: provider.ChunkTypeError, Err: errors.New("boom"), Text: "boom"}
	for name, opts := range map[string]UIMessageStreamResultOptions{
		"default":                   {},
		"redactToolErrors false":    {RedactToolErrors: portstreamBool(false)},
		"onError with no redaction": {OnError: func(error) string { return "x" }, RedactToolErrors: portstreamBool(false)},
	} {
		chunk := portstreamUIChunk(t, part, opts)
		want := "An error occurred."
		if opts.OnError != nil {
			want = "x"
		}
		if chunk["type"] != "error" || chunk["errorText"] != want {
			t.Fatalf("%s: chunk = %v, want error with %q", name, chunk, want)
		}
	}
}

// TS 1a94af38c7 toUIMessageChunk "error text of failed tool calls".
func TestPortStreamUIChunkToolErrorText(t *testing.T) {
	t.Parallel()
	formatError := func(err error) string { return "handled: " + err.Error() }
	toolError := provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
		ToolCallID: "call-1", ToolName: "runtimeTool", Input: map[string]interface{}{"value": "input"},
		Error: errors.New("tool failed"), Dynamic: true,
	}}
	invalidCall := provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "call-1", ToolName: "runtimeTool", RawArguments: "{broken", Dynamic: true, Invalid: true,
		Error: errors.New("tool failed"),
	}}
	cases := []struct {
		name string
		opts UIMessageStreamResultOptions
		want string
	}{
		{"keeps the text that the model received by default", UIMessageStreamResultOptions{}, "Error: tool failed"},
		{"uses the default onError when tool errors are redacted", UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, "An error occurred."},
		{"uses a provided onError", UIMessageStreamResultOptions{OnError: formatError}, "handled: tool failed"},
		{"keeps the model text when a provided onError does not redact tool errors", UIMessageStreamResultOptions{OnError: formatError, RedactToolErrors: portstreamBool(false)}, "Error: tool failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := portstreamUIChunk(t, toolError, tc.opts)
			if out["type"] != "tool-output-error" || out["errorText"] != tc.want || out["toolCallId"] != "call-1" || out["dynamic"] != true {
				t.Fatalf("tool error chunk = %v, want errorText %q", out, tc.want)
			}
			in := portstreamUIChunk(t, invalidCall, tc.opts)
			if in["type"] != "tool-input-error" || in["errorText"] != tc.want {
				t.Fatalf("invalid call chunk = %v, want errorText %q", in, tc.want)
			}
		})
	}
}

// TS 1a94af38c7 "uses onError for invalid tool calls of provider-executed
// tools": the model gets no tool result for them.
func TestPortStreamUIChunkInvalidProviderExecutedCallUsesOnError(t *testing.T) {
	t.Parallel()
	chunk := portstreamUIChunk(t, provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
		ID: "call-1", ToolName: "runtimeTool", RawArguments: "{broken", Dynamic: true, Invalid: true,
		ProviderExecuted: true, Error: errors.New("tool failed"),
	}}, UIMessageStreamResultOptions{})
	if chunk["type"] != "tool-input-error" || chunk["errorText"] != "An error occurred." || chunk["providerExecuted"] != true {
		t.Fatalf("chunk = %v, want the onError text for a provider-executed invalid call", chunk)
	}
}

// TS 1a94af38c7 stream-text.test.ts "redactToolErrors: true" (toUIMessageStream,
// toUIMessageStreamResponse, pipeUIMessageStreamToResponse) and the default.
func TestPortStreamResultRedactToolErrors(t *testing.T) {
	t.Parallel()
	steps := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", Arguments: map[string]interface{}{"value": "value"}, RawArguments: `{ "value": "value" }`}},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r72streamTestUsage},
	}
	newResult := func() *StreamTextResult {
		r, err := StreamText(context.Background(), StreamTextOptions{
			Model: r71streamModel(nil, steps), Prompt: "test-input",
			Tools:   []types.Tool{r71streamTool1(true, errors.New("test error"), nil)},
			OnError: func(context.Context, error) {},
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	errorTexts := func(chunks []UIMessageChunk) []string {
		var texts []string
		for _, c := range chunks {
			if c["type"] == "tool-output-error" {
				texts = append(texts, c["errorText"].(string))
			}
		}
		return texts
	}
	collect := func(r *StreamTextResult, opts UIMessageStreamResultOptions) []UIMessageChunk {
		ch, errs := r.ToUIMessageStream(context.Background(), opts)
		var out []UIMessageChunk
		for c := range ch {
			out = append(out, c)
		}
		for range errs {
		}
		return out
	}

	if got := errorTexts(collect(newResult(), UIMessageStreamResultOptions{})); len(got) != 1 || got[0] != "Error: test error" {
		t.Fatalf("default error texts = %v, want [Error: test error]", got)
	}
	if got := errorTexts(collect(newResult(), UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)})); len(got) != 1 || got[0] != "An error occurred." {
		t.Fatalf("redacted error texts = %v, want [An error occurred.]", got)
	}

	resp, err := newResult().ToUIMessageStreamResponse(context.Background(), nil, UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)})
	if err != nil {
		t.Fatal(err)
	}
	body := portstreamReadAll(t, resp.Body)
	if !strings.Contains(body, `"errorText":"An error occurred."`) || strings.Contains(body, "test error") {
		t.Fatalf("response body = %s, want the redacted text only", body)
	}

	var sb strings.Builder
	if err := newResult().PipeUIMessageStreamToResponse(context.Background(), &sb, UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), `"errorText":"An error occurred."`) || strings.Contains(sb.String(), "test error") {
		t.Fatalf("piped body = %s, want the redacted text only", sb.String())
	}
}

func portstreamReadAll(t *testing.T, r interface{ Read([]byte) (int, error) }) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String()
		}
	}
}

// TS ceeee29f9a and 1a94af38c7 "tool error text mismatch warning".
func TestPortStreamToolErrorTextMismatchWarning(t *testing.T) {
	// SetLogWarnings is process-global: these cases run one after the other.
	var mu sync.Mutex
	var messages []string
	record := func(options LogWarningsOptions) {
		for _, w := range options.Warnings {
			if strings.Contains(w.Message, "differs from the error text the model received") {
				mu.Lock()
				messages = append(messages, w.Message)
				mu.Unlock()
			}
		}
	}

	startStep := provider.StreamChunk{Type: provider.ChunkTypeStartStep}
	finishStep := provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop}
	toolCall := func(id string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: id, ToolName: "test-tool", Arguments: map[string]interface{}{"value": "valid"}, Dynamic: true}}
	}
	toolError := func(id string) provider.StreamChunk {
		return provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: id, ToolName: "test-tool", Error: errors.New("execution failed"), Dynamic: true}}
	}
	reasoning := []provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningStart, ID: "r1"},
		{Type: provider.ChunkTypeReasoning, ID: "r1", Reasoning: "thinking", Text: "thinking"},
		{Type: provider.ChunkTypeReasoningEnd, ID: "r1"},
	}
	join := func(parts ...interface{}) []provider.StreamChunk {
		var out []provider.StreamChunk
		for _, p := range parts {
			switch v := p.(type) {
			case provider.StreamChunk:
				out = append(out, v)
			case []provider.StreamChunk:
				out = append(out, v...)
			}
		}
		return out
	}
	toolErrorThenReasoning := join(startStep, toolCall("call-1"), toolError("call-1"), finishStep, startStep, reasoning, finishStep)

	cases := []struct {
		name  string
		parts []provider.StreamChunk
		opts  UIMessageStreamResultOptions
		warns int
	}{
		{"warns when a masked tool error is followed by reasoning from a later step", toolErrorThenReasoning, UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 1},
		{"warns once per stream", join(toolErrorThenReasoning, startStep, toolCall("call-2"), toolError("call-2"), finishStep, startStep, reasoning, finishStep), UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 1},
		{"does not warn by default", toolErrorThenReasoning, UIMessageStreamResultOptions{}, 0},
		{"does not warn when a provided onError does not redact tool errors", toolErrorThenReasoning, UIMessageStreamResultOptions{OnError: func(err error) string { return err.Error() }, RedactToolErrors: portstreamBool(false)}, 0},
		{"warns when onError returns a text that differs", toolErrorThenReasoning, UIMessageStreamResultOptions{OnError: func(err error) string { return err.Error() }}, 1},
		{"does not warn when onError returns the model text", toolErrorThenReasoning, UIMessageStreamResultOptions{OnError: func(err error) string { return "Error: " + err.Error() }}, 0},
		{"does not warn when reasoning is not sent", toolErrorThenReasoning, UIMessageStreamResultOptions{SendReasoning: portstreamBool(false), RedactToolErrors: portstreamBool(true)}, 0},
		{"does not warn when the reasoning precedes the tool error",
			join(startStep, reasoning, toolCall("call-1"), toolError("call-1"), finishStep, startStep,
				provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "t1"}, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "t1", Text: "The tool failed."}, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "t1"}, finishStep),
			UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 0},
		{"does not warn when the reasoning follows the tool error in the same step", join(startStep, toolCall("call-1"), toolError("call-1"), reasoning, finishStep), UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 0},
		{"warns when a masked tool error precedes the first step", join(toolError("call-1"), startStep, reasoning, finishStep), UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 1},
		{"warns when a masked invalid tool call is followed by reasoning from a later step",
			join(startStep,
				provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", RawArguments: "{broken", Dynamic: true, Invalid: true, Error: errors.New("invalid tool input")}},
				finishStep, startStep, reasoning, finishStep),
			UIMessageStreamResultOptions{RedactToolErrors: portstreamBool(true)}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			messages = nil
			mu.Unlock()
			SetLogWarnings(record)
			t.Cleanup(func() { SetLogWarnings(nil) })

			ch, errs := ToUIMessageStream(context.Background(), testutil.NewMockTextStream(tc.parts), tc.opts)
			for range ch {
			}
			for range errs {
			}
			mu.Lock()
			defer mu.Unlock()
			if len(messages) != tc.warns {
				t.Fatalf("warnings = %d (%v), want %d", len(messages), messages, tc.warns)
			}
			if tc.warns > 0 && !strings.Contains(messages[0], `The error text sent to the UI for tool call "call-1" differs from the error text the model received`) {
				t.Fatalf("warning = %q", messages[0])
			}
		})
	}
}

// TS 1a94af38c7 to-ui-message-stream.test.ts "sends a tool execution error / an
// invalid tool call that converts back to the tool result the model received":
// the UI message of the default stream converts back to the error text the
// model got.
func TestPortStreamToolErrorConvertsBackToModelToolResult(t *testing.T) {
	t.Parallel()
	for name, parts := range map[string][]provider.StreamChunk{
		"tool execution error": {
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "input"}, Dynamic: true}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "test-tool", Input: map[string]interface{}{"value": "input"}, Error: errors.New("execution failed"), Dynamic: true}},
		},
		"invalid tool call": {
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "test-tool", Arguments: map[string]interface{}{"value": "input"}, Dynamic: true, Invalid: true, Error: errors.New("invalid tool input")}},
			// streamText follows an invalid tool call with a tool-error part
			// that carries the error text for the model.
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "call-1", ToolName: "test-tool", Input: map[string]interface{}{"value": "input"}, Error: &invalidToolCallError{cause: errors.New("invalid tool input")}, Dynamic: true, InvalidToolCall: true}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			chunks := append([]provider.StreamChunk{{Type: provider.ChunkTypeStartStep}}, parts...)
			chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop})
			ui, uiErrs := ToUIMessageStream(context.Background(), testutil.NewMockTextStream(chunks), UIMessageStreamResultOptions{})
			messages, readErrs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{Stream: ui})
			var last *UIMessage
			for m := range messages {
				m := m
				last = &m
			}
			for range readErrs {
			}
			for range uiErrs {
			}
			if last == nil {
				t.Fatal("no UI message")
			}
			modelMessages, err := ConvertToModelMessages(context.Background(), []UIMessage{*last})
			if err != nil {
				t.Fatal(err)
			}
			var got *types.ToolResultOutput
			for _, m := range modelMessages {
				for _, part := range m.Content {
					if tr, ok := part.(types.ToolResultContent); ok {
						got = tr.Output
					}
				}
			}
			want := "Error: execution failed"
			if name == "invalid tool call" {
				want = "Error: invalid tool input"
			}
			if got == nil || got.Type != types.ToolResultOutputErrorText || got.Value != want {
				t.Fatalf("model tool result output = %+v, want error-text %q", got, want)
			}
		})
	}
}
