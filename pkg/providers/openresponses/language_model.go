package openresponses

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Open Responses
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// OpenResponsesProviderOptions holds providerOptions.openai for the Open
// Responses model. Unlike the OpenAI chat/Responses packages, Open Responses
// has no "forceReasoning" concept — reasoning.effort/summary are always sent
// when resolved, with no reasoning-model gating (row 3b9f025/e69a836), so
// there is nothing to force.
type OpenResponsesProviderOptions struct {
	ReasoningSummary    string                 `json:"reasoningSummary,omitempty"`
	ReasoningSummarySet bool                   `json:"-"`
	ReasoningEffort     string                 `json:"reasoningEffort,omitempty"`
	StrictJSONSchema    *bool                  `json:"strictJsonSchema,omitempty"`
	TextVerbosity       string                 `json:"textVerbosity,omitempty"`
	Raw                 map[string]interface{} `json:"-"`
}

// NewLanguageModel creates a new Open Responses language model
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
	return m.provider.config.Name + ".responses"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Tool support depends on the underlying model
	// Return true as many local models support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Image support depends on the underlying model
	// Return true for vision models (users need to verify their model supports it)
	return true
}

// SupportedURLs reports the URL patterns this model can receive directly.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^https?://.*$`},
	}
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	// Build request body
	reqBody, warnings, err := m.buildRequestBody(opts, false)
	if err != nil {
		return nil, err
	}

	// Make API request
	var response OpenResponsesResponse
	err = m.provider.client.PostJSON(ctx, "", reqBody, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Row 75f86f4: a 200 response with an embedded error object maps to a
	// 400 ProviderError.
	if response.Error != nil {
		return nil, providererrors.NewProviderError(m.provider.config.Name, 400, response.Error.Code, response.Error.Message, nil)
	}

	// Row 75f86f4: a 200 response with no `output` field is not a valid
	// (if unusual) result -- it means the API returned nothing to work
	// with, and must raise a descriptive error rather than silently
	// producing an empty result.
	if response.Output == nil {
		detail := ""
		if response.IncompleteDetails != nil {
			detail = response.IncompleteDetails.Reason
		}
		if detail == "" {
			detail = response.Status
		}
		message := "Responses API returned no output"
		if detail != "" {
			message = fmt.Sprintf("Responses API returned no output (%s)", detail)
		}
		return nil, providererrors.NewProviderError(m.provider.config.Name, 500, "", message, nil)
	}

	// Convert response to GenerateResult
	result := m.convertResponse(response)
	result.Warnings = warnings

	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// Build request body with streaming enabled
	reqBody, warnings, err := m.buildRequestBody(opts, true)
	if err != nil {
		return nil, err
	}

	// Make streaming API request
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	// Create stream wrapper
	return newOpenResponsesStream(httpResp.Body, warnings, m.provider.config.Name), nil
}

