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
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts)
	if err != nil {
		return nil, m.handleError(err)
	}
	var response cohereV2Response
	err = m.provider.client.PostJSON(ctx, "/v2/chat", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result, err := m.convertV2Response(response)
	if err != nil {
		return nil, err
	}
	result.Warnings = append(warnings, result.Warnings...)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody, warnings, err := m.buildRequestBodyWithWarnings(opts)
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
	return streaming.NewWarningsStream(newCohereV2Stream(httpResp.Body), warnings), nil
}

// buildRequestBody builds the Cohere v2 chat request body without warnings.
// Kept for callers (and tests) that don't need the warning slice.
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	body, _, err := m.buildRequestBodyWithWarnings(opts)
	return body, err
}

// buildRequestBodyWithWarnings builds the Cohere v2 chat request body,
// mirroring TS CohereChatLanguageModel.getArgs: standardized settings
// (frequency_penalty, presence_penalty, p, k, seed, stop_sequences),
// response_format (json mode), tools/tool_choice, and thinking.
func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
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
			return nil, nil, err
		}
		messages = append(messages, cohereMsgs...)
		documents = append(documents, cohereDocs...)
	}
	body["messages"] = messages
	if len(documents) > 0 {
		body["documents"] = documents
	}

	// standardized settings:
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["p"] = *opts.TopP
	}
	if opts.TopK != nil {
		body["k"] = *opts.TopK
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	if len(opts.StopSequences) > 0 {
		body["stop_sequences"] = opts.StopSequences
	}

	// response format:
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		responseFormat := map[string]interface{}{"type": "json_object"}
		if opts.ResponseFormat.Schema != nil {
			responseFormat["json_schema"] = opts.ResponseFormat.Schema
		}
		body["response_format"] = responseFormat
	}

	// tools:
	cohereTools, cohereToolChoice, toolWarnings := prepareCohereTools(opts.Tools, opts.ToolChoice, opts.ToolChoice.Type != "")
	var warnings []types.Warning
	warnings = append(warnings, toolWarnings...)
	if cohereTools != nil {
		body["tools"] = cohereTools
	}
	if cohereToolChoice != nil {
		body["tool_choice"] = cohereToolChoice
	}

	// reasoning:
	if thinking := m.resolveThinking(opts); thinking != nil {
		body["thinking"] = thinking
	}

	return body, warnings, nil
}

func (m *LanguageModel) toCohereMessages(src []types.Message) ([]map[string]interface{}, []map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(src))
	documents := []map[string]interface{}{}
	for _, msg := range src {
		if msg.Role != types.RoleUser {
			// AssistantToolCallContentMode: Omit -- Cohere's own TS converter
			// (convert-to-cohere-chat-prompt.ts) sets
			// `content: toolCalls.length > 0 ? undefined : text`, leaving the
			// "content" key out entirely whenever any tool call is present.
			out = append(out, prompt.ToOpenAIMessages([]types.Message{msg}, prompt.ToOpenAIMessagesOptions{
				AssistantToolCallContentMode: prompt.AssistantToolCallContentOmit,
			})...)
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
				if text, ok := c["text"].(string); ok {
					b.WriteString(text)
				}
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
	// providerOptions.cohere.thinking takes precedence over top-level
	// Reasoning (TS resolveCohereThinking: `if (cohereOptions.thinking) { ... }`).
	if cohereOpts, ok := opts.ProviderOptions["cohere"].(map[string]interface{}); ok {
		if thinkingRaw, present := cohereOpts["thinking"]; present && thinkingRaw != nil {
			if thinkingMap, ok := thinkingRaw.(map[string]interface{}); ok {
				thinkingType := "enabled"
				if t, ok := thinkingMap["type"].(string); ok && t != "" {
					thinkingType = t
				}
				result := map[string]interface{}{"type": thinkingType}
				if tb, ok := thinkingMap["tokenBudget"]; ok && tb != nil {
					result["token_budget"] = tb
				}
				return result
			}
		}
	}

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

// convertCohereUsage decodes a raw Cohere v2 usage object into SDK usage
// format, matching TS convertCohereUsage (cohere/src/convert-cohere-usage.ts):
// Raw preserves the complete usage object -- tokens, billed_units and
// cached_tokens -- rather than a hand-built subset (0599400).
func convertCohereUsage(raw json.RawMessage) types.Usage {
	if len(raw) == 0 || string(raw) == "null" {
		return types.Usage{}
	}

	var usage struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &usage); err != nil {
		return types.Usage{}
	}

	var rawMap map[string]interface{}
	_ = json.Unmarshal(raw, &rawMap)

	inputTokens := int64(usage.Tokens.InputTokens)
	outputTokens := int64(usage.Tokens.OutputTokens)
	totalTokens := inputTokens + outputTokens
	return types.Usage{
		InputTokens:   &inputTokens,
		OutputTokens:  &outputTokens,
		TotalTokens:   &totalTokens,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &inputTokens},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &outputTokens},
		Raw:           rawMap,
	}
}

