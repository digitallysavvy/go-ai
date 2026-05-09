package cohere

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Cohere
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Cohere language model
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
	return "cohere"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return false
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, m.handleError(err)
	}
	var response cohereV2Response
	err = m.provider.client.PostJSON(ctx, "/v2/chat", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	return m.convertV2Response(response), nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, m.handleError(err)
	}
	reqBody["stream"] = true
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v2/chat",
		Body:   reqBody,
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return newCohereV2Stream(httpResp.Body), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	body := map[string]interface{}{"model": m.modelID}
	var messages []map[string]interface{}
	var documents []map[string]interface{}
	if opts.Prompt.System != "" {
		messages = append(messages, map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		})
	}
	if opts.Prompt.IsSimple() {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": opts.Prompt.Text,
		})
	} else if opts.Prompt.IsMessages() {
		cohereMsgs, cohereDocs, err := m.toCohereMessages(opts.Prompt.Messages)
		if err != nil {
			return nil, err
		}
		messages = append(messages, cohereMsgs...)
		documents = append(documents, cohereDocs...)
	}
	body["messages"] = messages
	if len(documents) > 0 {
		body["documents"] = documents
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if thinking := m.resolveThinking(opts); thinking != nil {
		body["thinking"] = thinking
	}
	return body, nil
}

func (m *LanguageModel) toCohereMessages(src []types.Message) ([]map[string]interface{}, []map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(src))
	documents := []map[string]interface{}{}
	for _, msg := range src {
		if msg.Role != types.RoleUser {
			out = append(out, prompt.ToOpenAIMessages([]types.Message{msg})...)
			continue
		}
		content := make([]map[string]interface{}, 0, len(msg.Content))
		hasImage := false
		for _, part := range msg.Content {
			switch p := part.(type) {
			case types.TextContent:
				if p.Text != "" {
					content = append(content, map[string]interface{}{"type": "text", "text": p.Text})
				}
			case types.ImageContent:
				normalized, err := prompt.NormalizeImageContent(p)
				if err != nil {
					return nil, nil, err
				}
				url, err := fileDataToImageURL(normalized)
				if err != nil {
					return nil, nil, err
				}
				img := map[string]interface{}{"url": url}
				if detail := cohereImageDetail(normalized.ProviderOptions); detail != "" {
					img["detail"] = detail
				}
				content = append(content, map[string]interface{}{"type": "image_url", "image_url": img})
				hasImage = true
			case types.FileContent:
				normalized, err := prompt.NormalizeFileContent(p)
				if err != nil {
					return nil, nil, err
				}
				if strings.HasPrefix(normalized.FileData.MediaType, "image") {
					url, err := fileDataToImageURL(normalized)
					if err != nil {
						return nil, nil, err
					}
					img := map[string]interface{}{"url": url}
					if detail := cohereImageDetail(normalized.ProviderOptions); detail != "" {
						img["detail"] = detail
					}
					content = append(content, map[string]interface{}{"type": "image_url", "image_url": img})
					hasImage = true
				} else {
					text, err := cohereDocumentText(normalized)
					if err != nil {
						return nil, nil, err
					}
					document := map[string]interface{}{
						"data": map[string]interface{}{"text": text},
					}
					if normalized.Filename != "" {
						document["data"].(map[string]interface{})["title"] = normalized.Filename
					}
					documents = append(documents, document)
				}
			}
		}
		if hasImage {
			out = append(out, map[string]interface{}{"role": "user", "content": content})
			continue
		}
		var b strings.Builder
		for _, c := range content {
			if c["type"] == "text" {
				b.WriteString(c["text"].(string))
			}
		}
		out = append(out, map[string]interface{}{"role": "user", "content": b.String()})
	}
	return out, documents, nil
}

func fileDataToImageURL(file types.FileContent) (string, error) {
	switch file.FileData.Type {
	case types.FileDataTypeURL:
		return file.FileData.URL, nil
	case types.FileDataTypeData:
		media := file.FileData.MediaType
		if media == "" {
			media = "image/jpeg"
		}
		return "data:" + media + ";base64," + base64.StdEncoding.EncodeToString(file.FileData.Data), nil
	default:
		return "", providererrors.NewValidationError("messages[].content[].file", "unsupported image file data type for cohere", nil)
	}
}

