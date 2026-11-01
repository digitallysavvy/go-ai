package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r75streamNormalize round-trips v through JSON so Go chunk maps compare with
// TS expectations written as JSON.
func r75streamNormalize(t *testing.T, v interface{}) interface{} {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %#v: %v", v, err)
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return out
}

func r75streamAssertJSON(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	var wantValue interface{}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("%s: bad want JSON: %v", label, err)
	}
	gotValue := r75streamNormalize(t, got)
	if !reflect.DeepEqual(gotValue, wantValue) {
		gb, _ := json.Marshal(gotValue)
		t.Fatalf("%s:\n got  %s\n want %s", label, gb, want)
	}
}

const r75streamProviderMetadata = `{"testProvider":{"signature":"sig-1"}}`

// to-ui-message-chunk.test.ts 212: maps custom and lifecycle parts.
func TestR75StreamToUIMessageChunkCustomAndLifecycle(t *testing.T) {
	meta := json.RawMessage(r75streamProviderMetadata)
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{
		Type:             provider.ChunkTypeCustom,
		CustomContent:    &types.CustomContent{Kind: "openai.compaction"},
		ProviderMetadata: meta,
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("custom part produced no chunk")
	}
	r75streamAssertJSON(t, "custom", chunk, `{"type":"custom","kind":"openai.compaction","providerMetadata":`+r75streamProviderMetadata+`}`)

	metadataFn := func(map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"model": "test-model"}
	}
	chunk, ok = ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeStart}, UIMessageStreamResultOptions{
		MessageMetadata:   metadataFn,
		ResponseMessageID: "msg-1",
	})
	if !ok {
		t.Fatal("start produced no chunk")
	}
	r75streamAssertJSON(t, "start", chunk, `{"type":"start","messageId":"msg-1","messageMetadata":{"model":"test-model"}}`)

	sendStart := false
	if chunk, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeStart}, UIMessageStreamResultOptions{SendStart: &sendStart}); ok || chunk != nil {
		t.Fatalf("start with sendStart false = %#v, %v", chunk, ok)
	}

	usage := &types.Usage{InputTokens: r75streamInt64(1), OutputTokens: r75streamInt64(2), TotalTokens: r75streamInt64(3)}
	finishPart := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: usage}
	chunk, ok = ToUIMessageChunk(finishPart, UIMessageStreamResultOptions{MessageMetadata: metadataFn})
	if !ok {
		t.Fatal("finish produced no chunk")
	}
	r75streamAssertJSON(t, "finish", chunk, `{"type":"finish","finishReason":"stop","messageMetadata":{"model":"test-model"}}`)

	sendFinish := false
	if chunk, ok := ToUIMessageChunk(finishPart, UIMessageStreamResultOptions{SendFinish: &sendFinish}); ok || chunk != nil {
		t.Fatalf("finish with sendFinish false = %#v, %v", chunk, ok)
	}

	chunk, ok = ToUIMessageChunk(provider.StreamChunk{Type: "start-step"}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("start-step produced no chunk")
	}
	r75streamAssertJSON(t, "start-step", chunk, `{"type":"start-step"}`)

	chunk, ok = ToUIMessageChunk(provider.StreamChunk{
		Type:             provider.ChunkTypeFinishStep,
		FinishReason:     types.FinishReasonStop,
		RawFinishReason:  "stop",
		Usage:            usage,
		ProviderMetadata: meta,
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("finish-step produced no chunk")
	}
	r75streamAssertJSON(t, "finish-step", chunk, `{"type":"finish-step"}`)

	chunk, ok = ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: "user"}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("abort produced no chunk")
	}
	r75streamAssertJSON(t, "abort", chunk, `{"type":"abort","reason":"user"}`)
}

func r75streamInt64(v int64) *int64 { return &v }

