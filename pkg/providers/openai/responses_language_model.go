package openai

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

// ResponsesLanguageModel implements provider.LanguageModel using OpenAI's
// Responses API (/v1/responses). It supports both chat-style and agentic
// workflows, compaction, tool search, and structured reasoning output.
type ResponsesLanguageModel struct {
	provider *Provider
	modelID  string
}

// NewResponsesLanguageModel returns a ResponsesLanguageModel for the given model ID.
func NewResponsesLanguageModel(p *Provider, modelID string) *ResponsesLanguageModel {
	return &ResponsesLanguageModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the provider spec version.
func (m *ResponsesLanguageModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider identifier. Uses the ".responses" suffix so
// callers can distinguish Responses API models from Chat Completions models.
func (m *ResponsesLanguageModel) Provider() string { return "openai.responses" }

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
	body, store, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, err
	}

	var resp responses.ResponsesAPIResponse
	if err := m.provider.client.PostJSON(ctx, "/responses", body, &resp); err != nil {
		return nil, m.wrapErr(err)
	}

	return m.convertResponse(resp, store)
}

// DoStream performs streaming generation via POST /v1/responses with stream=true.
func (m *ResponsesLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, _, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/responses",
		Body:   body,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.wrapErr(err)
	}

	return newResponsesStream(httpResp.Body), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Request body construction
// ─────────────────────────────────────────────────────────────────────────────

// buildRequestBody constructs the Responses API request body.
// Returns the body map, the effective store flag, and any error.
func (m *ResponsesLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, bool, error) {
	// Extract provider options early (store needed before input conversion).
	store := true
	storeExplicit := false
	previousResponseID := ""
	promptCacheRetention := ""
	reasoningEffort := ""
	reasoningSummary := ""
	serviceTier := ""
	user := ""
	maxToolCalls := 0
	parallelToolCalls := (*bool)(nil)
	truncation := ""
	includeFields := []string(nil)

	if opts.ProviderOptions != nil {
		if openaiOpts, ok := opts.ProviderOptions["openai"].(map[string]interface{}); ok {
			if v, ok := openaiOpts["store"].(bool); ok {
				store = v
				storeExplicit = true
			}
			if v, ok := openaiOpts["previousResponseId"].(string); ok {
				previousResponseID = v
			}
			if v, ok := openaiOpts["promptCacheRetention"].(string); ok {
				promptCacheRetention = v
			}
			if v, ok := openaiOpts["reasoningEffort"].(string); ok {
				reasoningEffort = v
			}
			if v, ok := openaiOpts["reasoningSummary"].(string); ok {
				reasoningSummary = v
			}
			if v, ok := openaiOpts["serviceTier"].(string); ok {
				serviceTier = v
			}
			if v, ok := openaiOpts["user"].(string); ok {
				user = v
			}
			if v, ok := openaiOpts["maxToolCalls"].(int); ok {
				maxToolCalls = v
			}
			if v, ok := openaiOpts["parallelToolCalls"].(bool); ok {
				parallelToolCalls = &v
			}
			if v, ok := openaiOpts["truncation"].(string); ok {
				truncation = v
			}
			if v, ok := openaiOpts["include"].([]string); ok {
				includeFields = v
			}
		}
	}

	// Determine system message mode based on model type.
	systemMsgMode := "system"
	if isReasoningModel(m.modelID) {
		systemMsgMode = "developer"
	}

	// Convert prompt to Responses API input format.
	input := responses.ConvertPromptToInput(opts.Prompt, systemMsgMode)

	body := map[string]interface{}{
		"model":  m.modelID,
		"stream": stream,
		"input":  input,
	}

	// Reasoning effort (from top-level Reasoning param or provider options).
	effort := ""
	if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			effort = "none"
		case types.ReasoningMinimal:
			effort = "minimal"
		case types.ReasoningLow:
			effort = "low"
		case types.ReasoningMedium:
			effort = "medium"
		case types.ReasoningHigh:
			effort = "high"
		case types.ReasoningXHigh:
			effort = "xhigh"
		}
	}
	// Provider-level reasoningEffort overrides the top-level param.
	if reasoningEffort != "" {
		effort = reasoningEffort
	}
	if effort != "" || reasoningSummary != "" {
		reasoning := map[string]interface{}{}
		if effort != "" {
			reasoning["effort"] = effort
		}
		if reasoningSummary != "" {
			reasoning["summary"] = reasoningSummary
		}
		body["reasoning"] = reasoning
	}

	// Temperature and top_p: forbidden for reasoning models unless effort="none".
	supportsNonReasoningParams := !isReasoningModel(m.modelID) || effort == "none"
	if supportsNonReasoningParams {
		if opts.Temperature != nil {
			body["temperature"] = *opts.Temperature
		}
		if opts.TopP != nil {
			body["top_p"] = *opts.TopP
		}
	}

	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}

	// Response format (JSON schema / structured output).
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "" {
		body["text"] = map[string]interface{}{
			"format": map[string]interface{}{
				"type": opts.ResponseFormat.Type,
			},
		}
	}

	// Tools.
	if len(opts.Tools) > 0 {
		body["tools"] = responses.PrepareTools(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = convertResponsesToolChoice(opts.ToolChoice)
		}
	}

	// Build include list — always add reasoning.encrypted_content when
	// store=false and we have a reasoning model, so multi-turn works without
	// server-side persistence.
	if !store && isReasoningModel(m.modelID) {
		includeFields = appendUnique(includeFields, "reasoning.encrypted_content")
	}
	if len(includeFields) > 0 {
		body["include"] = includeFields
	}

	// Provider options fields.
	if storeExplicit {
		body["store"] = store
	}
	if previousResponseID != "" {
		body["previous_response_id"] = previousResponseID
	}
	if promptCacheRetention != "" {
		body["prompt_cache_retention"] = promptCacheRetention
	}
	if serviceTier != "" {
		body["service_tier"] = serviceTier
	}
	if user != "" {
		body["user"] = user
	}
	if maxToolCalls > 0 {
		body["max_tool_calls"] = maxToolCalls
	}
	if parallelToolCalls != nil {
		body["parallel_tool_calls"] = *parallelToolCalls
	}
	if truncation != "" {
		body["truncation"] = truncation
	}

	return body, store, nil
}

