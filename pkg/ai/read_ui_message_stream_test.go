package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Ports of approval-related cases from
// ai/packages/ai/src/ui/process-ui-message-stream.test.ts, plus a
// ReadUIMessages ("readUIMessageStream") snapshot test and a
// repeated-toolCallId-across-steps regression.
// ---------------------------------------------------------------------------

// sendChunks feeds chunks through a channel to ReadUIMessages and collects
// every emitted snapshot plus a possible terminal error.
func sendChunks(t *testing.T, message *UIMessage, chunks []UIMessageChunk) ([]UIMessage, error) {
	t.Helper()
	ch := make(chan UIMessageChunk)
	snapshots, errs := ReadUIMessages(context.Background(), ReadUIMessagesOptions{
		Message: message,
		Stream:  ch,
	})

	done := make(chan struct{})
	var got []UIMessage
	go func() {
		for m := range snapshots {
			got = append(got, m)
		}
		close(done)
	}()

	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	<-done

	var err error
	select {
	case err = <-errs:
	default:
	}
	return got, err
}

func findToolUIMessagePart(msg UIMessage, toolType string) *ToolUIPart {
	for _, p := range msg.Parts {
		if tool, ok := AsToolUIPart(p); ok && tool.Type == toolType {
			return tool
		}
	}
	return nil
}

// ports read-ui-message-stream.test.ts: a basic streaming round trip
// produces incremental UIMessage snapshots ending in the fully assembled
// message.
func TestReadUIMessages_BasicSnapshot(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start", "messageId": "msg-1"},
		{"type": "start-step"},
		{"type": "text-start", "id": "t1"},
		{"type": "text-delta", "id": "t1", "delta": "Hello"},
		{"type": "text-delta", "id": "t1", "delta": ", world!"},
		{"type": "text-end", "id": "t1"},
		{"type": "finish-step"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, snapshots)

	final := snapshots[len(snapshots)-1]
	assert.Equal(t, "msg-1", final.ID)
	require.Len(t, final.Parts, 2) // step-start + text
	textPart, ok := final.Parts[1].(*TextUIPart)
	require.True(t, ok)
	assert.Equal(t, "Hello, world!", textPart.Text)
	assert.Equal(t, UIPartStateDone, textPart.State)
}

// ports describe('tool approval request with signature')
func TestReadUIMessages_ApprovalRequestWithSignature(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start"},
		{"type": "start-step"},
		{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "tool1", "input": map[string]interface{}{"value": "value"}},
		{"type": "tool-approval-request", "approvalId": "id-1", "toolCallId": "call-1", "reason": "requires operator review", "signature": "test-sig"},
		{"type": "finish-step"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	final := snapshots[len(snapshots)-1]

	toolPart := findToolUIMessagePart(final, "tool-tool1")
	require.NotNil(t, toolPart)
	assert.Equal(t, ToolStateApprovalRequested, toolPart.State)
	require.NotNil(t, toolPart.Approval)
	assert.Equal(t, "id-1", toolPart.Approval.ID)
	assert.Equal(t, "requires operator review", toolPart.Approval.RequestReason)
	assert.Equal(t, "test-sig", toolPart.Approval.Signature)
	assert.Equal(t, "", toolPart.Approval.Reason)
}

// ports describe('tool approval response with signature'):
// "preserves request details separately from the response reason"
func TestReadUIMessages_ApprovalResponsePreservesRequestReasonSeparately(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start"},
		{"type": "start-step"},
		{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "tool1", "input": map[string]interface{}{"value": "value"}},
		{"type": "tool-approval-request", "approvalId": "id-1", "toolCallId": "call-1", "reason": "requires operator review", "signature": "test-sig"},
		{"type": "tool-approval-response", "approvalId": "id-1", "approved": true, "reason": "approved by operator"},
		{"type": "finish-step"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	final := snapshots[len(snapshots)-1]

	toolPart := findToolUIMessagePart(final, "tool-tool1")
	require.NotNil(t, toolPart)
	assert.Equal(t, ToolStateApprovalResponded, toolPart.State)
	require.NotNil(t, toolPart.Approval)
	assert.Equal(t, "id-1", toolPart.Approval.ID)
	require.NotNil(t, toolPart.Approval.Approved)
	assert.True(t, *toolPart.Approval.Approved)
	assert.Equal(t, "requires operator review", toolPart.Approval.RequestReason)
	assert.Equal(t, "approved by operator", toolPart.Approval.Reason)
	assert.Equal(t, "test-sig", toolPart.Approval.Signature)
}

// ports "preserves approval descriptors through request and response states"
func TestReadUIMessages_ApprovalDescriptorThroughRequestAndResponse(t *testing.T) {
	descriptor := map[string]interface{}{
		"action":      "deleteAccount",
		"permissions": []interface{}{"account:delete"},
		"risk":        "high",
	}
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "deleteAccount", "input": map[string]interface{}{"userId": "user-123"}},
		{"type": "tool-approval-request", "approvalDescriptor": descriptor, "approvalId": "approval-1", "toolCallId": "call-1"},
		{"type": "tool-approval-response", "approvalId": "approval-1", "approved": true},
	})
	require.NoError(t, err)
	require.Len(t, snapshots, 3)

	requested := findToolUIMessagePart(snapshots[1], "tool-deleteAccount")
	require.NotNil(t, requested)
	require.NotNil(t, requested.Approval)
	assert.Equal(t, "approval-1", requested.Approval.ID)
	assert.Equal(t, descriptor, requested.Approval.Descriptor)
	assert.Nil(t, requested.Approval.Approved)

	responded := findToolUIMessagePart(snapshots[2], "tool-deleteAccount")
	require.NotNil(t, responded)
	require.NotNil(t, responded.Approval)
	assert.Equal(t, "approval-1", responded.Approval.ID)
	assert.Equal(t, descriptor, responded.Approval.Descriptor)
	require.NotNil(t, responded.Approval.Approved)
	assert.True(t, *responded.Approval.Approved)
}