// to-ui-message-chunk.test.ts 475: preserves schema input for transformed
// tool approval requests.
func TestR75StreamToUIMessageChunkApprovalSchemaInput(t *testing.T) {
	chunk, ok := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolApprovalRequest,
		ToolApprovalRequest: &types.ToolApprovalRequestContent{
			ApprovalID: "approval-1",
			ToolCall: types.ToolCall{
				ID:        "call-1",
				ToolName:  "staticTool",
				Arguments: map[string]interface{}{"value": "trimmed"},
			},
			InputSchemaInput: map[string]interface{}{"value": " trimmed "},
		},
	}, UIMessageStreamResultOptions{})
	if !ok {
		t.Fatal("approval request produced no chunk")
	}
	r75streamAssertJSON(t, "approval request", chunk, `{"type":"tool-approval-request","approvalId":"approval-1","toolCallId":"call-1","inputSchemaInput":{"value":" trimmed "}}`)

	// Same input: no inputSchemaInput (TS isDeepEqualData check).
	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolApprovalRequest,
		ToolApprovalRequest: &types.ToolApprovalRequestContent{
			ApprovalID: "approval-2",
			ToolCall: types.ToolCall{
				ID:        "call-2",
				ToolName:  "staticTool",
				Arguments: map[string]interface{}{"value": "same"},
			},
			InputSchemaInput: map[string]interface{}{"value": "same"},
		},
	}, UIMessageStreamResultOptions{})
	r75streamAssertJSON(t, "approval request same input", chunk, `{"type":"tool-approval-request","approvalId":"approval-2","toolCallId":"call-2"}`)
}

// to-ui-message-chunk.test.ts 679: parts that do not produce UI chunks.
func TestR75StreamToUIMessageChunkNoChunkParts(t *testing.T) {
	for _, part := range []provider.StreamChunk{
		{Type: provider.ChunkTypeToolInputEnd, ID: "call-1"},
		{Type: provider.ChunkTypeRaw, Raw: map[string]interface{}{"provider": "raw"}},
	} {
		if chunk, ok := ToUIMessageChunk(part, UIMessageStreamResultOptions{}); ok || chunk != nil {
			t.Fatalf("%s produced %#v, %v", part.Type, chunk, ok)
		}
	}
}

