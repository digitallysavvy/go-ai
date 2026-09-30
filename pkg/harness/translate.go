package harness

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranslateOptions configures TranslatePart. Mirrors TS
// `translateStreamPart`'s options parameter.
type TranslateOptions struct {
	// IsProviderExecuted reports whether the tool call that produced a
	// tool-result event ran inside the harness runtime. Host results are
	// echoed back as tool-result events too, so the event alone cannot say
	// who ran the tool — only the originating tool-call can, and correlating
	// the two is the caller's job (run_prompt.go tracks it). A nil func
	// treats every failure as provider-executed, matching TS's `?? true`
	// default.
	IsProviderExecuted func(toolCallID string) bool
}

func (o TranslateOptions) isProviderExecuted(toolCallID string) bool {
	if o.IsProviderExecuted == nil {
		return true
	}
	return o.IsProviderExecuted(toolCallID)
}

// TranslatePart translates one harness stream part into zero or more
// provider.StreamChunk values consumed by ai.NewStreamTextResultFromParts.
// Mirrors TS `translateStreamPart`.
//
// Most variants are close to the identity function. The adapters are:
//   - harnessMetadata -> ProviderMetadata (JSON)
//   - tool-call is not translated here: schema validation against the merged
//     tool set is done by run_prompt.go, which has the tool set in scope.
//   - a failed tool-result from a provider-executed tool becomes a
//     types.ToolResult with a non-nil Error (mirrors TS's "tool-error").
//   - file-change and compaction have no first-class provider.StreamChunk
//     equivalent: each fans out into a synthetic dynamic + provider-executed
//     tool-call / tool-result pair (reserved names "fileChange" /
//     "compaction") so the event stays observable, exactly like TS.
//   - stream-start, finish-step and finish are handled by run_prompt.go,
//     which owns step/turn boundaries; this function returns nil for them.
func TranslatePart(part StreamPart, opts TranslateOptions) []provider.StreamChunk {
	switch p := part.(type) {
	case *StreamStartPart:
		// Unlike TS (which discards this — HarnessStreamTextResult emits its
		// own message-level "start" part instead), Go's external-stream seam
		// uses provider.ChunkTypeStreamStart to record pre-stream warnings on
		// the step. Forwarding it is a strict improvement with no observable
		// behavior change for callers that ignore warnings.
		if len(p.Warnings) == 0 {
			return nil
		}
		return []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: convertWarnings(p.Warnings)}}

	case *TextStartPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: p.ID, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *TextDeltaPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeText, ID: p.ID, Text: p.Delta, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *TextEndPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeTextEnd, ID: p.ID, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *ReasoningStartPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeReasoningStart, ID: p.ID, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *ReasoningDeltaPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeReasoning, ID: p.ID, Reasoning: p.Delta, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *ReasoningEndPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeReasoningEnd, ID: p.ID, ProviderMetadata: marshalMetadata(p.HarnessMetadata)}}

	case *ToolInputStartPart:
		return []provider.StreamChunk{{
			Type: provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{
				ID: p.ID, ToolName: p.ToolName, Title: p.Title,
				ProviderExecuted: p.ProviderExecuted, Dynamic: p.Dynamic,
				ProviderMetadata: convertProviderMetadata(p.ProviderMetadata),
			},
		}}

	case *ToolInputDeltaPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeToolInputDelta, ID: p.ID, Text: p.Delta}}

	case *ToolInputEndPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: p.ID}}}

	case *ToolCallPart:
		// Validated and emitted by run_prompt.go (async schema parsing needs
		// the merged tool set in scope).
		return nil

	case *ToolApprovalRequestPart:
		// Handled directly by run_prompt.go.
		return nil

	case *ToolResultPart:
		if p.IsError && opts.isProviderExecuted(p.ToolCallID) {
			return []provider.StreamChunk{{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: p.ToolCallID, ToolName: p.ToolName,
					Error:            fmt.Errorf("%v", p.Result),
					ProviderExecuted: true,
					Dynamic:          p.Dynamic,
					Preliminary:      p.Preliminary,
					ProviderMetadata: convertProviderMetadata(p.ProviderMetadata),
				},
			}}
		}
		return []provider.StreamChunk{{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: p.ToolCallID, ToolName: p.ToolName, Result: p.Result,
				Dynamic:          p.Dynamic,
				Preliminary:      p.Preliminary,
				ProviderMetadata: convertProviderMetadata(p.ProviderMetadata),
			},
		}}

	case *FileChangePart:
		toolCallID := "harness-file-change-" + newID()
		payload := map[string]interface{}{"event": p.Event, "path": p.Path}
		md := convertProviderMetadata(ProviderMetadata(p.HarnessMetadata))
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
				ID: toolCallID, ToolName: "fileChange", Arguments: payload,
				Dynamic: true, ProviderExecuted: true, ProviderMetadata: md,
			}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID: toolCallID, ToolName: "fileChange", Result: payload,
				Dynamic: true, ProviderExecuted: true, ProviderMetadata: md,
			}},
		}

	case *CompactionPart:
		toolCallID := "harness-compaction-" + newID()
		output := map[string]interface{}{"trigger": p.Trigger, "summary": p.Summary}
		if p.TokensBefore != nil {
			output["tokensBefore"] = *p.TokensBefore
		}
		if p.TokensAfter != nil {
			output["tokensAfter"] = *p.TokensAfter
		}
		md := convertProviderMetadata(ProviderMetadata(p.HarnessMetadata))
		return []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
				ID: toolCallID, ToolName: "compaction", Arguments: map[string]interface{}{},
				Dynamic: true, ProviderExecuted: true, ProviderMetadata: md,
			}},
			{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID: toolCallID, ToolName: "compaction", Result: output,
				Dynamic: true, ProviderExecuted: true, ProviderMetadata: md,
			}},
		}

	case *ErrorPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeError, Text: fmt.Sprintf("%v", p.Error)}}

	case *RawPart:
		return []provider.StreamChunk{{Type: provider.ChunkTypeRaw, Raw: p.RawValue}}

	case *FinishStepPart, *FinishPart:
		// Step/turn boundaries are owned by run_prompt.go, which assembles
		// the surrounding lifecycle (host tool completion, telemetry) before
		// emitting the matching provider.ChunkTypeFinishStep/Finish itself.
		return nil

	default:
		return nil
	}
}

// newID generates a short random identifier for synthetic tool calls
// (file-change / compaction), mirroring TS's use of `generateId()` there.
func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func convertWarnings(warnings []CallWarning) []types.Warning {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]types.Warning, len(warnings))
	for i, w := range warnings {
		out[i] = types.Warning{Type: string(w.Type), Setting: w.Setting, Details: w.Details, Message: w.Message}
		if w.Type == CallWarningUnsupportedTool {
			out[i].Feature = w.Tool
		}
	}
	return out
}

func convertProviderMetadata(md ProviderMetadata) map[string]interface{} {
	if len(md) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(md))
	for k, v := range md {
		out[k] = v
	}
	return out
}

func marshalMetadata(md Metadata) json.RawMessage {
	if len(md) == 0 {
		return nil
	}
	raw, err := json.Marshal(md)
	if err != nil {
		return nil
	}
	return raw
}
