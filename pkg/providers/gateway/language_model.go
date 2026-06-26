package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for AI Gateway
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new AI Gateway language model
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "gateway"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Gateway passes through to underlying models, assume tools are supported
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	// Gateway passes through to underlying models, assume structured output is supported
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Gateway passes through to underlying models, assume image input is supported
	return true
}

// SupportedURLs reports that Gateway can pass through direct file URLs.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"*/*": {`.*`},
	}
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Convert options to request body
	reqBody, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Create headers for the request
	headers := m.getModelConfigHeaders(false)

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)

	// Make API request
	var result types.GenerateResult
	err = m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/language-model",
		Body:    reqBody,
		Headers: headers,
	}, &result)
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	return &result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Convert options to request body
	reqBody, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	// Create headers for the request
	headers := m.getModelConfigHeaders(true)
	headers["Accept"] = "text/event-stream"

	// Add observability headers if in Vercel environment
	o11y := GetO11yHeaders(ctx)
	AddO11yHeaders(headers, o11y)

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/language-model",
		Body:    reqBody,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleErrorWithContext(ctx, err)
	}

	// Create streaming parser and wrap it in a proper TextStream implementation
	stream := streaming.NewSSEParser(httpResp.Body)

	return &gatewayTextStream{
		parser:           stream,
		body:             httpResp.Body,
		includeRawChunks: opts.IncludeRawChunks,
	}, nil
}

// gatewayTextStream implements provider.TextStream for Gateway provider
type gatewayTextStream struct {
	parser           *streaming.SSEParser
	body             io.ReadCloser
	err              error
	includeRawChunks bool
}

// gatewayStreamChunk represents a chunk from the Gateway streaming API
// This follows the LanguageModelV3StreamPart format
type gatewayStreamChunk struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`

	// For text-delta chunks
	Delta     string `json:"delta,omitempty"`
	TextDelta string `json:"textDelta,omitempty"`

	// For reasoning-delta chunks
	ReasoningDelta string `json:"reasoningDelta,omitempty"`

	// For tool-call chunks
	ToolCallID       string                 `json:"toolCallId,omitempty"`
	ToolCallType     string                 `json:"toolCallType,omitempty"`
	ToolCallName     string                 `json:"toolCallName,omitempty"`
	ToolCallArgs     string                 `json:"toolCallArgs,omitempty"`
	ToolCallArgsText string                 `json:"toolCallArgsText,omitempty"`
	ToolCallArgsJSON map[string]interface{} `json:"-"` // Parsed from ToolCallArgs

	// For finish chunks
	FinishReason types.FinishReason `json:"finishReason,omitempty"`

	// For usage chunks
	Usage *struct {
		PromptTokens     *int64 `json:"promptTokens,omitempty"`
		CompletionTokens *int64 `json:"completionTokens,omitempty"`
		TotalTokens      *int64 `json:"totalTokens,omitempty"`
	} `json:"usage,omitempty"`

	// For error chunks
	Error string `json:"error,omitempty"`

	Warnings []types.Warning `json:"warnings,omitempty"`

	// For response-metadata chunks
	ModelID   string `json:"modelId,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`

	// For source chunks
	SourceType       string          `json:"sourceType,omitempty"`
	SourceID         string          `json:"sourceId,omitempty"`
	URL              string          `json:"url,omitempty"`
	MediaType        string          `json:"mediaType,omitempty"`
	Title            string          `json:"title,omitempty"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`

	// For file/custom chunks
	Data            json.RawMessage        `json:"data,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`

	raw map[string]interface{}
}

// Read implements io.Reader
func (s *gatewayTextStream) Read(p []byte) (n int, err error) {
	return s.body.Read(p)
}

// Close implements io.Closer
func (s *gatewayTextStream) Close() error {
	return s.body.Close()
}

// Next returns the next chunk in the stream
func (s *gatewayTextStream) Next() (*provider.StreamChunk, error) {
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

	// Parse the JSON data from the SSE event
	var chunk gatewayStreamChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		s.err = fmt.Errorf("failed to parse stream chunk: %w", err)
		return nil, s.err
	}
	_ = json.Unmarshal([]byte(event.Data), &chunk.raw)

	// Convert Gateway chunk to provider StreamChunk
	return s.convertChunk(&chunk)
}

