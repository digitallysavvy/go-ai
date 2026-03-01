package responses

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