// to-ui-message-chunk.test.ts 500: tool result, tool error, tool denial and
// approval parts (exact chunks).
func TestR75StreamToUIMessageChunkToolResultErrorDenialApproval(t *testing.T) {
	meta := map[string]interface{}{"testProvider": map[string]interface{}{"signature": "sig-1"}}
	toolMetadata := map[string]interface{}{"clientName": "test-client"}
	tools := []types.Tool{
		{Name: "staticTool"},
		{Name: "dynamicTool", Type: types.ToolTypeDynamic},
	}

	chunk, _ := ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID:       "call-1",
			ToolName:         "dynamicTool",
			Input:            map[string]interface{}{"value": "input"},
			Result:           map[string]interface{}{"value": "output"},
			ProviderExecuted: true,
			ProviderMetadata: meta,
			ToolMetadata:     toolMetadata,
			Dynamic:          true,
			Preliminary:      true,
		},
	}, UIMessageStreamResultOptions{Tools: tools})
	r75streamAssertJSON(t, "tool result", chunk, `{"type":"tool-output-available","toolCallId":"call-1","output":{"value":"output"},"providerExecuted":true,"providerMetadata":`+r75streamProviderMetadata+`,"toolMetadata":{"clientName":"test-client"},"dynamic":true,"preliminary":true}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID: "call-undefined",
			ToolName:   "dynamicTool",
			Input:      map[string]interface{}{"value": "input"},
		},
	}, UIMessageStreamResultOptions{Tools: tools})
	r75streamAssertJSON(t, "tool result undefined output", chunk, `{"type":"tool-output-available","toolCallId":"call-undefined","output":null,"dynamic":true}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID:       "call-2",
			ToolName:         "dynamicTool",
			Input:            map[string]interface{}{"value": "input"},
			Result:           map[string]interface{}{"code": "provider-error"},
			Error:            errors.New("provider-error"),
			ProviderExecuted: true,
			ProviderMetadata: meta,
			ToolMetadata:     toolMetadata,
			Dynamic:          true,
		},
	}, UIMessageStreamResultOptions{
		Tools:   tools,
		OnError: func(error) string { return "should not be used for provider-executed errors" },
	})
	r75streamAssertJSON(t, "provider tool error", chunk, `{"type":"tool-output-error","toolCallId":"call-2","errorText":"{\"code\":\"provider-error\"}","providerExecuted":true,"providerMetadata":`+r75streamProviderMetadata+`,"toolMetadata":{"clientName":"test-client"},"dynamic":true}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID:       "call-string-error",
			ToolName:         "dynamicTool",
			Input:            map[string]interface{}{"value": "input"},
			Result:           "provider string error",
			Error:            errors.New("provider string error"),
			ProviderExecuted: true,
			Dynamic:          true,
		},
	}, UIMessageStreamResultOptions{})
	r75streamAssertJSON(t, "provider string tool error", chunk, `{"type":"tool-output-error","toolCallId":"call-string-error","errorText":"provider string error","providerExecuted":true,"dynamic":true}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolResult,
		ToolResult: &types.ToolResult{
			ToolCallID: "call-3",
			ToolName:   "staticTool",
			Input:      map[string]interface{}{"value": "input"},
			Error:      errors.New("tool failed"),
		},
	}, UIMessageStreamResultOptions{
		Tools:   tools,
		OnError: func(err error) string { return "handled: " + err.Error() },
	})
	r75streamAssertJSON(t, "local tool error", chunk, `{"type":"tool-output-error","toolCallId":"call-3","errorText":"handled: tool failed"}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type:       provider.ChunkTypeToolOutputDenied,
		ToolResult: &types.ToolResult{ToolCallID: "call-4", ToolName: "staticTool"},
	}, UIMessageStreamResultOptions{})
	r75streamAssertJSON(t, "tool output denied", chunk, `{"type":"tool-output-denied","toolCallId":"call-4"}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolApprovalRequest,
		ToolApprovalRequest: &types.ToolApprovalRequestContent{
			ApprovalID: "approval-1",
			ToolCall: types.ToolCall{
				ID:        "call-5",
				ToolName:  "staticTool",
				Arguments: map[string]interface{}{"value": "input"},
			},
			Reason:      "requires operator review",
			IsAutomatic: true,
		},
	}, UIMessageStreamResultOptions{})
	r75streamAssertJSON(t, "approval request", chunk, `{"type":"tool-approval-request","approvalId":"approval-1","toolCallId":"call-5","reason":"requires operator review","isAutomatic":true}`)

	chunk, _ = ToUIMessageChunk(provider.StreamChunk{
		Type: provider.ChunkTypeToolApprovalResponse,
		ToolApprovalResponse: &types.ToolApprovalResponseContent{
			ApprovalID: "approval-1",
			ToolCall: types.ToolCall{
				ID:        "call-5",
				ToolName:  "staticTool",
				Arguments: map[string]interface{}{"value": "input"},
			},
			Approved:         false,
			Reason:           "not allowed",
			ProviderExecuted: true,
		},
	}, UIMessageStreamResultOptions{})
	r75streamAssertJSON(t, "approval response", chunk, `{"type":"tool-approval-response","approvalId":"approval-1","approved":false,"reason":"not allowed","providerExecuted":true}`)
}

// r75streamUIRecorder collects the end and step-end events of a UI stream.
type r75streamUIRecorder struct {
	mu         sync.Mutex
	ends       []map[string]interface{}
	steps      []map[string]interface{}
	errorsSeen []error
}

func (r *r75streamUIRecorder) onEnd(event map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, event)
}

func (r *r75streamUIRecorder) onStep(event map[string]interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, event)
}

func (r *r75streamUIRecorder) onError(err error) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorsSeen = append(r.errorsSeen, err)
	return "An error occurred."
}