// buildRequestBody builds the Open Responses API request body
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning
	provOpts := extractOpenResponsesProviderOptions(opts.ProviderOptions, m.provider.config.Name)
	if openResponsesTruthyString(provOpts.Raw["conversation"]) && openResponsesTruthyString(provOpts.Raw["previousResponseId"]) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "conversation",
			Details: "conversation and previousResponseId cannot be used together",
		})
	}

	// Convert messages to Open Responses format
	input, instructions, conversionWarnings, err := ConvertToOpenResponsesInputForProviderStrict(opts.Prompt.Messages, opts.Prompt.System, m.provider.config.Name, m.provider.config.StrictResponseInput)
	if err != nil {
		return nil, warnings, err
	}
	warnings = append(warnings, conversionWarnings...)

	body := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}
	if stream {
		body["stream"] = true
	}

	// Add instructions if present
	if instructions != "" {
		body["instructions"] = instructions
	}

	// Add optional parameters
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	// Note: Open Responses doesn't support frequencyPenalty, presencePenalty,
	// stopSequences, topK, or seed. These are ignored with TS-shaped warnings.
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "frequencyPenalty",
		})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "presencePenalty",
		})
	}

	// These are ignored with warnings
	if opts.StopSequences != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "stopSequences",
		})
	}
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "topK",
		})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "seed",
		})
	}

	// Convert tools, skipping provider-defined tools (OR-CORE does not carry
	// an extension registry) with an "unsupported" warning, mirroring TS's
	// getArgs behavior when no matching extension is registered. Only send
	// the tools field when the resulting list is non-empty (TS: `tools:
	// convertedTools.length ? convertedTools : undefined`).
	convertedTools, toolWarnings := convertToolsToOpenResponses(opts.Tools)
	warnings = append(warnings, toolWarnings...)
	if len(convertedTools) > 0 {
		body["tools"] = convertedTools
	}

	// Convert tool choice
	if allowedToolsChoice := openResponsesAllowedToolsChoice(provOpts.Raw); allowedToolsChoice != nil {
		body["tool_choice"] = allowedToolsChoice
	} else if opts.ToolChoice.Type == "tool" && openResponsesToolChoiceTargetsProviderTool(opts.Tools, opts.ToolChoice.ToolName) {
		// Targets a provider-defined tool we couldn't encode; omit tool_choice
		// like TS does when no extension is registered for it.
	} else if opts.ToolChoice.Type != "" {
		body["tool_choice"] = convertToolChoiceToOpenResponses(opts.ToolChoice)
	}

	// Add response format if present. TS only serializes responseFormat when
	// responseFormat.type === "json"; text verbosity may create text by itself.
	hasJSONResponseFormat := opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json"
	if hasJSONResponseFormat || provOpts.TextVerbosity != "" {
		textConfig := map[string]interface{}{}
		if hasJSONResponseFormat {
			format := map[string]interface{}{"type": "json_object"}
			if opts.ResponseFormat.Schema != nil {
				format["type"] = "json_schema"
				name := opts.ResponseFormat.Name
				if name == "" {
					name = "response"
				}
				format["name"] = name
				if opts.ResponseFormat.Description != "" {
					format["description"] = opts.ResponseFormat.Description
				}
				format["schema"] = opts.ResponseFormat.Schema
				strict := true
				if provOpts.StrictJSONSchema != nil {
					strict = *provOpts.StrictJSONSchema
				}
				format["strict"] = strict
			}
			textConfig["format"] = format
		}
		if provOpts.TextVerbosity != "" {
			textConfig["verbosity"] = provOpts.TextVerbosity
		}
		body["text"] = textConfig
	}

	// Map top-level Reasoning to Open Responses reasoning.effort. The Open
	// Responses package (unlike the OpenAI chat/responses model packages)
	// applies no "is this a reasoning model" gating: reasoning.effort /
	// reasoning.summary are always sent to the endpoint when resolved,
	// regardless of the model ID (OR-CORE item 8/9). A resolved provider
	// option (reasoningEffort raw string) always takes precedence and is
	// passed through verbatim, matching TS's
	// `openResponsesOptions?.reasoningEffort ?? mapReasoningToProviderEffort(...)`.
	var reasoningEffort string
	if opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			reasoningEffort = "none"
		case types.ReasoningMinimal:
			// TS effortMap maps minimal -> low for Open Responses (there is no
			// "minimal" effort value on this API) and reports a compatibility
			// warning when the mapped value differs from the requested one.
			reasoningEffort = "low"
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "reasoning",
				Details: `reasoning "minimal" is not directly supported by this model. mapped to effort "low".`,
			})
		case types.ReasoningLow:
			reasoningEffort = "low"
		case types.ReasoningMedium:
			reasoningEffort = "medium"
		case types.ReasoningHigh:
			reasoningEffort = "high"
		case types.ReasoningXHigh:
			reasoningEffort = "xhigh"
			// ReasoningDefault ("provider-default"): omit.
		}
	}
	resolvedReasoningSummary := provOpts.ReasoningSummary
	if provOpts.ReasoningSummarySet && resolvedReasoningSummary != "" {
		switch resolvedReasoningSummary {
		case "concise", "detailed", "auto":
			// valid
		default:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningSummary",
				Details: fmt.Sprintf("reasoningSummary %q is not a supported value (concise, detailed, auto)", resolvedReasoningSummary),
			})
			resolvedReasoningSummary = ""
		}
	}
	if provOpts.ReasoningEffort != "" {
		reasoningEffort = provOpts.ReasoningEffort
	}
	if reasoningEffort != "" || resolvedReasoningSummary != "" {
		reasoning := map[string]interface{}{}
		if reasoningEffort != "" {
			reasoning["effort"] = reasoningEffort
		}
		if resolvedReasoningSummary != "" {
			reasoning["summary"] = resolvedReasoningSummary
		}
		body["reasoning"] = reasoning
	}
	addOpenResponsesProviderOptionBodyFields(body, provOpts.Raw)
	if store, ok := provOpts.Raw["store"].(bool); ok && !store {
		addOpenResponsesInclude(body, "reasoning.encrypted_content")
	}
	if serviceTier, ok := body["service_tier"].(string); ok {
		switch serviceTier {
		case "flex":
			if !openResponsesSupportsFlexProcessing(m.modelID) {
				delete(body, "service_tier")
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "serviceTier",
					Details: "flex processing is only available for o3, o4-mini, and gpt-5 models",
				})
			}
		case "priority":
			if !openResponsesSupportsPriorityProcessing(m.modelID) {
				delete(body, "service_tier")
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "serviceTier",
					Details: "priority processing is only available for supported models (gpt-4, gpt-5, gpt-5-mini, o3, o4-mini) and requires Enterprise access. gpt-5-nano is not supported",
				})
			}
		}
	}

	return body, warnings, nil
}

