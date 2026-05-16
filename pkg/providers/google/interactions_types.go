package google

import "encoding/json"

const (
	InteractionStatusInProgress     = "in_progress"
	InteractionStatusRequiresAction = "requires_action"
	InteractionStatusCompleted      = "completed"
	InteractionStatusFailed         = "failed"
	InteractionStatusCancelled      = "cancelled"
	InteractionStatusIncomplete     = "incomplete"
)

// InteractionsAgent identifies a Gemini Interactions API agent preset.
type InteractionsAgent struct {
	Name string
}

// GoogleInteractionsProviderOptions contains Google-specific options for the
// Interactions API. Set it under ProviderOptions["google"].
type GoogleInteractionsProviderOptions struct {
	PreviousInteractionID string
	Store                 *bool
	MediaResolution       string
	SystemInstruction     string
	ResponseModalities    []string
	ServiceTier           string
	ThinkingLevel         string
	ThinkingSummaries     string
	ImageConfig           map[string]interface{}
	AgentConfig           map[string]interface{}
	PollingTimeoutMs      int
}

type interactionsRequest struct {
	Model                 string                   `json:"model,omitempty"`
	Agent                 string                   `json:"agent,omitempty"`
	Input                 interface{}              `json:"input"`
	SystemInstruction     string                   `json:"system_instruction,omitempty"`
	Tools                 []map[string]interface{} `json:"tools,omitempty"`
	ResponseFormat        interface{}              `json:"response_format,omitempty"`
	ResponseMimeType      string                   `json:"response_mime_type,omitempty"`
	ResponseModalities    []string                 `json:"response_modalities,omitempty"`
	PreviousInteractionID string                   `json:"previous_interaction_id,omitempty"`
	ServiceTier           string                   `json:"service_tier,omitempty"`
	Store                 *bool                    `json:"store,omitempty"`
	GenerationConfig      map[string]interface{}   `json:"generation_config,omitempty"`
	AgentConfig           map[string]interface{}   `json:"agent_config,omitempty"`
	Background            bool                     `json:"background,omitempty"`
	Stream                bool                     `json:"stream,omitempty"`
}

type interactionsResponse struct {
	ID                    string                     `json:"id,omitempty"`
	Created               string                     `json:"created,omitempty"`
	Updated               string                     `json:"updated,omitempty"`
	Status                string                     `json:"status,omitempty"`
	Model                 string                     `json:"model,omitempty"`
	Agent                 string                     `json:"agent,omitempty"`
	Outputs               []interactionsContentBlock `json:"outputs,omitempty"`
	Usage                 *interactionsUsage         `json:"usage,omitempty"`
	ServiceTier           string                     `json:"service_tier,omitempty"`
	PreviousInteractionID string                     `json:"previous_interaction_id,omitempty"`
	ResponseModalities    []string                   `json:"response_modalities,omitempty"`
	Raw                   map[string]json.RawMessage `json:"-"`
}

type interactionsContentBlock struct {
	Type        string                     `json:"type"`
	Text        string                     `json:"text,omitempty"`
	Data        string                     `json:"data,omitempty"`
	MimeType    string                     `json:"mime_type,omitempty"`
	URI         string                     `json:"uri,omitempty"`
	Resolution  string                     `json:"resolution,omitempty"`
	Signature   string                     `json:"signature,omitempty"`
	Summary     []interactionsContentBlock `json:"summary,omitempty"`
	Content     *interactionsContentBlock  `json:"content,omitempty"`
	ID          string                     `json:"id,omitempty"`
	Name        string                     `json:"name,omitempty"`
	ServerName  string                     `json:"server_name,omitempty"`
	Arguments   map[string]interface{}     `json:"arguments,omitempty"`
	CallID      string                     `json:"call_id,omitempty"`
	Result      interface{}                `json:"result,omitempty"`
	IsError     *bool                      `json:"is_error,omitempty"`
	Annotations []interactionsAnnotation   `json:"annotations,omitempty"`
	SearchType  string                     `json:"search_type,omitempty"`
	Raw         map[string]interface{}     `json:"-"`
}

type interactionsAnnotation struct {
	Type           string                 `json:"type"`
	URL            string                 `json:"url,omitempty"`
	Title          string                 `json:"title,omitempty"`
	StartIndex     *int                   `json:"start_index,omitempty"`
	EndIndex       *int                   `json:"end_index,omitempty"`
	FileName       string                 `json:"file_name,omitempty"`
	DocumentURI    string                 `json:"document_uri,omitempty"`
	Source         string                 `json:"source,omitempty"`
	PageNumber     *int                   `json:"page_number,omitempty"`
	MediaID        string                 `json:"media_id,omitempty"`
	CustomMetadata map[string]interface{} `json:"custom_metadata,omitempty"`
	Name           string                 `json:"name,omitempty"`
	PlaceID        string                 `json:"place_id,omitempty"`
}

type interactionsUsage struct {
	TotalInputTokens        *int64                       `json:"total_input_tokens,omitempty"`
	TotalOutputTokens       *int64                       `json:"total_output_tokens,omitempty"`
	TotalThoughtTokens      *int64                       `json:"total_thought_tokens,omitempty"`
	TotalCachedTokens       *int64                       `json:"total_cached_tokens,omitempty"`
	TotalToolUseTokens      *int64                       `json:"total_tool_use_tokens,omitempty"`
	TotalTokens             *int64                       `json:"total_tokens,omitempty"`
	InputTokensByModality   []interactionsModalityTokens `json:"input_tokens_by_modality,omitempty"`
	OutputTokensByModality  []interactionsModalityTokens `json:"output_tokens_by_modality,omitempty"`
	CachedTokensByModality  []interactionsModalityTokens `json:"cached_tokens_by_modality,omitempty"`
	ToolUseTokensByModality []interactionsModalityTokens `json:"tool_use_tokens_by_modality,omitempty"`
	GroundingToolCount      []map[string]interface{}     `json:"grounding_tool_count,omitempty"`
}

type interactionsModalityTokens struct {
	Modality string `json:"modality,omitempty"`
	Tokens   *int64 `json:"tokens,omitempty"`
}

type interactionsEvent struct {
	EventType     string                    `json:"event_type"`
	EventID       string                    `json:"event_id,omitempty"`
	Index         *int                      `json:"index,omitempty"`
	Interaction   *interactionsResponse     `json:"interaction,omitempty"`
	InteractionID string                    `json:"interaction_id,omitempty"`
	Status        string                    `json:"status,omitempty"`
	Content       *interactionsContentBlock `json:"content,omitempty"`
	Delta         *interactionsContentBlock `json:"delta,omitempty"`
	Error         *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

func isTerminalInteractionStatus(status string) bool {
	switch status {
	case InteractionStatusCompleted, InteractionStatusFailed, InteractionStatusCancelled, InteractionStatusIncomplete:
		return true
	default:
		return false
	}
}
