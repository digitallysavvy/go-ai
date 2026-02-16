package googlevertex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for Google Vertex AI
// It uses the same Gemini models and API format as Google Generative AI
// but with Vertex AI endpoints and authentication
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Google Vertex AI language model
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v3"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "google-vertex"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Gemini Pro models support function calling
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	// Gemini supports JSON mode
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Gemini Pro Vision and 1.5 models support images
	return m.modelID == "gemini-pro-vision" ||
		m.modelID == "gemini-1.5-pro" ||
		m.modelID == "gemini-1.5-flash" ||
		m.modelID == "gemini-1.5-flash-8b" ||
		m.modelID == "gemini-2.0-flash-exp"
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Build path for Vertex AI
	// The base URL already includes the project/location/publishers path
	// Format: /models/{model}:generateContent
	path := fmt.Sprintf("/models/%s:generateContent", m.modelID)

	// Make API request
	var response vertexResponse
	err := m.provider.client.PostJSON(ctx, path, reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Convert response to GenerateResult
	return m.convertResponse(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Build path for Vertex AI streaming
	// The base URL already includes the project/location/publishers path
	// Format: /models/{model}:streamGenerateContent?alt=sse
	path := fmt.Sprintf("/models/%s:streamGenerateContent?alt=sse", m.modelID)

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create stream wrapper
	return newVertexStream(httpResp.Body), nil
}

// buildRequestBody builds the Google Vertex AI request body
// Uses the same format as Google Generative AI
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) map[string]interface{} {
	body := map[string]interface{}{}

	// Convert messages to Google format
	if opts.Prompt.IsMessages() {
		body["contents"] = prompt.ToGoogleMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["contents"] = prompt.ToGoogleMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}

	// Add system instruction if present
	if opts.Prompt.System != "" {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": opts.Prompt.System},
			},
		}
	}

	// Build generation config
	genConfig := map[string]interface{}{}
	if opts.Temperature != nil {
		genConfig["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *opts.MaxTokens
	}
	if opts.TopP != nil {
		genConfig["topP"] = *opts.TopP
	}
	if opts.TopK != nil {
		genConfig["topK"] = *opts.TopK
	}
	if len(opts.StopSequences) > 0 {
		genConfig["stopSequences"] = opts.StopSequences
	}

	// Add response MIME type for JSON mode
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json_object" {
		genConfig["responseMimeType"] = "application/json"
	}

	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	// Add tools if present
	if len(opts.Tools) > 0 {
		body["tools"] = []map[string]interface{}{
			{
				"functionDeclarations": tool.ToGoogleFormat(opts.Tools),
			},
		}
	}

	return body
}

// convertResponse converts a Vertex AI response to GenerateResult
// Uses the same response format as Google Generative AI
func (m *LanguageModel) convertResponse(response vertexResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertVertexUsage(response.UsageMetadata),
		RawResponse: response,
	}

	// Extract content from first candidate
	if len(response.Candidates) > 0 {
		candidate := response.Candidates[0]

		// Extract text from parts
		var textParts []string
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			// Handle function calls
			if part.FunctionCall != nil {
				result.ToolCalls = append(result.ToolCalls, types.ToolCall{
					ID:        part.FunctionCall.Name, // Vertex doesn't provide IDs
					ToolName:  part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				})
			}
		}

		if len(textParts) > 0 {
			result.Text = textParts[0]
		}

		// Map finish reason
		switch candidate.FinishReason {
		case "STOP":
			result.FinishReason = types.FinishReasonStop
		case "MAX_TOKENS":
			result.FinishReason = types.FinishReasonLength
		case "SAFETY":
			result.FinishReason = types.FinishReasonContentFilter
		default:
			result.FinishReason = types.FinishReasonOther
		}
	}

	return result
}

// handleError converts various errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("google-vertex", 0, "", err.Error(), err)
}

