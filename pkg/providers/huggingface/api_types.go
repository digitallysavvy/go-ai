package huggingface

// hfUsage mirrors TS HuggingFaceResponsesUsage.
type hfUsage struct {
	InputTokens        int64 `json:"input_tokens"`
	InputTokensDetails *struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"input_tokens_details,omitempty"`
	OutputTokens        int64 `json:"output_tokens"`
	OutputTokensDetails *struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details,omitempty"`
	TotalTokens int64 `json:"total_tokens"`
}

// hfIncompleteDetails mirrors the response's incomplete_details object.
type hfIncompleteDetails struct {
	Reason string `json:"reason"`
}

// hfResponseError mirrors the top-level response.error object.
type hfResponseError struct {
	Message string `json:"message"`
}

// hfAnnotation mirrors a url_citation annotation on message output text.
type hfAnnotation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

// hfContentItem mirrors a message/reasoning output item's content entry
// (output_text or reasoning_text).
type hfContentItem struct {
	Type        string         `json:"type"`
	Text        string         `json:"text"`
	Annotations []hfAnnotation `json:"annotations,omitempty"`
}

// hfOutputItem mirrors one entry of the response's output array, covering
// every item type the Go port needs to read: message, reasoning,
// function_call, mcp_call, mcp_list_tools.
type hfOutputItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Role string `json:"role,omitempty"`

	// message / reasoning
	Content []hfContentItem `json:"content,omitempty"`

	// function_call
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`

	// mcp_list_tools
	ServerLabel string        `json:"server_label,omitempty"`
	Tools       []interface{} `json:"tools,omitempty"`
}

// hfResponse mirrors TS huggingfaceResponsesResponseSchema (only the fields
// the Go port reads).
type hfResponse struct {
	ID                string               `json:"id"`
	Model             string               `json:"model"`
	CreatedAt         int64                `json:"created_at"`
	Error             *hfResponseError     `json:"error"`
	IncompleteDetails *hfIncompleteDetails `json:"incomplete_details"`
	Usage             *hfUsage             `json:"usage"`
	Output            []hfOutputItem       `json:"output"`
}

// hfStreamItem mirrors the "item" field carried by
// response.output_item.added/done events.
type hfStreamItem struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Role   string `json:"role,omitempty"`
	CallID string `json:"call_id,omitempty"`
	Name   string `json:"name,omitempty"`

	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

// hfStreamResponse mirrors the "response" field carried by
// response.created/response.completed events (only the fields read).
type hfStreamResponse struct {
	ID                string               `json:"id"`
	Model             string               `json:"model"`
	CreatedAt         int64                `json:"created_at"`
	IncompleteDetails *hfIncompleteDetails `json:"incomplete_details"`
	Usage             *hfUsage             `json:"usage"`
}

// hfStreamEvent is a flexible envelope covering every SSE event shape the Go
// port needs to read: response.created, response.output_item.added/done,
// response.output_text.delta, response.reasoning_text.delta/done,
// response.completed. Unknown/unhandled event types (e.g.
// response.in_progress, mcp_call additions) simply leave every field zero,
// mirroring TS's `z.object({type: z.string()}).loose()` fallback.
type hfStreamEvent struct {
	Type     string            `json:"type"`
	Item     *hfStreamItem     `json:"item,omitempty"`
	ItemID   string            `json:"item_id,omitempty"`
	Delta    string            `json:"delta,omitempty"`
	Response *hfStreamResponse `json:"response,omitempty"`
}
