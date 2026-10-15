package workflow

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// ModelCallStreamPart is one part of a durable workflow model-call stream,
// the Go counterpart of TS `ModelCallStreamPart` from
// `packages/workflow/src/model-call.ts`. It is a provider stream chunk; the two
// extra TS members map to ChunkTypeToolApprovalRequest (with Signature and
// InputSchemaInput) and ChunkTypeResetStep.
type ModelCallStreamPart = provider.StreamChunk

// ChunkTypeResetStep marks a retried model-call step: parts streamed by the
// invalidated attempt must be forgotten (TS `{ type: 'reset-step' }`).
const ChunkTypeResetStep provider.ChunkType = "reset-step"

func chunkProviderMetadata(raw json.RawMessage) (map[string]interface{}, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var md map[string]interface{}
	if err := json.Unmarshal(raw, &md); err != nil || md == nil {
		return nil, false
	}
	return md, true
}

// ToUIMessageChunk converts a single ModelCallStreamPart to a UI message chunk
// (TS `toUIMessageChunk` from `@ai-sdk/workflow`). The boolean is false for
// parts that do not map to a UI chunk. It differs from ai.ToUIMessageChunk:
// errors are not masked (errorText is the error message), tool results map to
// tool-output-available / tool-output-error, and step boundaries pass through.
func ToUIMessageChunk(part ModelCallStreamPart) (ai.UIMessageChunk, bool) {
	withMeta := func(c ai.UIMessageChunk) ai.UIMessageChunk {
		if md, ok := chunkProviderMetadata(part.ProviderMetadata); ok {
			c["providerMetadata"] = md
		}
		return c
	}
	switch part.Type {
	case ChunkTypeResetStep:
		return ai.UIMessageChunk{"type": "reset-step"}, true
	case provider.ChunkTypeTextStart:
		return withMeta(ai.UIMessageChunk{"type": "text-start", "id": part.ID}), true
	case provider.ChunkTypeText:
		return withMeta(ai.UIMessageChunk{"type": "text-delta", "id": part.ID, "delta": part.Text}), true
	case provider.ChunkTypeTextEnd:
		return withMeta(ai.UIMessageChunk{"type": "text-end", "id": part.ID}), true
	case provider.ChunkTypeReasoningStart:
		return withMeta(ai.UIMessageChunk{"type": "reasoning-start", "id": part.ID}), true
	case provider.ChunkTypeReasoning:
		delta := part.Reasoning
		if delta == "" {
			delta = part.Text
		}
		return withMeta(ai.UIMessageChunk{"type": "reasoning-delta", "id": part.ID, "delta": delta}), true
	case provider.ChunkTypeReasoningEnd:
		return withMeta(ai.UIMessageChunk{"type": "reasoning-end", "id": part.ID}), true

	case provider.ChunkTypeFile:
		if part.GeneratedFileContent == nil {
			return nil, false
		}
		// Same data: URL derivation as the core converter; the workflow
		// converter does not forward provider metadata on files.
		c, ok := ai.ToUIMessageChunk(provider.StreamChunk{
			Type:                 provider.ChunkTypeFile,
			GeneratedFileContent: part.GeneratedFileContent,
		}, ai.UIMessageStreamResultOptions{})
		return c, ok

	case provider.ChunkTypeSource:
		src := part.SourceContent
		if src == nil {
			return nil, false
		}
		switch src.SourceType {
		case "url":
			c := ai.UIMessageChunk{"type": "source-url", "sourceId": src.ID, "url": src.URL}
			if src.Title != "" {
				c["title"] = src.Title
			}
			return withMeta(c), true
		case "document":
			c := ai.UIMessageChunk{"type": "source-document", "sourceId": src.ID, "mediaType": src.MediaType}
			if src.Title != "" {
				c["title"] = src.Title
			}
			if src.Filename != "" {
				c["filename"] = src.Filename
			}
			return withMeta(c), true
		}
		return nil, false

	case provider.ChunkTypeToolInputStart:
		if part.ToolCall == nil {
			return nil, false
		}
		c := ai.UIMessageChunk{"type": "tool-input-start", "toolCallId": part.ToolCall.ID, "toolName": part.ToolCall.ToolName}
		if part.ToolCall.ProviderExecuted {
			c["providerExecuted"] = true
		}
		return c, true
	case provider.ChunkTypeToolInputDelta:
		return ai.UIMessageChunk{"type": "tool-input-delta", "toolCallId": part.ID, "inputTextDelta": part.Text}, true

	case provider.ChunkTypeToolCall:
		tc := part.ToolCall
		if tc == nil {
			return nil, false
		}
		if tc.Invalid {
			errorText := "Invalid tool call"
			if tc.Error != nil {
				errorText = tc.Error.Error()
			}
			return ai.UIMessageChunk{
				"type":       "tool-input-error",
				"toolCallId": tc.ID,
				"toolName":   tc.ToolName,
				"input":      ai.InvalidToolCallInput(*tc),
				"errorText":  errorText,
			}, true
		}
		c := ai.UIMessageChunk{"type": "tool-input-available", "toolCallId": tc.ID, "toolName": tc.ToolName, "input": tc.Arguments}
		if tc.ProviderExecuted {
			c["providerExecuted"] = true
		}
		if len(tc.ProviderMetadata) > 0 {
			c["providerMetadata"] = tc.ProviderMetadata
		} else if md, ok := chunkProviderMetadata(part.ProviderMetadata); ok {
			c["providerMetadata"] = md
		}
		return c, true

	case provider.ChunkTypeToolResult:
		tr := part.ToolResult
		if tr == nil {
			return nil, false
		}
		if tr.Error != nil {
			return ai.UIMessageChunk{"type": "tool-output-error", "toolCallId": tr.ToolCallID, "errorText": tr.Error.Error()}, true
		}
		return ai.UIMessageChunk{"type": "tool-output-available", "toolCallId": tr.ToolCallID, "output": tr.Result}, true

	case provider.ChunkTypeError:
		text := part.Text
		if part.Err != nil {
			text = part.Err.Error()
		}
		return ai.UIMessageChunk{"type": "error", "errorText": text}, true

	case provider.ChunkTypeToolApprovalRequest:
		req := part.ToolApprovalRequest
		if req == nil {
			return nil, false
		}
		id := req.ToolCallID
		if id == "" {
			id = req.ToolCall.ID
		}
		c := ai.UIMessageChunk{"type": "tool-approval-request", "approvalId": req.ApprovalID, "toolCallId": id}
		if req.InputSchemaInput != nil {
			c["inputSchemaInput"] = req.InputSchemaInput
		}
		if req.Signature != "" {
			c["signature"] = req.Signature
		}
		return c, true

	// Step boundaries are written by the WorkflowAgent between tool execution
	// and the next model step so convertToModelMessages splits messages.
	case provider.ChunkTypeFinishStep:
		return ai.UIMessageChunk{"type": "finish-step"}, true
	case provider.ChunkTypeStartStep:
		return ai.UIMessageChunk{"type": "start-step"}, true
	case provider.ChunkTypeToolOutputDenied:
		id := ""
		if part.ToolResult != nil {
			id = part.ToolResult.ToolCallID
		}
		return ai.UIMessageChunk{"type": "tool-output-denied", "toolCallId": id}, true
	}
	// tool-input-end, model-call start/metadata/end, raw and anything else.
	return nil, false
}