// convertResponsesToolChoice maps a types.ToolChoice to the Responses API format.
func convertResponsesToolChoice(tc types.ToolChoice) interface{} {
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

// appendUnique appends s to slice if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// ─────────────────────────────────────────────────────────────────────────────
// Non-streaming response conversion
// ─────────────────────────────────────────────────────────────────────────────

func (m *ResponsesLanguageModel) convertResponse(resp responses.ResponsesAPIResponse, store bool) (*types.GenerateResult, error) {
	result := &types.GenerateResult{
		Usage:       convertResponsesUsage(resp.Usage),
		RawResponse: resp,
	}

	var toolCalls []types.ToolCall

	for _, rawItem := range resp.Output {
		// Peek at type field.
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawItem, &peek); err != nil {
			continue
		}

		switch peek.Type {
		case "message":
			var item responses.AssistantMessageItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			for _, part := range item.Content {
				result.Text += part.Text
			}
			result.Content = append(result.Content, types.TextContent{Text: result.Text})

		case "function_call":
			var item responses.FunctionCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			tc := types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: args,
			}
			toolCalls = append(toolCalls, tc)

		case "reasoning":
			// Parse encrypted_content and summary text.
			var item struct {
				Type             string `json:"type"`
				ID               string `json:"id,omitempty"`
				EncryptedContent string `json:"encrypted_content,omitempty"`
				Summary          []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"summary,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var summaryText string
			for _, s := range item.Summary {
				summaryText += s.Text
			}
			rc := types.ReasoningContent{
				Text:             summaryText,
				EncryptedContent: item.EncryptedContent,
			}
			result.Content = append(result.Content, rc)

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

		case "compaction":
			var item responses.CompactionEvent
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			chunk := responses.CompactionEventToChunk(item)
			if chunk.CustomContent != nil {
				result.Content = append(result.Content, *chunk.CustomContent)
			}
		}
	}

	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
		result.FinishReason = types.FinishReasonToolCalls
	} else {
		result.FinishReason = mapResponsesFinishReason(resp.IncompleteDetails, false)
	}

	return result, nil
}

// mapResponsesFinishReason maps Responses API incomplete_details to a FinishReason.
func mapResponsesFinishReason(details *responses.IncompleteDetails, hasToolCalls bool) types.FinishReason {
	if hasToolCalls {
		return types.FinishReasonToolCalls
	}
	if details == nil {
		return types.FinishReasonStop
	}
	switch details.Reason {
	case "max_output_tokens":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonStop
	}
}

