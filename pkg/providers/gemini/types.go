package gemini

import (
	"encoding/base64"
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Candidate holds a single candidate from a Gemini API response.
type Candidate struct {
	Content struct {
		Parts []Part `json:"parts"`
		Role  string `json:"role"`
	} `json:"content"`
	FinishReason       string          `json:"finishReason"`
	FinishMessage      string          `json:"finishMessage,omitempty"`
	Index              int             `json:"index"`
	GroundingMetadata  json.RawMessage `json:"groundingMetadata,omitempty"`
	UrlContextMetadata json.RawMessage `json:"urlContextMetadata,omitempty"`
	SafetyRatings      json.RawMessage `json:"safetyRatings,omitempty"`
}

// Response represents the Gemini API response body.
type Response struct {
	Candidates     []Candidate     `json:"candidates"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
	PromptFeedback json.RawMessage `json:"promptFeedback,omitempty"`
	// ResponseID is the provider-assigned response ID (TS: response.responseId).
	ResponseID string `json:"responseId,omitempty"`
	// ServiceTier is kept for compatibility with older responses. Current
	// Gemini API responses report it inside usageMetadata.
	ServiceTier string `json:"serviceTier,omitempty"`
}

// tokenDetail is one entry of a *TokensDetails array (modality + token count).
type tokenDetail struct {
	Modality   string `json:"modality,omitempty"`
	TokenCount int    `json:"tokenCount,omitempty"`
}

// UsageMetadata represents token usage information returned by Gemini.
// Field set matches TS GoogleUsageMetadata (convert-google-usage.ts) so that
// json.Marshal(UsageMetadata) reproduces the same raw usage object.
type UsageMetadata struct {
	PromptTokenCount           int           `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount       int           `json:"candidatesTokenCount,omitempty"`
	ToolUsePromptTokenCount    int           `json:"toolUsePromptTokenCount,omitempty"`
	TotalTokenCount            int           `json:"totalTokenCount,omitempty"`
	CachedContentTokenCount    int           `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount         int           `json:"thoughtsTokenCount,omitempty"`
	TrafficType                string        `json:"trafficType,omitempty"`
	ServiceTier                string        `json:"serviceTier,omitempty"`
	PromptTokensDetails        []tokenDetail `json:"promptTokensDetails,omitempty"`
	CacheTokensDetails         []tokenDetail `json:"cacheTokensDetails,omitempty"`
	CandidatesTokensDetails    []tokenDetail `json:"candidatesTokensDetails,omitempty"`
	ToolUsePromptTokensDetails []tokenDetail `json:"toolUsePromptTokensDetails,omitempty"`
}

// promptFeedbackBlockReason extracts the blockReason field from a raw
// promptFeedback payload, if present.
func promptFeedbackBlockReason(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var pf struct {
		BlockReason string `json:"blockReason"`
	}
	if err := json.Unmarshal(raw, &pf); err != nil {
		return ""
	}
	return pf.BlockReason
}

// isConfirmedPromptBlockReason reports whether blockReason represents an
// actual prompt block (TS isConfirmedPromptBlockReason): present, non-empty,
// and not one of the "unspecified" sentinel values.
func isConfirmedPromptBlockReason(blockReason string) bool {
	return blockReason != "" &&
		blockReason != "BLOCK_REASON_UNSPECIFIED" &&
		blockReason != "BLOCKED_REASON_UNSPECIFIED"
}

type ModalityTokenCounts struct {
	TextTokens  int32 `json:"textTokens,omitempty"`
	ImageTokens int32 `json:"imageTokens,omitempty"`
	AudioTokens int32 `json:"audioTokens,omitempty"`
	VideoTokens int32 `json:"videoTokens,omitempty"`
}

// Part represents a single part in a Gemini content block.
// ExecutableCode and CodeExecutionResult are populated only by Google Generative AI,
// not by Vertex AI. Both fields are safe to include in the shared type — JSON
// unmarshaling simply leaves them nil when absent.
type Part struct {
	Text             string `json:"text,omitempty"`
	Thought          bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
	FunctionCall     *struct {
		ID   string                 `json:"id,omitempty"`
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	} `json:"functionCall,omitempty"`
	InlineData *struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"` // base64-encoded
	} `json:"inlineData,omitempty"`
	// ExecutableCode is set when the model requests code execution (Google only).
	ExecutableCode *struct {
		Language string `json:"language"`
		Code     string `json:"code"`
	} `json:"executableCode,omitempty"`
	// CodeExecutionResult is set when the model returns a code execution result (Google only).
	CodeExecutionResult *struct {
		Outcome string `json:"outcome"`
		Output  string `json:"output"`
	} `json:"codeExecutionResult,omitempty"`
}