func openResponsesSupportsFlexProcessing(modelID string) bool {
	return strings.HasPrefix(modelID, "o3") ||
		strings.HasPrefix(modelID, "o4-mini") ||
		(strings.HasPrefix(modelID, "gpt-5") && !strings.HasPrefix(modelID, "gpt-5-chat"))
}

func openResponsesSupportsPriorityProcessing(modelID string) bool {
	return strings.HasPrefix(modelID, "gpt-4") ||
		(strings.HasPrefix(modelID, "gpt-5") &&
			!strings.HasPrefix(modelID, "gpt-5-nano") &&
			!strings.HasPrefix(modelID, "gpt-5-chat") &&
			!strings.HasPrefix(modelID, "gpt-5.4-nano")) ||
		strings.HasPrefix(modelID, "o3") ||
		strings.HasPrefix(modelID, "o4-mini")
}

func extractOpenResponsesProviderOptions(providerOptions map[string]interface{}, providerName string) OpenResponsesProviderOptions {
	if providerOptions == nil {
		return OpenResponsesProviderOptions{}
	}
	keys := []string{"openai", providerName, "openResponses", "open-responses"}
	for _, key := range keys {
		raw, ok := providerOptions[key]
		if !ok {
			continue
		}
		data, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var opts OpenResponsesProviderOptions
		if err := json.Unmarshal(data, &opts); err == nil {
			if values, ok := raw.(map[string]interface{}); ok {
				opts.Raw = values
				if summary, ok := values["reasoningSummary"]; ok {
					opts.ReasoningSummarySet = true
					if text, ok := summary.(string); ok {
						opts.ReasoningSummary = text
					}
				}
			}
			return opts
		}
	}
	return OpenResponsesProviderOptions{}
}