func cohereDocumentText(file types.FileContent) (string, error) {
	switch file.FileData.Type {
	case types.FileDataTypeText:
		return file.FileData.Text, nil
	case types.FileDataTypeData:
		return string(file.FileData.Data), nil
	case types.FileDataTypeURL:
		return "", providererrors.NewValidationError("messages[].content[].file", "unsupported file URL data for cohere", nil)
	case types.FileDataTypeReference:
		return "", providererrors.NewValidationError("messages[].content[].file", "unsupported file reference data for cohere", nil)
	default:
		return "", providererrors.NewValidationError("messages[].content[].file", "unsupported file data type for cohere", nil)
	}
}

func cohereImageDetail(providerOptions map[string]interface{}) string {
	raw, ok := providerOptions["cohere"]
	if !ok {
		return ""
	}
	asMap, ok := raw.(map[string]interface{})
	if !ok {
		return ""
	}
	if d, ok := asMap["detail"].(string); ok {
		return d
	}
	return ""
}

func (m *LanguageModel) resolveThinking(opts *provider.GenerateOptions) map[string]interface{} {
	if opts.Reasoning == nil {
		return nil
	}
	switch *opts.Reasoning {
	case types.ReasoningNone:
		return map[string]interface{}{"type": "disabled"}
	case types.ReasoningMinimal:
		return map[string]interface{}{"type": "enabled", "token_budget": 1024}
	case types.ReasoningLow:
		return map[string]interface{}{"type": "enabled", "token_budget": 3277}
	case types.ReasoningMedium:
		return map[string]interface{}{"type": "enabled", "token_budget": 9830}
	case types.ReasoningHigh:
		return map[string]interface{}{"type": "enabled", "token_budget": 19661}
	case types.ReasoningXHigh:
		return map[string]interface{}{"type": "enabled", "token_budget": 29491}
	default:
		return nil
	}
}

func (m *LanguageModel) convertV2Response(resp cohereV2Response) *types.GenerateResult {
	result := &types.GenerateResult{
		FinishReason: mapCohereV2FinishReason(resp.FinishReason),
		RawResponse:  resp,
	}
	inputTokens := int64(resp.Usage.Tokens.InputTokens)
	outputTokens := int64(resp.Usage.Tokens.OutputTokens)
	totalTokens := inputTokens + outputTokens
	result.Usage = types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}
	result.Usage.InputDetails = &types.InputTokenDetails{NoCacheTokens: &inputTokens}
	result.Usage.OutputDetails = &types.OutputTokenDetails{TextTokens: &outputTokens}
	result.Usage.Raw = map[string]interface{}{
		"input_tokens":  resp.Usage.Tokens.InputTokens,
		"output_tokens": resp.Usage.Tokens.OutputTokens,
	}
	for _, item := range resp.Message.Content {
		switch item.Type {
		case "text":
			result.Text += item.Text
		case "thinking":
			if item.Thinking != "" {
				result.Content = append(result.Content, types.ReasoningContent{Text: item.Thinking})
			}
		}
	}
	for _, tc := range resp.Message.ToolCalls {
		var args map[string]interface{}
		json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
		result.ToolCalls = append(result.ToolCalls, types.ToolCall{
			ID:        tc.ID,
			ToolName:  tc.Function.Name,
			Arguments: args,
		})
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
}

func mapCohereV2FinishReason(reason string) types.FinishReason {
	switch reason {
	case "COMPLETE":
		return types.FinishReasonStop
	case "MAX_TOKENS":
		return types.FinishReasonLength
	case "TOOL_CALL":
		return types.FinishReasonToolCalls
	case "ERROR":
		return types.FinishReasonError
	default:
		return types.FinishReasonOther
	}
}