// convertChunk converts a Gateway stream chunk to a provider StreamChunk
func (s *gatewayTextStream) convertChunk(chunk *gatewayStreamChunk) (*provider.StreamChunk, error) {
	switch chunk.Type {
	case "stream-start":
		return &provider.StreamChunk{
			Type:     provider.ChunkTypeStreamStart,
			Warnings: chunk.Warnings,
		}, nil

	case "raw":
		if !s.includeRawChunks {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeRaw,
			Raw:  chunk.raw,
		}, nil

	case "response-metadata":
		metadata := &provider.ResponseMetadata{
			ID:      chunk.ID,
			ModelID: chunk.ModelID,
		}
		if chunk.Timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, chunk.Timestamp); err == nil {
				metadata.Timestamp = parsed
			}
		}
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: metadata,
		}, nil

	case "text-start":
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeTextStart,
			ID:               chunk.ID,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "text-delta":
		text := chunk.TextDelta
		if text == "" {
			text = chunk.Delta
		}
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeText,
			ID:               chunk.ID,
			Text:             text,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "text-end":
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeTextEnd,
			ID:               chunk.ID,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "reasoning-start":
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningStart,
			ID:               chunk.ID,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "reasoning-delta":
		reasoning := chunk.ReasoningDelta
		if reasoning == "" {
			reasoning = chunk.Delta
		}
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoning,
			ID:               chunk.ID,
			Reasoning:        reasoning,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "reasoning-end":
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               chunk.ID,
			ProviderMetadata: chunk.ProviderMetadata,
		}, nil

	case "tool-call", "tool-call-delta":
		// Parse tool call arguments if present
		var args map[string]interface{}
		if chunk.ToolCallArgs != "" {
			_ = json.Unmarshal([]byte(chunk.ToolCallArgs), &args) //nolint:errcheck
		} else if chunk.ToolCallArgsText != "" {
			_ = json.Unmarshal([]byte(chunk.ToolCallArgsText), &args) //nolint:errcheck
		}

		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        chunk.ToolCallID,
				ToolName:  chunk.ToolCallName,
				Arguments: args,
			},
		}, nil

	case "finish":
		result := &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: chunk.FinishReason,
		}

		// Add usage information if present
		if chunk.Usage != nil {
			result.Usage = &types.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}

		return result, nil

	case "source":
		sourceID := chunk.SourceID
		if sourceID == "" {
			sourceID = chunk.ID
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeSource,
			SourceContent: &types.SourceContent{
				SourceType:       chunk.SourceType,
				ID:               sourceID,
				URL:              chunk.URL,
				MediaType:        chunk.MediaType,
				Title:            chunk.Title,
				Filename:         chunk.Filename,
				ProviderMetadata: chunk.ProviderMetadata,
			},
		}, nil

	case "file":
		file := &types.GeneratedFileContent{
			MediaType:        chunk.MediaType,
			URL:              chunk.URL,
			ProviderMetadata: chunk.ProviderMetadata,
			ProviderOptions:  chunk.ProviderOptions,
		}
		if len(chunk.Data) > 0 {
			var dataString string
			if json.Unmarshal(chunk.Data, &dataString) == nil {
				file.FileData = types.FileData{Type: types.FileDataTypeData, DataString: dataString, MediaType: chunk.MediaType}
			} else {
				var fileData types.FileData
				if json.Unmarshal(chunk.Data, &fileData) == nil {
					file.FileData = fileData
					file.URL = fileData.URL
				}
			}
		}
		return &provider.StreamChunk{
			Type:                 provider.ChunkTypeFile,
			GeneratedFileContent: file,
		}, nil

	case "custom":
		return &provider.StreamChunk{
			Type: provider.ChunkTypeCustom,
			CustomContent: &types.CustomContent{
				Kind:             chunk.Kind,
				ProviderOptions:  chunk.ProviderOptions,
				ProviderMetadata: chunk.ProviderMetadata,
			},
		}, nil

	case "usage":
		if chunk.Usage != nil {
			return &provider.StreamChunk{
				Type: provider.ChunkTypeUsage,
				Usage: &types.Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
					TotalTokens:  chunk.Usage.TotalTokens,
				},
			}, nil
		}
		// Skip empty usage chunks
		return s.Next()

	case "error":
		return &provider.StreamChunk{
			Type:        provider.ChunkTypeError,
			AbortReason: chunk.Error,
		}, fmt.Errorf("stream error: %s", chunk.Error)

	default:
		// Skip unknown chunk types and get next chunk
		return s.Next()
	}
}

