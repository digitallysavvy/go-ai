package acp

import (
	"encoding/json"
	"fmt"

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
	case TypeToolCallCandidate, TypeQuestionRequest, TypeQuestionResolved:
		if err := validateACPFrame(typ, fields); err != nil {
			return nil, seq, err
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

// validateACPFrame checks the keys the TS zod schemas of the ACP extension
// frames require: a string `requestId`, and a `toolCall` (required on
// candidates, optional but not null on question requests) that is an object
// with string `toolCallId` and `title` (the `z.custom<ACPToolCall>` check).
func validateACPFrame(typ string, fields map[string]json.RawMessage) error {
	invalid := func(key string) error {
		return fmt.Errorf("acp: invalid %s frame: %q is missing or has the wrong type", typ, key)
	}
	var requestID string
	if raw, ok := fields["requestId"]; !ok || string(raw) == "null" || json.Unmarshal(raw, &requestID) != nil {
		return invalid("requestId")
	}
	toolCallOK := func(raw json.RawMessage) bool {
		var tc struct {
			ToolCallID json.RawMessage `json:"toolCallId"`
			Title      json.RawMessage `json:"title"`
		}
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil || json.Unmarshal(raw, &tc) != nil {
			return false
		}
		var str string
		return len(tc.ToolCallID) > 0 && tc.ToolCallID[0] == '"' && json.Unmarshal(tc.ToolCallID, &str) == nil &&
			len(tc.Title) > 0 && tc.Title[0] == '"' && json.Unmarshal(tc.Title, &str) == nil
	}
	switch typ {
	case TypeToolCallCandidate:
		if !toolCallOK(fields["toolCall"]) {
			return invalid("toolCall")
		}
	case TypeQuestionRequest:
		if raw, ok := fields["nativeToolCall"]; ok && !toolCallOK(raw) {
			return invalid("nativeToolCall")
		}
	}
	return nil
}

// StartMessage is the ACP inbound `start` frame: the shared harness-v1 base
// with `prompt` overridden to an ACP text-block array (shadowing the
// embedded string field for JSON purposes) plus ACP-specific fields. Mirrors
// TS `startMessageSchema` (`acp-v1-bridge-protocol.ts`).
type StartMessage struct {
	bridge.StartBase

	Prompt                []ContentBlock         `json:"prompt"`
	Instructions          string                 `json:"instructions,omitempty"`
	InstructionMapping    *InstructionMapping    `json:"instructionMapping,omitempty"`
	OutputSchemaMapping   *OutputSchemaMapping   `json:"outputSchemaMapping,omitempty"`
	MCPServers            map[string]any         `json:"mcpServers,omitempty"`
	BuiltinTools          []BuiltinToolMapping   `json:"builtinTools"`
	PermissionModeMapping *PermissionModeMapping `json:"permissionModeMapping,omitempty"`
	ModelMapping          *ModelMapping          `json:"modelMapping,omitempty"`
	TurnStartConfig       TurnStartConfig        `json:"turnStartConfig"`
	RecoveryMode          *RecoveryMode          `json:"recoveryMode,omitempty"`

	// OmitTools drops the `tools` key from the frame. TS's
	// `startMessageSchema` declares `tools` optional and the harness sends
	// `tools: options.tools == null ? undefined : ...`, so a prompt with no
	// tools option carries no `tools` key (JSON.stringify drops undefined),
	// unlike the base schema's default `[]`.
	OmitTools bool `json:"-"`
}

// MarshalJSON honors OmitTools by shadowing the embedded StartBase.Tools
// field with an omitted-when-nil one.
func (m StartMessage) MarshalJSON() ([]byte, error) {
	type plain StartMessage
	if !m.OmitTools {
		return json.Marshal(plain(m))
	}
	return json.Marshal(struct {
		plain
		Tools *[]harness.ToolSpec `json:"tools,omitempty"`
	}{plain: plain(m)})
}

// RecoveryMode tags a "start" frame sent to a respawned bridge process as a
// process-loss recovery attempt rather than a fresh turn. Mirrors TS's
// `lossyRecoverySchema`/`coldRestoreSchema` union on `startMessageSchema`
// (`acp-v1-bridge-protocol.ts`).
type RecoveryMode struct {
	// Type is "lossy-rerun" or "cold-restore".
	Type         string `json:"type"`
	ACPSessionID string `json:"acpSessionId"`
	// Reason is only present for "lossy-rerun".
	Reason string `json:"reason,omitempty"`
}