// decodeInlineData base64-decodes an inlineData payload.
// Errors are intentionally ignored: if the data is malformed the caller receives
// nil bytes, which propagates as an empty content block rather than a hard failure.
// The Gemini API guarantees valid base64 for inlineData fields.
func decodeInlineData(encoded string) []byte {
	data, _ := base64.StdEncoding.DecodeString(encoded)
	return data
}

// convertUsage converts a Gemini UsageMetadata payload to the SDK Usage struct.
// Mirrors TS convertGoogleUsage (convert-google-usage.ts): input tokens are
// promptTokenCount + toolUsePromptTokenCount, noCache/cacheRead and the
// output text/reasoning split are always populated (not gated on being
// non-zero), and Raw carries the full usage object.
func convertUsage(usage *UsageMetadata) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	promptTokens := int64(usage.PromptTokenCount)
	candidatesTokens := int64(usage.CandidatesTokenCount)
	toolUsePromptTokens := int64(usage.ToolUsePromptTokenCount)
	cachedContentTokens := int64(usage.CachedContentTokenCount)
	thoughtsTokens := int64(usage.ThoughtsTokenCount)

	inputTokens := promptTokens + toolUsePromptTokens
	noCacheTokens := inputTokens - cachedContentTokens
	outputTokens := candidatesTokens + thoughtsTokens
	totalTokens := inputTokens + outputTokens

	// Input token details: cache breakdown (always) plus text/image split
	// (Go-only addition; TS convertGoogleUsage has no modality split).
	var textTokens *int64
	var imageTokens *int64
	for _, detail := range usage.PromptTokensDetails {
		switch detail.Modality {
		case "TEXT":
			v := int64(detail.TokenCount)
			textTokens = &v
		case "IMAGE":
			v := int64(detail.TokenCount)
			imageTokens = &v
		}
	}

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedContentTokens,
			TextTokens:      textTokens,
			ImageTokens:     imageTokens,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &candidatesTokens,
			ReasoningTokens: &thoughtsTokens,
		},
	}

	if raw, err := json.Marshal(usage); err == nil {
		var rawMap map[string]interface{}
		if json.Unmarshal(raw, &rawMap) == nil {
			result.Raw = rawMap
		}
	}

	return result
}

func modalityTokenCounts(usage *UsageMetadata) ModalityTokenCounts {
	var counts ModalityTokenCounts
	add := func(modality string, tokenCount int) {
		switch modality {
		case "TEXT":
			counts.TextTokens += int32(tokenCount)
		case "IMAGE":
			counts.ImageTokens += int32(tokenCount)
		case "AUDIO":
			counts.AudioTokens += int32(tokenCount)
		case "VIDEO":
			counts.VideoTokens += int32(tokenCount)
		}
	}
	if usage == nil {
		return counts
	}
	for _, detail := range usage.PromptTokensDetails {
		add(detail.Modality, detail.TokenCount)
	}
	for _, detail := range usage.CandidatesTokensDetails {
		add(detail.Modality, detail.TokenCount)
	}
	return counts
}