// Err implements TextStream
func (s *gatewayTextStream) Err() error {
	return s.err
}

// buildRequestBody converts GenerateOptions to the request body format
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, streaming bool) (map[string]interface{}, error) {
	body := make(map[string]interface{})

	// Convert prompt - gateway expects the V3 format
	if opts.Prompt.IsMessages() {
		// Convert messages to the format expected by gateway
		messages := make([]map[string]interface{}, 0, len(opts.Prompt.Messages))
		for _, msg := range opts.Prompt.Messages {
			message := map[string]interface{}{
				"role": string(msg.Role),
			}

			// Convert content parts
			if len(msg.Content) > 0 {
				content := make([]map[string]interface{}, 0, len(msg.Content))
				for _, part := range msg.Content {
					contentPart, err := m.convertContentPart(part)
					if err != nil {
						return nil, err
					}
					content = append(content, contentPart)
				}
				message["content"] = content
			}

			messages = append(messages, message)
		}
		body["prompt"] = messages
	} else if opts.Prompt.IsSimple() {
		// Simple text prompt - convert to user message
		body["prompt"] = []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": opts.Prompt.Text,
					},
				},
			},
		}
	}

	// Add system message if present
	if opts.Prompt.System != "" {
		body["system"] = opts.Prompt.System
	}

	// Add temperature
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}

	// Add top-p
	if opts.TopP != nil {
		body["topP"] = *opts.TopP
	}

	// Add top-k
	if opts.TopK != nil {
		body["topK"] = *opts.TopK
	}

	// Add presence penalty
	if opts.PresencePenalty != nil {
		body["presencePenalty"] = *opts.PresencePenalty
	}

	// Add frequency penalty
	if opts.FrequencyPenalty != nil {
		body["frequencyPenalty"] = *opts.FrequencyPenalty
	}

	// Add stop sequences
	if len(opts.StopSequences) > 0 {
		body["stopSequences"] = opts.StopSequences
	}

	// Add max tokens
	if opts.MaxTokens != nil {
		body["maxTokens"] = *opts.MaxTokens
	}

	// Add seed
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Add response format
	if opts.ResponseFormat != nil {
		format := map[string]interface{}{
			"type": opts.ResponseFormat.Type,
		}
		if opts.ResponseFormat.Schema != nil {
			format["schema"] = opts.ResponseFormat.Schema
		}
		if opts.ResponseFormat.Name != "" {
			format["name"] = opts.ResponseFormat.Name
		}
		if opts.ResponseFormat.Description != "" {
			format["description"] = opts.ResponseFormat.Description
		}
		body["responseFormat"] = format
	}

	// Add tools
	if len(opts.Tools) > 0 {
		tools := make([]map[string]interface{}, 0, len(opts.Tools))
		for _, tool := range opts.Tools {
			toolMap := map[string]interface{}{"name": tool.Name}
			if tool.Type == types.ToolTypeProviderDefined {
				toolMap["type"] = types.ToolTypeProviderDefined
				toolMap["id"] = tool.ProviderID
				if tool.ProviderArgs != nil {
					toolMap["args"] = tool.ProviderArgs
				} else {
					toolMap["args"] = map[string]interface{}{}
				}
				tools = append(tools, toolMap)
				continue
			}
			toolMap["description"] = tool.Description
			toolMap["parameters"] = tool.Parameters
			if tool.ProviderExecuted {
				toolMap["providerExecuted"] = true
			}
			tools = append(tools, toolMap)
		}
		body["tools"] = tools
	}

	// Add tool choice
	if opts.ToolChoice.Type != "" {
		body["toolChoice"] = map[string]interface{}{
			"type":     string(opts.ToolChoice.Type),
			"toolName": opts.ToolChoice.ToolName,
		}
	}

	// Add headers
	if len(opts.Headers) > 0 {
		body["headers"] = opts.Headers
	}

	if providerOptions := m.providerOptions(opts); len(providerOptions) > 0 {
		body["providerOptions"] = providerOptions
	}

	return body, nil
}

