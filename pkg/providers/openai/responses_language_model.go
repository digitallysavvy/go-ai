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
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
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
func (m *ResponsesLanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider identifier. Uses the ".responses" suffix so
// callers can distinguish Responses API models from Chat Completions models.
func (m *ResponsesLanguageModel) Provider() string { return m.provider.responsesProviderName() }

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
	body, store, warnings, err := m.buildRequest(opts, false)
	if err != nil {
		return nil, err
	}
	webSearchToolName := responsesWebSearchToolName(opts.Tools)

	var resp responses.ResponsesAPIResponse
	if err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Query:   m.provider.responsesQuery(),
		Body:    body,
		Headers: opts.Headers,
	}, &resp); err != nil {
		return nil, m.wrapErr(err)
	}

	result, err := m.convertResponse(resp, store, webSearchToolName, m.provider.responsesProviderOptionsName())
	if err != nil {
		return nil, err
	}
	result.Warnings = append(warnings, result.Warnings...)
	return result, nil
}

// DoStream performs streaming generation via POST /v1/responses with stream=true.
func (m *ResponsesLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, _, warnings, err := m.buildRequest(opts, true)
	if err != nil {
		return nil, err
	}
	webSearchToolName := responsesWebSearchToolName(opts.Tools)

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Query:   m.provider.responsesQuery(),
		Body:    body,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.wrapErr(err)
	}

	return streaming.NewWarningsStream(newResponsesStream(httpResp.Body, opts.IncludeRawChunks, webSearchToolName, m.provider.responsesProviderOptionsName()), warnings), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Request body construction
// ─────────────────────────────────────────────────────────────────────────────

// buildRequestBody constructs the Responses API request body.
// Returns the body map, the effective store flag, and any error.
func (m *ResponsesLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, bool, error) {
	body, store, _, err := m.buildRequest(opts, stream)
	return body, store, err
}

