package responses

import "encoding/json"

// ─────────────────────────────────────────────────────────────────────────────
// Responses API message item types
// ─────────────────────────────────────────────────────────────────────────────

// AssistantMessageItem represents an assistant message in the Responses API output.
// The Phase field indicates which phase of a multi-turn or agentic flow the message
// belongs to. It is present only when the model sets it.
type AssistantMessageItem struct {
	// Type is always "message".
	Type string `json:"type"`

	// Role is always "assistant".
	Role string `json:"role"`

	// ID is the unique identifier for this message item.
	ID string `json:"id,omitempty"`

	// Phase indicates the agentic flow phase, if set by the model.
	// Values: "commentary", "final_answer", or nil (not set).
	Phase *string `json:"phase,omitempty"`

	// Content holds the message text parts.
	Content []AssistantMessageContent `json:"content,omitempty"`
}

// AssistantMessageContent is a single content part in an assistant message.
type AssistantMessageContent struct {
	// Type is "output_text".
	Type string `json:"type"`

	// Text is the text content.
	Text string `json:"text"`
}

// FunctionCallItem represents a function call output item.
type FunctionCallItem struct {
	// Type is "function_call".
	Type string `json:"type"`

	// ID is the unique identifier for this item.
	ID string `json:"id,omitempty"`

	// CallID links this call to its output.
	CallID string `json:"call_id"`

	// Name is the function name.
	Name string `json:"name"`

	// Arguments is the JSON-encoded argument string.
	Arguments string `json:"arguments"`
}

// CustomToolCallItem represents a custom tool call output item.
type CustomToolCallItem struct {
	// Type is "custom_tool_call".
	Type string `json:"type"`

	// ID is the unique identifier for this item.
	ID string `json:"id,omitempty"`

	// CallID links this call to its output.
	CallID string `json:"call_id"`

	// Name is the custom tool name.
	Name string `json:"name"`

	// Input is the raw input string for the custom tool.
	Input string `json:"input"`
}

// CustomToolCallOutput is the response sent back to the API after handling a
// CustomToolCallItem. Set Output to a plain string for simple results, or to a
// []CustomToolCallOutputPart for rich multi-content responses.
type CustomToolCallOutput struct {
	// Type is always "custom_tool_call_output".
	Type string `json:"type"`

	// CallID matches the CustomToolCallItem.CallID this output is for.
	CallID string `json:"call_id"`

	// Output is the tool result: either a plain string or a slice of
	// CustomToolCallOutputPart for multi-part content (text, image, file).
	Output interface{} `json:"output"`
}

// CustomToolCallOutputPart is a single content item in a multi-part custom
// tool output. Set Type to "input_text", "input_image", or "input_file" and
// populate the corresponding fields.
type CustomToolCallOutputPart struct {
	// Type is "input_text", "input_image", or "input_file".
	Type string `json:"type"`

	// Text is the text content (input_text only).
	Text string `json:"text,omitempty"`

	// ImageURL is the image URL (input_image only).
	ImageURL string `json:"image_url,omitempty"`

	// Filename is the file name (input_file only).
	Filename string `json:"filename,omitempty"`

	// FileData is the base64-encoded file content (input_file only).
	FileData string `json:"file_data,omitempty"`
}

// CompactionEvent is received in the Responses API SSE stream when the server
// has compacted the conversation context. Callers should forward the
// EncryptedContent in subsequent requests to maintain conversation continuity.
// Surface this to consumers as a CustomContent{Kind: "openai-compaction"} chunk.
type CompactionEvent struct {
	// Type is always "compaction".
	Type string `json:"type"`

	// ItemID is the unique identifier of the compacted item.
	ItemID string `json:"item_id,omitempty"`

	// EncryptedContent is the opaque encrypted context blob.
	// Must be forwarded verbatim in subsequent Responses API requests.
	EncryptedContent string `json:"encrypted_content,omitempty"`
}