// r75streamRunUIChunks runs chunks through CreateUIMessageStreamWithOptions
// (TS createUIMessageStream -> handleUIMessageStreamFinish) and returns the
// output chunks and stream errors.
func r75streamRunUIChunks(t *testing.T, chunks []UIMessageChunk, opts UIMessageStreamOptions) ([]UIMessageChunk, []error) {
	t.Helper()
	opts.Execute = func(w UIMessageStreamWriter) {
		for _, c := range chunks {
			w.Write(c)
		}
	}
	out, errCh := CreateUIMessageStreamWithOptions(context.Background(), opts)
	var got []UIMessageChunk
	for c := range out {
		got = append(got, c)
	}
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	return got, errs
}

func r75streamParts(t *testing.T, message interface{}) []interface{} {
	t.Helper()
	m, ok := message.(UIMessageChunk)
	if !ok {
		t.Fatalf("message is %T", message)
	}
	return uiParts(m)
}

func r75streamIDGen(id string) IDGenerator { return func() string { return id } }

// handle-ui-message-stream-finish.test.ts 230: empty original messages.
func TestR75StreamUIFinishEmptyOriginalMessages(t *testing.T) {
	rec := &r75streamUIRecorder{}
	chunks := []UIMessageChunk{
		{"type": "start", "messageId": "msg-789"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "Response"},
		{"type": "text-end", "id": "text-1"},
		{"type": "finish"},
	}
	_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
		OriginalMessages:  []UIMessageChunk{},
		GenerateMessageID: r75streamIDGen("msg-789"),
		OnError:           rec.onError,
		OnFinish:          rec.onEnd,
	})
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if len(rec.ends) != 1 {
		t.Fatalf("onFinish calls = %d, want 1", len(rec.ends))
	}
	event := rec.ends[0]
	if event["isContinuation"] != false {
		t.Fatalf("isContinuation = %v", event["isContinuation"])
	}
	messages, _ := event["messages"].([]UIMessageChunk)
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if !reflect.DeepEqual(r75streamNormalize(t, messages[0]), r75streamNormalize(t, event["responseMessage"])) {
		t.Fatalf("messages[0] = %#v, responseMessage = %#v", messages[0], event["responseMessage"])
	}
}

// handle-ui-message-stream-finish.test.ts 392, 418, 456, 481: abort scenarios.
func TestR75StreamUIFinishAbortScenarios(t *testing.T) {
	t.Run("abort reason passes through", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := []UIMessageChunk{
			{"type": "start", "messageId": "msg-abort-reason"},
			{"type": "text-start", "id": "text-1"},
			{"type": "text-delta", "id": "text-1", "delta": "Starting text"},
			{"type": "abort", "reason": "manual abort"},
			{"type": "finish"},
		}
		got, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-abort-reason"),
			OnError:           rec.onError,
			OnFinish:          rec.onEnd,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if !reflect.DeepEqual(got, chunks) {
			t.Fatalf("chunks = %#v", got)
		}
		if len(rec.ends) != 1 {
			t.Fatalf("onFinish calls = %d", len(rec.ends))
		}
	})

	t.Run("isAborted false without abort chunk", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := []UIMessageChunk{
			{"type": "start", "messageId": "msg-normal"},
			{"type": "text-start", "id": "text-1"},
			{"type": "text-delta", "id": "text-1", "delta": "Complete text"},
			{"type": "text-end", "id": "text-1"},
			{"type": "finish"},
		}
		_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages: []UIMessageChunk{{
				"id": "user-msg-1", "role": "user",
				"parts": []interface{}{map[string]interface{}{"type": "text", "text": "Test request"}},
			}},
			GenerateMessageID: r75streamIDGen("msg-normal"),
			OnError:           rec.onError,
			OnFinish:          rec.onEnd,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if len(rec.ends) != 1 {
			t.Fatalf("onFinish calls = %d", len(rec.ends))
		}
		event := rec.ends[0]
		if event["isAborted"] != false || event["isContinuation"] != false {
			t.Fatalf("isAborted = %v, isContinuation = %v", event["isAborted"], event["isContinuation"])
		}
		if id := event["responseMessage"].(UIMessageChunk)["id"]; id != "msg-normal" {
			t.Fatalf("responseMessage.id = %v", id)
		}
	})

	t.Run("abort in pass-through mode", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := []UIMessageChunk{
			{"type": "start", "messageId": "msg-abort-passthrough"},
			{"type": "text-start", "id": "text-1"},
			{"type": "text-delta", "id": "text-1", "delta": "Text before abort"},
			{"type": "abort"},
			{"type": "finish"},
		}
		got, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-abort-passthrough"),
			OnError:           rec.onError,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if !reflect.DeepEqual(got, chunks) {
			t.Fatalf("chunks = %#v", got)
		}
		if len(rec.errorsSeen) != 0 {
			t.Fatalf("onError called: %v", rec.errorsSeen)
		}
	})

	t.Run("multiple abort chunks", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := []UIMessageChunk{
			{"type": "start", "messageId": "msg-multiple-abort"},
			{"type": "text-start", "id": "text-1"},
			{"type": "abort"},
			{"type": "text-delta", "id": "text-1", "delta": "Some text"},
			{"type": "abort"},
			{"type": "finish"},
		}
		got, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-multiple-abort"),
			OnError:           rec.onError,
			OnFinish:          rec.onEnd,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if !reflect.DeepEqual(got, chunks) {
			t.Fatalf("chunks = %#v", got)
		}
		if len(rec.ends) != 1 || rec.ends[0]["isAborted"] != true {
			t.Fatalf("onFinish = %#v", rec.ends)
		}
	})
}