func (m *ResponsesLanguageModel) buildRequest(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, bool, []types.Warning, error) {
	// Extract provider options early (store needed before input conversion).
	store := true
	storeExplicit := false
	conversation := ""
	previousResponseID := ""
	promptCacheRetention := ""
	reasoningEffort := ""
	reasoningSummary := ""
	textVerbosity := ""
	serviceTier := ""
	user := ""
	passThroughUnsupportedFiles := false
	maxToolCalls := 0
	parallelToolCalls := (*bool)(nil)
	truncation := ""
	includeFields := []string(nil)
	var allowedTools *responses.AllowedToolsToolChoice
	providerOptionsName := m.provider.responsesProviderOptionsName()

	if opts.ProviderOptions != nil {
		openaiOpts, ok := opts.ProviderOptions[providerOptionsName].(map[string]interface{})
		if !ok && providerOptionsName != "openai" {
			openaiOpts, ok = opts.ProviderOptions["openai"].(map[string]interface{})
		}
		if ok {
			if v, ok := openaiOpts["store"].(bool); ok {
				store = v
				storeExplicit = true
			}
			if v, ok := openaiOpts["conversation"].(string); ok {
				conversation = v
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
			if v, ok := openaiOpts["textVerbosity"].(string); ok {
				textVerbosity = v
			}
			if v, ok := openaiOpts["serviceTier"].(string); ok {
				serviceTier = v
			}
			if v, ok := openaiOpts["user"].(string); ok {
				user = v
			}
			if v, ok := openaiOpts["passThroughUnsupportedFiles"].(bool); ok {
				passThroughUnsupportedFiles = v
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
			allowedTools = parseAllowedTools(openaiOpts["allowedTools"])
		}
	}

	var warnings []types.Warning
	if conversation != "" && previousResponseID != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "conversation",
			Details: "conversation and previousResponseId cannot be used together",
		})
	}

	// Determine system message mode based on model type.
	systemMsgMode := "system"
	if isReasoningModel(m.modelID) {
		systemMsgMode = "developer"
	}

	// Convert prompt to Responses API input format.
	input, err := responses.ConvertPromptToInputWithOptions(opts.Prompt, systemMsgMode, responses.ConvertOptions{
		PassThroughUnsupportedFiles: passThroughUnsupportedFiles,
		HasPreviousResponseID:       previousResponseID != "",
		HasConversation:             conversation != "",
		Store:                       store,
		CustomToolNames:             customToolNames(opts.Tools),
		HasLocalShellTool:           hasTool(opts.Tools, "openai.local_shell"),
		HasShellTool:                hasTool(opts.Tools, "openai.shell"),
		HasApplyPatchTool:           hasTool(opts.Tools, "openai.apply_patch"),
		FileIDPrefixes:              m.provider.responsesFileIDPrefixes(),
		ProviderOptionsName:         providerOptionsName,
	})
	if err != nil {
		return nil, store, warnings, err
	}

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

	// Temperature and top_p: forbidden for reasoning models except GPT-5.1 and
	// later families when reasoning effort is disabled.
	supportsNonReasoningParams := !isReasoningModel(m.modelID) || (effort == "none" && supportsNonReasoningParameters(m.modelID))
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

	// Response format and/or text verbosity → "text" object.
	// The text object is created when either a response format or textVerbosity is set.
	hasFormat := opts.ResponseFormat != nil && opts.ResponseFormat.Type != ""
	if hasFormat || textVerbosity != "" {
		textObj := map[string]interface{}{}
		if hasFormat {
			textObj["format"] = map[string]interface{}{
				"type": opts.ResponseFormat.Type,
			}
		}
		if textVerbosity != "" {
			textObj["verbosity"] = textVerbosity
		}
		body["text"] = textObj
	}

	// Tools.
	if len(opts.Tools) > 0 {
		body["tools"] = responses.PrepareTools(opts.Tools)
		if allowedTools != nil {
			body["tool_choice"] = *allowedTools
		} else if opts.ToolChoice.Type != "" {
			body["tool_choice"] = convertResponsesToolChoice(opts.ToolChoice, opts.Tools)
		}
	}

	// Build include list — always add reasoning.encrypted_content when
	// store=false and we have a reasoning model, so multi-turn works without
	// server-side persistence.
	if !store && isReasoningModel(m.modelID) {
		includeFields = appendUnique(includeFields, "reasoning.encrypted_content")
	}
	if hasTool(opts.Tools, "openai.web_search") || hasTool(opts.Tools, "openai.web_search_preview") {
		includeFields = appendUnique(includeFields, "web_search_call.action.sources")
	}
	if len(includeFields) > 0 {
		body["include"] = includeFields
	}

	// Provider options fields.
	if conversation != "" {
		body["conversation"] = conversation
	}
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

	return body, store, warnings, nil
}

func parseAllowedTools(value interface{}) *responses.AllowedToolsToolChoice {
	raw, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	names := stringSliceFromInterface(raw["toolNames"])
	if len(names) == 0 {
		return nil
	}
	mode, _ := raw["mode"].(string)
	if mode == "" {
		mode = "auto"
	}
	tools := make([]responses.AllowedToolsToolEntry, len(names))
	for i, name := range names {
		tools[i] = responses.AllowedToolsToolEntry{Type: "function", Name: name}
	}
	return &responses.AllowedToolsToolChoice{
		Type:  "allowed_tools",
		Mode:  mode,
		Tools: tools,
	}
}

