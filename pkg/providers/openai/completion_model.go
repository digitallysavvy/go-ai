package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// CompletionModel implements the OpenAI Completions API language model.
type CompletionModel struct {
	provider *Provider
	modelID  string
}

// NewCompletionModel creates a new OpenAI completion model.
func NewCompletionModel(provider *Provider, modelID string) *CompletionModel {
	return &CompletionModel{provider: provider, modelID: modelID}
}

func (m *CompletionModel) SpecificationVersion() string { return "v4" }

func (m *CompletionModel) Provider() string { return m.provider.completionProviderName() }

func (m *CompletionModel) ModelID() string { return m.modelID }

func (m *CompletionModel) SupportsTools() bool { return false }

func (m *CompletionModel) SupportsStructuredOutput() bool { return false }

func (m *CompletionModel) SupportsImageInput() bool { return false }

func (m *CompletionModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	body, warnings, err := m.buildCompletionRequest(opts, false)
	if err != nil {
		return nil, err
	}

	var response openAICompletionResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/completions",
		Query:   m.provider.completionQuery(),
		Body:    body,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	result := m.convertCompletionResponse(response, warnings)
	result.RawRequest = body
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	result.ResponseMetadata = completionResponseMetadata(response.ID, response.Created, response.Model, result.ResponseHeaders)
	return result, nil
}

func (m *CompletionModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, warnings, err := m.buildCompletionRequest(opts, true)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/completions",
		Query:  m.provider.completionQuery(),
		Body:   body,
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Accept": "text/event-stream",
		}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	inner := newCompletionStreamWithMetadata(httpResp.Body, opts.IncludeRawChunks, m.Provider(), httpResp.Header)
	return streaming.NewWarningsStream(inner, warnings), nil
}

func (m *CompletionModel) buildCompletionRequest(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	warnings := completionWarnings(opts)
	completionPrompt, stopSequences, err := convertToCompletionPrompt(opts.Prompt)
	if err != nil {
		return nil, nil, err
	}

	body := map[string]interface{}{
		"model":  m.modelID,
		"prompt": completionPrompt,
	}
	stop := append([]string{}, stopSequences...)
	stop = append(stop, opts.StopSequences...)
	if len(stop) > 0 {
		body["stop"] = stop
	}

	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	for key, value := range m.completionProviderOptions(opts.ProviderOptions) {
		switch key {
		case "echo":
			body["echo"] = value
		case "logitBias":
			body["logit_bias"] = value
		case "logit_bias":
			body["logit_bias"] = value
		case "logprobs":
			if enabled, ok := value.(bool); ok {
				if enabled {
					body["logprobs"] = 0
				}
				continue
			}
			body["logprobs"] = value
		case "suffix":
			body["suffix"] = value
		case "user":
			body["user"] = value
		}
	}

	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}

	return body, warnings, nil
}

func (m *CompletionModel) completionProviderOptions(providerOptions map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	copyCompletionOptions := func(name string) {
		raw, ok := providerOptions[name]
		if !ok {
			return
		}
		if opts, ok := raw.(map[string]interface{}); ok {
			for k, v := range opts {
				merged[k] = v
			}
		}
	}
	copyCompletionOptions("openai")
	if name := m.provider.completionProviderOptionsName(); name != "openai" {
		copyCompletionOptions(name)
	}
	return merged
}

func completionWarnings(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if len(opts.Tools) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "tools"})
	}
	if opts.ToolChoice.Type != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "toolChoice"})
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "" && opts.ResponseFormat.Type != "text" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "responseFormat",
			Details: "JSON response format is not supported.",
		})
	}
	return warnings
}

func convertToCompletionPrompt(prompt types.Prompt) (string, []string, error) {
	messages := prompt.Messages
	if prompt.IsSimple() {
		messages = []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: prompt.Text}},
		}}
	}
	if prompt.System != "" {
		messages = append([]types.Message{{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: prompt.System}},
		}}, messages...)
	}

	text := ""
	if len(messages) > 0 && messages[0].Role == types.RoleSystem {
		text += completionTextContent(messages[0].Content) + "\n\n"
		messages = messages[1:]
	}

	for _, message := range messages {
		switch message.Role {
		case types.RoleSystem:
			return "", nil, fmt.Errorf("unexpected system message in prompt")
		case types.RoleUser:
			text += "user:\n" + completionTextContent(message.Content) + "\n\n"
		case types.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				return "", nil, fmt.Errorf("unsupported functionality: tool-call messages")
			}
			assistantText, err := completionAssistantTextContent(message.Content)
			if err != nil {
				return "", nil, err
			}
			text += "assistant:\n" + assistantText + "\n\n"
		case types.RoleTool:
			return "", nil, fmt.Errorf("unsupported functionality: tool messages")
		default:
			return "", nil, fmt.Errorf("unsupported role: %s", message.Role)
		}
	}

	text += "assistant:\n"
	return text, []string{"\nuser:"}, nil
}