// handle-ui-message-stream-finish.test.ts 546 and 579: reader cancellation
// with a declared outcome, and an observed abort before cancellation.
func TestR75StreamUIFinishCancellationOutcomes(t *testing.T) {
	run := func(t *testing.T, chunks []UIMessageChunk, declared *UIMessageStreamOutcome) map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		rec := &r75streamUIRecorder{}
		out, errCh := CreateUIMessageStreamWithOptions(ctx, UIMessageStreamOptions{
			Execute: func(w UIMessageStreamWriter) {
				for _, c := range chunks {
					w.Write(c)
				}
				if declared != nil {
					w.SetOutcome(*declared)
				}
				<-ctx.Done()
			},
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-1"),
			OnError:           rec.onError,
			OnEnd:             rec.onEnd,
		})
		for range chunks {
			<-out
		}
		cancel()
		for range out {
		}
		for range errCh {
		}
		if len(rec.ends) != 1 {
			t.Fatalf("onEnd calls = %d, want 1", len(rec.ends))
		}
		return rec.ends[0]
	}

	failErr := errors.New("stream failed")
	for _, declared := range []UIMessageStreamOutcome{
		{Status: UIMessageStreamOutcomeCompleted},
		{Status: UIMessageStreamOutcomeFailed, Error: failErr},
		{Status: UIMessageStreamOutcomeAborted},
	} {
		t.Run("declared "+string(declared.Status), func(t *testing.T) {
			declared := declared
			event := run(t, []UIMessageChunk{{"type": "start", "messageId": "msg-1"}}, &declared)
			want := map[string]interface{}{"status": string(declared.Status)}
			if declared.Error != nil {
				want["error"] = declared.Error
			}
			if !reflect.DeepEqual(event["outcome"], want) {
				t.Fatalf("outcome = %#v, want %#v", event["outcome"], want)
			}
			if _, ok := event["isCancelled"]; ok {
				t.Fatalf("isCancelled set: %#v", event)
			}
		})
	}

	t.Run("observed abort wins over cancellation", func(t *testing.T) {
		event := run(t, []UIMessageChunk{{"type": "start", "messageId": "msg-1"}, {"type": "abort"}}, nil)
		if event["isAborted"] != true {
			t.Fatalf("isAborted = %v", event["isAborted"])
		}
		if !reflect.DeepEqual(event["outcome"], map[string]interface{}{"status": "aborted"}) {
			t.Fatalf("outcome = %#v", event["outcome"])
		}
		if _, ok := event["isCancelled"]; ok {
			t.Fatalf("isCancelled set: %#v", event)
		}
	})
}