// ModelCallToUIChunkTransformOptions configures CreateModelCallToUIChunkTransform.
type ModelCallToUIChunkTransformOptions struct {
	// UIStartIndex is the number of transformed UI chunks to omit from the
	// output when resuming. Replay the source ModelCallStreamPart stream from
	// index 0: raw parts and UI chunks are not one-to-one, so a UI chunk index
	// must not be used as the source stream's start index. Must be >= 0.
	UIStartIndex int
}

// ModelCallToUIChunkTransform converts ModelCallStreamPart to UI message
// chunks, wrapping them with start/start-step and finish-step/finish
// lifecycle chunks (TS `createModelCallToUIChunkTransform`). It is stateful
// and single use.
type ModelCallToUIChunkTransform struct {
	uiStartIndex int
	uiChunkIndex int
}

// CreateModelCallToUIChunkTransform validates the options and returns a new
// transform. TS throws RangeError for an invalid uiStartIndex; Go returns the
// same message as an error.
func CreateModelCallToUIChunkTransform(opts ...ModelCallToUIChunkTransformOptions) (*ModelCallToUIChunkTransform, error) {
	var o ModelCallToUIChunkTransformOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.UIStartIndex < 0 {
		return nil, errors.New("uiStartIndex must be a non-negative safe integer")
	}
	return &ModelCallToUIChunkTransform{uiStartIndex: o.UIStartIndex}, nil
}