func completionTextContent(parts []types.ContentPart) string {
	text := ""
	for _, part := range parts {
		if t, ok := part.(types.TextContent); ok {
			text += t.Text
		}
	}
	return text
}

func completionAssistantTextContent(parts []types.ContentPart) (string, error) {
	text := ""
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			text += p.Text
		case *types.TextContent:
			if p != nil {
				text += p.Text
			}
		case types.ToolCallContent, *types.ToolCallContent:
			return "", fmt.Errorf("unsupported functionality: tool-call messages")
		}
	}
	return text, nil
}

func (m *CompletionModel) convertCompletionResponse(response openAICompletionResponse, warnings []types.Warning) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:            convertOpenAICompletionUsage(response.Usage),
		RawResponse:      response,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{}},
	}
	if len(response.Choices) == 0 {
		return result
	}

	choice := response.Choices[0]
	result.Text = choice.Text
	result.Content = []types.ContentPart{types.TextContent{Text: choice.Text}}
	result.FinishReason = providerutils.MapOpenAIFinishReason(choice.FinishReason)
	if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
		result.ProviderMetadata["openai"].(map[string]interface{})["logprobs"] = json.RawMessage(choice.Logprobs)
	}
	return result
}

func convertOpenAICompletionUsage(usage *openAICompletionUsage) types.Usage {
	if usage == nil {
		return types.Usage{}
	}

	input := int64(usage.PromptTokens)
	output := int64(usage.CompletionTokens)
	total := int64(usage.TotalTokens)
	return types.Usage{
		InputTokens:  &input,
		OutputTokens: &output,
		TotalTokens:  &total,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens: &input,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens: &output,
		},
		Raw: map[string]interface{}{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		},
	}
}

func completionResponseMetadata(id string, created *int64, model string, headers map[string]string) *types.ResponseMetadata {
	metadata := &types.ResponseMetadata{
		ID:      id,
		ModelID: model,
		Headers: headers,
	}
	if created != nil {
		metadata.Timestamp = time.Unix(*created, 0).UTC()
	}
	return metadata
}

func (m *CompletionModel) handleError(err error) error {
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

type openAICompletionResponse struct {
	ID      string                   `json:"id"`
	Created *int64                   `json:"created"`
	Model   string                   `json:"model"`
	Choices []openAICompletionChoice `json:"choices"`
	Usage   *openAICompletionUsage   `json:"usage"`
}

type openAICompletionChoice struct {
	Text         string          `json:"text"`
	FinishReason string          `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs"`
}

type openAICompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type completionStream struct {
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	includeRawChunks bool
	err              error
	flushQueue       []*provider.StreamChunk
	metadataEmitted  bool
	pendingRaw       []*provider.StreamChunk
	pendingMetadata  *provider.StreamChunk
	textStarted      bool
	outputStarted    bool
	finished         bool
	finishReason     types.FinishReason
	usage            *types.Usage
	providerMetadata map[string]interface{}
	providerName     string
	responseHeaders  http.Header
}

func newCompletionStream(reader io.ReadCloser, includeRawChunks bool) *completionStream {
	return newCompletionStreamWithMetadata(reader, includeRawChunks, "openai.completion", nil)
}

func newCompletionStreamWithMetadata(reader io.ReadCloser, includeRawChunks bool, providerName string, headers http.Header) *completionStream {
	if providerName == "" {
		providerName = "openai.completion"
	}
	return &completionStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		includeRawChunks: includeRawChunks,
		finishReason:     types.FinishReasonOther,
		providerMetadata: map[string]interface{}{"openai": map[string]interface{}{}},
		providerName:     providerName,
		responseHeaders:  headers,
	}
}

