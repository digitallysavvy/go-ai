package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements the provider.LanguageModel interface for Google (Gemini)
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Google language model
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
	return "google"
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
	// Gemini Pro Vision supports images
	return m.modelID == "gemini-pro-vision" ||
		   m.modelID == "gemini-1.5-pro" ||
		   m.modelID == "gemini-1.5-flash"
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody := m.buildRequestBody(opts)

	// Build path with API key
	path := fmt.Sprintf("/v1beta/models/%s:generateContent?key=%s", m.modelID, m.provider.APIKey())

	// Make API request
	var response googleResponse
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

	// Build path with API key
	path := fmt.Sprintf("/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", m.modelID, m.provider.APIKey())

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
	return newGoogleStream(httpResp.Body), nil
}

// buildRequestBody builds the Google API request body
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

// convertResponse converts a Google response to GenerateResult
func (m *LanguageModel) convertResponse(response googleResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		RawResponse: response,
	}

	// Extract usage if available
	if response.UsageMetadata != nil {
		result.Usage = types.Usage{
			InputTokens:  response.UsageMetadata.PromptTokenCount,
			OutputTokens: response.UsageMetadata.CandidatesTokenCount,
			TotalTokens:  response.UsageMetadata.TotalTokenCount,
		}
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
					ID:        part.FunctionCall.Name, // Google doesn't provide IDs
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
	return providererrors.NewProviderError("google", 0, "", err.Error(), err)
}

// googleResponse represents the Google API response
type googleResponse struct {
	Candidates []struct {
		Content struct {
			Parts []googlePart `json:"parts"`
			Role  string       `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
		Index        int    `json:"index"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

// googlePart represents a part in Google's content structure
type googlePart struct {
	Text         string `json:"text,omitempty"`
	FunctionCall *struct {
		Name string                 `json:"name"`
		Args map[string]interface{} `json:"args"`
	} `json:"functionCall,omitempty"`
}

// googleStream implements provider.TextStream for Google streaming
type googleStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
}

// newGoogleStream creates a new Google stream
func newGoogleStream(reader io.ReadCloser) *googleStream {
	return &googleStream{
		reader: reader,
		parser: streaming.NewSSEParser(reader),
	}
}

// Read implements io.Reader
func (s *googleStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *googleStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *googleStream) Next() (*provider.StreamChunk, error) {
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
	var chunkData googleResponse
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
func (s *googleStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