func addOpenResponsesProviderOptionBodyFields(body map[string]interface{}, raw map[string]interface{}) {
	if raw == nil {
		return
	}
	mappings := map[string]string{
		"conversation":         "conversation",
		"maxToolCalls":         "max_tool_calls",
		"metadata":             "metadata",
		"parallelToolCalls":    "parallel_tool_calls",
		"previousResponseId":   "previous_response_id",
		"store":                "store",
		"user":                 "user",
		"instructions":         "instructions",
		"serviceTier":          "service_tier",
		"include":              "include",
		"promptCacheKey":       "prompt_cache_key",
		"promptCacheRetention": "prompt_cache_retention",
		"safetyIdentifier":     "safety_identifier",
		"truncation":           "truncation",
	}
	for optionKey, bodyKey := range mappings {
		if value, ok := raw[optionKey]; ok {
			body[bodyKey] = value
		}
	}
	if topLogprobs, ok := openResponsesTopLogprobs(raw["logprobs"]); ok {
		body["top_logprobs"] = topLogprobs
		addOpenResponsesInclude(body, "message.output_text.logprobs")
	}
	if value, ok := raw["contextManagement"]; ok {
		if entries := openResponsesContextManagementEntries(value); len(entries) > 0 {
			out := make([]map[string]interface{}, 0, len(entries))
			for _, entry := range entries {
				mapped := map[string]interface{}{}
				if v, ok := entry["type"]; ok {
					mapped["type"] = v
				}
				if v, ok := entry["compactThreshold"]; ok {
					mapped["compact_threshold"] = v
				}
				out = append(out, mapped)
			}
			body["context_management"] = out
		}
	}
}

func openResponsesContextManagementEntries(value interface{}) []map[string]interface{} {
	switch entries := value.(type) {
	case []map[string]interface{}:
		return entries
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(entries))
		for _, entry := range entries {
			if values, ok := entry.(map[string]interface{}); ok {
				out = append(out, values)
			}
		}
		return out
	default:
		return nil
	}
}

func openResponsesTopLogprobs(value interface{}) (interface{}, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return 20, true
		}
	case int:
		if v != 0 {
			return v, true
		}
	case int64:
		if v != 0 {
			return v, true
		}
	case float64:
		if v != 0 {
			return v, true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i != 0 {
			return i, true
		}
	}
	return nil, false
}

func addOpenResponsesInclude(body map[string]interface{}, include string) {
	existing := openResponsesIncludeValues(body["include"])
	for _, value := range existing {
		if value == include {
			return
		}
	}
	body["include"] = append(existing, include)
}

func openResponsesIncludeValues(value interface{}) []interface{} {
	switch values := value.(type) {
	case []interface{}:
		return values
	case []string:
		out := make([]interface{}, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out
	default:
		return nil
	}
}

func openResponsesAllowedToolsChoice(raw map[string]interface{}) map[string]interface{} {
	if raw == nil {
		return nil
	}
	value, ok := raw["allowedTools"]
	if !ok || value == nil {
		return nil
	}
	allowedTools, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	names := openResponsesStringList(allowedTools["toolNames"])
	if len(names) == 0 {
		return nil
	}
	mode, _ := allowedTools["mode"].(string)
	if mode == "" {
		mode = "auto"
	}
	tools := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"name": name,
		})
	}
	if len(tools) == 0 {
		return nil
	}
	return map[string]interface{}{
		"type":  "allowed_tools",
		"mode":  mode,
		"tools": tools,
	}
}

func openResponsesStringList(value interface{}) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []interface{}:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func openResponsesTruthyString(value interface{}) bool {
	text, ok := value.(string)
	return ok && text != ""
}

// convertToolsToOpenResponses converts AI SDK tools to Open Responses format.
// Provider-defined tools (Type == "provider") are skipped with an
// "unsupported" warning: OR-CORE does not carry an extension registry to
// encode them (see OR-EXT, item 12), mirroring TS's getArgs behavior when no
// matching extension is registered for a provider tool.
func convertToolsToOpenResponses(tools []types.Tool) ([]FunctionTool, []types.Warning) {
	result := make([]FunctionTool, 0, len(tools))
	var warnings []types.Warning

	for _, t := range tools {
		if t.Type == "provider" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("provider-defined tool %s", t.ProviderID),
			})
			continue
		}

		ft := FunctionTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
		}

		if t.Parameters != nil {
			// Convert parameters to map if possible
			if paramsMap, ok := t.Parameters.(map[string]interface{}); ok {
				ft.Parameters = paramsMap
			}
		}

		ft.Strict = t.Strict

		result = append(result, ft)
	}

	return result, warnings
}

