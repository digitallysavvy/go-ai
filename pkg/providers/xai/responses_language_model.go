package xai

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
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// XAIResponsesProviderOptions contains XAI-specific options for the Responses API path.
type XAIResponsesProviderOptions struct {
	// ReasoningSummary controls the reasoning summary detail level included in responses.
	// Valid values: "auto", "concise", "detailed".
	ReasoningSummary string `json:"reasoningSummary,omitempty"`

	// ReasoningEffort overrides the top-level opts.Reasoning for the Responses API.
	// Supports full granularity: "none", "minimal", "low", "medium", "high", "xhigh".
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// Logprobs enables log probability output for generated tokens.
	Logprobs *bool `json:"logprobs,omitempty"`

	// TopLogprobs is the number of most likely tokens to return per position.
	// Setting this implicitly enables Logprobs.
	TopLogprobs *int `json:"topLogprobs,omitempty"`

	// Store controls whether the response is stored server-side for multi-turn use.
	// When false, reasoning.encrypted_content is automatically added to Include.
	Store *bool `json:"store,omitempty"`

	// PreviousResponseID links this request to a prior Responses API response
	// for stateful multi-turn conversations.
	PreviousResponseID string `json:"previousResponseId,omitempty"`

	// Include lists additional output fields to include (e.g., "reasoning.encrypted_content").
	Include []string `json:"include,omitempty"`
}

// ResponsesLanguageModel implements provider.LanguageModel using xAI's Responses API
// (/v1/responses). The Responses API is the default language model path for xAI.
type ResponsesLanguageModel struct {
	provider *Provider
	modelID  string
}

// NewResponsesLanguageModel creates a new XAI Responses API language model.
func NewResponsesLanguageModel(p *Provider, modelID string) *ResponsesLanguageModel {
	return &ResponsesLanguageModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the provider spec version.
func (m *ResponsesLanguageModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider identifier. Uses the ".responses" suffix to
// distinguish Responses API models from Chat Completions models.
func (m *ResponsesLanguageModel) Provider() string { return "xai.responses" }

// ModelID returns the model ID.
func (m *ResponsesLanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports whether the model supports tool calling.
func (m *ResponsesLanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports whether the model supports JSON schema output.
func (m *ResponsesLanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports whether the model accepts image inputs.
func (m *ResponsesLanguageModel) SupportsImageInput() bool { return true }

// DoGenerate performs non-streaming generation via POST /v1/responses.
func (m *ResponsesLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	var warnings []types.Warning
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported-setting",
			Message: "XAI Responses API does not support stopSequences",
		})
	}

	body, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, err
	}

	var resp responses.ResponsesAPIResponse
	if err := m.provider.client.PostJSON(ctx, "/v1/responses", body, &resp); err != nil {
		return nil, m.wrapErr(err)
	}

	result, err := m.convertResponse(resp, opts.Tools)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		result.Warnings = append(warnings, result.Warnings...)
	}
	return result, nil
}

// DoStream performs streaming generation via POST /v1/responses with stream=true.
func (m *ResponsesLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/responses",
		Body:   body,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.wrapErr(err)
	}

	return newXAIResponsesStream(httpResp.Body), nil
}