// ToolSearchCallItem is emitted by the Responses API when the model invokes a
// tool search. For client-executed mode, route it to the user's Execute function.
// For server-executed mode, it is informational (OpenAI resolved the search).
//
// Wire format matches the Responses API tool_search_call output item.
type ToolSearchCallItem struct {
	// Type is always "tool_search_call".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID string `json:"id,omitempty"`

	// Status is "in_progress", "completed", or "incomplete".
	Status string `json:"status,omitempty"`

	// Execution is "server" or "client".
	Execution string `json:"execution,omitempty"`

	// CallID links this call to its output. Null for server-executed searches.
	CallID *string `json:"call_id"`

	// Arguments holds the search arguments as an arbitrary JSON value.
	// For server mode, often an object like {"paths": ["tool_name"]}.
	// For client mode, the object matches the Parameters schema you provided.
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolSearchOutputItem is emitted by the Responses API after the server has
// resolved a tool_search_call. It pairs with ToolSearchCallItem via CallID.
// For server-executed searches, CallID is null and Tools contains the matched
// tool definitions. For client-executed searches, CallID links back to the
// original ToolSearchCallItem.
type ToolSearchOutputItem struct {
	// Type is always "tool_search_output".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID string `json:"id,omitempty"`

	// Status is "in_progress", "completed", or "incomplete".
	Status string `json:"status,omitempty"`

	// Execution is "server" or "client".
	Execution string `json:"execution,omitempty"`

	// CallID links this output to its ToolSearchCallItem. Null for server-executed searches.
	CallID *string `json:"call_id"`

	// Tools is the list of matched tool definitions returned by the search.
	Tools []json.RawMessage `json:"tools,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Responses API tool definition types (sent in requests)
// ─────────────────────────────────────────────────────────────────────────────

// FunctionToolDef represents a standard function tool in an API request.
type FunctionToolDef struct {
	// Type is always "function".
	Type string `json:"type"`

	// Name is the function name.
	Name string `json:"name"`

	// Description is an optional description.
	Description string `json:"description,omitempty"`

	// Parameters is the JSON Schema for the function parameters.
	Parameters interface{} `json:"parameters,omitempty"`

	// Strict enables strict schema validation.
	Strict *bool `json:"strict,omitempty"`
}

// LocalShellToolDef represents the local_shell tool in an API request.
// No additional fields are needed; its presence enables the tool.
type LocalShellToolDef struct {
	// Type is always "local_shell".
	Type string `json:"type"`
}

// ApplyPatchToolDef represents the apply_patch tool in an API request.
type ApplyPatchToolDef struct {
	// Type is always "apply_patch".
	Type string `json:"type"`
}

// ShellToolDef represents the shell container tool in an API request.
type ShellToolDef struct {
	// Type is always "shell".
	Type string `json:"type"`

	// Environment specifies the container environment (optional).
	Environment *ShellToolDefEnvironment `json:"environment,omitempty"`
}

// ShellToolDefEnvironment is the environment field in a ShellToolDef request.
type ShellToolDefEnvironment struct {
	// Type is "container_auto", "container_reference", or "local".
	Type string `json:"type"`

	// FileIDs are files to mount (container_auto only).
	FileIDs []string `json:"file_ids,omitempty"`

	// MemoryLimit sets the memory limit (container_auto only).
	MemoryLimit *string `json:"memory_limit,omitempty"`

	// NetworkPolicy controls network access (container_auto only).
	NetworkPolicy *ShellNetworkPolicy `json:"network_policy,omitempty"`

	// Skills are executable skills in the container.
	Skills []ShellSkill `json:"skills,omitempty"`

	// ContainerID is an existing container reference (container_reference only).
	ContainerID *string `json:"container_id,omitempty"`
}

// CustomToolDef represents a custom tool in an API request.
type CustomToolDef struct {
	// Type is always "custom".
	Type string `json:"type"`

	// Name is the custom tool name.
	Name string `json:"name"`

	// Description is an optional description.
	Description *string `json:"description,omitempty"`

	// Format specifies output format constraints.
	Format *CustomToolDefFormat `json:"format,omitempty"`
}

// CustomToolDefFormat specifies the output format constraints for a custom tool.
type CustomToolDefFormat struct {
	// Type is "grammar" or "text".
	Type string `json:"type"`

	// Syntax is "regex" or "lark" (grammar only).
	Syntax *string `json:"syntax,omitempty"`

	// Definition is the grammar or regex string (grammar only).
	Definition *string `json:"definition,omitempty"`
}

// ToolSearchToolDef represents a tool_search tool in an API request.
// Server mode (default): OpenAI resolves tool matches internally.
// Client mode: the model emits tool_search_call events for the client to handle.
type ToolSearchToolDef struct {
	// Type is always "tool_search".
	Type string `json:"type"`

	// Execution is "server" (default) or "client".
	// Omit to use the default server-side execution.
	Execution string `json:"execution,omitempty"`

	// Description describes the search behavior. Only used for client execution.
	Description string `json:"description,omitempty"`

	// Parameters is the JSON schema for client search arguments.
	// Only relevant for client execution mode.
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Responses API input types (sent in requests to /v1/responses)
// ─────────────────────────────────────────────────────────────────────────────

// SystemMessage is sent as the first input item for system/developer prompts.
type SystemMessage struct {
	Type    string `json:"type,omitempty"` // omit; legacy field
	Role    string `json:"role"`           // "system" or "developer"
	Content string `json:"content"`
}

// UserMessage represents a user turn in the Responses API input.
// Content is either a plain string (for text-only messages) or a slice of
// UserTextPart / UserImageURLPart / UserFilePart for multi-modal input.
type UserMessage struct {
	Role    string      `json:"role"` // "user"
	Content interface{} `json:"content"`
}

// UserTextPart is a plain-text content part in a user message.
type UserTextPart struct {
	Type string `json:"type"` // "input_text"
	Text string `json:"text"`
}

// UserImageURLPart is an image content part in a user message.
type UserImageURLPart struct {
	Type     string `json:"type"`      // "input_image"
	ImageURL string `json:"image_url"`
}

// UserFilePart references a file by URL in a user message.
type UserFilePart struct {
	Type    string `json:"type"`     // "input_file"
	FileURL string `json:"file_url"`
}

// FunctionCallOutputItem sends a function tool result back to the Responses API.
// It pairs with a FunctionCallItem via CallID.
type FunctionCallOutputItem struct {
	Type   string      `json:"type"`   // "function_call_output"
	CallID string      `json:"call_id"`
	Output interface{} `json:"output"` // string or []CustomToolCallOutputPart
}

// ─────────────────────────────────────────────────────────────────────────────
// Responses API response types (non-streaming POST /responses body)
// ─────────────────────────────────────────────────────────────────────────────

// ResponsesAPIResponse is the body returned by a non-streaming POST /responses.
type ResponsesAPIResponse struct {
	ID                string             `json:"id"`
	CreatedAt         int64              `json:"created_at"`
	Model             string             `json:"model"`
	ServiceTier       string             `json:"service_tier,omitempty"`
	Output            []json.RawMessage  `json:"output"`
	Usage             ResponsesAPIUsage  `json:"usage"`
	IncompleteDetails *IncompleteDetails `json:"incomplete_details,omitempty"`
}

// ResponsesAPIUsage holds token counts from a Responses API response.
type ResponsesAPIUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	InputTokensDetails *struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *struct {
		ReasoningTokens int `json:"reasoning_tokens,omitempty"`
	} `json:"output_tokens_details,omitempty"`
}

// IncompleteDetails explains why a Responses API response was cut short.
type IncompleteDetails struct {
	Reason string `json:"reason,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Responses API streaming event types (SSE from POST /responses with stream=true)
// ─────────────────────────────────────────────────────────────────────────────

// ResponsesStreamEvent is used to peek at the "type" field of an SSE data payload
// before full parsing.
type ResponsesStreamEvent struct {
	Type string `json:"type"`
}

// ResponseCreatedEvent is emitted at the start of a streaming response.
type ResponseCreatedEvent struct {
	Type     string `json:"type"` // "response.created"
	Response struct {
		ID    string `json:"id"`
		Model string `json:"model"`
	} `json:"response"`
}

// OutputItemAddedEvent is emitted when the model begins a new output item.
type OutputItemAddedEvent struct {
	Type        string `json:"type"` // "response.output_item.added"
	OutputIndex int    `json:"output_index"`
	Item        struct {
		Type   string `json:"type"`
		ID     string `json:"id,omitempty"`
		CallID string `json:"call_id,omitempty"` // function_call
		Name   string `json:"name,omitempty"`    // function_call
	} `json:"item"`
}

// OutputTextDeltaEvent carries an incremental text chunk.
type OutputTextDeltaEvent struct {
	Type        string `json:"type"` // "response.output_text.delta"
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// FunctionCallArgumentsDeltaEvent carries an incremental tool input chunk.
type FunctionCallArgumentsDeltaEvent struct {
	Type        string `json:"type"` // "response.function_call_arguments.delta"
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// ReasoningSummaryTextDeltaEvent carries an incremental reasoning text chunk.
type ReasoningSummaryTextDeltaEvent struct {
	Type        string `json:"type"` // "response.reasoning_summary_text.delta"
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	Delta       string `json:"delta"`
}

// OutputItemDoneEvent is emitted when an output item is fully assembled.
// Item holds the complete item as raw JSON for type-specific parsing.
type OutputItemDoneEvent struct {
	Type        string          `json:"type"` // "response.output_item.done"
	OutputIndex int             `json:"output_index"`
	Item        json.RawMessage `json:"item"`
}

// ResponseCompletedEvent is the terminal streaming event carrying final usage.
type ResponseCompletedEvent struct {
	Type     string `json:"type"` // "response.completed"
	Response struct {
		ID                string             `json:"id"`
		Usage             ResponsesAPIUsage  `json:"usage"`
		IncompleteDetails *IncompleteDetails `json:"incomplete_details,omitempty"`
	} `json:"response"`
}

// ResponsesStreamErrorEvent carries an API error during streaming.
type ResponsesStreamErrorEvent struct {
	Type    string `json:"type"` // "error"
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}