// openResponsesToolChoiceTargetsProviderTool reports whether toolName refers
// to a provider-defined tool in tools, used to omit tool_choice for tools
// this package cannot encode (item 7).
func openResponsesToolChoiceTargetsProviderTool(tools []types.Tool, toolName string) bool {
	for _, t := range tools {
		if t.Name == toolName {
			return t.Type == "provider"
		}
	}
	return false
}

// convertToolChoiceToOpenResponses converts tool choice to Open Responses format
func convertToolChoiceToOpenResponses(toolChoice types.ToolChoice) interface{} {
	switch toolChoice.Type {
	case "auto":
		return "auto"
	case "required":
		return "required"
	case "none":
		return "none"
	case "tool":
		return map[string]interface{}{
			"type": "function",
			"name": toolChoice.ToolName,
		}
	default:
		return "auto"
	}
}

// convertResponse converts an Open Responses response to GenerateResult
func (m *LanguageModel) convertResponse(response OpenResponsesResponse) *types.GenerateResult {
	result := &types.GenerateResult{
		RawResponse: response,
	}

	// Convert usage information
	if response.Usage != nil {
		result.Usage = convertOpenResponsesUsage(response.Usage)
	}

	// Extract content from output items
	var textParts []string
	var toolCalls []types.ToolCall
	hasToolCalls := false

	for _, item := range response.Output {
		switch item.Type {
		case "message":
			// Extract text from message content
			for _, part := range item.Content {
				if part.Type == "output_text" {
					textParts = append(textParts, part.Text)
				}
			}

		case "reasoning":
			// Include reasoning parts when the item has an ID or encrypted_content (#12869).
			// Previously these were skipped when ItemID was absent; now we include them
			// as long as either the ID or EncryptedContent is non-empty.
			if item.ID != "" || item.EncryptedContent != "" {
				// Reasoning items expose their text through Summary, not Content.
				var summaryText string
				for _, part := range item.Summary {
					if part.Type == "summary_text" || part.Type == "text" {
						summaryText += part.Text
					}
				}
				if summaryText != "" {
					textParts = append(textParts, summaryText)
				}
				itemCopy := item
				// Preserve EncryptedContent so callers can forward this reasoning
				// block in subsequent turns (input-side round-trip, #12869), plus
				// full itemId/reasoningSummary/reasoningContent metadata (row
				// 1c1a9d1) so replay can reconstruct the original item exactly.
				result.Content = append(result.Content, types.ReasoningContent{
					Text:             summaryText,
					EncryptedContent: item.EncryptedContent,
					ProviderMetadata: openResponsesReasoningProviderMetadata(m.providerName(), &itemCopy),
				})
			}

		case "function_call":
			// Extract tool call
			hasToolCalls = true

			var args map[string]interface{}
			if item.Arguments != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			}

			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.CallID,
				ToolName:         item.Name,
				Arguments:        args,
				ProviderMetadata: openResponsesToolCallMetadata(m.providerName(), item.ID, item.Namespace),
			})

		case "custom_tool_call":
			hasToolCalls = true
			toolCalls = append(toolCalls, types.ToolCall{
				ID:        item.CallID,
				ToolName:  item.Name,
				Arguments: map[string]interface{}{"input": item.Input},
			})
		}
	}

	// Combine text parts
	if len(textParts) > 0 {
		result.Text = textParts[0]
		if len(textParts) > 1 {
			// Join multiple text parts
			fullText := ""
			for _, part := range textParts {
				fullText += part
			}
			result.Text = fullText
		}
	}

	// Set tool calls
	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}

	// Determine finish reason
	finishReason := ""
	if response.IncompleteDetails != nil {
		finishReason = response.IncompleteDetails.Reason
	}
	result.FinishReason = MapOpenResponsesFinishReason(finishReason, hasToolCalls)

	return result
}

