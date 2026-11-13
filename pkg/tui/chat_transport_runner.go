package tui

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// toolCallMeta remembers the tool name/title first seen for a tool call ID so
// later chunks that only carry the ID (tool-input-delta, tool-approval-*,
// tool-output-*) can still populate a fully-formed provider.StreamChunk for
// the terminal renderer.
type toolCallMeta struct {
	name  string
	title string
}

// streamViaTransport drives one turn through a ChatTransport instead of an
// agent.Agent, mirroring TS AgentTUIRunner.streamMessages' transport branch
// (agent-tui-runner.ts). It sends the current message history and adapts the
// returned UI message chunk stream into an AgentTUIStreamResult the existing
// TerminalRenderer can consume.
//
// seed is the assistant message of the previous turn when this request resumes
// it (after a tool approval); the response chunks continue that message, as
// TS readUIMessageStream({ message: result.message }) does.
func (r *AgentTUIRunner) streamViaTransport(ctx context.Context, messages []types.Message, seed *ai.UIMessage) (AgentTUIStreamResult, error) {
	chunks, errs := r.transport.SendMessages(ctx, ai.ChatTransportSendMessagesRequest{
		ChatID:   r.chatID,
		Trigger:  "submit-message",
		Messages: append([]types.Message(nil), messages...),
	})

	stream := newTransportTextStream(chunks, errs)
	stream.seed = seed
	src := NewStreamRenderSourceFromTextStream(stream, nil, types.StepResult{})
	stream.target = src
	return AgentTUIStreamResult{Stream: src}, nil
}

// transportTextStream adapts a ChatTransport's (<-chan ai.UIMessageChunk,
// <-chan error) response into a provider.TextStream so it can drive the
// existing StreamRenderSource/TerminalRenderer pipeline. Once the underlying
// channels are drained it reconstructs the final []types.Message (including
// any types.ToolApprovalRequestContent) and writes it back onto the
// StreamRenderSource it was created for, so AgentTUIRunner.Run can continue
// its existing (agent.Agent-shaped) approval-detection logic unmodified.
type transportTextStream struct {
	chunks <-chan ai.UIMessageChunk
	errs   <-chan error

	// target is populated by streamViaTransport immediately after
	// construction; Next() writes the final response messages onto it once
	// the stream completes.
	target *StreamRenderSource

	// seed is the assistant message this response continues, if any.
	seed *ai.UIMessage

	done chan struct{}

	mu       sync.Mutex
	closed   bool
	finished bool
	err      error

	raw      []ai.UIMessageChunk
	toolMeta map[string]toolCallMeta
	// approvalToToolCall maps an approvalId back to the toolCallId it was
	// requested for, since tool-approval-response chunks only carry the
	// approvalId.
	approvalToToolCall map[string]string
}

func newTransportTextStream(chunks <-chan ai.UIMessageChunk, errs <-chan error) *transportTextStream {
	return &transportTextStream{
		chunks:             chunks,
		errs:               errs,
		done:               make(chan struct{}),
		toolMeta:           map[string]toolCallMeta{},
		approvalToToolCall: map[string]string{},
	}
}

func (t *transportTextStream) Next() (*provider.StreamChunk, error) {
	for {
		t.mu.Lock()
		if t.finished {
			err := t.err
			t.mu.Unlock()
			if err != nil {
				return nil, err
			}
			return nil, io.EOF
		}
		t.mu.Unlock()

		select {
		case <-t.done:
			continue
		case chunk, ok := <-t.chunks:
			if !ok {
				t.chunks = nil
				// A transport sends its failure on errs before closing both
				// channels, so a ready error must not lose to the closed chunk
				// channel in the select above.
				t.finish(t.pendingErr())
				continue
			}
			t.mu.Lock()
			t.raw = append(t.raw, chunk)
			t.mu.Unlock()
			if pc, handled := t.convert(chunk); handled {
				return pc, nil
			}
		case err, ok := <-t.errs:
			if !ok {
				t.errs = nil
				continue
			}
			if err != nil {
				t.finish(err)
				continue
			}
		}
	}
}