// convertResponsesUsage converts Responses API usage to types.Usage.
func convertResponsesUsage(u responses.ResponsesAPIUsage) types.Usage {
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
	return providererrors.NewProviderError("openai.responses", 0, "", err.Error(), err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Streaming implementation
// ─────────────────────────────────────────────────────────────────────────────

// responsesToolAccum accumulates streaming tool call fragments for one output item.
type responsesToolAccum struct {
	id        string // call_id / item id
	name      string
	arguments string
}

// responsesReasoningAccum accumulates streaming reasoning summary text.
type responsesReasoningAccum struct {
	text string
}

// responsesStream implements provider.TextStream for the Responses API SSE stream.
type responsesStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error

	// Accumulated tool calls keyed by output_index.
	toolAccum map[int]*responsesToolAccum
	// Accumulated reasoning text keyed by output_index.
	reasoningAccum map[int]*responsesReasoningAccum
	// Item type by output_index, set on output_item.added.
	itemTypes map[int]string
	// Chunks ready to emit without reading more SSE events.
	flushQueue []*provider.StreamChunk
}

func newResponsesStream(r io.ReadCloser) *responsesStream {
	return &responsesStream{
		reader:         r,
		parser:         streaming.NewSSEParser(r),
		toolAccum:      make(map[int]*responsesToolAccum),
		reasoningAccum: make(map[int]*responsesReasoningAccum),
		itemTypes:      make(map[int]string),
	}
}

// Close implements provider.TextStream.
func (s *responsesStream) Close() error { return s.reader.Close() }

// Err implements provider.TextStream.
func (s *responsesStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// Next implements provider.TextStream. It reads SSE events and converts them to
// StreamChunks following the Responses API event schema.
//
// Event handling:
//   - response.created              → no chunk (captures responseId metadata)
//   - response.output_item.added    → initialize accumulator entries
//   - response.output_text.delta    → ChunkTypeText immediately
//   - response.function_call_arguments.delta → accumulate tool input
//   - response.reasoning_summary_text.delta  → ChunkTypeReasoning immediately
//   - response.output_item.done     → flush tool-call chunks from accumulator
//   - response.completed            → ChunkTypeFinish with usage
//   - error                         → ChunkTypeError
func (s *responsesStream) Next() (*provider.StreamChunk, error) {
	// Drain any queued chunks first.
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

	// Standard [DONE] terminator (some OpenAI SSE streams still send it).
	if streaming.IsStreamDone(event) {
		s.err = io.EOF
		return nil, io.EOF
	}

	// Parse the "type" discriminator.
	var peek responses.ResponsesStreamEvent
	if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
		return s.Next() // skip malformed events
	}

	switch peek.Type {

	case "response.created":
		// No chunk emitted; the responseId is surfaced via ChunkTypeFinish metadata.
		return s.Next()

	case "response.output_item.added":
		var e responses.OutputItemAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		s.itemTypes[e.OutputIndex] = e.Item.Type
		if e.Item.Type == "function_call" {
			s.toolAccum[e.OutputIndex] = &responsesToolAccum{
				id:   e.Item.CallID,
				name: e.Item.Name,
			}
		} else if e.Item.Type == "reasoning" {
			s.reasoningAccum[e.OutputIndex] = &responsesReasoningAccum{}
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

	case "response.reasoning_summary_text.delta":
		var e responses.ReasoningSummaryTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Delta == "" {
			return s.Next()
		}
		// Accumulate for providerMetadata and emit as reasoning chunk.
		if accum, ok := s.reasoningAccum[e.OutputIndex]; ok {
			accum.text += e.Delta
		}
		return &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: e.Delta,
		}, nil

	case "response.output_item.done":
		var e responses.OutputItemDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		return s.handleOutputItemDone(e)

	case "response.completed":
		var e responses.ResponseCompletedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			s.err = io.EOF
			return nil, io.EOF
		}
		usage := convertResponsesUsage(e.Response.Usage)
		finishReason := mapResponsesFinishReason(e.Response.IncompleteDetails, false)

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
		s.err = fmt.Errorf("openai.responses stream error: %s (code: %s)", e.Message, e.Code)
		return &provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: e.Message,
		}, nil

	default:
		// Unknown event types (web_search_call, code_interpreter_call, etc.) —
		// skip silently; they are provider-internal and require no client action.
		return s.Next()
	}
}

// handleOutputItemDone flushes the completed item at e.OutputIndex.
// For function_call items it emits a ChunkTypeToolCall.
// For compaction items it emits a ChunkTypeCustom.
// All other item types were emitted incrementally and need no action here.
func (s *responsesStream) handleOutputItemDone(e responses.OutputItemDoneEvent) (*provider.StreamChunk, error) {
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

	case "compaction":
		var item responses.CompactionEvent
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		chunk := responses.CompactionEventToChunk(item)
		return chunk, nil

	case "reasoning":
		// Reasoning text was already emitted as ChunkTypeReasoning deltas.
		// Forward encrypted_content so callers can round-trip it for multi-turn
		// reasoning when store=false.
		delete(s.reasoningAccum, e.OutputIndex)
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID               string `json:"id,omitempty"`
			EncryptedContent string `json:"encrypted_content,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil || (item.ID == "" && item.EncryptedContent == "") {
			return s.Next()
		}
		meta := map[string]interface{}{}
		if item.EncryptedContent != "" {
			meta["encryptedContent"] = item.EncryptedContent
		}
		if item.ID != "" {
			meta["itemId"] = item.ID
		}
		providerMeta, _ := json.Marshal(map[string]interface{}{"openai": meta})
		return &provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               "reasoning-" + item.ID,
			ProviderMetadata: providerMeta,
		}, nil

	case "custom_tool_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.CustomToolCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: map[string]interface{}{"input": item.Input},
			},
		}, nil

	default:
		delete(s.itemTypes, e.OutputIndex)
		return s.Next()
	}
}

// Ensure ResponsesLanguageModel satisfies the provider.LanguageModel interface
// at compile time.
var _ provider.LanguageModel = (*ResponsesLanguageModel)(nil)