// convertOpenResponsesUsage converts Open Responses usage to AI SDK usage
func convertOpenResponsesUsage(usage *Usage) types.Usage {
	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	totalTokens := int64(usage.TotalTokens)

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Calculate cached tokens
	var cachedTokens int64
	if usage.InputTokensDetails != nil {
		cachedTokens = int64(usage.InputTokensDetails.CachedTokens)
	}

	// Calculate reasoning tokens
	var reasoningTokens int64
	if usage.OutputTokensDetails != nil {
		reasoningTokens = int64(usage.OutputTokensDetails.ReasoningTokens)
	}

	// Set input token details
	if cachedTokens > 0 {
		noCacheTokens := inputTokens - cachedTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:   &noCacheTokens,
			CacheReadTokens: &cachedTokens,
		}
	}

	// Set output token details
	if reasoningTokens > 0 {
		textTokens := outputTokens - reasoningTokens
		result.OutputDetails = &types.OutputTokenDetails{
			TextTokens:      &textTokens,
			ReasoningTokens: &reasoningTokens,
		}
	}

	// Store raw usage
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
		"total_tokens":  usage.TotalTokens,
	}

	return result
}

// handleError converts errors to provider errors
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError(m.provider.config.Name, 0, "", err.Error(), err)
}

// openResponsesStream implements provider.TextStream for Open Responses streaming
type openResponsesStream struct {
	reader       io.ReadCloser
	parser       *streaming.SSEParser
	err          error
	warnings     []types.Warning
	providerName string

	// Track state for tool calls and finish reason
	toolCallsByItemID map[string]*toolCallState
	hasToolCalls      bool
	finishReason      types.FinishReason

	// activeReasoningID tracks the item id of an in-progress reasoning block
	// (row a0d2e8c/6fe187f), so an unfinished reasoning block can be closed
	// with its original id if the stream ends before output_item.done fires.
	activeReasoningID string

	// pending holds chunks queued to be returned by subsequent Next() calls,
	// used when a single event must emit more than one chunk (e.g. a
	// reasoning-end synthesized before the finish chunk).
	pending []*provider.StreamChunk
}

// toolCallState tracks the state of a tool call during streaming
type toolCallState struct {
	ID               string
	ToolName         string
	ArgsJSON         string // Accumulated JSON string
	ProviderMetadata map[string]interface{}
}

// newOpenResponsesStream creates a new Open Responses stream
func newOpenResponsesStream(reader io.ReadCloser, warnings []types.Warning, providerName ...string) *openResponsesStream {
	name := "open-responses"
	if len(providerName) > 0 && providerName[0] != "" {
		name = providerName[0]
	}
	return &openResponsesStream{
		reader:            reader,
		parser:            streaming.NewSSEParser(reader),
		warnings:          warnings,
		providerName:      name,
		toolCallsByItemID: make(map[string]*toolCallState),
		finishReason:      types.FinishReasonOther,
	}
}

// Read implements io.Reader
func (s *openResponsesStream) Read(p []byte) (n int, err error) {
	return s.reader.Read(p)
}

// Close implements io.Closer
func (s *openResponsesStream) Close() error {
	return s.reader.Close()
}

// Next returns the next chunk in the stream
func (s *openResponsesStream) Next() (*provider.StreamChunk, error) {
	if len(s.pending) > 0 {
		chunk := s.pending[0]
		s.pending = s.pending[1:]
		return chunk, nil
	}
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
	var streamEvent StreamEvent
	if err := json.Unmarshal([]byte(event.Data), &streamEvent); err != nil {
		return nil, fmt.Errorf("failed to parse stream event: %w", err)
	}

	// Handle different event types
	return s.handleStreamEvent(&streamEvent)
}