// convertVertexUsage converts Vertex AI usage to detailed Usage struct
func convertVertexUsage(usage *vertexUsageMetadata) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	promptTokens := int64(usage.PromptTokenCount)
	candidatesTokens := int64(usage.CandidatesTokenCount)
	cachedContentTokens := int64(usage.CachedContentTokenCount)
	thoughtsTokens := int64(usage.ThoughtsTokenCount)

	// Calculate totals
	totalOutputTokens := candidatesTokens + thoughtsTokens
	totalTokens := promptTokens + totalOutputTokens

	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &totalOutputTokens,
		TotalTokens:  &totalTokens,
	}

	// Parse text and image tokens from promptTokensDetails
	var textTokens *int64
	var imageTokens *int64
	if usage.PromptTokensDetails != nil && len(usage.PromptTokensDetails) > 0 {
		var textCount, imageCount int64
		for _, detail := range usage.PromptTokensDetails {
			switch detail.Modality {
			case "TEXT":
				textCount += int64(detail.TokenCount)
			case "IMAGE":
				imageCount += int64(detail.TokenCount)
			}
		}
		if textCount > 0 {
			textTokens = &textCount
		}
		if imageCount > 0 {
			imageTokens = &imageCount
		}
	}

	// Set input token details (cache information and text/image breakdown)
	if cachedContentTokens > 0 || textTokens != nil || imageTokens != nil {
		noCacheTokens := promptTokens - cachedContentTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cachedContentTokens,
			CacheWriteTokens: nil, // Vertex doesn't report cache write tokens separately
			TextTokens:       textTokens,
			ImageTokens:      imageTokens,
		}
	}

	// Set output token details (text vs reasoning tokens)
	if thoughtsTokens > 0 {
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &candidatesTokens,
			ReasoningTokens: &thoughtsTokens,
		}
	}

	// Store raw usage for provider-specific details
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

// vertexResponse represents the Vertex AI API response
// Uses the same format as Google Generative AI
type vertexResponse struct {
	Candidates []struct {
		Content struct {
			Parts []vertexPart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	UsageMetadata *vertexUsageMetadata `json:"usageMetadata,omitempty"`
}

// vertexUsageMetadata represents Vertex AI's usage information
type vertexUsageMetadata struct {
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

// vertexPart represents a part in Vertex AI's content structure
type vertexPart struct {
	Text         string `json:"text,omitempty"`
	FunctionCall *struct {
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	} `json:"functionCall,omitempty"`
}

// vertexStream implements provider.TextStream for Vertex AI streaming
type vertexStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newVertexStream creates a new Vertex AI stream
func newVertexStream(reader io.ReadCloser) *vertexStream {
	return &vertexStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
	}
}

// Close implements io.Closer
func (s *vertexStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *vertexStream) Next() (*provider.StreamChunk, error) {
	if s.err != nil {
		return nil, s.err
	}

	// Get next SSE event
	event, err := s.parser.Next()
	if err != nil {
		s.err = err
		return nil, err
	}

	// Check for stream completion
	if streaming.IsStreamDone(event) {
		s.err = io.EOF
		return nil, io.EOF
	}

	// Parse the event data as JSON
	var chunkData vertexResponse
	if err := json.Unmarshal([]byte(event.Data), &chunkData); err != nil {
		return nil, fmt.Errorf("failed to parse stream chunk: %w", err)
	}

	// Extract text from candidates
	if len(chunkData.Candidates) > 0 {
		candidate := chunkData.Candidates[0]

		// Extract text from parts
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				return &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					Text: part.Text,
				}, nil
			}
		}

		// Check for finish reason
		if candidate.FinishReason != "" && candidate.FinishReason != "STOP" {
			var finishReason types.FinishReason
			switch candidate.FinishReason {
			case "STOP":
				finishReason = types.FinishReasonStop
			case "MAX_TOKENS":
				finishReason = types.FinishReasonLength
			case "SAFETY":
				finishReason = types.FinishReasonContentFilter
			default:
				finishReason = types.FinishReasonOther
			}

			return &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: finishReason,
			}, nil
		}
	}

	// Empty chunk, get next
	return s.Next()
}

// Err returns any error that occurred during streaming
func (s *vertexStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