// handle-ui-message-stream-finish.test.ts 703, 752, 820, 864, 903: step
// callbacks.
func TestR75StreamUIFinishStepCallbacks(t *testing.T) {
	text := func(id, delta string) []UIMessageChunk {
		return []UIMessageChunk{
			{"type": "text-start", "id": id},
			{"type": "text-delta", "id": id, "delta": delta},
			{"type": "text-end", "id": id},
		}
	}
	cat := func(groups ...[]UIMessageChunk) []UIMessageChunk {
		var out []UIMessageChunk
		for _, g := range groups {
			out = append(out, g...)
		}
		return out
	}

	t.Run("multiple steps", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := cat(
			[]UIMessageChunk{{"type": "start", "messageId": "msg-multi-step"}},
			text("text-1", "Step 1"), []UIMessageChunk{{"type": "finish-step"}, {"type": "start-step"}},
			text("text-2", "Step 2"), []UIMessageChunk{{"type": "finish-step"}, {"type": "start-step"}},
			text("text-3", "Step 3"), []UIMessageChunk{{"type": "finish-step"}, {"type": "finish"}},
		)
		_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-multi-step"),
			OnError:           rec.onError,
			OnStepFinish:      rec.onStep,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if len(rec.steps) != 3 {
			t.Fatalf("onStepFinish calls = %d, want 3", len(rec.steps))
		}
		for i, want := range []int{1, 3, 5} {
			if got := len(r75streamParts(t, rec.steps[i]["responseMessage"])); got != want {
				t.Fatalf("step %d parts = %d, want %d", i+1, got, want)
			}
		}
	})

	t.Run("both onStepFinish and onFinish", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := cat([]UIMessageChunk{{"type": "start", "messageId": "msg-both"}}, text("text-1", "Hello"),
			[]UIMessageChunk{{"type": "finish-step"}, {"type": "finish"}})
		_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-both"),
			OnError:           rec.onError,
			OnStepFinish:      rec.onStep,
			OnFinish:          rec.onEnd,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if len(rec.steps) != 1 || len(rec.ends) != 1 {
			t.Fatalf("steps = %d, ends = %d", len(rec.steps), len(rec.ends))
		}
	})

	t.Run("continuation", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := cat([]UIMessageChunk{{"type": "start", "messageId": "assistant-msg-1"}}, text("text-1", " continued"),
			[]UIMessageChunk{{"type": "finish-step"}, {"type": "finish"}})
		_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages: []UIMessageChunk{
				{"id": "user-msg-1", "role": "user", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "Continue this"}}},
				{"id": "assistant-msg-1", "role": "assistant", "parts": []interface{}{map[string]interface{}{"type": "text", "text": "This is"}}},
			},
			GenerateMessageID: r75streamIDGen("msg-999"),
			OnError:           rec.onError,
			OnStepFinish:      rec.onStep,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if len(rec.steps) != 1 {
			t.Fatalf("onStepFinish calls = %d", len(rec.steps))
		}
		event := rec.steps[0]
		if event["isContinuation"] != true {
			t.Fatalf("isContinuation = %v", event["isContinuation"])
		}
		if id := event["responseMessage"].(UIMessageChunk)["id"]; id != "assistant-msg-1" {
			t.Fatalf("responseMessage.id = %v", id)
		}
		if messages, _ := event["messages"].([]UIMessageChunk); len(messages) != 2 {
			t.Fatalf("messages = %d, want 2", len(messages))
		}
	})

	t.Run("deep-cloned step messages", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		var stepParts int
		chunks := cat([]UIMessageChunk{{"type": "start", "messageId": "msg-clone"}}, text("text-1", "Hello"),
			[]UIMessageChunk{{"type": "finish-step"}, {"type": "finish"}})
		_, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-clone"),
			OnError:           rec.onError,
			OnStepFinish: func(event map[string]interface{}) {
				message := event["responseMessage"].(UIMessageChunk)
				message["parts"] = append(uiParts(message), map[string]interface{}{"type": "text", "text": "MUTATION!"})
				stepParts = len(uiParts(message))
			},
			OnFinish: rec.onEnd,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if stepParts != 2 {
			t.Fatalf("step parts after mutation = %d, want 2", stepParts)
		}
		if got := len(r75streamParts(t, rec.ends[0]["responseMessage"])); got != 1 {
			t.Fatalf("onFinish parts = %d, want 1", got)
		}
	})

	t.Run("no callbacks passes through", func(t *testing.T) {
		rec := &r75streamUIRecorder{}
		chunks := cat([]UIMessageChunk{{"type": "start", "messageId": "msg-passthrough"}}, text("text-1", "Test"),
			[]UIMessageChunk{{"type": "finish-step"}, {"type": "finish"}})
		got, errs := r75streamRunUIChunks(t, chunks, UIMessageStreamOptions{
			OriginalMessages:  []UIMessageChunk{},
			GenerateMessageID: r75streamIDGen("msg-passthrough"),
			OnError:           rec.onError,
		})
		if len(errs) != 0 {
			t.Fatalf("errors: %v", errs)
		}
		if !reflect.DeepEqual(got, chunks) {
			t.Fatalf("chunks = %#v", got)
		}
		if len(rec.errorsSeen) != 0 {
			t.Fatalf("onError called: %v", rec.errorsSeen)
		}
	})
}