// handleStreamEvent processes a stream event and returns appropriate chunk
func (s *openResponsesStream) handleStreamEvent(event *StreamEvent) (*provider.StreamChunk, error) {
	switch event.Type {
	case "response.output_item.added":
		// New output item started
		if event.Item != nil && event.Item.Type == "function_call" {
			// Initialize tool call tracking
			s.toolCallsByItemID[event.Item.ID] = &toolCallState{
				ID:               event.Item.CallID,
				ToolName:         event.Item.Name,
				ArgsJSON:         "",
				ProviderMetadata: openResponsesToolCallMetadata(s.providerName, event.Item.ID, event.Item.Namespace),
			}
			return s.Next()
		}
		// Row a0d2e8c: a new reasoning block starts streaming its summary.
		if event.Item != nil && event.Item.Type == "reasoning" {
			s.activeReasoningID = event.Item.ID
			return &provider.StreamChunk{
				Type: provider.ChunkTypeReasoningStart,
				ID:   event.Item.ID,
			}, nil
		}
		// Don't emit a chunk for this event
		return s.Next()

	case "response.output_text.delta":
		// Text delta
		return &provider.StreamChunk{
			Type: provider.ChunkTypeText,
			Text: event.Delta,
		}, nil

	// Row a0d2e8c: reasoning summary/text deltas. response.reasoning_text.delta
	// is an LM Studio extension not in the official Responses spec, but both
	// carry the same {item_id, delta} shape.
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return &provider.StreamChunk{
			Type:      provider.ChunkTypeReasoning,
			ID:        event.ItemID,
			Reasoning: event.Delta,
		}, nil

	case "response.function_call_arguments.delta":
		// Tool call arguments delta. Row fb82a6c: create the accumulator
		// lazily if a delta arrives without a prior output_item.added.
		state, ok := s.toolCallsByItemID[event.ItemID]
		if !ok {
			state = &toolCallState{
				ProviderMetadata: openResponsesToolCallMetadata(s.providerName, event.ItemID, ""),
			}
			s.toolCallsByItemID[event.ItemID] = state
		}
		state.ArgsJSON += event.Delta
		return s.Next()

	case "response.function_call_arguments.done":
		// Tool call arguments complete. Row fb82a6c: create the accumulator
		// lazily if this arrives without a prior output_item.added.
		state, ok := s.toolCallsByItemID[event.ItemID]
		if !ok {
			state = &toolCallState{
				ProviderMetadata: openResponsesToolCallMetadata(s.providerName, event.ItemID, ""),
			}
			s.toolCallsByItemID[event.ItemID] = state
		}
		// Use the final arguments from the event if provided
		if event.Arguments != "" {
			state.ArgsJSON = event.Arguments
		}
		return s.Next()

	case "response.output_item.done":
		// Output item complete
		if event.Item == nil {
			return s.Next()
		}
		switch event.Item.Type {
		case "function_call":
			s.hasToolCalls = true
			// Row fb82a6c: the done item's own id/call_id/name are
			// authoritative; fall back to the accumulator (built from
			// output_item.added and/or delta events, if any were seen) only
			// for provider metadata and for arguments when the done event
			// itself doesn't carry them.
			state, ok := s.toolCallsByItemID[event.Item.ID]
			id := event.Item.CallID
			toolName := event.Item.Name
			argsJSON := event.Item.Arguments
			providerMetadata := openResponsesToolCallMetadata(s.providerName, event.Item.ID, event.Item.Namespace)
			if ok {
				if id == "" {
					id = state.ID
				}
				if toolName == "" {
					toolName = state.ToolName
				}
				if argsJSON == "" {
					argsJSON = state.ArgsJSON
				}
				if state.ProviderMetadata != nil {
					providerMetadata = state.ProviderMetadata
				}
			}
			var args map[string]interface{}
			if argsJSON != "" {
				_ = json.Unmarshal([]byte(argsJSON), &args) //nolint:errcheck
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               id,
					ToolName:         toolName,
					Arguments:        args,
					ProviderMetadata: providerMetadata,
				},
			}, nil
		case "custom_tool_call":
			s.hasToolCalls = true
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:        event.Item.CallID,
					ToolName:  event.Item.Name,
					Arguments: map[string]interface{}{"input": event.Item.Input},
				},
			}, nil
		case "reasoning":
			// Row a0d2e8c/6fe187f: close the reasoning block using its real
			// item id (not a synthesized "reasoning-"+id), carrying full
			// provider metadata ({itemId, reasoningSummary, reasoningContent,
			// reasoningEncryptedContent}) like createReasoningProviderMetadata.
			if s.activeReasoningID == event.Item.ID {
				s.activeReasoningID = ""
			}
			providerMeta := openResponsesReasoningProviderMetadata(s.providerName, event.Item)
			return &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningEnd,
				ID:               event.Item.ID,
				ProviderMetadata: providerMeta,
			}, nil
		}
		return s.Next()

	case "response.completed", "response.incomplete":
		finishReason := ""
		if event.Response != nil && event.Response.IncompleteDetails != nil {
			finishReason = event.Response.IncompleteDetails.Reason
		}
		fr := MapOpenResponsesFinishReason(finishReason, s.hasToolCalls)

		var usage *types.Usage
		if event.Response != nil && event.Response.Usage != nil {
			u := convertOpenResponsesUsage(event.Response.Usage)
			usage = &u
		}

		s.err = io.EOF
		finishChunk := &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			FinishReason: fr,
			Usage:        usage,
		}
		// Row 6fe187f: close an unfinished reasoning block (using its
		// original item id) before the finish chunk, mirroring TS's
		// transform stream flush().
		if s.activeReasoningID != "" {
			reasoningEnd := &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: s.activeReasoningID}
			s.activeReasoningID = ""
			s.pending = append(s.pending, finishChunk)
			return reasoningEnd, nil
		}
		return finishChunk, nil

	case "response.failed":
		// Response failed
		s.finishReason = types.FinishReasonError
		if event.Response != nil && event.Error != nil {
			s.err = fmt.Errorf("%s: %s", event.Error.Code, event.Error.Message)
			return nil, s.err
		}
		return s.Next()

	case "error":
		// Error event
		if event.Error != nil {
			s.err = fmt.Errorf("%s: %s", event.Error.Code, event.Error.Message)
			return nil, s.err
		}
		return s.Next()

	default:
		// Unknown event type, skip
		return s.Next()
	}
}

