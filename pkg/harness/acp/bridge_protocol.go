package acp

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge"
)

// ACP-specific outbound frame discriminators (beyond the shared harness-v1
// vocabulary). Mirrors TS `outboundMessageSchema`'s extra union members.
const (
	TypeToolCallCandidate = "acp-tool-call-candidate"
	TypeQuestionRequest   = "acp-question-request"
	TypeQuestionResolved  = "acp-question-resolved"
)

// ToolCallCandidateFrame asks the host to classify a native tool call the
// bridge could not resolve to a harness tool name: is it MCP-routed
// (isMcpToolCall) or the implementation's native askUserQuestions mechanism?
// The host replies with a `tool-result` frame keyed by RequestID (not the
// tool call's own id).
type ToolCallCandidateFrame struct {
	RequestID string   `json:"requestId"`
	ToolCall  ToolCall `json:"toolCall"`
}

// FrameType returns "acp-tool-call-candidate".
func (ToolCallCandidateFrame) FrameType() string { return TypeToolCallCandidate }

// QuestionRequestFrame asks the host to translate one native clarifying
// question into an askUserQuestions tool call (or report it unhandled). The
// host replies with a `tool-result` frame keyed by RequestID.
type QuestionRequestFrame struct {
	RequestID      string          `json:"requestId"`
	NativeRequest  json.RawMessage `json:"nativeRequest"`
	NativeToolCall *ToolCall       `json:"nativeToolCall,omitempty"`
}

// FrameType returns "acp-question-request".
func (QuestionRequestFrame) FrameType() string { return TypeQuestionRequest }

// QuestionResolvedFrame tells the host a previously forwarded native
// question no longer needs a response (e.g. the implementation cancelled
// it).
type QuestionResolvedFrame struct {
	RequestID string `json:"requestId"`
}

// FrameType returns "acp-question-resolved".
func (QuestionResolvedFrame) FrameType() string { return TypeQuestionResolved }

// decodeOutbound wraps bridge.DecodeOutbound with the three ACP-specific
// extension frames above. Mirrors TS `outboundMessageSchema`.
func decodeOutbound(data []byte) (bridge.OutboundMessage, *float64, error) {
	typ, fields, err := harness.ReadTagged(data)
	if err != nil {
		return nil, nil, err
	}
	var seq *float64
	if raw, ok := fields["seq"]; ok {
		var s float64
		if json.Unmarshal(raw, &s) == nil {
			seq = &s
		}
	}
	switch typ {
	case TypeToolCallCandidate:
		var f ToolCallCandidateFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, seq, err
		}
		return f, seq, nil
	case TypeQuestionRequest:
		var f QuestionRequestFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, seq, err
		}
		return f, seq, nil
	case TypeQuestionResolved:
		var f QuestionResolvedFrame
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, seq, err
		}
		return f, seq, nil
	}
	return bridge.DecodeOutbound(data)
}

// StartMessage is the ACP inbound `start` frame: the shared harness-v1 base
// with `prompt` overridden to an ACP text-block array (shadowing the
// embedded string field for JSON purposes) plus ACP-specific fields. Mirrors
// TS `startMessageSchema` (`acp-v1-bridge-protocol.ts`).
type StartMessage struct {
	bridge.StartBase

	Prompt                []TextContentBlock     `json:"prompt"`
	Instructions          string                 `json:"instructions,omitempty"`
	InstructionMapping    *InstructionMapping    `json:"instructionMapping,omitempty"`
	OutputSchemaMapping   *OutputSchemaMapping   `json:"outputSchemaMapping,omitempty"`
	MCPServers            map[string]any         `json:"mcpServers,omitempty"`
	BuiltinTools          []BuiltinToolMapping   `json:"builtinTools"`
	PermissionModeMapping *PermissionModeMapping `json:"permissionModeMapping,omitempty"`
	ModelMapping          *ModelMapping          `json:"modelMapping,omitempty"`
	TurnStartConfig       TurnStartConfig        `json:"turnStartConfig"`
}