// ports "preserves an approval descriptor restored from a persisted message":
// the approval is requested on one connection and answered on another, so
// the descriptor has to survive being restored from `lastMessage`.
func TestReadUIMessages_ApprovalDescriptorRestoredFromPersistedMessage(t *testing.T) {
	descriptor := map[string]interface{}{
		"action":      "deleteAccount",
		"permissions": []interface{}{"account:delete"},
		"risk":        "high",
	}
	lastMessage := UIMessage{
		Role: UIMessageRoleAssistant,
		ID:   "msg-123",
		Parts: []UIMessagePart{
			&ToolUIPart{
				Type:       "tool-deleteAccount",
				ToolCallID: "call-1",
				State:      ToolStateApprovalRequested,
				Input:      map[string]interface{}{"userId": "user-123"},
				Approval:   &ToolUIPartApproval{ID: "approval-1", Descriptor: descriptor},
			},
		},
	}

	snapshots, err := sendChunks(t, &lastMessage, []UIMessageChunk{
		{"type": "tool-approval-response", "approvalId": "approval-1", "approved": true},
	})
	require.NoError(t, err)
	require.Len(t, snapshots, 1)

	responded := findToolUIMessagePart(snapshots[0], "tool-deleteAccount")
	require.NotNil(t, responded)
	require.NotNil(t, responded.Approval)
	assert.Equal(t, "approval-1", responded.Approval.ID)
	assert.Equal(t, descriptor, responded.Approval.Descriptor)
	require.NotNil(t, responded.Approval.Approved)
	assert.True(t, *responded.Approval.Approved)
}

