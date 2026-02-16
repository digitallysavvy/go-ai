package openresponses

// OpenResponsesRequestBody represents the request body for the Open Responses API
type OpenResponsesRequestBody struct {
	// Model is the model ID to use (e.g., "llama-2-7b", "mistral-7b")
	Model string `json:"model"`

	// Input is the context for the model - either a string or array of message items
	Input interface{} `json:"input"`

	// Instructions are additional instructions to guide the model
	Instructions string `json:"instructions,omitempty"`

	// MaxOutputTokens is the maximum number of tokens to generate
	MaxOutputTokens *int `json:"max_output_tokens,omitempty"`

	// Temperature controls randomness (0.0 to 2.0)
	Temperature *float64 `json:"temperature,omitempty"`

	// TopP controls nucleus sampling
	TopP *float64 `json:"top_p,omitempty"`

	// PresencePenalty penalizes tokens based on presence
	PresencePenalty *float64 `json:"presence_penalty,omitempty"`

	// FrequencyPenalty penalizes tokens based on frequency
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`

	// Tools are the available function tools
	Tools []FunctionTool `json:"tools,omitempty"`

	// ToolChoice controls which tool to use
	ToolChoice interface{} `json:"tool_choice,omitempty"`

	// Text contains text output configuration
	Text *TextConfig `json:"text,omitempty"`

	// Stream enables streaming responses
	Stream bool `json:"stream,omitempty"`
}

// TextConfig contains text output configuration
type TextConfig struct {
	Format interface{} `json:"format,omitempty"`
}

// FunctionTool represents a function tool definition
type FunctionTool struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Strict      bool                   `json:"strict,omitempty"`
}

// Message item types for input

// MessageItem represents a message in the conversation
type MessageItem struct {
	Type    string      `json:"type"`
	Role    string      `json:"role,omitempty"`
	Content interface{} `json:"content"`
	ID      string      `json:"id,omitempty"`
	Status  string      `json:"status,omitempty"`
}

// InputTextContent represents text input
type InputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// InputImageContent represents image input
type InputImageContent struct {
	Type     string `json:"type"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// OutputTextContent represents text output
type OutputTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// FunctionCallItem represents a function call
type FunctionCallItem struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	ID        string `json:"id,omitempty"`
	Status    string `json:"status,omitempty"`
}

// FunctionCallOutputItem represents function call output
type FunctionCallOutputItem struct {
	Type   string      `json:"type"`
	CallID string      `json:"call_id"`
	Output interface{} `json:"output"`
	ID     string      `json:"id,omitempty"`
	Status string      `json:"status,omitempty"`
}

// OpenResponsesResponse represents the non-streaming response
type OpenResponsesResponse struct {
	ID          string       `json:"id"`
	Object      string       `json:"object"`
	CreatedAt   int64        `json:"created_at"`
	CompletedAt *int64       `json:"completed_at,omitempty"`
	Status      string       `json:"status"`
	Model       string       `json:"model"`
	Output      []OutputItem `json:"output"`
	Usage       *Usage       `json:"usage,omitempty"`

	// Incomplete details if response wasn't completed
	IncompleteDetails *IncompleteDetails `json:"incomplete_details,omitempty"`

	// Error if response failed
	Error *ResponseError `json:"error,omitempty"`

	// Instructions that were used
	Instructions string `json:"instructions,omitempty"`

	// Tools that were available
	Tools []FunctionTool `json:"tools,omitempty"`

	// Other optional fields
	Temperature      *float64 `json:"temperature,omitempty"`
	TopP             *float64 `json:"top_p,omitempty"`
	PresencePenalty  *float64 `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64 `json:"frequency_penalty,omitempty"`
}

// OutputItem represents an item in the response output
type OutputItem struct {
	Type    string        `json:"type"`
	ID      string        `json:"id,omitempty"`
	Role    string        `json:"role,omitempty"`
	Content []ContentPart `json:"content,omitempty"`
	Status  string        `json:"status,omitempty"`

	// For function_call type
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`

	// For reasoning type
	Summary           []ContentPart `json:"summary,omitempty"`
	EncryptedContent  string        `json:"encrypted_content,omitempty"`
}

// ContentPart represents a part of message content
type ContentPart struct {
	Type        string       `json:"type"`
	Text        string       `json:"text,omitempty"`
	Refusal     string       `json:"refusal,omitempty"`
	Annotations []Annotation `json:"annotations,omitempty"`
}

// Annotation represents an annotation on text
type Annotation struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	EndIndex   int    `json:"end_index,omitempty"`
	Title      string `json:"title,omitempty"`
}

// Usage represents token usage information
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`

	// Detailed token breakdown
	InputTokensDetails  *InputTokensDetails  `json:"input_tokens_details,omitempty"`
	OutputTokensDetails *OutputTokensDetails `json:"output_tokens_details,omitempty"`
}

// InputTokensDetails provides detailed input token information
type InputTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// OutputTokensDetails provides detailed output token information
type OutputTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// IncompleteDetails explains why a response was incomplete
type IncompleteDetails struct {
	Reason string `json:"reason"`
}

// ResponseError represents an error in the response
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Streaming event types

// StreamEvent represents a server-sent event
type StreamEvent struct {
	Type           string          `json:"type"`
	SequenceNumber int             `json:"sequence_number,omitempty"`
	Response       *OpenResponsesResponse `json:"response,omitempty"`
	OutputIndex    int             `json:"output_index,omitempty"`
	Item           *OutputItem     `json:"item,omitempty"`
	ItemID         string          `json:"item_id,omitempty"`
	ContentIndex   int             `json:"content_index,omitempty"`
	Delta          string          `json:"delta,omitempty"`
	Text           string          `json:"text,omitempty"`
	CallID         string          `json:"call_id,omitempty"`
	Arguments      string          `json:"arguments,omitempty"`
	Error          *ResponseError  `json:"error,omitempty"`
}
