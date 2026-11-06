package ai

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// StreamTransformStopper stops the source stream of a StreamText call. It is
// the Go form of the `stopStream` function TS passes to every
// experimental_transform factory.
type StreamTransformStopper func()

type streamTransformStopContextKey struct{}

// WithStreamTransformStopper returns a context carrying stop, retrievable by
// a StreamTransformFunc via StreamTransformStopFromContext. processStream
// installs this around every ExperimentalTransform call.
func WithStreamTransformStopper(ctx context.Context, stop StreamTransformStopper) context.Context {
	return context.WithValue(ctx, streamTransformStopContextKey{}, stop)
}

// StreamTransformStopFromContext retrieves the stop function installed by
// WithStreamTransformStopper (TS: the `stopStream` option). Calling it ends
// the call after the current chunk: the provider stream is closed, no further
// source chunks reach any transform, no further steps run, and the call's
// recorded steps, finish reason and total usage come from the finish-step and
// finish chunks the transform itself emitted (matching TS, where the recorded
// result is derived from the transformed stream). ok is false outside of a
// StreamTransformFunc call.
func StreamTransformStopFromContext(ctx context.Context) (stop StreamTransformStopper, ok bool) {
	stop, ok = ctx.Value(streamTransformStopContextKey{}).(StreamTransformStopper)
	return stop, ok
}

// transformRecorder derives the recorded step content from the chunks that
// leave the experimental_transform chain, mirroring the TS eventProcessor
// that sits after the transforms (stream-text.ts). It is only used when
// transforms are configured; without them the raw aggregation is identical.
type transformRecorder struct {
	blocks      blockContent
	content     []types.ContentPart
	toolCalls   []types.ToolCall
	toolResults []types.ToolResult
	sources     []types.SourceContent
	files       []types.GeneratedFileContent
	startStep   *provider.StreamChunk
	finishStep  *provider.StreamChunk
	finish      *provider.StreamChunk
}

func (rec *transformRecorder) resetStep() {
	rec.blocks.reset()
	rec.content = nil
	rec.toolCalls = nil
	rec.toolResults = nil
	rec.sources = nil
	rec.files = nil
	rec.startStep = nil
	rec.finishStep = nil
}

func isPendingProviderResult(tr types.ToolResult) bool {
	return tr.ProviderExecuted && tr.Result == nil && tr.Error == nil && tr.ModelOutput == nil
}

// record folds one transformed chunk into the recorded step.
func (rec *transformRecorder) record(c provider.StreamChunk) {
	switch c.Type {
	case provider.ChunkTypeStartStep:
		rec.resetStep()
		cc := c
		rec.startStep = &cc
	case provider.ChunkTypeTextStart:
		rec.content = rec.blocks.openText(rec.content, c.ID, c.ProviderMetadata)
	case provider.ChunkTypeText:
		rec.content = rec.blocks.textDelta(rec.content, c.ID, c.Text, c.ProviderMetadata)
	case provider.ChunkTypeTextEnd:
		rec.content = rec.blocks.closeText(rec.content, c.ID, c.ProviderMetadata, c.Citations)
	case provider.ChunkTypeReasoningStart:
		rec.content = rec.blocks.openReasoning(rec.content, c.ID, c.ProviderMetadata)
	case provider.ChunkTypeReasoning:
		text := c.Reasoning
		if text == "" {
			text = c.Text
		}
		if text != "" || len(c.ProviderMetadata) > 0 {
			rec.content = rec.blocks.reasoningDelta(rec.content, c.ID, text, c.ProviderMetadata)
		}
	case provider.ChunkTypeReasoningEnd:
		rec.content = rec.blocks.closeReasoning(rec.content, c.ID, c.ProviderMetadata)
	case provider.ChunkTypeToolCall:
		if c.ToolCall == nil {
			return
		}
		call := *c.ToolCall
		syncRawArguments(&call)
		rec.toolCalls = append(rec.toolCalls, call)
		rec.content = append(rec.content, toolCallContentOf(call))
	case provider.ChunkTypeToolResult:
		if c.ToolResult == nil || c.ToolResult.Preliminary {
			return
		}
		tr := *c.ToolResult
		if isPendingProviderResult(tr) {
			return
		}
		for _, existing := range rec.toolResults {
			if existing.ToolCallID == tr.ToolCallID && existing.ProviderExecuted == tr.ProviderExecuted {
				return
			}
		}
		rec.toolResults = append(rec.toolResults, tr)
		rec.content = append(rec.content, toolResultContentFromToolResult(tr))
	case provider.ChunkTypeToolApprovalRequest:
		if c.ToolApprovalRequest != nil {
			rec.content = append(rec.content, *c.ToolApprovalRequest)
		}
	case provider.ChunkTypeToolApprovalResponse:
		if c.ToolApprovalResponse != nil {
			rec.content = append(rec.content, *c.ToolApprovalResponse)
		}
	case provider.ChunkTypeSource:
		if c.SourceContent != nil {
			rec.sources = append(rec.sources, *c.SourceContent)
			rec.content = append(rec.content, *c.SourceContent)
		}
	case provider.ChunkTypeFile:
		if c.GeneratedFileContent != nil {
			rec.files = append(rec.files, *c.GeneratedFileContent)
			rec.content = append(rec.content, *c.GeneratedFileContent)
		}
	case provider.ChunkTypeCustom:
		if c.CustomContent != nil {
			rec.content = append(rec.content, *c.CustomContent)
		}
	case provider.ChunkTypeReasoningFile:
		if c.ReasoningFileContent != nil {
			rec.content = append(rec.content, *c.ReasoningFileContent)
		}
	case provider.ChunkTypeFinishStep:
		cc := c
		rec.finishStep = &cc
	case provider.ChunkTypeFinish:
		cc := c
		rec.finish = &cc
	}
}

// syncRawArguments keeps RawArguments (the string TS `input` replay uses) in
// step with Arguments when a transform edited the decoded input.
func syncRawArguments(call *types.ToolCall) {
	if call.RawArguments == "" || call.Arguments == nil {
		return
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(call.RawArguments), &decoded); err == nil && reflect.DeepEqual(decoded, call.Arguments) {
		return
	}
	if raw, err := jsonstr.Marshal(call.Arguments); err == nil {
		call.RawArguments = string(raw)
	}
}

func toolCallContentOf(call types.ToolCall) types.ToolCallContent {
	return types.ToolCallContent{
		ToolCallID:       call.ID,
		ToolName:         call.ToolName,
		Title:            call.Title,
		Input:            call.RawArguments,
		Arguments:        call.Arguments,
		ProviderExecuted: call.ProviderExecuted,
		ProviderMetadata: providerMetadataRaw(call.ProviderMetadata),
		ToolMetadata:     call.ToolMetadata,
		Dynamic:          call.Dynamic,
		Invalid:          call.Invalid,
		Error:            toolCallContentError(call.Error),
		ThoughtSignature: call.ThoughtSignature,
	}
}