type cohereV2Response struct {
	GenerationID string `json:"generation_id"`
	Message      struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text,omitempty"`
			Thinking string `json:"thinking,omitempty"`
		} `json:"content"`
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
	Usage        struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"usage"`
}

type cohereV2Stream struct {
	reader            io.ReadCloser
	parser            *streaming.SSEParser
	err               error
	flushQueue        []*provider.StreamChunk
	contentType       map[int]string // index → "text" | "thinking"
	pendingTools      map[string]*cohereV2PendingTool
	isActiveReasoning bool
}

type cohereV2PendingTool struct {
	id        string
	name      string
	arguments string
	finished  bool
}

func newCohereV2Stream(reader io.ReadCloser) *cohereV2Stream {
	return &cohereV2Stream{
		reader:       reader,
		parser:       streaming.NewSSEParser(reader),
		contentType:  make(map[int]string),
		pendingTools: make(map[string]*cohereV2PendingTool),
	}
}

func (s *cohereV2Stream) Close() error { return s.reader.Close() }
func (s *cohereV2Stream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *cohereV2Stream) Next() (*provider.StreamChunk, error) {
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
		s.err = err
		return nil, err
	}
	if streaming.IsStreamDone(event) {
		s.err = io.EOF
		return nil, io.EOF
	}

	// Cohere v2 SSE: each event has a type field plus a delta field.
	var ev struct {
		Type  string          `json:"type"`
		Index int             `json:"index"`
		Delta json.RawMessage `json:"delta"`
	}
	if err := json.Unmarshal([]byte(event.Data), &ev); err != nil {
		return s.Next()
	}

	switch ev.Type {
	case "message-start":
		return s.Next()

	case "content-start":
		var delta struct {
			Message struct {
				Content struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil {
			contentType := delta.Message.Content.Type
			s.contentType[ev.Index] = contentType
			if contentType == "thinking" {
				s.isActiveReasoning = true
				return &provider.StreamChunk{
					Type: provider.ChunkTypeReasoningStart,
					ID:   fmt.Sprintf("reasoning-%d", ev.Index),
				}, nil
			}
		}
		return s.Next()

	case "content-delta":
		ctype := s.contentType[ev.Index]
		if ctype == "thinking" {
			var delta struct {
				Message struct {
					Content struct {
						Thinking string `json:"thinking"`
					} `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Message.Content.Thinking != "" {
				return &provider.StreamChunk{
					Type:      provider.ChunkTypeReasoning,
					Reasoning: delta.Message.Content.Thinking,
					ID:        fmt.Sprintf("reasoning-%d", ev.Index),
				}, nil
			}
		} else {
			var delta struct {
				Message struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal(ev.Delta, &delta); err == nil && delta.Message.Content.Text != "" {
				return &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					Text: delta.Message.Content.Text,
				}, nil
			}
		}
		return s.Next()

	case "content-end":
		ctype := s.contentType[ev.Index]
		if ctype == "thinking" {
			s.isActiveReasoning = false
			return &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningEnd,
				ID:   fmt.Sprintf("reasoning-%d", ev.Index),
			}, nil
		}
		return s.Next()

	case "tool-call-start":
		var delta struct {
			Message struct {
				ToolCalls struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil {
			id := delta.Message.ToolCalls.ID
			s.pendingTools[id] = &cohereV2PendingTool{
				id:        id,
				name:      delta.Message.ToolCalls.Function.Name,
				arguments: delta.Message.ToolCalls.Function.Arguments,
			}
		}
		return s.Next()

	case "tool-call-delta":
		var delta struct {
			Message struct {
				ToolCalls struct {
					Function struct {
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil {
			// Cohere v2 supports only one pending tool call at a time; append to it.
			for _, tc := range s.pendingTools {
				if !tc.finished {
					tc.arguments += delta.Message.ToolCalls.Function.Arguments
					break
				}
			}
		}
		return s.Next()

	case "tool-call-end":
		for id, tc := range s.pendingTools {
			if !tc.finished {
				tc.finished = true
				var args map[string]interface{}
				if tc.arguments != "" {
					json.Unmarshal([]byte(tc.arguments), &args) //nolint:errcheck
				}
				delete(s.pendingTools, id)
				return &provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:        tc.id,
						ToolName:  tc.name,
						Arguments: args,
					},
				}, nil
			}
		}
		return s.Next()

	case "message-end":
		var delta struct {
			FinishReason string `json:"finish_reason"`
			Usage        struct {
				Tokens struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil {
			inputTokens := int64(delta.Usage.Tokens.InputTokens)
			outputTokens := int64(delta.Usage.Tokens.OutputTokens)
			totalTokens := inputTokens + outputTokens
			usage := &types.Usage{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			}
			usage.InputDetails = &types.InputTokenDetails{NoCacheTokens: &inputTokens}
			usage.OutputDetails = &types.OutputTokenDetails{TextTokens: &outputTokens}
			return &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: mapCohereV2FinishReason(delta.FinishReason),
				Usage:        usage,
			}, nil
		}
		s.err = io.EOF
		return nil, io.EOF

	default:
		// citation-start, citation-end, tool-plan-delta, etc. — ignore
		return s.Next()
	}
}