func (s *completionStream) Next() (*provider.StreamChunk, error) {
	if len(s.flushQueue) > 0 {
		chunk := s.flushQueue[0]
		s.flushQueue = s.flushQueue[1:]
		return chunk, nil
	}
	if s.err != nil {
		return nil, s.err
	}

	event, err := s.parser.Next()
	if err != nil {
		if err == io.EOF && !s.finished {
			s.finish()
			return s.Next()
		}
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
		s.finish()
		return s.Next()
	}
	var chunk openAICompletionChunk
	if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
		s.queueRawChunk(event.Data)
		s.finishReason = types.FinishReasonError
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		})
		return s.Next()
	}
	rawChunk := s.rawChunk(event.Data)
	if len(chunk.Error) > 0 {
		s.finishReason = types.FinishReasonError
		if !s.outputStarted {
			s.flushQueue = nil
			s.pendingRaw = nil
			s.pendingMetadata = nil
			s.err = newOpenAIStreamProviderError(s.providerName, json.RawMessage(event.Data), s.responseHeaders)
			return nil, s.err
		}
		if rawChunk != nil {
			s.flushQueue = append(s.flushQueue, rawChunk)
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: openAIStreamErrorText(chunk.Error),
		})
		return s.Next()
	}
	if !s.metadataEmitted {
		s.metadataEmitted = true
		s.pendingMetadata = &provider.StreamChunk{
			Type: provider.ChunkTypeResponseMetadata,
			ResponseMetadata: &provider.ResponseMetadata{
				ID:        chunk.ID,
				ModelID:   chunk.Model,
				Timestamp: completionStreamTimestamp(chunk.Created),
			},
		}
	}
	if chunk.Usage != nil {
		usage := convertOpenAICompletionUsage(chunk.Usage)
		s.usage = &usage
	}
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			s.finishReason = providerutils.MapOpenAIFinishReason(*choice.FinishReason)
		}
		if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
			s.providerMetadata["openai"].(map[string]interface{})["logprobs"] = json.RawMessage(choice.Logprobs)
		}
		if choice.Text != "" {
			s.outputStarted = true
			if rawChunk != nil {
				s.pendingRaw = append(s.pendingRaw, rawChunk)
			}
			s.flushPendingMetadata()
			if !s.textStarted {
				s.textStarted = true
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "0"})
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				ID:   "0",
				Text: choice.Text,
			})
		}
	}
	if rawChunk != nil {
		s.pendingRaw = append(s.pendingRaw, rawChunk)
	}
	return s.Next()
}

func (s *completionStream) finish() {
	if s.finished {
		s.err = io.EOF
		return
	}
	s.finished = true
	s.flushPendingMetadata()
	if s.textStarted {
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "0"})
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.finishReason,
		Usage:            s.usage,
		ProviderMetadata: mustMarshalCompletionProviderMetadata(s.providerMetadata),
	})
	s.err = io.EOF
}

func (s *completionStream) flushPendingMetadata() {
	if len(s.pendingRaw) > 0 {
		s.flushQueue = append(s.flushQueue, s.pendingRaw...)
		s.pendingRaw = nil
	}
	if s.pendingMetadata != nil {
		s.flushQueue = append(s.flushQueue, s.pendingMetadata)
		s.pendingMetadata = nil
	}
}

func (s *completionStream) queueRawChunk(data string) {
	if raw := s.rawChunk(data); raw != nil {
		s.flushQueue = append(s.flushQueue, raw)
	}
}

func (s *completionStream) rawChunk(data string) *provider.StreamChunk {
	if !s.includeRawChunks {
		return nil
	}
	var raw interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		raw = data
	}
	return &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw}
}

func mustMarshalCompletionProviderMetadata(metadata map[string]interface{}) json.RawMessage {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return raw
}

func completionStreamTimestamp(created *int64) time.Time {
	if created == nil {
		return time.Time{}
	}
	return time.Unix(*created, 0).UTC()
}

func (s *completionStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *completionStream) Close() error {
	return s.reader.Close()
}

type openAICompletionChunk struct {
	ID      string                        `json:"id"`
	Created *int64                        `json:"created"`
	Model   string                        `json:"model"`
	Choices []openAICompletionChunkChoice `json:"choices"`
	Usage   *openAICompletionUsage        `json:"usage"`
	Error   json.RawMessage               `json:"error,omitempty"`
}

type openAICompletionChunkChoice struct {
	Text         string          `json:"text"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs"`
}

var _ provider.LanguageModel = (*CompletionModel)(nil)