func (m *LanguageModel) providerOptions(opts *provider.GenerateOptions) map[string]interface{} {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	providerOptions := cloneProviderOptions(opts.ProviderOptions)
	configGatewayOptions := m.provider.configGatewayProviderOptions()
	if len(configGatewayOptions) > 0 {
		providerOptions["gateway"] = configGatewayOptions
	}
	if gatewayOptions, ok := opts.ProviderOptions["gateway"]; ok {
		if configGatewayOptions == nil {
			providerOptions["gateway"] = gatewayOptions
			return enforceGatewayServiceTier(providerOptions)
		}
		if callerGatewayOptions, ok := gatewayOptions.(map[string]interface{}); ok {
			merged := cloneMapStringInterface(configGatewayOptions)
			for k, v := range callerGatewayOptions {
				merged[k] = v
			}
			providerOptions["gateway"] = merged
		} else {
			providerOptions["gateway"] = gatewayOptions
		}
	}
	return enforceGatewayServiceTier(providerOptions)
}

func (p *Provider) configGatewayProviderOptions() map[string]interface{} {
	out := map[string]interface{}{}
	if p.config.DisallowPromptTraining {
		out["disallowPromptTraining"] = true
	}
	if p.config.HIPAACompliant {
		out["hipaaCompliant"] = true
	}
	if p.config.QuotaEntityID != "" {
		out["quotaEntityId"] = p.config.QuotaEntityID
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func enforceGatewayServiceTier(providerOptions map[string]interface{}) map[string]interface{} {
	gatewayOptions, ok := providerOptions["gateway"].(map[string]interface{})
	if !ok {
		return providerOptions
	}
	serviceTier, ok := gatewayOptions["serviceTier"].(string)
	if !ok || serviceTier == "" {
		return providerOptions
	}
	for providerName, raw := range providerOptions {
		if providerName == "gateway" {
			continue
		}
		if opts, ok := raw.(map[string]interface{}); ok {
			delete(opts, "serviceTier")
			delete(opts, "service_tier")
		}
	}
	return providerOptions
}

func cloneProviderOptions(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		if nested, ok := v.(map[string]interface{}); ok {
			out[k] = cloneMapStringInterface(nested)
		} else {
			out[k] = v
		}
	}
	return out
}

func cloneMapStringInterface(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// convertContentPart converts a content part to the gateway format
func (m *LanguageModel) convertContentPart(part types.ContentPart) (map[string]interface{}, error) {
	switch v := part.(type) {
	case types.TextContent:
		result := map[string]interface{}{
			"type": "text",
			"text": v.Text,
		}
		addProviderOptions(result, v.ProviderOptions)
		addProviderMetadata(result, v.ProviderMetadata)
		return result, nil

	case types.ImageContent:
		result := map[string]interface{}{
			"type": "image",
		}

		// Handle image URL or data
		if v.URL != "" {
			result["image"] = v.URL
		} else if len(v.Image) > 0 {
			// Image is binary data - encode as base64 data URL
			mediaType := v.MimeType
			if mediaType == "" {
				mediaType = "image/jpeg"
			}
			encoded := base64.StdEncoding.EncodeToString(v.Image)
			result["image"] = fmt.Sprintf("data:%s;base64,%s", mediaType, encoded)
		}

		addProviderOptions(result, v.ProviderOptions)
		return result, nil

	case types.FileContent:
		data, err := gatewayFileDataMap(v.FileData, v.Data, v.URL, v.Reference, v.Text)
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"type": "file",
			"data": data,
		}

		mediaType := v.MediaType
		if mediaType == "" {
			mediaType = v.MimeType
		}
		if mediaType != "" {
			result["mediaType"] = mediaType
		}
		if v.Filename != "" {
			result["filename"] = v.Filename
		}
		addProviderOptions(result, v.ProviderOptions)
		addProviderMetadata(result, v.ProviderMetadata)

		return result, nil

	case types.ReasoningFileContent:
		data, err := gatewayFileDataMap(v.FileData, v.Data, "", "", "")
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"type":      "reasoning-file",
			"data":      data,
			"mediaType": v.MediaType,
		}
		addProviderOptions(result, v.ProviderOptions)
		addProviderMetadata(result, v.ProviderMetadata)
		return result, nil

	case types.ToolResultContent:
		return gatewayToolResultMap(v)

	default:
		return nil, fmt.Errorf("unsupported content part type: %s", part.ContentType())
	}
}