// buildRequestBody constructs the Responses API request body.
func (m *ResponsesLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, error) {
	// Extract XAI-specific provider options.
	var xaiOpts XAIResponsesProviderOptions
	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["xai"]; ok {
			if jsonData, err := json.Marshal(raw); err == nil {
				json.Unmarshal(jsonData, &xaiOpts) //nolint:errcheck
			}
		}
	}

	input := responses.ConvertPromptToInput(opts.Prompt, "system")

	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
		"input":  input,
	}

	// Reasoning effort: provider option takes precedence over top-level opts.Reasoning.
	// Effort map mirrors TS: {minimal:'low', low:'low', medium:'medium', high:'high', xhigh:'high'}.
	// ReasoningNone → omit effort field entirely.
	var effort string
	if xaiOpts.ReasoningEffort != "" {
		effort = xaiOpts.ReasoningEffort
	} else if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			// omit — do not set effort
		case types.ReasoningMinimal, types.ReasoningLow:
			effort = "low"
		case types.ReasoningMedium:
			effort = "medium"
		case types.ReasoningHigh, types.ReasoningXHigh:
			effort = "high"
		}
	}

	if effort != "" || xaiOpts.ReasoningSummary != "" {
		reasoning := map[string]interface{}{}
		if effort != "" {
			reasoning["effort"] = effort
		}
		if xaiOpts.ReasoningSummary != "" {
			reasoning["summary"] = xaiOpts.ReasoningSummary
		}
		body["reasoning"] = reasoning
	}

	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	// Logprobs: setting TopLogprobs implicitly enables logprobs.
	logprobs := xaiOpts.Logprobs != nil && *xaiOpts.Logprobs
	if xaiOpts.TopLogprobs != nil {
		logprobs = true
	}
	if logprobs {
		body["logprobs"] = true
	}
	if xaiOpts.TopLogprobs != nil {
		body["top_logprobs"] = *xaiOpts.TopLogprobs
	}

	// Store: when false, auto-add reasoning.encrypted_content to include for multi-turn reasoning.
	include := make([]string, len(xaiOpts.Include))
	copy(include, xaiOpts.Include)
	if xaiOpts.Store != nil && !*xaiOpts.Store {
		body["store"] = false
		hasEncrypted := false
		for _, v := range include {
			if v == "reasoning.encrypted_content" {
				hasEncrypted = true
				break
			}
		}
		if !hasEncrypted {
			include = append(include, "reasoning.encrypted_content")
		}
	}
	if len(include) > 0 {
		body["include"] = include
	}

	// Previous response ID for stateful multi-turn conversations.
	if xaiOpts.PreviousResponseID != "" {
		body["previous_response_id"] = xaiOpts.PreviousResponseID
	}

	// Response format (JSON schema / structured output).
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		var format map[string]interface{}
		if opts.ResponseFormat.Schema != nil {
			name := opts.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			format = map[string]interface{}{
				"type":   "json_schema",
				"strict": true,
				"name":   name,
				"schema": opts.ResponseFormat.Schema,
			}
			if opts.ResponseFormat.Description != "" {
				format["description"] = opts.ResponseFormat.Description
			}
		} else {
			format = map[string]interface{}{"type": "json_object"}
		}
		body["text"] = map[string]interface{}{"format": format}
	}

	// Tools.
	if len(opts.Tools) > 0 {
		body["tools"] = prepareXAIResponsesTools(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = convertXAIResponsesToolChoice(opts.ToolChoice)
		}
	}

	return body, nil
}

// convertXAIResponsesToolChoice maps a types.ToolChoice to the Responses API format.
func convertXAIResponsesToolChoice(tc types.ToolChoice) interface{} {
	switch tc.Type {
	case types.ToolChoiceNone:
		return "none"
	case types.ToolChoiceRequired:
		return "required"
	case types.ToolChoiceTool:
		return map[string]interface{}{
			"type": "function",
			"name": tc.ToolName,
		}
	default:
		return "auto"
	}
}