// openResponsesReasoningProviderMetadata builds the provider metadata for a
// reasoning content part / reasoning-end chunk, mirroring TS
// createReasoningProviderMetadata: {itemId, reasoningSummary:[{type,text}],
// reasoningContent:[{type,text}]|null, reasoningEncryptedContent?}.
func openResponsesReasoningProviderMetadata(providerName string, item *OutputItem) json.RawMessage {
	if item == nil {
		return nil
	}
	summary := make([]map[string]interface{}, 0, len(item.Summary))
	for _, part := range item.Summary {
		summary = append(summary, map[string]interface{}{"type": "summary_text", "text": part.Text})
	}
	var reasoningContent interface{}
	if len(item.Content) > 0 {
		content := make([]map[string]interface{}, 0, len(item.Content))
		for _, part := range item.Content {
			content = append(content, map[string]interface{}{"type": "reasoning_text", "text": part.Text})
		}
		reasoningContent = content
	}
	payload := map[string]interface{}{
		"itemId":           item.ID,
		"reasoningSummary": summary,
		"reasoningContent": reasoningContent,
	}
	if item.EncryptedContent != "" {
		payload["reasoningEncryptedContent"] = item.EncryptedContent
	}
	raw, err := json.Marshal(map[string]interface{}{providerName: payload})
	if err != nil {
		return nil
	}
	return raw
}

func openResponsesToolCallMetadata(providerName, itemID, namespace string) map[string]interface{} {
	if itemID == "" && namespace == "" {
		return nil
	}
	payload := map[string]interface{}{}
	if itemID != "" {
		payload["itemId"] = itemID
	}
	if namespace != "" {
		payload["namespace"] = namespace
	}
	return map[string]interface{}{providerName: payload}
}

func (m *LanguageModel) providerName() string {
	if m != nil && m.provider != nil && m.provider.config.Name != "" {
		return m.provider.config.Name
	}
	return "open-responses"
}

// Err returns any error that occurred during streaming
func (s *openResponsesStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