func stringSliceFromInterface(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// convertResponsesToolChoice maps a types.ToolChoice to the Responses API format.
func convertResponsesToolChoice(tc types.ToolChoice, tools []types.Tool) interface{} {
	switch tc.Type {
	case types.ToolChoiceNone:
		return "none"
	case types.ToolChoiceRequired:
		return "required"
	case types.ToolChoiceTool:
		name, tool := resolveResponsesToolChoiceName(tc.ToolName, tools)
		switch name {
		case "code_interpreter", "file_search", "image_generation", "web_search_preview", "web_search", "mcp", "apply_patch":
			return map[string]interface{}{"type": name}
		}
		if tool != nil {
			if _, ok := tool.ProviderOptions.(openaitool.CustomTool); ok {
				return map[string]interface{}{"type": "custom", "name": name}
			}
		}
		return map[string]interface{}{
			"type": "function",
			"name": name,
		}
	default:
		return "auto"
	}
}

func resolveResponsesToolChoiceName(name string, tools []types.Tool) (string, *types.Tool) {
	providerNames := map[string]string{
		"openai.code_interpreter":   "code_interpreter",
		"openai.file_search":        "file_search",
		"openai.image_generation":   "image_generation",
		"openai.web_search_preview": "web_search_preview",
		"openai.web_search":         "web_search",
		"openai.mcp":                "mcp",
		"openai.apply_patch":        "apply_patch",
		"code_interpreter":          "code_interpreter",
		"file_search":               "file_search",
		"image_generation":          "image_generation",
		"web_search_preview":        "web_search_preview",
		"web_search":                "web_search",
		"mcp":                       "mcp",
		"apply_patch":               "apply_patch",
	}
	if mapped, ok := providerNames[name]; ok {
		for i := range tools {
			if tools[i].Name == name || tools[i].Name == "openai."+mapped || tools[i].ProviderID == "openai."+mapped {
				return mapped, &tools[i]
			}
		}
		return mapped, nil
	}
	for i := range tools {
		tool := &tools[i]
		if tool.Name == name || tool.ProviderID == name {
			if mapped, ok := providerNames[tool.Name]; ok {
				return mapped, tool
			}
			if mapped, ok := providerNames[tool.ProviderID]; ok {
				return mapped, tool
			}
			return tool.Name, tool
		}
	}
	return name, nil
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

func customToolNames(tools []types.Tool) map[string]bool {
	if len(tools) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, tool := range tools {
		if _, ok := tool.ProviderOptions.(openaitool.CustomTool); ok {
			names[tool.Name] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func hasTool(tools []types.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name || tool.ProviderID == name {
			return true
		}
	}
	return false
}

func responsesWebSearchToolName(tools []types.Tool) string {
	for _, tool := range tools {
		switch {
		case tool.Name == "openai.web_search_preview" || tool.ProviderID == "openai.web_search_preview":
			return "web_search_preview"
		case tool.Name == "openai.web_search" || tool.ProviderID == "openai.web_search":
			return "web_search"
		}
	}
	return "web_search"
}

// ─────────────────────────────────────────────────────────────────────────────
// Non-streaming response conversion
// ─────────────────────────────────────────────────────────────────────────────

func (m *ResponsesLanguageModel) convertResponse(resp responses.ResponsesAPIResponse, store bool, webSearchToolName string, providerOptionsName ...string) (*types.GenerateResult, error) {
	providerName := "openai"
	if len(providerOptionsName) > 0 && providerOptionsName[0] != "" {
		providerName = providerOptionsName[0]
	}
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
				result.Content = append(result.Content, types.TextContent{
					Text:            part.Text,
					ProviderOptions: openAIResponsesMessageProviderOptions(providerName, item.ID, item.Phase),
				})
			}

		case "function_call":
			var item responses.FunctionCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			tc := types.ToolCall{
				ID:               item.CallID,
				ToolName:         item.Name,
				Arguments:        args,
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, item.Namespace),
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
				ProviderMetadata: openAIResponsesReasoningMetadata(providerName, item.ID),
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

		case "web_search_call":
			var item WebSearchCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := webSearchToolName
			if toolName == "" {
				toolName = "web_search"
			}
			tc := types.ToolCall{
				ID:               item.ID,
				ToolName:         toolName,
				Arguments:        map[string]interface{}{},
				ProviderExecuted: true,
			}
			toolCalls = append(toolCalls, tc)
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       item.ID,
					ToolName:         toolName,
					Input:            "{}",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
				types.ToolResultContent{
					ToolCallID:       item.ID,
					ToolName:         toolName,
					Result:           mapWebSearchOutput(item.Action),
					ProviderExecuted: true,
				},
			)

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

func openAIResponsesToolCallMetadata(providerName, itemID, namespace string) map[string]interface{} {
	openai := map[string]interface{}{}
	if itemID != "" {
		openai["itemId"] = itemID
	}
	if namespace != "" {
		openai["namespace"] = namespace
	}
	if len(openai) == 0 {
		return nil
	}
	return map[string]interface{}{providerName: openai}
}

func openAIResponsesReasoningMetadata(providerName, itemID string) json.RawMessage {
	if itemID == "" {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{
		providerName: map[string]interface{}{"itemId": itemID},
	})
	return raw
}

func openAIResponsesMessageProviderOptions(providerName, itemID string, phase *string) map[string]interface{} {
	openai := map[string]interface{}{}
	if itemID != "" {
		openai["itemId"] = itemID
	}
	if phase != nil && *phase != "" {
		openai["phase"] = *phase
	}
	if len(openai) == 0 {
		return nil
	}
	return map[string]interface{}{providerName: openai}
}

func mapWebSearchOutput(action *WebSearchAction) map[string]interface{} {
	if action == nil {
		return map[string]interface{}{}
	}
	result := map[string]interface{}{}
	switch action.Type {
	case "search":
		mapped := map[string]interface{}{"type": "search"}
		if action.Query != nil {
			mapped["query"] = *action.Query
		}
		if action.Queries != nil {
			mapped["queries"] = action.Queries
		}
		result["action"] = mapped
	case "open_page":
		mapped := map[string]interface{}{"type": "openPage", "url": nil}
		if action.URL != nil {
			mapped["url"] = *action.URL
		}
		result["action"] = mapped
	case "find_in_page":
		mapped := map[string]interface{}{"type": "findInPage", "url": nil, "pattern": nil}
		if action.URL != nil {
			mapped["url"] = *action.URL
		}
		if action.Pattern != nil {
			mapped["pattern"] = *action.Pattern
		}
		result["action"] = mapped
	}
	if action.Sources != nil {
		sources := make([]map[string]interface{}, 0, len(action.Sources))
		for _, source := range action.Sources {
			mapped := map[string]interface{}{"type": source.Type}
			if source.URL != "" {
				mapped["url"] = source.URL
			}
			if source.Name != "" {
				mapped["name"] = source.Name
			}
			sources = append(sources, mapped)
		}
		result["sources"] = sources
	}
	return result
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
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

// ─────────────────────────────────────────────────────────────────────────────
// Streaming implementation
// ─────────────────────────────────────────────────────────────────────────────

// responsesToolAccum accumulates streaming tool call fragments for one output item.
type responsesToolAccum struct {
	id        string // call_id
	itemID    string
	name      string
	namespace string
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
	flushQueue        []*provider.StreamChunk
	includeRawChunks  bool
	webSearchToolName string
	providerName      string
	responseID        string
}

func newResponsesStream(r io.ReadCloser, includeRawChunks bool, args ...string) *responsesStream {
	toolName := "web_search"
	if len(args) > 0 && args[0] != "" {
		toolName = args[0]
	}
	providerName := "openai"
	if len(args) > 1 && args[1] != "" {
		providerName = args[1]
	}
	return &responsesStream{
		reader:            r,
		parser:            streaming.NewSSEParser(r),
		toolAccum:         make(map[int]*responsesToolAccum),
		reasoningAccum:    make(map[int]*responsesReasoningAccum),
		itemTypes:         make(map[int]string),
		includeRawChunks:  includeRawChunks,
		webSearchToolName: toolName,
		providerName:      providerName,
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

func (s *responsesStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, error) {
	if chunk == nil {
		return s.Next()
	}
	if len(s.flushQueue) == 0 {
		return chunk, nil
	}
	queue := make([]*provider.StreamChunk, 0, len(s.flushQueue)+1)
	queue = append(queue, s.flushQueue[0], chunk)
	queue = append(queue, s.flushQueue[1:]...)
	s.flushQueue = queue
	return s.Next()
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
	if s.includeRawChunks {
		var raw interface{}
		if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
			raw = event.Data
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeRaw,
			Raw:  raw,
		})
	}

	// Parse the "type" discriminator.
	var peek responses.ResponsesStreamEvent
	if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		})
	}

	switch peek.Type {

	case "response.created":
		var e responses.ResponseCreatedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if e.Response.ID != "" {
			s.responseID = e.Response.ID
		}
		metadata := &provider.ResponseMetadata{
			ID:      e.Response.ID,
			ModelID: e.Response.Model,
		}
		if e.Response.CreatedAt != 0 {
			metadata.Timestamp = time.Unix(e.Response.CreatedAt, 0)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: metadata,
		})

	case "response.output_item.added":
		var e responses.OutputItemAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		s.itemTypes[e.OutputIndex] = e.Item.Type
		switch e.Item.Type {
		case "function_call":
			s.toolAccum[e.OutputIndex] = &responsesToolAccum{
				id:        e.Item.CallID,
				itemID:    e.Item.ID,
				name:      e.Item.Name,
				namespace: e.Item.Namespace,
			}
		case "web_search_call":
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:               e.Item.ID,
						ToolName:         s.webSearchToolName,
						ProviderExecuted: true,
					},
				},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputEnd,
					ToolCall: &types.ToolCall{
						ID:               e.Item.ID,
						ToolName:         s.webSearchToolName,
						ProviderExecuted: true,
					},
				},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               e.Item.ID,
						ToolName:         s.webSearchToolName,
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
				},
			)
			return s.Next()
		case "reasoning":
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
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: e.Delta,
		})

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
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			Reasoning: e.Delta,
		})

	case "response.output_item.done":
		var e responses.OutputItemDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		return s.handleOutputItemDone(e)

	case "response.completed", "response.incomplete":
		var e responses.ResponseCompletedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			s.err = io.EOF
			return nil, io.EOF
		}
		usage := convertResponsesUsage(e.Response.Usage)
		finishReason := mapResponsesFinishReason(e.Response.IncompleteDetails, false)

		var meta json.RawMessage
		responseID := s.responseID
		if responseID == "" {
			responseID = e.Response.ID
		}
		if responseID != "" {
			meta, _ = json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"responseId": responseID}})
		}

		s.err = io.EOF
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeFinish,
			FinishReason:     finishReason,
			Usage:            &usage,
			ProviderMetadata: meta,
		})

	case "response.failed":
		var e responses.ResponseFailedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			s.err = io.EOF
			return nil, io.EOF
		}
		usage := convertResponsesUsage(e.Response.Usage)
		finishReason := types.FinishReason("error")
		if e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason != "" {
			finishReason = mapResponsesFinishReason(e.Response.IncompleteDetails, false)
		}

		metaMap := map[string]interface{}{}
		responseID := s.responseID
		if responseID == "" {
			responseID = e.Response.ID
		}
		if responseID != "" {
			metaMap["responseId"] = responseID
		}
		if e.Response.ServiceTier != "" {
			metaMap["serviceTier"] = e.Response.ServiceTier
		}
		var meta json.RawMessage
		if len(metaMap) > 0 {
			meta, _ = json.Marshal(map[string]interface{}{s.providerName: metaMap})
		}

		s.err = io.EOF
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeFinish,
			FinishReason:     finishReason,
			Usage:            &usage,
			ProviderMetadata: meta,
		})

	case "error":
		var e responses.ResponsesStreamErrorEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: e.Message,
		})

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
		var item responses.FunctionCallItem
		if err := json.Unmarshal(e.Item, &item); err == nil {
			if item.ID != "" {
				accum.itemID = item.ID
			}
			if item.Namespace != "" {
				accum.namespace = item.Namespace
			}
			if accum.arguments == "" && item.Arguments != "" {
				json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			}
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               accum.id,
				ToolName:         accum.name,
				Arguments:        args,
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, accum.itemID, accum.namespace),
			},
		})

	case "compaction":
		var item responses.CompactionEvent
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		chunk := responses.CompactionEventToChunk(item)
		return s.emitParsedChunk(chunk)

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
		providerMeta, _ := json.Marshal(map[string]interface{}{s.providerName: meta})
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               "reasoning-" + item.ID,
			ProviderMetadata: providerMeta,
		})

	case "custom_tool_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.CustomToolCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: map[string]interface{}{"input": item.Input},
			},
		})

	case "web_search_call":
		delete(s.itemTypes, e.OutputIndex)
		var item WebSearchCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.ID,
				ToolName:   s.webSearchToolName,
				Result:     mapWebSearchOutput(item.Action),
			},
		})

	default:
		delete(s.itemTypes, e.OutputIndex)
		return s.Next()
	}
}

// Ensure ResponsesLanguageModel satisfies the provider.LanguageModel interface
// at compile time.
var _ provider.LanguageModel = (*ResponsesLanguageModel)(nil)