// pendingErr returns a transport error that is already waiting on errs.
func (t *transportTextStream) pendingErr() error {
	for t.errs != nil {
		select {
		case err, ok := <-t.errs:
			if !ok {
				t.errs = nil
				return nil
			}
			if err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

func (t *transportTextStream) convert(chunk ai.UIMessageChunk) (*provider.StreamChunk, bool) {
	return uiMessageChunkToProviderChunk(chunk, t.toolMeta, t.approvalToToolCall)
}

func (t *transportTextStream) finish(err error) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.finished = true
	t.err = err
	raw := t.raw
	target := t.target
	t.mu.Unlock()
	close(t.done)

	if err != nil || target == nil {
		return
	}
	final, messages, step := buildTransportResult(raw, t.seed)
	target.responseMessages = messages
	target.finalStep = step
	target.usage = step.Usage
	target.uiMessage = final
}

func (t *transportTextStream) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil && !errors.Is(t.err, io.EOF) {
		return t.err
	}
	return nil
}

func (t *transportTextStream) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.mu.Unlock()
	t.finish(nil)
	return nil
}

// buildTransportResult replays the raw UI message chunks captured during
// streaming through ai.ReadUIMessages to obtain the final UIMessage snapshot,
// then converts it into model messages via ai.ConvertToModelMessages so
// AgentTUIRunner's existing tool-approval-request detection (which inspects
// types.Message content parts) keeps working unmodified.
//
// seed, when set, is the assistant message the response continues: without it
// the output of an approved tool (a tool-output chunk for a call that is not
// in the replayed message) makes the whole response unreadable.
func buildTransportResult(raw []ai.UIMessageChunk, seed *ai.UIMessage) (*ai.UIMessage, []types.Message, types.StepResult) {
	if len(raw) == 0 {
		return nil, nil, types.StepResult{}
	}

	replay := make(chan ai.UIMessageChunk, len(raw))
	for _, chunk := range raw {
		replay <- chunk
	}
	close(replay)

	msgCh, errCh := ai.ReadUIMessages(context.Background(), ai.ReadUIMessagesOptions{Stream: replay, Message: seed})
	var final ai.UIMessage
	var got bool
	for msg := range msgCh {
		final = msg
		got = true
	}
	if err := <-errCh; err != nil || !got {
		return nil, nil, types.StepResult{}
	}

	modelMessages, err := ai.ConvertToModelMessages(context.Background(), []ai.UIMessage{final})
	if err != nil {
		return nil, nil, types.StepResult{}
	}
	return &final, modelMessages, stepResultFromFinishChunk(raw)
}

// applyUIToolApproval records the user's decision on the tool part of a UI
// message (TS applyToolApprovalResponse: state approval-responded plus the
// approval object), so the next request resumes the right call.
func applyUIToolApproval(message *ai.UIMessage, toolCallID string, approvalID string, response AgentTUIToolApprovalResponse) {
	if message == nil {
		return
	}
	for _, part := range message.Parts {
		tool, ok := part.(*ai.ToolUIPart)
		if !ok || tool.ToolCallID != toolCallID {
			continue
		}
		approved := response.Approved
		tool.State = ai.ToolStateApprovalResponded
		tool.Approval = &ai.ToolUIPartApproval{ID: approvalID, Approved: &approved, Reason: response.Reason}
		return
	}
}