// convertResponse converts a Responses API response to a GenerateResult.
func (m *ResponsesLanguageModel) convertResponse(resp responses.ResponsesAPIResponse, tools []types.Tool) (*types.GenerateResult, error) {
	result := &types.GenerateResult{
		Usage:       convertXAIResponsesUsage(resp.Usage),
		RawResponse: resp,
	}

	// Resolve user-registered tool names for provider-executed tools.
	toolNames := resolveProviderToolNames(tools)

	var toolCalls []types.ToolCall
	var hasFunctionCall bool // true only when a function_call item is encountered

	for _, rawItem := range resp.Output {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawItem, &peek); err != nil {
			continue
		}

		switch peek.Type {
		case "message":
			// Parse with annotations support for url_citation.
			var item struct {
				Content []struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					Annotations []struct {
						Type  string `json:"type"`
						URL   string `json:"url,omitempty"`
						Title string `json:"title,omitempty"`
					} `json:"annotations,omitempty"`
				} `json:"content"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var msgText string
			for _, part := range item.Content {
				msgText += part.Text
				for _, ann := range part.Annotations {
					if ann.Type == "url_citation" && ann.URL != "" {
						title := ann.Title
						if title == "" {
							title = ann.URL
						}
						result.Content = append(result.Content, types.SourceContent{
							SourceType: "url",
							URL:        ann.URL,
							Title:      title,
						})
					}
				}
			}
			if msgText != "" {
				result.Text += msgText
				result.Content = append(result.Content, types.TextContent{Text: msgText})
			}

		case "function_call":
			var item responses.FunctionCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			toolCalls = append(toolCalls, types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: args,
			})
			hasFunctionCall = true

		case "custom_tool_call":
			var item responses.CustomToolCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: map[string]interface{}{"input": item.Input},
			})

		case "reasoning":
			// Extract reasoning text from summary parts and encrypted content.
			var item struct {
				Type             string `json:"type"`
				ID               string `json:"id,omitempty"`
				EncryptedContent string `json:"encrypted_content,omitempty"`
				Summary          []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"summary,omitempty"`
				// Content is a fallback when summary is absent.
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var reasoningText string
			if len(item.Summary) > 0 {
				for _, s := range item.Summary {
					reasoningText += s.Text
				}
			} else {
				for _, c := range item.Content {
					reasoningText += c.Text
				}
			}
			rc := types.ReasoningContent{
				Text:             reasoningText,
				EncryptedContent: item.EncryptedContent,
			}
			// Forward provider metadata: reasoning item ID and encrypted content
			// for callers that need to round-trip them in subsequent turns.
			if item.ID != "" || item.EncryptedContent != "" {
				meta := map[string]interface{}{}
				if item.ID != "" {
					meta["itemId"] = item.ID
				}
				if item.EncryptedContent != "" {
					meta["reasoningEncryptedContent"] = item.EncryptedContent
				}
				if raw, err := json.Marshal(map[string]interface{}{"xai": meta}); err == nil {
					rc.ProviderMetadata = raw
				}
			}
			result.Content = append(result.Content, rc)

		case "web_search_call", "x_search_call", "code_interpreter_call",
			"code_execution_call", "view_image_call", "view_x_video_call":
			var item struct {
				ID   string `json:"id"`
				Name string `json:"name,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         resolvedToolName(peek.Type, item.Name, toolNames),
				ProviderExecuted: true,
			})

		case "file_search_call":
			var item struct {
				ID      string   `json:"id"`
				Queries []string `json:"queries,omitempty"`
				Results []struct {
					FileID   string  `json:"file_id"`
					Filename string  `json:"filename"`
					Score    float64 `json:"score"`
					Text     string  `json:"text"`
				} `json:"results,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := toolNames["xai.file_search"]
			if toolName == "" {
				toolName = "xai.file_search"
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         toolName,
				ProviderExecuted: true,
			})
			// Emit a synthetic tool-result content part with file search queries and results.
			type fileSearchResult struct {
				FileID   string  `json:"fileId"`
				Filename string  `json:"filename"`
				Score    float64 `json:"score"`
				Text     string  `json:"text"`
			}
			resultData := map[string]interface{}{
				"queries": item.Queries,
			}
			if item.Results != nil {
				mapped := make([]fileSearchResult, len(item.Results))
				for i, r := range item.Results {
					mapped[i] = fileSearchResult{FileID: r.FileID, Filename: r.Filename, Score: r.Score, Text: r.Text}
				}
				resultData["results"] = mapped
			} else {
				resultData["results"] = nil
			}
			result.Content = append(result.Content, types.ToolResultContent{
				ToolCallID: item.ID,
				ToolName:   toolName,
				Result:     resultData,
			})

		case "mcp_call":
			var item struct {
				ID   string `json:"id"`
				Name string `json:"name,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			mcpName := toolNames["xai.mcp"]
			if mcpName == "" {
				mcpName = item.Name
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         mcpName,
				ProviderExecuted: true,
			})
		}
	}

	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}
	// Use hasFunctionCall (not len(toolCalls)) to avoid treating provider-executed
	// tools (web_search, mcp, etc.) as finish reason "tool-calls".
	// Mirrors TS SDK xai-responses-language-model.ts hasFunctionCall logic.
	if hasFunctionCall {
		result.FinishReason = types.FinishReasonToolCalls
	} else {
		result.FinishReason = mapXAIResponsesFinishReason(resp.Status, resp.IncompleteDetails)
	}

	return result, nil
}

// resolveProviderToolNames scans the registered tools list and returns a map of
// canonical SDK tool name → user-registered name. In Go, provider tools are
// identified by their canonical Name (e.g. "xai.web_search"); this map lets
// callers look up what name the user registered for each provider tool.
func resolveProviderToolNames(tools []types.Tool) map[string]string {
	knownIDs := map[string]bool{
		"xai.web_search": true, "xai.x_search": true,
		"xai.code_execution": true, "xai.file_search": true,
		"xai.mcp": true, "xai.view_image": true, "xai.view_x_video": true,
	}
	names := make(map[string]string)
	for _, t := range tools {
		if knownIDs[t.Name] {
			names[t.Name] = t.Name
		}
	}
	return names
}

// resolvedToolName returns the user-registered tool name for a given output type
// and item name, falling back to the canonical SDK name. Mirrors TS tool name
// resolution logic for web_search, x_search, code_execution sub-tool variants.
func resolvedToolName(outputType, itemName string, toolNames map[string]string) string {
	webSearchSubTools := map[string]bool{
		"web_search": true, "web_search_with_snippets": true, "browse_page": true,
	}
	xSearchSubTools := map[string]bool{
		"x_user_search": true, "x_keyword_search": true,
		"x_semantic_search": true, "x_thread_fetch": true,
	}

	switch {
	case outputType == "web_search_call" || webSearchSubTools[itemName]:
		if n := toolNames["xai.web_search"]; n != "" {
			return n
		}
		return "xai.web_search"
	case outputType == "x_search_call" || xSearchSubTools[itemName]:
		if n := toolNames["xai.x_search"]; n != "" {
			return n
		}
		return "xai.x_search"
	case outputType == "code_interpreter_call" || outputType == "code_execution_call" || itemName == "code_execution":
		if n := toolNames["xai.code_execution"]; n != "" {
			return n
		}
		return "xai.code_execution"
	case outputType == "view_image_call":
		return "xai.view_image"
	case outputType == "view_x_video_call":
		return "xai.view_x_video"
	default:
		return providerToolNameFromType(outputType)
	}
}

// providerToolNameFromType maps Responses API output item types to SDK tool names.
func providerToolNameFromType(outputType string) string {
	switch outputType {
	case "web_search_call":
		return "xai.web_search"
	case "x_search_call":
		return "xai.x_search"
	case "code_interpreter_call", "code_execution_call":
		return "xai.code_execution"
	case "view_image_call":
		return "xai.view_image"
	case "view_x_video_call":
		return "xai.view_x_video"
	default:
		return outputType
	}
}

// mapXAIResponsesFinishReason maps the Responses API status and incomplete_details
// to a FinishReason. status is the primary signal; incomplete_details provides
// truncation reason when status == "incomplete".
//
// Mirrors TS SDK xai-responses-language-model.ts mapXaiResponsesFinishReason.
func mapXAIResponsesFinishReason(status string, details *responses.IncompleteDetails) types.FinishReason {
	switch status {
	case "completed", "stop", "":
		// "": no status provided (older API versions) — treat as stop.
		return types.FinishReasonStop
	case "tool_calls":
		return types.FinishReasonToolCalls
	case "incomplete":
		if details != nil {
			switch details.Reason {
			case "max_output_tokens":
				return types.FinishReasonLength
			case "content_filter":
				return types.FinishReasonContentFilter
			}
		}
		return types.FinishReasonLength
	case "length":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonStop
	}
}

// convertXAIResponsesUsage converts Responses API usage to types.Usage.
func convertXAIResponsesUsage(u responses.ResponsesAPIUsage) types.Usage {
	inputTokens := int64(u.InputTokens)
	outputTokens := int64(u.OutputTokens)
	total := inputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &total,
	}

	if u.InputTokensDetails != nil && u.InputTokensDetails.CachedTokens > 0 {
		cached := int64(u.InputTokensDetails.CachedTokens)
		noCached := inputTokens - cached
		result.InputDetails = &types.InputTokenDetails{
			CacheReadTokens: &cached,
			NoCacheTokens:   &noCached,
		}
	}

	if u.OutputTokensDetails != nil && u.OutputTokensDetails.ReasoningTokens > 0 {
		reasoning := int64(u.OutputTokensDetails.ReasoningTokens)
		textOut := outputTokens - reasoning
		result.OutputDetails = &types.OutputTokenDetails{
			ReasoningTokens: &reasoning,
			TextTokens:      &textOut,
		}
	}

	return result
}

func (m *ResponsesLanguageModel) wrapErr(err error) error {
	return providererrors.NewProviderError("xai.responses", 0, "", err.Error(), err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Streaming implementation
// ─────────────────────────────────────────────────────────────────────────────

type xaiResponsesToolAccum struct {
	id        string
	name      string
	arguments string
}

// xaiResponsesStream implements provider.TextStream for the XAI Responses API SSE stream.
// XAI's Responses API uses the same SSE event schema as the OpenAI Responses API.
type xaiResponsesStream struct {
	reader          io.ReadCloser
	parser          *streaming.SSEParser
	err             error
	toolAccum       map[int]*xaiResponsesToolAccum
	itemTypes       map[int]string
	flushQueue      []*provider.StreamChunk
	// activeReasoning tracks item IDs for which a reasoning-start chunk has been emitted.
	// Used to emit the matching reasoning-end in output_item.done, and to avoid
	// double-emitting reasoning-start for encrypted reasoning (no summary events sent).
	activeReasoning map[string]struct{}
}

func newXAIResponsesStream(r io.ReadCloser) *xaiResponsesStream {
	return &xaiResponsesStream{
		reader:          r,
		parser:          streaming.NewSSEParser(r),
		toolAccum:       make(map[int]*xaiResponsesToolAccum),
		itemTypes:       make(map[int]string),
		activeReasoning: make(map[string]struct{}),
	}
}

// Close implements provider.TextStream.
func (s *xaiResponsesStream) Close() error { return s.reader.Close() }

// Err implements provider.TextStream.
func (s *xaiResponsesStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next implements provider.TextStream.
func (s *xaiResponsesStream) Next() (*provider.StreamChunk, error) {
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

	var peek responses.ResponsesStreamEvent
	if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
		return s.Next()
	}

	switch peek.Type {
	case "response.created":
		return s.Next()

	case "response.output_item.added":
		var e responses.OutputItemAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		s.itemTypes[e.OutputIndex] = e.Item.Type
		if e.Item.Type == "function_call" {
			s.toolAccum[e.OutputIndex] = &xaiResponsesToolAccum{
				id:   e.Item.CallID,
				name: e.Item.Name,
			}
		}
		return s.Next()

	case "response.output_text.delta":
		var e responses.OutputTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Delta == "" {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: e.Delta,
		}, nil

	case "response.function_call_arguments.delta":
		var e responses.FunctionCallArgumentsDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if accum, ok := s.toolAccum[e.OutputIndex]; ok {
			accum.arguments += e.Delta
		}
		return s.Next()

	// reasoning_summary_part.added fires when a reasoning block starts.
	// Emit reasoning-start with providerMetadata so consumers know the item ID.
	case "response.reasoning_summary_part.added":
		var e responses.ReasoningSummaryPartAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		blockID := "reasoning-" + e.ItemID
		s.activeReasoning[e.ItemID] = struct{}{}
		meta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningStart,
			ID:               blockID,
			ProviderMetadata: meta,
		}, nil

	case "response.reasoning_summary_text.delta":
		var e responses.ReasoningSummaryTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Delta == "" {
			return s.Next()
		}
		blockID := "reasoning-" + e.ItemID
		meta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoning,
			ID:               blockID,
			Reasoning:        e.Delta,
			ProviderMetadata: meta,
		}, nil

	// Gap 6: raw reasoning text (not summary) — emitted by some Grok models.
	// Also emits reasoning-start on first delta if not already started.
	case "response.reasoning_text.delta":
		var e struct {
			ItemID string `json:"item_id"`
			Delta  string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Delta == "" {
			return s.Next()
		}
		blockID := "reasoning-" + e.ItemID
		meta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
		if _, started := s.activeReasoning[e.ItemID]; !started {
			// First delta without a prior summary_part.added — emit start now.
			s.activeReasoning[e.ItemID] = struct{}{}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoning,
				ID:               blockID,
				Reasoning:        e.Delta,
				ProviderMetadata: meta,
			})
			return &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               blockID,
				ProviderMetadata: meta,
			}, nil
		}
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoning,
			ID:               blockID,
			Reasoning:        e.Delta,
			ProviderMetadata: meta,
		}, nil

	// Gap 7: url_citation annotations delivered when a text item is finalised.
	case "response.output_text.done":
		var e struct {
			Annotations []struct {
				Type  string `json:"type"`
				URL   string `json:"url,omitempty"`
				Title string `json:"title,omitempty"`
			} `json:"annotations,omitempty"`
		}
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		for _, ann := range e.Annotations {
			if ann.Type == "url_citation" && ann.URL != "" {
				title := ann.Title
				if title == "" {
					title = ann.URL
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeSource,
					SourceContent: &types.SourceContent{
						SourceType: "url",
						URL:        ann.URL,
						Title:      title,
					},
				})
			}
		}
		return s.Next()

	// Gap 7: individual annotation added mid-stream.
	case "response.output_text.annotation.added":
		var e struct {
			Annotation struct {
				Type  string `json:"type"`
				URL   string `json:"url,omitempty"`
				Title string `json:"title,omitempty"`
			} `json:"annotation"`
		}
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Annotation.Type == "url_citation" && e.Annotation.URL != "" {
			title := e.Annotation.Title
			if title == "" {
				title = e.Annotation.URL
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeSource,
				SourceContent: &types.SourceContent{
					SourceType: "url",
					URL:        e.Annotation.URL,
					Title:      title,
				},
			}, nil
		}
		return s.Next()

	case "response.output_item.done":
		var e responses.OutputItemDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		return s.handleOutputItemDone(e)

	// Gap 4: response.done is an alias for response.completed.
	case "response.completed", "response.done":
		var e responses.ResponseCompletedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			s.err = io.EOF
			return nil, io.EOF
		}
		usage := convertXAIResponsesUsage(e.Response.Usage)
		finishReason := mapXAIResponsesFinishReason(e.Response.Status, e.Response.IncompleteDetails)

		var meta json.RawMessage
		if e.Response.ID != "" {
			meta, _ = json.Marshal(map[string]interface{}{"responseId": e.Response.ID})
		}

		s.err = io.EOF
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeFinish,
			FinishReason:     finishReason,
			Usage:            &usage,
			ProviderMetadata: meta,
		}, nil

	case "error":
		var e responses.ResponsesStreamErrorEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		s.err = fmt.Errorf("xai.responses stream error: %s (code: %s)", e.Message, e.Code)
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: e.Message,
		}, nil

	default:
		return s.Next()
	}
}

func (s *xaiResponsesStream) handleOutputItemDone(e responses.OutputItemDoneEvent) (*provider.StreamChunk, error) {
	itemType := s.itemTypes[e.OutputIndex]

	switch itemType {
	case "function_call":
		accum, ok := s.toolAccum[e.OutputIndex]
		if !ok {
			return s.Next()
		}
		delete(s.toolAccum, e.OutputIndex)
		delete(s.itemTypes, e.OutputIndex)

		var args map[string]interface{}
		if accum.arguments != "" {
			json.Unmarshal([]byte(accum.arguments), &args) //nolint:errcheck
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        accum.id,
				ToolName:  accum.name,
				Arguments: args,
			},
		}, nil

	case "web_search_call", "x_search_call", "code_interpreter_call",
		"code_execution_call", "view_image_call", "view_x_video_call":
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.ID,
				ToolName:         providerToolNameFromType(itemType),
				ProviderExecuted: true,
			},
		}, nil

	case "file_search_call":
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.ID,
				ToolName:         "xai.file_search",
				ProviderExecuted: true,
			},
		}, nil

	case "mcp_call":
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID   string `json:"id"`
			Name string `json:"name,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.ID,
				ToolName:         item.Name,
				ProviderExecuted: true,
			},
		}, nil

	// Gap 3: custom_tool_call handled on done (input arrives via custom_tool_call_input.delta).
	case "custom_tool_call":
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID    string `json:"id"`
			Name  string `json:"name,omitempty"`
			Input string `json:"input,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.ID,
				ToolName:         item.Name,
				Arguments:        map[string]interface{}{"input": item.Input},
				ProviderExecuted: true,
			},
		}, nil

	// Gap 2: forward encrypted_content from reasoning items so callers can
	// round-trip it in subsequent turns when store=false.
	// Also closes the reasoning block: emit a fallback reasoning-start if
	// reasoning_summary_part.added was never sent (encrypted reasoning path),
	// then emit reasoning-end.
	case "reasoning":
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID               string `json:"id,omitempty"`
			EncryptedContent string `json:"encrypted_content,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		if item.ID == "" && item.EncryptedContent == "" {
			return s.Next()
		}
		meta := map[string]interface{}{}
		if item.ID != "" {
			meta["itemId"] = item.ID
		}
		if item.EncryptedContent != "" {
			meta["reasoningEncryptedContent"] = item.EncryptedContent
		}
		providerMeta, _ := json.Marshal(map[string]interface{}{"xai": meta})
		blockID := "reasoning-" + item.ID

		// If no reasoning-start was emitted yet (encrypted reasoning path where
		// reasoning_summary_part.added events are not sent), emit one now.
		if _, started := s.activeReasoning[item.ID]; !started {
			s.activeReasoning[item.ID] = struct{}{}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningEnd,
				ID:               blockID,
				ProviderMetadata: providerMeta,
			})
			delete(s.activeReasoning, item.ID)
			startMeta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": item.ID}})
			return &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               blockID,
				ProviderMetadata: startMeta,
			}, nil
		}

		delete(s.activeReasoning, item.ID)
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               blockID,
			ProviderMetadata: providerMeta,
		}, nil

	default:
		delete(s.itemTypes, e.OutputIndex)
		return s.Next()
	}
}

// Compile-time interface check.
var _ provider.LanguageModel = (*ResponsesLanguageModel)(nil)