// stream-text.test.ts 20546: tools with custom schema > should send tool
// calls. The JSON schema tool is sent as is with tool choice required, and
// the tool-call part carries the parsed input and the tool's title.
func TestR75StreamCustomSchemaToolCall(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"properties":           map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
		"required":             []interface{}{"value"},
		"additionalProperties": false,
	}
	var gotTools []types.Tool
	var gotChoice types.ToolChoice
	var gotPrompt []types.Message
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		gotTools, gotChoice, gotPrompt = o.Tools, o.ToolChoice, o.Prompt.Messages
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-0", ModelID: "mock-model-id"}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: usage(3, 10)},
		}), nil
	}}
	r, parts := r73streamParts(t, StreamTextOptions{
		Model:      model,
		Prompt:     "test-input",
		Tools:      []types.Tool{{Name: "tool1", Title: "Tool 1", Parameters: schema}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
	})
	if len(gotTools) != 1 || gotTools[0].Name != "tool1" || !reflect.DeepEqual(r75streamNormalize(t, gotTools[0].Parameters), r75streamNormalize(t, schema)) {
		t.Fatalf("model tools = %#v", gotTools)
	}
	if gotChoice.Type != types.ToolChoiceRequired {
		t.Fatalf("tool choice = %#v", gotChoice)
	}
	if len(gotPrompt) != 1 || gotPrompt[0].Role != types.RoleUser {
		t.Fatalf("prompt = %#v", gotPrompt)
	}
	if got, want := r73streamNames(parts), []string{"start", "start-step", "tool-call:call-1:tool1", "finish-step:stop", "finish:stop"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parts = %q, want %q", got, want)
	}
	var call *types.ToolCall
	for i := range parts {
		if parts[i].Type == provider.ChunkTypeToolCall {
			call = parts[i].ToolCall
		}
	}
	if call == nil || call.Title != "Tool 1" || !reflect.DeepEqual(call.Arguments, map[string]interface{}{"value": "value"}) || call.Invalid {
		t.Fatalf("tool-call part = %#v", call)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("result error = %v", err)
	}
	if fr := r.FinishReason(); fr != types.FinishReasonStop {
		t.Fatalf("finish reason = %q", fr)
	}
	if u := r.TotalUsage(); u.TotalTokens == nil || *u.TotalTokens != 13 {
		t.Fatalf("total usage = %#v", u)
	}
}

