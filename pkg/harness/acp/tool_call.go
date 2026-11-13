package acp

import "encoding/json"

// ToolKind is a broad ACP tool capability category. Mirrors TS `ACPToolKind`.
type ToolKind string

// ToolCallStatus is the ACP tool call lifecycle status. Mirrors TS
// `ACPToolCallStatus`.
type ToolCallStatus string

// ToolCallLocation is a file location referenced by a tool call. Mirrors TS
// `ACPToolCallLocation`.
type ToolCallLocation struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// ToolCall is the native ACP tool call shape used by isMcpToolCall and
// AskUserQuestionsSettings callbacks. Mirrors TS `ACPToolCall` (only the
// fields those callbacks need are carried across the bridge wire for the
// acp-tool-call-candidate / acp-question-request frames; RawInput/RawOutput
// are opaque JSON).
type ToolCall struct {
	ToolCallID string `json:"toolCallId"`
	// Name is the ACP implementation's own programmatic tool identifier
	// (TS `ACPToolCall["name"]`, added in ACP SDK 1.5 / harness-acp 1.0.64).
	// It is populated only when the implementation reports it; when present
	// it is the most reliable signal for tool identity (stable across
	// locales/UI wording, unlike Title, and more specific than Kind), so
	// classifiers that need to identify a specific tool should check it
	// before falling back to Title/Kind-based heuristics.
	Name   string         `json:"name,omitempty"`
	Title  string         `json:"title"`
	Kind   ToolKind       `json:"kind,omitempty"`
	Status ToolCallStatus `json:"status,omitempty"`
	// Content is the ACP tool call's `content` blocks (TS
	// `ACPToolCall["content"]`: content, diff and terminal entries), kept as
	// decoded JSON objects so callbacks see what the implementation reported.
	Content   []map[string]any   `json:"content,omitempty"`
	Locations []ToolCallLocation `json:"locations,omitempty"`
	RawInput  any                `json:"rawInput,omitempty"`
	RawOutput any                `json:"rawOutput,omitempty"`
	// Meta is the ACP tool call's `_meta` field (TS `ACPToolCall["_meta"]`),
	// used by isMcpToolCall classifiers that route on implementation-specific
	// metadata (e.g. Grok Build's `x.ai/tool` namespace) rather than raw
	// input shape.
	Meta map[string]any `json:"_meta,omitempty"`

	// nameSet records a non-null `name` key in the bridge frame the tool
	// call was decoded from, so HasName can tell
	// `name: ""` from an absent name (TS `toolCall.name != null`).
	nameSet bool
}

// HasName reports whether the tool call carries a programmatic name: TS
// `toolCall.name != null`. A tool call decoded from a bridge frame has one
// when the frame held a non-null `name`, an empty string included; any other
// ToolCall has one when Name is not empty.
func (tc ToolCall) HasName() bool {
	return tc.nameSet || tc.Name != ""
}

// markDecodedName records, for a tool call decoded from raw (a bridge
// frame's toolCall object), whether its last exact `name` key is present and
// not null, as TS JSON.parse sees it. Without one, Name is cleared:
// encoding/json also fills it from a "Name" key and keeps an earlier value
// over a later null, which TS would not see.
func (tc *ToolCall) markDecodedName(raw json.RawMessage) {
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return
	}
	name, ok := keys["name"]
	tc.nameSet = ok && string(name) != "null"
	if !tc.nameSet {
		tc.Name = ""
	}
}