func gatewayFileDataMap(fileData types.FileData, legacyData []byte, legacyURL, legacyReference, legacyText string) (map[string]interface{}, error) {
	fileType := fileData.Type
	switch {
	case fileType == "" && fileData.DataString != "":
		fileType = types.FileDataTypeData
	case fileType == "" && len(fileData.Data) > 0:
		fileType = types.FileDataTypeData
	case fileType == "" && len(legacyData) > 0:
		fileType = types.FileDataTypeData
	case fileType == "" && fileData.URL != "":
		fileType = types.FileDataTypeURL
	case fileType == "" && legacyURL != "":
		fileType = types.FileDataTypeURL
	case fileType == "" && len(fileData.Reference) > 0:
		fileType = types.FileDataTypeReference
	case fileType == "" && legacyReference != "":
		fileType = types.FileDataTypeReference
	case fileType == "" && fileData.Text != "":
		fileType = types.FileDataTypeText
	case fileType == "" && legacyText != "":
		fileType = types.FileDataTypeText
	}

	switch fileType {
	case types.FileDataTypeData, "":
		dataString := fileData.DataString
		if dataString == "" {
			data := fileData.Data
			if len(data) == 0 {
				data = legacyData
			}
			dataString = base64.StdEncoding.EncodeToString(data)
		}
		return map[string]interface{}{"type": "data", "data": dataString}, nil
	case types.FileDataTypeURL:
		url := fileData.URL
		if url == "" {
			url = legacyURL
		}
		return map[string]interface{}{"type": "url", "url": url}, nil
	case types.FileDataTypeReference:
		reference := fileData.Reference
		if len(reference) == 0 && legacyReference != "" {
			reference = types.ProviderReference{"provider": legacyReference}
		}
		return map[string]interface{}{"type": "reference", "reference": reference}, nil
	case types.FileDataTypeText:
		text := fileData.Text
		if text == "" {
			text = legacyText
		}
		return map[string]interface{}{"type": "text", "text": text}, nil
	default:
		return nil, fmt.Errorf("unsupported file data type: %s", fileType)
	}
}

func gatewayToolResultMap(part types.ToolResultContent) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"type":       "tool-result",
		"toolCallId": part.ToolCallID,
		"toolName":   part.ToolName,
	}
	if part.Title != "" {
		result["title"] = part.Title
	}
	if part.Input != nil {
		result["input"] = part.Input
	}
	if part.ProviderExecuted {
		result["providerExecuted"] = true
	}
	if part.Dynamic {
		result["dynamic"] = true
	}
	if part.Preliminary {
		result["preliminary"] = true
	}
	if part.ToolMetadata != nil {
		result["toolMetadata"] = part.ToolMetadata
	}
	addProviderOptions(result, part.ProviderOptions)
	addProviderMetadata(result, part.ProviderMetadata)

	switch {
	case part.Output != nil:
		output, err := gatewayToolResultOutputMap(*part.Output)
		if err != nil {
			return nil, err
		}
		result["output"] = output
	case part.Error != "":
		result["output"] = map[string]interface{}{"type": "error-text", "value": part.Error}
	case part.Result != nil:
		result["output"] = map[string]interface{}{"type": "json", "value": part.Result}
	}

	return result, nil
}