func (m *LanguageModel) convertV2Response(resp cohereV2Response) (*types.GenerateResult, error) {
	result := &types.GenerateResult{
		FinishReason: mapCohereV2FinishReason(resp.FinishReason),
		RawResponse:  resp,
	}
	result.Usage = convertCohereUsage(resp.Usage)
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

	// citations -> source content parts, matching TS doGenerate's citation
	// loop (cohere-chat-language-model.ts:196-214). Only doGenerate emits
	// these; Cohere's streaming citation-start/citation-end events carry no
	// accumulable citation payload in the TS SDK's limited chunk schema, so
	// streaming intentionally ignores them (see cohereV2Stream.Next default
	// case).
	for _, citation := range resp.Message.Citations {
		title := "Document"
		if len(citation.Sources) > 0 && citation.Sources[0].Document != nil && citation.Sources[0].Document.Title != "" {
			title = citation.Sources[0].Document.Title
		}
		cohereMeta := map[string]interface{}{
			"start":   citation.Start,
			"end":     citation.End,
			"text":    citation.Text,
			"sources": citation.Sources,
		}
		if citation.Type != "" {
			cohereMeta["citationType"] = citation.Type
		}
		metaJSON, err := json.Marshal(map[string]interface{}{"cohere": cohereMeta})
		if err != nil {
			return nil, err
		}
		result.Content = append(result.Content, types.SourceContent{
			SourceType:       "document",
			ID:               streaming.GenerateID(),
			MediaType:        "text/plain",
			Title:            title,
			ProviderMetadata: metaJSON,
		})
	}

	for _, tc := range resp.Message.ToolCalls {
		args, err := parseCohereToolArguments(tc.Function.Arguments)
		if err != nil {
			return nil, err
		}
		result.ToolCalls = append(result.ToolCalls, types.ToolCall{
			ID:        tc.ID,
			ToolName:  tc.Function.Name,
			Arguments: args,
		})
	}
	return result, nil
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("cohere", 0, "", err.Error(), err)
}

func mapCohereV2FinishReason(reason string) types.FinishReason {
	switch reason {
	case "COMPLETE", "STOP_SEQUENCE":
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
		Citations []cohereCitation `json:"citations"`
	} `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Usage        json.RawMessage `json:"usage"`
}

// cohereCitation mirrors the Cohere v2 chat citation shape, matching TS
// cohere-chat-language-model.ts's inline citation type used in the
// `cohereChatResponseSchema.message.citations` field.
type cohereCitation struct {
	Start   int                    `json:"start"`
	End     int                    `json:"end"`
	Text    string                 `json:"text"`
	Sources []cohereCitationSource `json:"sources"`
	Type    string                 `json:"type,omitempty"`
}

type cohereCitationSource struct {
	Type     string             `json:"type,omitempty"`
	ID       string             `json:"id,omitempty"`
	Document *cohereCitationDoc `json:"document,omitempty"`
}

type cohereCitationDoc struct {
	ID    string `json:"id,omitempty"`
	Text  string `json:"text"`
	Title string `json:"title"`
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

func parseCohereToolArguments(raw string) (map[string]interface{}, error) {
	if raw == "" || raw == "null" {
		return map[string]interface{}{}, nil
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, providererrors.NewValidationError(
			"tool_calls[].function.arguments",
			fmt.Sprintf("invalid JSON arguments in Cohere tool call: %v", err),
			err,
		)
	}
	if parsed == nil {
		return map[string]interface{}{}, nil
	}
	obj, ok := parsed.(map[string]interface{})
	if !ok {
		return nil, providererrors.NewValidationError(
			"tool_calls[].function.arguments",
			"Cohere tool call arguments must decode to a JSON object",
			nil,
		)
	}
	return obj, nil
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
				args, err := parseCohereToolArguments(tc.arguments)
				if err != nil {
					// Streaming parity: keep the stream alive on malformed incremental
					// tool JSON and emit an empty-args tool call.
					args = map[string]interface{}{}
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
			FinishReason string          `json:"finish_reason"`
			Usage        json.RawMessage `json:"usage"`
		}
		if err := json.Unmarshal(ev.Delta, &delta); err == nil {
			usage := convertCohereUsage(delta.Usage)
			return &provider.StreamChunk{
				Type:         provider.ChunkTypeFinish,
				FinishReason: mapCohereV2FinishReason(delta.FinishReason),
				Usage:        &usage,
			}, nil
		}
		s.err = io.EOF
		return nil, io.EOF

	default:
		// citation-start, citation-end, tool-plan-delta, etc. — ignore
		return s.Next()
	}
}