// Regression: tool call IDs can repeat across steps (e.g. the same tool
// invoked twice in a multi-step agent run). updateToolPart's comment says
// "Tool call IDs can repeat across steps, so earlier steps are never updated
// here" -- verify a second start-step boundary starts a fresh tool part
// instead of mutating the finished one from the previous step.
func TestReadUIMessages_RepeatedToolCallIDAcrossSteps(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start"},
		{"type": "start-step"},
		{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "search", "input": map[string]interface{}{"q": "first"}},
		{"type": "tool-output-available", "toolCallId": "call-1", "output": "result-1"},
		{"type": "finish-step"},
		{"type": "start-step"},
		{"type": "tool-input-available", "toolCallId": "call-1", "toolName": "search", "input": map[string]interface{}{"q": "second"}},
		{"type": "tool-output-available", "toolCallId": "call-1", "output": "result-2"},
		{"type": "finish-step"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	final := snapshots[len(snapshots)-1]

	var toolParts []*ToolUIPart
	for _, p := range final.Parts {
		if tool, ok := AsToolUIPart(p); ok {
			toolParts = append(toolParts, tool)
		}
	}
	require.Len(t, toolParts, 2, "expected two distinct tool parts, one per step, got: %#v", final.Parts)
	assert.Equal(t, "call-1", toolParts[0].ToolCallID)
	assert.Equal(t, map[string]interface{}{"q": "first"}, toolParts[0].Input)
	assert.Equal(t, "result-1", toolParts[0].Output)
	assert.Equal(t, "call-1", toolParts[1].ToolCallID)
	assert.Equal(t, map[string]interface{}{"q": "second"}, toolParts[1].Input)
	assert.Equal(t, "result-2", toolParts[1].Output)
}

// ports process-ui-message-stream.test.ts > describe('finish-step') > "preserves
// active text and reasoning parts across interleaved step boundaries": a
// merged stream's step can finish-step while another stream's text/reasoning
// part is still open, so finish-step must not force-close active parts.
func TestReadUIMessages_FinishStepPreservesActiveTextAndReasoning(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start"},
		{"type": "text-start", "id": "text-1"},
		{"type": "text-delta", "id": "text-1", "delta": "first "},
		{"type": "reasoning-start", "id": "reasoning-1"},
		{"type": "reasoning-delta", "id": "reasoning-1", "delta": "thinking "},
		{"type": "start-step"},
		{"type": "finish-step"},
		{"type": "text-delta", "id": "text-1", "delta": "second"},
		{"type": "reasoning-delta", "id": "reasoning-1", "delta": "continued"},
		{"type": "text-end", "id": "text-1"},
		{"type": "reasoning-end", "id": "reasoning-1"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	final := snapshots[len(snapshots)-1]
	require.Len(t, final.Parts, 3)

	text, ok := final.Parts[0].(*TextUIPart)
	require.True(t, ok, "expected a text part, got %#v", final.Parts[0])
	assert.Equal(t, "first second", text.Text)
	assert.Equal(t, UIPartState("done"), text.State)

	reasoning, ok := final.Parts[1].(*ReasoningUIPart)
	require.True(t, ok, "expected a reasoning part, got %#v", final.Parts[1])
	assert.Equal(t, "reasoning-1", reasoning.ID)
	assert.Equal(t, "thinking continued", reasoning.Text)
	assert.Equal(t, UIPartState("done"), reasoning.State)

	assert.Equal(t, "step-start", final.Parts[2].UIPartType())
}

// ports the tool-input-error branch of process-ui-message-stream.ts, which
// calls warnIfUIMessageHasDeprecatedRawInput([state.message]) only for
// static (non-dynamic) tool parts, since the deprecated field is only set on
// those.
func TestReadUIMessages_ToolInputErrorRawInputDeprecationWarning(t *testing.T) {
	t.Run("static tool part logs the deprecation warning", func(t *testing.T) {
		buf := setupLogWarnings(t)
		snapshots, err := sendChunks(t, nil, []UIMessageChunk{
			{"type": "start"},
			{"type": "start-step"},
			{"type": "tool-input-error", "toolCallId": "call-1", "toolName": "search", "input": "bad json", "errorText": "parse error"},
			{"type": "finish-step"},
			{"type": "finish"},
		})
		require.NoError(t, err)
		final := snapshots[len(snapshots)-1]
		part := findToolUIMessagePart(final, "tool-search")
		require.NotNil(t, part)
		assert.Equal(t, ToolStateOutputError, part.State)
		assert.Equal(t, "bad json", part.RawInput)
		assert.Contains(t, buf.String(), `Deprecated: "rawInput in output-error UI message parts". Use the "input" field instead. The "rawInput" field will be removed in the next major version.`)
	})

	t.Run("dynamic tool part does not set rawInput and does not warn", func(t *testing.T) {
		buf := setupLogWarnings(t)
		_, err := sendChunks(t, nil, []UIMessageChunk{
			{"type": "start"},
			{"type": "start-step"},
			{"type": "tool-input-error", "toolCallId": "call-1", "toolName": "search", "dynamic": true, "input": "bad json", "errorText": "parse error"},
			{"type": "finish-step"},
			{"type": "finish"},
		})
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})
}

// Ported from TS c5e90bb137 (#21480): resuming a hydrated message whose last
// tool part is still in the input-streaming state must continue accumulating
// its input from that part's RawInput, instead of starting from an empty
// accumulator (which previously raised a "missing tool call" error on the
// very next tool-input-delta after reconnecting).
func TestReadUIMessages_ContinuesHydratedPartialStaticToolCall(t *testing.T) {
	message := &UIMessage{
		ID:   "msg-123",
		Role: UIMessageRoleAssistant,
		Parts: []UIMessagePart{
			&ToolUIPart{
				Type:       "tool-createDocument",
				ToolCallID: "tool-1",
				State:      ToolStateInputStreaming,
				Input:      map[string]interface{}{"title": "Hel"},
				RawInput:   `{"title":"Hel`,
			},
		},
	}

	snapshots, err := sendChunks(t, message, []UIMessageChunk{
		{"type": "tool-input-delta", "toolCallId": "tool-1", "inputTextDelta": `lo"}`},
		{"type": "tool-input-available", "toolCallId": "tool-1", "toolName": "createDocument", "input": map[string]interface{}{"title": "Hello"}},
	})
	require.NoError(t, err)
	require.Len(t, snapshots, 2)

	deltaPart := findToolUIMessagePart(snapshots[0], "tool-createDocument")
	require.NotNil(t, deltaPart)
	assert.Equal(t, ToolStateInputStreaming, deltaPart.State)
	assert.Equal(t, map[string]interface{}{"title": "Hello"}, deltaPart.Input)
	assert.Equal(t, `{"title":"Hello"}`, deltaPart.RawInput)

	final := findToolUIMessagePart(snapshots[1], "tool-createDocument")
	require.NotNil(t, final)
	assert.Equal(t, ToolStateInputAvailable, final.State)
	assert.Equal(t, map[string]interface{}{"title": "Hello"}, final.Input)
	assert.Nil(t, final.RawInput)
}

// Ported from TS b032d70bf1 (#21615): tool-output-available/tool-output-error
// must prefer the output chunk's own toolMetadata over the stale metadata
// recorded when the tool's input became available, for both static and
// dynamic tools, while still falling back to the existing metadata when the
// output chunk omits it.
func TestReadUIMessages_ToolOutputChunkMetadataOverridesInputMetadata(t *testing.T) {
	snapshots, err := sendChunks(t, nil, []UIMessageChunk{
		{"type": "start", "messageId": "msg-123"},
		{"type": "start-step"},
		{"type": "tool-input-available", "toolCallId": "dynamic-success", "toolName": "tool-name", "input": map[string]interface{}{"query": "test"}, "dynamic": true},
		{"type": "tool-output-available", "toolCallId": "dynamic-success", "output": map[string]interface{}{"result": "provider-result"}, "dynamic": true, "toolMetadata": map[string]interface{}{"phase": "dynamic-output-available"}},
		{"type": "tool-input-available", "toolCallId": "dynamic-error", "toolName": "tool-name", "input": map[string]interface{}{"query": "test"}, "dynamic": true, "toolMetadata": map[string]interface{}{"phase": "dynamic-input"}},
		{"type": "tool-output-error", "toolCallId": "dynamic-error", "errorText": "error-text", "dynamic": true, "toolMetadata": map[string]interface{}{"phase": "dynamic-output-error"}},
		{"type": "tool-input-available", "toolCallId": "static-success", "toolName": "tool-name", "input": map[string]interface{}{"query": "test"}},
		{"type": "tool-output-available", "toolCallId": "static-success", "output": map[string]interface{}{"result": "provider-result"}, "toolMetadata": map[string]interface{}{"phase": "static-output-available"}},
		{"type": "tool-input-available", "toolCallId": "static-error", "toolName": "tool-name", "input": map[string]interface{}{"query": "test"}, "toolMetadata": map[string]interface{}{"phase": "static-input"}},
		{"type": "tool-output-error", "toolCallId": "static-error", "errorText": "error-text", "toolMetadata": map[string]interface{}{"phase": "static-output-error"}},
		{"type": "finish-step"},
		{"type": "finish"},
	})
	require.NoError(t, err)
	final := snapshots[len(snapshots)-1]

	cases := []struct {
		toolCallID, toolType, wantPhase string
	}{
		{"dynamic-success", "dynamic-tool", "dynamic-output-available"},
		{"dynamic-error", "dynamic-tool", "dynamic-output-error"},
		{"static-success", "tool-tool-name", "static-output-available"},
		{"static-error", "tool-tool-name", "static-output-error"},
	}
	for _, c := range cases {
		part := findToolUIMessagePart(final, c.toolType)
		for _, p := range final.Parts {
			if tool, ok := AsToolUIPart(p); ok && tool.ToolCallID == c.toolCallID {
				part = tool
			}
		}
		require.NotNil(t, part, "missing part for %s", c.toolCallID)
		require.NotNil(t, part.ToolMetadata, "missing toolMetadata for %s", c.toolCallID)
		assert.Equal(t, c.wantPhase, part.ToolMetadata["phase"], "unexpected toolMetadata for %s", c.toolCallID)
	}
}