// stream-text.test.ts 32311, 32362, 32393, 32488: a call that needs approval
// is denied (exact prompt, response messages, full stream denial part and UI
// message stream).
func TestR75StreamDeniedApprovalExact(t *testing.T) {
	var prompts [][]types.Message
	executed := 0
	result, err := StreamText(context.Background(), StreamTextOptions{
		Model: helloModel(&prompts), Messages: tsApprovalHistory(false), Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
		StopWhen:     []StopCondition{StepCountIs(3)},
		OnError:      func(context.Context, error) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	ui, uiErrs := CreateUIMessageStream(context.Background(), result)
	var chunks []UIMessageChunk
	for c := range ui {
		chunks = append(chunks, c)
	}
	for err := range uiErrs {
		t.Fatalf("ui error: %v", err)
	}
	r75streamAssertJSON(t, "ui message stream", chunks, `[
		{"type":"start"},
		{"type":"tool-output-denied","toolCallId":"call-1"},
		{"type":"start-step"},
		{"type":"text-start","id":"1"},
		{"type":"text-delta","id":"1","delta":"Hello, world!"},
		{"type":"text-end","id":"1"},
		{"type":"finish-step"},
		{"type":"finish","finishReason":"stop"}
	]`)
	if executed != 0 {
		t.Fatal("denied tool executed")
	}

	// Prompt: the approval request and response are removed; the tool
	// message holds the execution-denied result (reason undefined).
	if len(prompts) != 1 || len(prompts[0]) != 3 {
		t.Fatalf("prompts = %#v", prompts)
	}
	assistant := prompts[0][1]
	if assistant.Role != types.RoleAssistant || len(assistant.Content) != 1 {
		t.Fatalf("prompt assistant message = %#v", assistant)
	}
	if call, ok := assistant.Content[0].(types.ToolCallContent); !ok || call.ToolCallID != "call-1" || call.ToolName != "tool1" {
		t.Fatalf("prompt assistant content = %#v", assistant.Content[0])
	}
	toolMsg := prompts[0][2]
	if toolMsg.Role != types.RoleTool || len(toolMsg.Content) != 1 {
		t.Fatalf("prompt tool message = %#v", toolMsg)
	}
	tr, ok := toolMsg.Content[0].(types.ToolResultContent)
	if !ok || tr.ToolCallID != "call-1" || tr.ToolName != "tool1" || tr.Output == nil || tr.Output.Type != types.ToolResultOutputExecutionDenied || tr.Output.Reason != "" {
		t.Fatalf("prompt tool result = %#v (output %#v)", toolMsg.Content[0], tr.Output)
	}

	// Response messages: the denied tool result, then the assistant text.
	msgs := result.ResponseMessages()
	if len(msgs) != 2 || msgs[0].Role != types.RoleTool || msgs[1].Role != types.RoleAssistant {
		t.Fatalf("response messages = %#v", msgs)
	}
	if len(msgs[0].Content) != 1 {
		t.Fatalf("response tool message content = %#v", msgs[0].Content)
	}
	rtr, ok := msgs[0].Content[0].(types.ToolResultContent)
	if !ok || rtr.ToolCallID != "call-1" || rtr.ToolName != "tool1" || rtr.Output == nil || rtr.Output.Type != types.ToolResultOutputExecutionDenied || rtr.Output.Reason != "" {
		t.Fatalf("response tool result = %#v", msgs[0].Content[0])
	}
	if len(msgs[1].Content) != 1 {
		t.Fatalf("response assistant content = %#v", msgs[1].Content)
	}
	if text, ok := msgs[1].Content[0].(types.TextContent); !ok || text.Text != "Hello, world!" {
		t.Fatalf("response assistant text = %#v", msgs[1].Content[0])
	}
}

// stream-text.test.ts 32393: the full stream's tool-output-denied part names
// the tool (toolCallId + toolName).
func TestR75StreamDeniedApprovalFullStreamPart(t *testing.T) {
	executed := 0
	_, parts := r73streamParts(t, StreamTextOptions{
		Model: helloModel(nil), Messages: tsApprovalHistory(false), Tools: []types.Tool{approvalTool(&executed)},
		ToolApproval: map[string]interface{}{"tool1": "user-approval"},
		StopWhen:     []StopCondition{StepCountIs(3)},
	})
	if len(parts) < 2 || parts[1].Type != provider.ChunkTypeToolOutputDenied {
		t.Fatalf("parts = %q", r73streamNames(parts))
	}
	denied := parts[1].ToolResult
	if denied == nil || denied.ToolCallID != "call-1" || denied.ToolName != "tool1" {
		t.Fatalf("tool-output-denied part = %#v", denied)
	}
}