// stepResultFromFinishChunk best-effort extracts usage/performance from a
// "finish" chunk's messageMetadata, matching the shape TS's
// createResponseMetadata (agent-tui-runner.ts) writes: { usage: {
// totalTokens, outputTokens }, performance: { outputTokensPerSecond } }.
// Transports that don't populate messageMetadata simply yield a zero-value
// StepResult, so the renderer just omits response statistics.
func stepResultFromFinishChunk(raw []ai.UIMessageChunk) types.StepResult {
	for i := len(raw) - 1; i >= 0; i-- {
		chunk := raw[i]
		if stringField(chunk, "type") != "finish" {
			continue
		}
		metadata, _ := chunk["messageMetadata"].(map[string]interface{})
		if metadata == nil {
			return types.StepResult{}
		}
		var step types.StepResult
		if usage, ok := metadata["usage"].(map[string]interface{}); ok {
			if total, ok := numberField(usage, "totalTokens"); ok {
				step.Usage.TotalTokens = &total
			}
			if output, ok := numberField(usage, "outputTokens"); ok {
				step.Usage.OutputTokens = &output
			}
		}
		if perf, ok := metadata["performance"].(map[string]interface{}); ok {
			if tps, ok := floatField(perf, "outputTokensPerSecond"); ok {
				step.Performance.OutputTokensPerSecond = &tps
			}
		}
		return step
	}
	return types.StepResult{}
}

