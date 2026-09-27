package acp

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
	ToolCallID string             `json:"toolCallId"`
	Title      string             `json:"title"`
	Kind       ToolKind           `json:"kind,omitempty"`
	Status     ToolCallStatus     `json:"status,omitempty"`
	Locations  []ToolCallLocation `json:"locations,omitempty"`
	RawInput   any                `json:"rawInput,omitempty"`
	RawOutput  any                `json:"rawOutput,omitempty"`
}