func (t *ModelCallToUIChunkTransform) emit(chunks ...ai.UIMessageChunk) []ai.UIMessageChunk {
	var out []ai.UIMessageChunk
	for _, c := range chunks {
		if t.uiChunkIndex >= t.uiStartIndex {
			out = append(out, c)
		}
		t.uiChunkIndex++
	}
	return out
}

// Start returns the lifecycle chunks that open the stream (start, start-step).
func (t *ModelCallToUIChunkTransform) Start() []ai.UIMessageChunk {
	return t.emit(ai.UIMessageChunk{"type": "start"}, ai.UIMessageChunk{"type": "start-step"})
}

// Transform converts one part; it returns nil when the part produces no UI
// chunk or the chunk falls before UIStartIndex.
func (t *ModelCallToUIChunkTransform) Transform(part ModelCallStreamPart) []ai.UIMessageChunk {
	c, ok := ToUIMessageChunk(part)
	if !ok {
		return nil
	}
	return t.emit(c)
}

// Flush returns the lifecycle chunks that close the stream (finish-step,
// finish).
func (t *ModelCallToUIChunkTransform) Flush() []ai.UIMessageChunk {
	return t.emit(ai.UIMessageChunk{"type": "finish-step"}, ai.UIMessageChunk{"type": "finish"})
}

// Pipe runs the transform over a part channel: Start, Transform per part, then
// Flush when in closes. The output closes after Flush, or early when ctx is
// cancelled (without Flush).
func (t *ModelCallToUIChunkTransform) Pipe(ctx context.Context, in <-chan ModelCallStreamPart) <-chan ai.UIMessageChunk {
	out := make(chan ai.UIMessageChunk)
	go func() {
		defer close(out)
		send := func(chunks []ai.UIMessageChunk) bool {
			for _, c := range chunks {
				select {
				case out <- c:
				case <-ctx.Done():
					return false
				}
			}
			return true
		}
		if !send(t.Start()) {
			return
		}
		for {
			select {
			case part, ok := <-in:
				if !ok {
					send(t.Flush())
					return
				}
				if !send(t.Transform(part)) {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// NormalizeUIMessageStreamParts repairs the part framing of a UI message
// chunk stream so it is well formed for the UI stream consumer (TS
// `normalizeUIMessageStreamParts`): a missing "*-start" is synthesized for an
// orphaned delta or end, re-delivered start/delta/end chunks for an already
// open or ended part are dropped, and reset-step starts a new frame. A
// well-formed stream passes through unchanged. The output closes when in
// closes or ctx is cancelled.
func NormalizeUIMessageStreamParts(ctx context.Context, in <-chan ai.UIMessageChunk) <-chan ai.UIMessageChunk {
	out := make(chan ai.UIMessageChunk)
	go func() {
		defer close(out)
		n := newUIStreamNormalizer()
		for {
			select {
			case chunk, ok := <-in:
				if !ok {
					return
				}
				for _, c := range n.normalize(chunk) {
					select {
					case out <- c:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