func gatewayToolResultOutputMap(output types.ToolResultOutput) (map[string]interface{}, error) {
	result := map[string]interface{}{"type": string(output.Type)}
	if output.Value != nil {
		result["value"] = output.Value
	}
	if output.Reason != "" {
		result["reason"] = output.Reason
	}
	addProviderOptions(result, output.ProviderOptions)

	if len(output.Content) > 0 {
		content := make([]map[string]interface{}, 0, len(output.Content))
		for _, block := range output.Content {
			converted, err := gatewayToolResultContentBlockMap(block)
			if err != nil {
				return nil, err
			}
			content = append(content, converted)
		}
		result["value"] = content
	}
	return result, nil
}

func gatewayToolResultContentBlockMap(block types.ToolResultContentBlock) (map[string]interface{}, error) {
	switch b := block.(type) {
	case types.TextContentBlock:
		result := map[string]interface{}{"type": "text", "text": b.Text}
		addProviderOptions(result, b.ProviderOptions)
		return result, nil
	case types.ImageContentBlock:
		result := map[string]interface{}{
			"type":      "image",
			"data":      base64.StdEncoding.EncodeToString(b.Data),
			"mediaType": b.MediaType,
		}
		addProviderOptions(result, b.ProviderOptions)
		return result, nil
	case types.FileContentBlock:
		data, err := gatewayFileDataMap(b.FileData, b.Data, b.URL, b.Reference, b.Text)
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"type": "file",
			"data": data,
		}
		if b.MediaType != "" {
			result["mediaType"] = b.MediaType
		}
		if b.Filename != "" {
			result["filename"] = b.Filename
		}
		addProviderOptions(result, b.ProviderOptions)
		return result, nil
	case types.CustomContentBlock:
		return map[string]interface{}{
			"type":            "custom",
			"providerOptions": b.ProviderOptions,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported tool result content block type: %s", block.ToolResultContentType())
	}
}

func addProviderOptions(result map[string]interface{}, options map[string]interface{}) {
	if len(options) > 0 {
		result["providerOptions"] = options
	}
}

func addProviderMetadata(result map[string]interface{}, metadata json.RawMessage) {
	if len(metadata) == 0 {
		return
	}
	var decoded interface{}
	if err := json.Unmarshal(metadata, &decoded); err == nil {
		result["providerMetadata"] = decoded
	}
}

// getModelConfigHeaders returns headers specific to the gateway model configuration
func (m *LanguageModel) getModelConfigHeaders(streaming bool) map[string]string {
	return map[string]string{
		"ai-language-model-specification-version": "4",
		"ai-language-model-id":                    m.modelID,
		"ai-language-model-streaming":             fmt.Sprintf("%t", streaming),
	}
}

// handleError converts errors to appropriate provider errors
func (m *LanguageModel) handleError(err error) error {
	return m.handleErrorWithContext(context.Background(), err)
}

func (m *LanguageModel) handleErrorWithContext(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}

	// Check if it's a timeout error and convert to GatewayTimeoutError
	if gatewayerrors.IsTimeoutError(err) {
		return gatewayerrors.ConvertToGatewayTimeoutError(err, "gateway")
	}

	if gatewayerrors.IsGatewayError(err) {
		return err
	}

	// Check if it's already a provider error
	if providererrors.IsProviderError(err) {
		return err
	}

	var httpStatusErr *internalhttp.HTTPStatusError
	if errors.As(err, &httpStatusErr) {
		return m.provider.gatewayAPIErrorWithContext(ctx, &internalhttp.Response{
			StatusCode: httpStatusErr.StatusCode,
			Headers:    httpStatusErr.Headers,
			Body:       httpStatusErr.Body,
		})
	}

	return m.provider.gatewayUnknownError(err)
}