// uiMessageChunkToProviderChunk converts one ai.UIMessageChunk (the map-based
// UI message stream chunk shape sent by a ChatTransport) into the
// provider.StreamChunk vocabulary the TerminalRenderer already knows how to
// render, mirroring (in reverse) pkg/ai's
// convertProviderChunkToUIMessageChunks. Only the chunk types the renderer
// actually acts on (text/reasoning blocks, tool input/call/result, finish,
// error, abort) are translated; other chunk types (source-url, file,
// start-step, ...) are acknowledged but not renderable today, matching what
// TerminalRenderer's render model already ignores from a native agent.Agent
// stream. toolMeta/approvalToToolCall accumulate state across calls so later
// chunks that only carry an ID can still resolve a tool's name/title.
func uiMessageChunkToProviderChunk(chunk ai.UIMessageChunk, toolMeta map[string]toolCallMeta, approvalToToolCall map[string]string) (*provider.StreamChunk, bool) {
	chunkType := stringField(chunk, "type")
	switch chunkType {
	case "text-start":
		return &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: stringField(chunk, "id")}, true
	case "text-delta":
		return &provider.StreamChunk{Type: provider.ChunkTypeText, ID: stringField(chunk, "id"), Text: stringField(chunk, "delta")}, true
	case "text-end":
		return &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: stringField(chunk, "id")}, true

	case "reasoning-start":
		return &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: stringField(chunk, "id")}, true
	case "reasoning-delta":
		return &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: stringField(chunk, "id"), Reasoning: stringField(chunk, "delta")}, true
	case "reasoning-end":
		return &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: stringField(chunk, "id")}, true

	case "tool-input-start":
		id := stringField(chunk, "toolCallId")
		name := stringField(chunk, "toolName")
		title := stringField(chunk, "title")
		rememberToolMeta(toolMeta, id, name, title)
		return &provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{
			ID: id, ToolName: name, Title: title,
			ProviderExecuted: boolField(chunk, "providerExecuted"),
			Dynamic:          boolField(chunk, "dynamic"),
		}}, true
	case "tool-input-delta":
		return &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: stringField(chunk, "toolCallId"), Text: stringField(chunk, "inputTextDelta")}, true
	case "tool-input-available":
		id := stringField(chunk, "toolCallId")
		name := stringField(chunk, "toolName")
		title := stringField(chunk, "title")
		rememberToolMeta(toolMeta, id, name, title)
		return &provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
			ID: id, ToolName: name, Title: title,
			Arguments:        mapField(chunk, "input"),
			ProviderExecuted: boolField(chunk, "providerExecuted"),
			Dynamic:          boolField(chunk, "dynamic"),
		}}, true
	case "tool-input-error":
		// TS readUIMessageStream moves the part to output-error with the input
		// and errorText, which the renderer shows as a tool error section.
		id := stringField(chunk, "toolCallId")
		rememberToolMeta(toolMeta, id, stringField(chunk, "toolName"), stringField(chunk, "title"))
		meta := toolMeta[id]
		// TS statusForStreamChunk has no case for tool-input-error, so the
		// status line is left alone (toolInputErrorMarker tells observeStatus).
		return &provider.StreamChunk{Type: provider.ChunkTypeToolResult, Raw: toolInputErrorMarker{}, ToolResult: &types.ToolResult{
			ToolCallID: id, ToolName: meta.name, Title: meta.title,
			Input: mapField(chunk, "input"),
			Error: errors.New(stringField(chunk, "errorText")),
		}}, true

	case "tool-output-available":
		id := stringField(chunk, "toolCallId")
		meta := toolMeta[id]
		return &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
			ToolCallID: id, ToolName: meta.name, Title: meta.title,
			Result: chunk["output"],
		}}, true
	case "tool-output-error":
		id := stringField(chunk, "toolCallId")
		meta := toolMeta[id]
		return &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
			ToolCallID: id, ToolName: meta.name, Title: meta.title,
			Error: errors.New(stringField(chunk, "errorText")),
		}}, true
	case "tool-output-denied":
		// The reason shown is the one of the earlier approval response.
		id := stringField(chunk, "toolCallId")
		meta := toolMeta[id]
		return &provider.StreamChunk{Type: provider.ChunkTypeToolOutputDenied, ToolResult: &types.ToolResult{
			ToolCallID: id, ToolName: meta.name, Title: meta.title,
		}}, true

	case "tool-approval-request":
		id := stringField(chunk, "toolCallId")
		approvalID := stringField(chunk, "approvalId")
		if approvalID != "" && id != "" {
			approvalToToolCall[approvalID] = id
		}
		meta := toolMeta[id]
		return &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
			ToolCallID: id, ToolName: meta.name, Title: meta.title,
			ApprovalStatus: types.ToolApprovalStatusUserApproval,
			ApprovalID:     approvalID,
		}}, true
	case "tool-approval-response":
		// The renderer maps the approval id back to its tool call: approved
		// shows executing, denied the "denied" status with the reason (TS
		// approval-responded); only a tool-output-denied chunk renders the
		// tool as denied output.
		approved := boolField(chunk, "approved")
		approvalID := stringField(chunk, "approvalId")
		return &provider.StreamChunk{Type: provider.ChunkTypeToolApprovalResponse, ToolApprovalResponse: &types.ToolApprovalResponseContent{
			ApprovalID: approvalID, ToolCallID: approvalToToolCall[approvalID], Approved: approved, Reason: stringField(chunk, "reason"),
		}}, true

	case "start-step":
		return &provider.StreamChunk{Type: provider.ChunkTypeStartStep}, true

	case "error":
		return &provider.StreamChunk{Type: provider.ChunkTypeError, Text: stringField(chunk, "errorText")}, true
	case "abort":
		return &provider.StreamChunk{Type: provider.ChunkTypeAbort, AbortReason: stringField(chunk, "reason")}, true
	case "finish":
		return &provider.StreamChunk{Type: provider.ChunkTypeFinish}, true

	default:
		// "start", "finish-step", "source-url",
		// "source-document", "file", "reasoning-file", "custom",
		// "message-metadata", "tool-output-denied" variants, and any unknown
		// chunk type: acknowledged (already appended to raw) but not
		// rendered as a provider.StreamChunk.
		return nil, false
	}
}

// toolInputErrorMarker tags the chunk made from a tool-input-error UI chunk.
type toolInputErrorMarker struct{}

func rememberToolMeta(meta map[string]toolCallMeta, id, name, title string) {
	if id == "" {
		return
	}
	entry := meta[id]
	if name != "" {
		entry.name = name
	}
	if title != "" {
		entry.title = title
	}
	meta[id] = entry
}

func stringField(chunk ai.UIMessageChunk, key string) string {
	s, _ := chunk[key].(string)
	return s
}

func boolField(chunk ai.UIMessageChunk, key string) bool {
	b, _ := chunk[key].(bool)
	return b
}

func mapField(chunk ai.UIMessageChunk, key string) map[string]interface{} {
	m, _ := chunk[key].(map[string]interface{})
	return m
}

func numberField(m map[string]interface{}, key string) (int64, bool) {
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func floatField(m map[string]interface{}, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case int64:
		return float64(v), true
	case int:
		return float64(v), true
	}
	return 0, false
}
