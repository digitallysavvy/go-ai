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
	// ServiceTier is the service tier used for this request (non-streaming).
	// Values: "SERVICE_TIER_STANDARD", "SERVICE_TIER_FLEX", "SERVICE_TIER_PRIORITY".
	ServiceTier string `json:"serviceTier,omitempty"`
}

// UsageMetadata represents token usage information returned by Gemini.
type UsageMetadata struct {
	PromptTokenCount        int    `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int    `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         int    `json:"totalTokenCount,omitempty"`
	CachedContentTokenCount int    `json:"cachedContentTokenCount,omitempty"`
	ThoughtsTokenCount      int    `json:"thoughtsTokenCount,omitempty"`
	TrafficType             string `json:"trafficType,omitempty"`
	PromptTokensDetails     []struct {
		Modality   string `json:"modality,omitempty"`
		TokenCount int    `json:"tokenCount,omitempty"`
	} `json:"promptTokensDetails,omitempty"`
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
func convertUsage(usage *UsageMetadata) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	promptTokens := int64(usage.PromptTokenCount)
	candidatesTokens := int64(usage.CandidatesTokenCount)
	cachedContentTokens := int64(usage.CachedContentTokenCount)
	thoughtsTokens := int64(usage.ThoughtsTokenCount)

	totalOutputTokens := candidatesTokens + thoughtsTokens
	totalTokens := promptTokens + totalOutputTokens

	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &totalOutputTokens,
		TotalTokens:  &totalTokens,
	}

	// Input token details: cache breakdown and text/image split.
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
	if cachedContentTokens > 0 || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedContentTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedContentTokens,
			CacheWriteTokens: nil, // Gemini does not report cache write tokens separately
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	}

	// Output token details: text vs reasoning split.
	if thoughtsTokens > 0 {
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &candidatesTokens,
			ReasoningTokens: &thoughtsTokens,
		}
	}

	result.Raw = map[string]interface{}{
		"promptTokenCount":     usage.PromptTokenCount,
		"candidatesTokenCount": usage.CandidatesTokenCount,
		"totalTokenCount":      usage.TotalTokenCount,
	}
	if usage.CachedContentTokenCount > 0 {
		result.Raw["cachedContentTokenCount"] = usage.CachedContentTokenCount
	}
	if usage.ThoughtsTokenCount > 0 {
		result.Raw["thoughtsTokenCount"] = usage.ThoughtsTokenCount
	}
	if usage.TrafficType != "" {
		result.Raw["trafficType"] = usage.TrafficType
	}

	return result
}
