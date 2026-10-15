package xai

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// XAIResponsesProviderOptions contains XAI-specific options for the Responses API path.
type XAIResponsesProviderOptions struct {
	// ReasoningSummary is deprecated: xAI ignores the requested summary
	// level and always returns a detailed reasoning summary. It is kept on
	// the type for backward compatibility but is no longer sent on the
	// request (TS row 0a5dd0f9c3 stopped emitting reasoning.summary).
	//
	// Deprecated: xAI ignores this value.
	ReasoningSummary string `json:"reasoningSummary,omitempty"`

	// ReasoningEffort overrides the top-level opts.Reasoning for the Responses API.
	// Supports full granularity: "none", "minimal", "low", "medium", "high", "xhigh".
	ReasoningEffort string `json:"reasoningEffort,omitempty"`

	// Logprobs enables log probability output for generated tokens.
	Logprobs *bool `json:"logprobs,omitempty"`

	// TopLogprobs is the number of most likely tokens to return per position.
	// Setting this implicitly enables Logprobs.
	TopLogprobs *int `json:"topLogprobs,omitempty"`

	// MinP is the min-p sampling threshold between 0 and 1.
	MinP *float64 `json:"minP,omitempty"`

	// MaxTurns is the maximum number of agentic tool-calling turns.
	MaxTurns *int `json:"maxTurns,omitempty"`

	// ParallelToolCalls controls whether the model may call tools in
	// parallel. Defaults to true on the API.
	ParallelToolCalls *bool `json:"parallelToolCalls,omitempty"`

	// PromptCacheKey is a cache key used to route requests with shared
	// prompt prefixes. Echoed back in ProviderMetadata["xai"]["promptCacheKey"].
	PromptCacheKey string `json:"promptCacheKey,omitempty"`

	// SafetyIdentifier is a stable identifier used to attribute policy
	// violations to an end user. Echoed back in
	// ProviderMetadata["xai"]["safetyIdentifier"].
	SafetyIdentifier string `json:"safetyIdentifier,omitempty"`

	// User is a unique identifier for the end user, used for abuse
	// monitoring.
	User string `json:"user,omitempty"`

	// Store controls whether the response is stored server-side for multi-turn use.
	// When false, reasoning.encrypted_content is automatically added to Include.
	Store *bool `json:"store,omitempty"`

	// PreviousResponseID links this request to a prior Responses API response
	// for stateful multi-turn conversations.
	PreviousResponseID string `json:"previousResponseId,omitempty"`

	// Include lists additional output fields to include (e.g., "reasoning.encrypted_content").
	Include []string `json:"include,omitempty"`

	// ServiceTier selects the processing tier: "default" or "priority".
	ServiceTier string `json:"serviceTier,omitempty"`
}

// modelsWithoutReasoningEffortRe matches grok-4.20 reasoning/non-reasoning
// models (including dated variants like grok-4.20-0309-reasoning), which
// reject the reasoning effort parameter for every value, including "none".
// Mirrors TS supports-reasoning-effort.ts.
var modelsWithoutReasoningEffortRe = regexp.MustCompile(`^grok-4\.20(-\d{4})?-(non-)?reasoning$`)

// supportsReasoningEffort reports whether modelID accepts the reasoning
// effort parameter at all.
func supportsReasoningEffort(modelID string) bool {
	return !modelsWithoutReasoningEffortRe.MatchString(modelID)
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
	warnings := xaiResponsesWarnings(opts)

	body, bodyWarnings, err := m.buildRequestBody(opts, false)
	warnings = append(warnings, bodyWarnings...)
	if err != nil {
		return nil, err
	}

	var resp responses.ResponsesAPIResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: opts.Headers,
	}, &resp)
	if err != nil {
		return nil, m.wrapErr(err)
	}

	result, err := m.convertResponse(resp, opts.Tools)
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		result.Warnings = append(warnings, result.Warnings...)
	}
	result.ResponseHeaders = providerutils.ExtractHeaders(httpResp.Headers)
	// TS result.request.body: the request as sent.
	result.RawRequest = body
	// TS response.body is the raw parsed JSON, not the schema-filtered value.
	if len(httpResp.Body) > 0 {
		result.RawResponse = providerutils.RawJSONBody(httpResp.Body)
	}
	result.ResponseMetadata = providerutils.BuildResponseMetadata(resp.ID, resp.Model, resp.CreatedAt)
	result.ResponseMetadata.Headers = result.ResponseHeaders
	return result, nil
}

// DoStream performs streaming generation via POST /v1/responses with stream=true.
func (m *ResponsesLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	warnings := xaiResponsesWarnings(opts)
	body, bodyWarnings, err := m.buildRequestBody(opts, true)
	warnings = append(warnings, bodyWarnings...)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.wrapErr(err)
	}

	stream := newXAIResponsesStream(httpResp.Body, opts.IncludeRawChunks)
	stream.responseHeaders = httpResp.Header
	stream.requestBody = body
	stream.toolNames = resolveProviderToolNames(opts.Tools)
	return streaming.NewWarningsStream(stream, warnings), nil
}

func xaiResponsesWarnings(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts == nil {
		return warnings
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "stopSequences",
		})
	}
	// Row dc2f851: xAI Responses does not support frequencyPenalty/
	// presencePenalty (topK IS supported and forwarded as top_k, see
	// buildRequestBody).
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	return warnings
}

// buildRequestBody constructs the Responses API request body.
func (m *ResponsesLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning

	// Extract XAI-specific provider options.
	var xaiOpts XAIResponsesProviderOptions
	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["xai"]; ok {
			if jsonData, err := json.Marshal(raw); err == nil {
				json.Unmarshal(jsonData, &xaiOpts) //nolint:errcheck
			}
		}
	}

	// TS convertToXaiResponsesInput: a converter of its own (xAI item ids,
	// reasoning replay, verbatim tool errors, no inline non-image files).
	input, inputWarnings, err := responses.ConvertPromptToXAIInput(opts.Prompt)
	if err != nil {
		return nil, warnings, err
	}
	warnings = append(warnings, inputWarnings...)

	body := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}
	if stream {
		// TS sends stream only when streaming.
		body["stream"] = true
	}

	// Reasoning effort: provider option takes precedence over top-level
	// opts.Reasoning. Effort map mirrors TS: {minimal:'low', low:'low',
	// medium:'medium', high:'high', xhigh: grok-4.6 ? 'xhigh' : 'high'}.
	// Row 8e006de: grok-4.20-(non-)reasoning models reject the reasoning
	// effort parameter entirely (including "none"); reasoning:"none" maps to
	// effort "none" (not omitted) on models that do support it.
	effort := xaiOpts.ReasoningEffort
	// TS gates this whole branch on isCustomReasoning(reasoning), which
	// excludes the "provider-default" sentinel (types.ReasoningDefault):
	// requesting the provider's own default reasoning is never "custom"
	// reasoning and must never trigger the unsupported-model warning below.
	if effort == "" && opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		if !supportsReasoningEffort(m.modelID) {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning %q is not supported by this model.", string(*opts.Reasoning)),
			})
		} else {
			if *opts.Reasoning == types.ReasoningNone {
				effort = "none"
			} else {
				xhigh := "high"
				if m.modelID == "grok-4.6" {
					xhigh = "xhigh"
				}
				effort, _ = providerutils.MapReasoningToProviderEffort(*opts.Reasoning, map[types.ReasoningLevel]string{
					types.ReasoningMinimal: "low", types.ReasoningLow: "low",
					types.ReasoningMedium: "medium", types.ReasoningHigh: "high",
					types.ReasoningXHigh: xhigh, types.ReasoningMax: xhigh,
				}, &warnings)
			}
		}
	}

	// Row 0a5dd0f9c3: xAI ignores the requested reasoning summary level and
	// always returns a detailed summary, so reasoning.summary is no longer
	// sent (xaiOpts.ReasoningSummary is deprecated and read nowhere here).
	if effort != "" {
		body["reasoning"] = map[string]interface{}{"effort": effort}
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
	// topK is supported by xAI Responses (forwarded as top_k), unlike
	// frequencyPenalty/presencePenalty/stopSequences which are unsupported.
	if opts.TopK != nil {
		body["top_k"] = *opts.TopK
	}
	if xaiOpts.MinP != nil {
		body["min_p"] = *xaiOpts.MinP
	}
	if xaiOpts.MaxTurns != nil {
		body["max_turns"] = *xaiOpts.MaxTurns
	}
	if xaiOpts.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *xaiOpts.ParallelToolCalls
	}
	if xaiOpts.PromptCacheKey != "" {
		body["prompt_cache_key"] = xaiOpts.PromptCacheKey
	}
	if xaiOpts.SafetyIdentifier != "" {
		body["safety_identifier"] = xaiOpts.SafetyIdentifier
	}
	if xaiOpts.User != "" {
		body["user"] = xaiOpts.User
	}

	// Row 484293f: serviceTier ("default" or "priority"), surfaced back in
	// providerMetadata.xai.serviceTier from the response (see convertResponse).
	if xaiOpts.ServiceTier != "" {
		body["service_tier"] = xaiOpts.ServiceTier
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
		if err := validateXAIResponsesToolArgs(opts.Tools); err != nil {
			return nil, nil, err
		}
		preparedTools, toolWarnings := prepareXAIResponsesToolsWithWarnings(opts.Tools)
		warnings = append(warnings, toolWarnings...)
		body["tools"] = preparedTools
		if opts.ToolChoice.Type != "" {
			choice, choiceWarnings := resolveXAIResponsesToolChoice(opts.ToolChoice, opts.Tools)
			warnings = append(warnings, choiceWarnings...)
			if choice != nil {
				body["tool_choice"] = choice
			}
		}
	}

	return body, warnings, nil
}

func validateXAIResponsesFileParts(prompt types.Prompt) error {
	for _, msg := range prompt.Messages {
		if msg.Role != types.RoleUser {
			continue
		}
		for _, part := range msg.Content {
			file, ok := part.(types.FileContent)
			if !ok {
				continue
			}
			mediaType := xaiFileMediaType(file)
			fileData := file.FileData
			switch {
			case fileData.Type == types.FileDataTypeText || file.Text != "":
				return fmt.Errorf("xAI Responses API does not support text file parts")
			case fileData.Type == types.FileDataTypeData && !isXAIImageMediaType(mediaType):
				return fmt.Errorf("xAI Responses API does not support file part media type %s as inline data; use a URL or Files API reference for non-image files", mediaType)
			case fileData.Type == "" && len(file.Data) > 0 && !isXAIImageMediaType(mediaType):
				return fmt.Errorf("xAI Responses API does not support file part media type %s as inline data; use a URL or Files API reference for non-image files", mediaType)
			}
		}
	}
	return nil
}

func xaiFileMediaType(file types.FileContent) string {
	if file.FileData.MediaType != "" {
		return file.FileData.MediaType
	}
	if file.MediaType != "" {
		return file.MediaType
	}
	return file.MimeType
}

func isXAIImageMediaType(mediaType string) bool {
	return strings.HasPrefix(mediaType, "image/") || mediaType == "image"
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
	if resp.Usage == nil {
		// TS doGenerate: a response without usage reports explicit zeros.
		zero := int64(0)
		z := func() *int64 { v := zero; return &v }
		result.Usage = types.Usage{
			InputTokens:   z(),
			OutputTokens:  z(),
			TotalTokens:   z(),
			InputDetails:  &types.InputTokenDetails{NoCacheTokens: z(), CacheReadTokens: z(), CacheWriteTokens: z()},
			OutputDetails: &types.OutputTokenDetails{TextTokens: z(), ReasoningTokens: z()},
		}
	}
	if resp.Usage != nil && resp.Usage.CostInUsdTicks != nil {
		result.ProviderMetadata = map[string]interface{}{
			"xai": map[string]interface{}{
				"costInUsdTicks": *resp.Usage.CostInUsdTicks,
			},
		}
	}
	if resp.Usage != nil && (resp.Usage.InputTokensCost != nil || resp.Usage.OutputTokensCost != nil) {
		if result.ProviderMetadata == nil {
			result.ProviderMetadata = map[string]interface{}{}
		}
		xaiMeta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
		if xaiMeta == nil {
			xaiMeta = map[string]interface{}{}
			result.ProviderMetadata["xai"] = xaiMeta
		}
		cost := map[string]interface{}{}
		if resp.Usage.InputTokensCost != nil {
			cost["inputTokensCost"] = *resp.Usage.InputTokensCost
		}
		if resp.Usage.OutputTokensCost != nil {
			cost["outputTokensCost"] = *resp.Usage.OutputTokensCost
		}
		xaiMeta["cost"] = cost
	}
	if resp.ServiceTier != "" {
		if result.ProviderMetadata == nil {
			result.ProviderMetadata = map[string]interface{}{}
		}
		xaiMeta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
		if xaiMeta == nil {
			xaiMeta = map[string]interface{}{}
			result.ProviderMetadata["xai"] = xaiMeta
		}
		xaiMeta["serviceTier"] = resp.ServiceTier
	}
	if resp.PromptCacheKey != "" || resp.SafetyIdentifier != "" {
		if result.ProviderMetadata == nil {
			result.ProviderMetadata = map[string]interface{}{}
		}
		xaiMeta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
		if xaiMeta == nil {
			xaiMeta = map[string]interface{}{}
			result.ProviderMetadata["xai"] = xaiMeta
		}
		if resp.PromptCacheKey != "" {
			xaiMeta["promptCacheKey"] = resp.PromptCacheKey
		}
		if resp.SafetyIdentifier != "" {
			xaiMeta["safetyIdentifier"] = resp.SafetyIdentifier
		}
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
			// TS emits, per content part, its text and then its url_citation
			// sources.
			for _, part := range item.Content {
				if part.Text != "" {
					result.Text += part.Text
					result.Content = append(result.Content, types.TextContent{Text: part.Text})
				}
				for _, ann := range part.Annotations {
					if ann.Type == "url_citation" && ann.URL != "" {
						title := ann.Title
						if title == "" {
							title = ann.URL
						}
						result.Content = append(result.Content, types.SourceContent{
							SourceType: "url",
							ID:         streaming.GenerateID(),
							URL:        ann.URL,
							Title:      title,
						})
					}
				}
			}

		case "function_call":
			var item responses.FunctionCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			toolCalls = append(toolCalls, types.ToolCall{
				ID:           item.CallID,
				ToolName:     item.Name,
				Arguments:    args,
				RawArguments: item.Arguments,
			})
			hasFunctionCall = true

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
			// TS: a reasoning part is emitted only when it has text or
			// encrypted content.
			if reasoningText != "" || item.EncryptedContent != "" {
				result.Content = append(result.Content, rc)
			}

		case "web_search_call", "x_search_call", "code_interpreter_call",
			"code_execution_call", "view_image_call", "view_x_video_call",
			"custom_tool_call", "mcp_call":
			var item struct {
				ID        string          `json:"id"`
				Name      string          `json:"name,omitempty"`
				Action    json.RawMessage `json:"action,omitempty"`
				Arguments string          `json:"arguments,omitempty"`
				Input     string          `json:"input,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := bareXAIToolName(peek.Type, item.Name, toolNames)
			// TS: the call's input is the item's arguments (input for
			// custom_tool_call), verbatim.
			toolInput := item.Arguments
			if peek.Type == "custom_tool_call" {
				toolInput = item.Input
			}
			var toolArgs map[string]interface{}
			_ = json.Unmarshal([]byte(toolInput), &toolArgs)
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         toolName,
				Arguments:        toolArgs,
				RawArguments:     toolInput,
				ProviderExecuted: true,
			})
			// Row 6843788: preserve the web_search action (query/queries/
			// open_page/find_in_page + sources) as a tool-result.
			if peek.Type == "web_search_call" {
				result.Content = append(result.Content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   toolName,
					Result:     mapWebSearchAction(item.Action),
				})
			}

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
				toolName = "file_search"
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

		case "image_generation_call":
			// Row fa2c2bb: server-side image generation tool.
			var item struct {
				ID     string  `json:"id"`
				Status string  `json:"status"`
				Prompt *string `json:"prompt,omitempty"`
				Result *string `json:"result,omitempty"`
			}
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := toolNames["xai.image_generation"]
			if toolName == "" {
				toolName = "image_generation"
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         toolName,
				RawArguments:     "{}",
				ProviderExecuted: true,
			})
			if item.Result != nil {
				resultValue := map[string]interface{}{"result": *item.Result}
				if item.Prompt != nil {
					resultValue["prompt"] = *item.Prompt
				}
				result.Content = append(result.Content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   toolName,
					Result:     resultValue,
				})
			} else {
				result.Content = append(result.Content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   toolName,
					Error:      fmt.Sprintf("Image generation failed (status: %s).", item.Status),
				})
			}
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
		// TS doGenerate maps response.status alone: an "incomplete" status is
		// "other" here (incomplete_details only drives the streaming path).
		result.FinishReason = mapXAIResponsesFinishReason(resp.Status, nil)
	}
	result.RawFinishReason = resp.Status

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
		"xai.image_generation": true,
	}
	names := make(map[string]string)
	for _, t := range tools {
		// TS-shaped tools are identified by ProviderID and carry their own
		// SDK name; Go-style tools use the canonical id as Name.
		if knownIDs[t.ProviderID] {
			names[t.ProviderID] = t.Name
		} else if knownIDs[t.Name] {
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
// mapWebSearchAction converts a raw web_search_call "action" object into the
// SDK's web-search tool-result shape (row 6843788), mirroring TS
// mapWebSearchAction: {action:{type,query?,queries?}|{type,url}|
// {type,url,pattern}, sources?:[{type:"url",url}]}.
func mapWebSearchAction(raw json.RawMessage) map[string]interface{} {
	result := map[string]interface{}{}
	if len(raw) == 0 {
		return result
	}
	var action struct {
		Type    string        `json:"type"`
		Query   *string       `json:"query,omitempty"`
		Queries []string      `json:"queries,omitempty"`
		URL     *string       `json:"url,omitempty"`
		Pattern *string       `json:"pattern,omitempty"`
		Sources []interface{} `json:"sources,omitempty"`
	}
	if err := json.Unmarshal(raw, &action); err != nil {
		return result
	}

	var sources []map[string]interface{}
	for _, s := range action.Sources {
		sm, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		if sm["type"] != "url" {
			continue
		}
		url, _ := sm["url"].(string)
		if url == "" {
			continue
		}
		sources = append(sources, map[string]interface{}{"type": "url", "url": url})
	}
	if len(sources) > 0 {
		result["sources"] = sources
	}

	switch action.Type {
	case "search":
		a := map[string]interface{}{"type": "search"}
		if action.Query != nil {
			a["query"] = *action.Query
		}
		if action.Queries != nil {
			a["queries"] = action.Queries
		}
		result["action"] = a
	case "open_page":
		url := ""
		if action.URL != nil {
			url = *action.URL
		}
		result["action"] = map[string]interface{}{"type": "openPage", "url": url}
	case "find_in_page":
		url := ""
		if action.URL != nil {
			url = *action.URL
		}
		pattern := ""
		if action.Pattern != nil {
			pattern = *action.Pattern
		}
		result["action"] = map[string]interface{}{"type": "findInPage", "url": url, "pattern": pattern}
	}
	return result
}

func mapXAIResponsesFinishReason(status string, details *responses.IncompleteDetails) types.FinishReason {
	// An incomplete response is mapped from its incomplete_details reason;
	// with no reason it is "other" (TS: reason ? map(reason) : 'other').
	if status == "incomplete" {
		if details == nil || details.Reason == "" {
			return types.FinishReasonOther
		}
		status = details.Reason
	}
	// Mirrors TS mapXaiResponsesFinishReason.
	switch status {
	case "stop", "completed":
		return types.FinishReasonStop
	case "length", "max_output_tokens":
		return types.FinishReasonLength
	case "tool_calls", "function_call":
		return types.FinishReasonToolCalls
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonOther
	}
}

// convertXAIResponsesUsage converts Responses API usage to types.Usage.
func convertXAIResponsesUsage(u *responses.ResponsesAPIUsage) types.Usage {
	if u == nil {
		return types.Usage{}
	}
	// TS convertXaiResponsesUsage: the breakdown is always reported, and a
	// cached count above input_tokens means input_tokens excludes the cache.
	var cached, reasoning int64
	if u.InputTokensDetails != nil {
		cached = int64(u.InputTokensDetails.CachedTokens)
	}
	if u.OutputTokensDetails != nil {
		reasoning = int64(u.OutputTokensDetails.ReasoningTokens)
	}
	inputTokens := int64(u.InputTokens)
	outputTokens := int64(u.OutputTokens)
	noCache := inputTokens - cached
	if cached > inputTokens {
		noCache = inputTokens
		inputTokens += cached
	}
	total := inputTokens + outputTokens
	textOut := outputTokens - reasoning

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &total,
		InputDetails: &types.InputTokenDetails{
			CacheReadTokens: &cached,
			NoCacheTokens:   &noCache,
		},
		OutputDetails: &types.OutputTokenDetails{
			ReasoningTokens: &reasoning,
			TextTokens:      &textOut,
		},
	}

	// Row 41e7760: keep the full raw usage object (not just the hand-picked
	// fields above), so xAI-specific keys like total_tokens,
	// num_sources_used, and num_server_side_tools_used are preserved.
	if u.Raw != nil {
		result.Raw = u.Raw
	} else {
		result.Raw = map[string]interface{}{
			"input_tokens":  u.InputTokens,
			"output_tokens": u.OutputTokens,
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
	responseHeaders http.Header
	reader          io.ReadCloser
	parser          *streaming.SSEParser
	err             error
	toolAccum       map[int]*xaiResponsesToolAccum
	itemTypes       map[int]string
	flushQueue      []*provider.StreamChunk
	// activeReasoning tracks item IDs for which a reasoning-start chunk has been emitted.
	// Used to emit the matching reasoning-end in output_item.done, and to avoid
	// double-emitting reasoning-start for encrypted reasoning (no summary events sent).
	activeReasoning         map[string]struct{}
	includeRawChunks        bool
	responseMetadataEmitted bool
	// hasFunctionCall is true only once a function_call output item has
	// completed (response.output_item.done). Mirrors TS
	// xai-responses-language-model.ts's hasFunctionCall, which is set on
	// response.output_item.done (not .added) and used to force the unified
	// finish reason to 'tool-calls' regardless of response.status.
	hasFunctionCall bool

	// toolNames maps canonical provider tool ids to the SDK names the caller
	// registered; seenToolCalls dedupes the added/done events of one item.
	toolNames     map[string]string
	seenToolCalls map[string]bool

	// textBlocks lists the open text block ids in start order. TS keeps them
	// in contentBlocks and ends them all in flush(), just before `finish`.
	textBlocks []string
	textSeen   map[string]bool
	// finish state recorded by response.completed / .done / .incomplete /
	// .failed; TS only enqueues `finish` from flush() at the end of the
	// stream, with whatever was recorded last (default: other, zero usage).
	finishReason    types.FinishReason
	rawFinishReason string
	finishUsage     *types.Usage
	finishMeta      json.RawMessage

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *xaiResponsesStream) RequestBody() interface{} { return s.requestBody }

func newXAIResponsesStream(r io.ReadCloser, includeRawChunks ...bool) *xaiResponsesStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &xaiResponsesStream{
		reader:           r,
		parser:           streaming.NewSSEParser(r),
		toolAccum:        make(map[int]*xaiResponsesToolAccum),
		itemTypes:        make(map[int]string),
		activeReasoning:  make(map[string]struct{}),
		includeRawChunks: emitRaw,
		textSeen:         make(map[string]bool),
		finishReason:     types.FinishReasonOther,
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

// emitParsedChunk decides how a freshly built chunk should be delivered:
// returned immediately (chunk, true) if nothing is queued ahead of it, or
// spliced into the pending flushQueue behind its first entry (nil, false)
// for the caller's Next() loop to continue draining. A nil chunk likewise
// means "nothing to deliver, continue the loop".
//
// This used to recurse by calling s.Next() directly in both of those cases.
// Go does not eliminate that tail call, and Next()'s own loop can call this
// method on almost every SSE event, so a long run of skip-worthy events
// could grow the goroutine stack without bound. Returning a "continue"
// signal instead lets Next()'s single top-level loop (and
// handleOutputItemDone's, which shares this same signal shape) do the
// looping without adding stack frames.
func (s *xaiResponsesStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, bool) {
	if chunk == nil {
		return nil, false
	}
	if len(s.flushQueue) == 0 {
		return chunk, true
	}
	queue := make([]*provider.StreamChunk, 0, len(s.flushQueue)+1)
	queue = append(queue, s.flushQueue[0], chunk)
	queue = append(queue, s.flushQueue[1:]...)
	s.flushQueue = queue
	return nil, false
}

// Next implements provider.TextStream.
func (s *xaiResponsesStream) Next() (*provider.StreamChunk, error) {
	for {
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
			if err == io.EOF {
				s.queueFlush()
				continue
			}
			s.err = err
			return nil, err
		}

		if streaming.IsStreamDone(event) {
			s.queueFlush()
			continue
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

		var peek responses.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
			msg, perr := providerutils.ChunkParseFailure(event.Data, err)
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: msg,
				Err:  perr,
			})
			if ok {
				return chunk, nil
			}
			continue
		}

		// TS xaiResponsesChunkSchema is a closed union: an event type it does
		// not list, or a listed one whose members do not match, fails the
		// parse and becomes an error part (the stream keeps going).
		if msg, perr := providerutils.CheckXAIResponsesChunk(event.Data); perr != nil {
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: msg,
				Err:  perr,
			})
			if ok {
				return chunk, nil
			}
			continue
		}

		switch peek.Type {
		case "response.created", "response.in_progress":
			var e responses.ResponseCreatedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if s.responseMetadataEmitted {
				continue
			}
			s.responseMetadataEmitted = true
			metadata := &provider.ResponseMetadata{
				ID:      e.Response.ID,
				ModelID: e.Response.Model,
				Headers: providerutils.ExtractHeaders(s.responseHeaders),
			}
			if e.Response.CreatedAt != 0 {
				metadata.Timestamp = time.Unix(e.Response.CreatedAt, 0)
			}
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: metadata,
			})
			if ok {
				return chunk, nil
			}
			continue

		case "response.output_item.added":
			var e responses.OutputItemAddedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.itemTypes[e.OutputIndex] = e.Item.Type
			if e.Item.Type == "message" {
				var added struct {
					Item json.RawMessage `json:"item"`
				}
				if json.Unmarshal([]byte(event.Data), &added) == nil {
					s.queueMessageItem(added.Item)
				}
			}
			if e.Item.Type == "function_call" {
				s.toolAccum[e.OutputIndex] = &xaiResponsesToolAccum{
					id:   e.Item.CallID,
					name: e.Item.Name,
				}
				// TS streams the call's input: tool-input-start now,
				// deltas as arguments arrive, tool-input-end at done.
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:     provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{ID: e.Item.CallID, ToolName: e.Item.Name},
				})
			}
			if isXAIProviderExecutedItem(e.Item.Type) {
				var added struct {
					Item json.RawMessage `json:"item"`
				}
				if json.Unmarshal([]byte(event.Data), &added) == nil {
					s.queueProviderExecutedItem(e.Item.Type, added.Item, false)
				}
			}
			continue

		case "response.output_text.delta":
			var e struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			// TS: text-start once per `text-<item_id>` block, then a
			// text-delta for every event (an empty delta included).
			blockID := "text-" + e.ItemID
			s.startTextBlock(blockID)
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeText,
				ID:   blockID,
				Text: e.Delta,
			})
			continue

		case "response.function_call_arguments.delta":
			var e responses.FunctionCallArgumentsDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if accum, ok := s.toolAccum[e.OutputIndex]; ok {
				accum.arguments += e.Delta
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   accum.id,
					Text: e.Delta,
				})
			}
			continue

		// reasoning_summary_part.added fires when a reasoning block starts.
		// Emit reasoning-start with providerMetadata so consumers know the item ID.
		case "response.reasoning_summary_part.added":
			var e responses.ReasoningSummaryPartAddedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			blockID := "reasoning-" + e.ItemID
			if _, alreadyStarted := s.activeReasoning[e.ItemID]; alreadyStarted {
				continue
			}
			s.activeReasoning[e.ItemID] = struct{}{}
			meta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               blockID,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

		case "response.reasoning_summary_text.delta":
			var e responses.ReasoningSummaryTextDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if e.Delta == "" {
				continue
			}
			blockID := "reasoning-" + e.ItemID
			meta, _ := json.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeReasoning,
				ID:               blockID,
				Reasoning:        e.Delta,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

		// Gap 6: raw reasoning text (not summary) — emitted by some Grok models.
		// Also emits reasoning-start on first delta if not already started.
		case "response.reasoning_text.delta":
			var e struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if e.Delta == "" {
				continue
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
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type:             provider.ChunkTypeReasoningStart,
					ID:               blockID,
					ProviderMetadata: meta,
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeReasoning,
				ID:               blockID,
				Reasoning:        e.Delta,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

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
				continue
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
			continue

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
				continue
			}
			if e.Annotation.Type == "url_citation" && e.Annotation.URL != "" {
				title := e.Annotation.Title
				if title == "" {
					title = e.Annotation.URL
				}
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type: provider.ChunkTypeSource,
					SourceContent: &types.SourceContent{
						SourceType: "url",
						URL:        e.Annotation.URL,
						Title:      title,
					},
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		case "response.output_item.done":
			var e responses.OutputItemDoneEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			chunk, ok := s.handleOutputItemDone(e)
			if ok {
				return chunk, nil
			}
			continue

		// Gap 4: response.done is an alias for response.completed. TS only
		// records the usage, finish reason and metadata here; `finish` is
		// enqueued by flush() at the end of the stream.
		case "response.completed", "response.done":
			var e responses.ResponseCompletedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.recordUsage(e.Response.Usage)
			// Mirrors TS: the unified finish reason is forced to
			// 'tool-calls' when a function_call item completed during the
			// stream, regardless of what response.status reports.
			if e.Response.Status != "" {
				if s.hasFunctionCall {
					s.finishReason = types.FinishReasonToolCalls
				} else {
					s.finishReason = mapXAIResponsesFinishReason(e.Response.Status, e.Response.IncompleteDetails)
				}
				s.rawFinishReason = e.Response.Status
			}
			s.recordMetadata(e.Response.Usage, e.Response.ServiceTier, e.Response.PromptCacheKey, e.Response.SafetyIdentifier)
			continue

		case "response.incomplete":
			var e struct {
				Type     string `json:"type"`
				Response struct {
					ID                string                       `json:"id,omitempty"`
					Usage             *responses.ResponsesAPIUsage `json:"usage,omitempty"`
					IncompleteDetails *responses.IncompleteDetails `json:"incomplete_details,omitempty"`
					ServiceTier       string                       `json:"service_tier,omitempty"`
					PromptCacheKey    string                       `json:"prompt_cache_key,omitempty"`
					SafetyIdentifier  string                       `json:"safety_identifier,omitempty"`
				} `json:"response"`
			}
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.recordUsage(e.Response.Usage)
			s.finishReason = mapXAIResponsesFinishReason("incomplete", e.Response.IncompleteDetails)
			s.rawFinishReason = "incomplete"
			if e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason != "" {
				s.rawFinishReason = e.Response.IncompleteDetails.Reason
			}
			s.recordMetadata(e.Response.Usage, e.Response.ServiceTier, e.Response.PromptCacheKey, e.Response.SafetyIdentifier)
			continue

		case "response.failed":
			var e responses.ResponseFailedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.finishReason = types.FinishReasonError
			s.rawFinishReason = "error"
			if e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason != "" {
				s.finishReason = mapXAIResponsesFinishReason("incomplete", e.Response.IncompleteDetails)
				s.rawFinishReason = e.Response.IncompleteDetails.Reason
			}
			s.recordUsage(e.Response.Usage)
			// Mirrors TS xai-responses-language-model.ts's response.failed
			// branch: response.error != null enqueues a structured error chunk
			// (createXaiResponsesStreamError); the finish chunk follows at the
			// end of the stream.
			if e.Response.Error != nil {
				var data interface{}
				_ = json.Unmarshal([]byte(event.Data), &data)
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: formatXAIResponseError(e.Response.Error.Code, e.Response.Error.Message),
					Err:  newXAIResponsesStreamError("xai", e.Response.Error.Message, e.Response.Error.Code, peek.Type, data),
				})
			}
			continue

		case "error":
			var e responses.ResponsesStreamErrorEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			streamErr := &XAIStreamError{Code: e.Code, Message: e.Message}
			var data interface{}
			_ = json.Unmarshal([]byte(event.Data), &data)
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: streamErr.Error(),
				Err:  newXAIResponsesStreamError("xai", e.Message, e.Code, peek.Type, data),
			})
			if ok {
				return chunk, nil
			}
			continue

		default:
			// Validated above: a listed event with nothing to emit.
			continue
		}

	}
}

// startTextBlock enqueues text-start the first time a text block id is seen.
func (s *xaiResponsesStream) startTextBlock(blockID string) {
	if s.textSeen[blockID] {
		return
	}
	s.textSeen[blockID] = true
	s.textBlocks = append(s.textBlocks, blockID)
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: blockID})
}

// queueMessageItem mirrors the TS `message` output item branch: text that was
// not already streamed through output_text.delta is emitted whole, and each
// url_citation annotation becomes a source (on every added/done event, as TS).
func (s *xaiResponsesStream) queueMessageItem(raw json.RawMessage) {
	var item struct {
		ID      string `json:"id"`
		Content []struct {
			Text        string `json:"text"`
			Annotations []struct {
				Type  string `json:"type"`
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"annotations"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	blockID := "text-" + item.ID
	for _, part := range item.Content {
		if part.Text != "" && !s.textSeen[blockID] {
			s.startTextBlock(blockID)
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: blockID, Text: part.Text})
		}
		for _, ann := range part.Annotations {
			if ann.Type != "url_citation" || ann.URL == "" {
				continue
			}
			title := ann.Title
			if title == "" {
				title = ann.URL
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:          provider.ChunkTypeSource,
				SourceContent: &types.SourceContent{SourceType: "url", URL: ann.URL, Title: title},
			})
		}
	}
}

func (s *xaiResponsesStream) recordUsage(u *responses.ResponsesAPIUsage) {
	if u == nil {
		return
	}
	usage := convertXAIResponsesUsage(u)
	s.finishUsage = &usage
}

func (s *xaiResponsesStream) recordMetadata(u *responses.ResponsesAPIUsage, serviceTier, promptCacheKey, safetyIdentifier string) {
	xaiMeta := map[string]interface{}{}
	if u != nil && u.CostInUsdTicks != nil {
		xaiMeta["costInUsdTicks"] = *u.CostInUsdTicks
	}
	if u != nil && (u.InputTokensCost != nil || u.OutputTokensCost != nil) {
		cost := map[string]interface{}{}
		if u.InputTokensCost != nil {
			cost["inputTokensCost"] = *u.InputTokensCost
		}
		if u.OutputTokensCost != nil {
			cost["outputTokensCost"] = *u.OutputTokensCost
		}
		xaiMeta["cost"] = cost
	}
	if serviceTier != "" {
		xaiMeta["serviceTier"] = serviceTier
	}
	if promptCacheKey != "" {
		xaiMeta["promptCacheKey"] = promptCacheKey
	}
	if safetyIdentifier != "" {
		xaiMeta["safetyIdentifier"] = safetyIdentifier
	}
	if len(xaiMeta) == 0 {
		s.finishMeta = nil
		return
	}
	s.finishMeta, _ = json.Marshal(map[string]interface{}{"xai": xaiMeta})
}

// queueFlush is TS flush(): end every text block, then enqueue `finish` with
// the last recorded finish reason, usage and metadata (other / zero usage when
// the stream ended without a terminal response event), and end the stream.
func (s *xaiResponsesStream) queueFlush() {
	for _, id := range s.textBlocks {
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id})
	}
	usage := s.finishUsage
	if usage == nil {
		zero := convertXAIResponsesUsage(&responses.ResponsesAPIUsage{})
		usage = &zero
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.finishReason,
		RawFinishReason:  s.rawFinishReason,
		Usage:            usage,
		ProviderMetadata: s.finishMeta,
	})
	s.err = io.EOF
}

func (s *xaiResponsesStream) handleOutputItemDone(e responses.OutputItemDoneEvent) (*provider.StreamChunk, bool) {
	itemType := s.itemTypes[e.OutputIndex]
	var doneType struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(e.Item, &doneType) == nil && doneType.Type == "message" {
		delete(s.itemTypes, e.OutputIndex)
		s.queueMessageItem(e.Item)
		return nil, false
	}

	switch itemType {
	case "function_call":
		s.hasFunctionCall = true
		accum, ok := s.toolAccum[e.OutputIndex]
		if !ok {
			return nil, false
		}
		delete(s.toolAccum, e.OutputIndex)
		delete(s.itemTypes, e.OutputIndex)

		// TS uses the done item's call_id / name / arguments.
		callID, name, rawArgs := accum.id, accum.name, accum.arguments
		var doneItem struct {
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}
		if json.Unmarshal(e.Item, &doneItem) == nil {
			if doneItem.CallID != "" {
				callID = doneItem.CallID
			}
			if doneItem.Name != "" {
				name = doneItem.Name
			}
			if doneItem.Arguments != "" || rawArgs == "" {
				rawArgs = doneItem.Arguments
			}
		}
		var args map[string]interface{}
		if rawArgs != "" {
			json.Unmarshal([]byte(rawArgs), &args) //nolint:errcheck
		}
		s.flushQueue = append(s.flushQueue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: callID, ToolCall: &types.ToolCall{ID: callID, ToolName: name}},
			&provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:           callID,
					ToolName:     name,
					Arguments:    args,
					RawArguments: rawArgs,
				},
			})
		return nil, false

	case "web_search_call", "x_search_call", "code_interpreter_call",
		"code_execution_call", "view_image_call", "view_x_video_call",
		"file_search_call", "image_generation_call", "custom_tool_call", "mcp_call":
		delete(s.itemTypes, e.OutputIndex)
		s.queueProviderExecutedItem(itemType, e.Item, true)
		return nil, false

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
			return nil, false
		}
		if item.ID == "" && item.EncryptedContent == "" {
			return nil, false
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
			return s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               blockID,
				ProviderMetadata: startMeta,
			})
		}

		delete(s.activeReasoning, item.ID)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               blockID,
			ProviderMetadata: providerMeta,
		})

	default:
		delete(s.itemTypes, e.OutputIndex)
		return nil, false
	}
}

// Compile-time interface check.
var _ provider.LanguageModel = (*ResponsesLanguageModel)(nil)

func isXAIProviderExecutedItem(t string) bool {
	switch t {
	case "web_search_call", "x_search_call", "code_interpreter_call", "code_execution_call",
		"view_image_call", "view_x_video_call", "custom_tool_call", "mcp_call",
		"file_search_call", "image_generation_call":
		return true
	}
	return false
}

// queueProviderExecutedItem mirrors the TS doStream handling of
// provider-executed output items (web/x search, code execution, file search,
// image generation, custom and MCP tools): on the first sighting of an item
// it streams tool-input-start / delta / end followed by the tool-call, and on
// done it adds the tool-result. custom_tool_call is only emitted on done.
func (s *xaiResponsesStream) queueProviderExecutedItem(itemType string, raw json.RawMessage, done bool) {
	var item struct {
		ID        string          `json:"id"`
		Name      string          `json:"name,omitempty"`
		Action    json.RawMessage `json:"action,omitempty"`
		Arguments string          `json:"arguments,omitempty"`
		Input     string          `json:"input,omitempty"`
		Status    string          `json:"status,omitempty"`
		Prompt    *string         `json:"prompt,omitempty"`
		Result    *string         `json:"result,omitempty"`
		Queries   []string        `json:"queries,omitempty"`
		Results   []struct {
			FileID   string  `json:"file_id"`
			Filename string  `json:"filename"`
			Score    float64 `json:"score"`
			Text     string  `json:"text"`
		} `json:"results,omitempty"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	if s.seenToolCalls == nil {
		s.seenToolCalls = map[string]bool{}
	}
	var toolName, toolInput string
	switch itemType {
	case "file_search_call":
		toolName = s.toolNames["xai.file_search"]
		if toolName == "" {
			toolName = "file_search"
		}
	case "image_generation_call":
		toolName = s.toolNames["xai.image_generation"]
		if toolName == "" {
			toolName = "image_generation"
		}
		toolInput = "{}"
	default:
		toolName = bareXAIToolName(itemType, item.Name, s.toolNames)
		toolInput = item.Arguments
		if itemType == "custom_tool_call" {
			toolInput = item.Input
		}
	}
	emit := !s.seenToolCalls[item.ID]
	if itemType == "custom_tool_call" {
		emit = emit && done
	}
	if emit {
		s.seenToolCalls[item.ID] = true
		var args map[string]interface{}
		_ = json.Unmarshal([]byte(toolInput), &args)
		s.flushQueue = append(s.flushQueue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: item.ID, ToolName: toolName}},
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: item.ID, Text: toolInput},
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: item.ID, ToolCall: &types.ToolCall{ID: item.ID, ToolName: toolName}},
			&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
				ID: item.ID, ToolName: toolName, Arguments: args, RawArguments: toolInput, ProviderExecuted: true,
			}},
		)
	}
	if !done {
		return
	}
	result := types.ToolResult{ToolCallID: item.ID, ToolName: toolName}
	switch itemType {
	case "file_search_call":
		results := interface{}(nil)
		if item.Results != nil {
			mapped := make([]map[string]interface{}, len(item.Results))
			for i, r := range item.Results {
				mapped[i] = map[string]interface{}{"fileId": r.FileID, "filename": r.Filename, "score": r.Score, "text": r.Text}
			}
			results = mapped
		}
		queries := item.Queries
		if queries == nil {
			queries = []string{}
		}
		result.Result = map[string]interface{}{"queries": queries, "results": results}
	case "image_generation_call":
		if item.Result != nil {
			v := map[string]interface{}{"result": *item.Result}
			if item.Prompt != nil {
				v["prompt"] = *item.Prompt
			}
			result.Result = v
		} else {
			result.Error = fmt.Errorf("Image generation failed (status: %s).", item.Status) //nolint:staticcheck // matches TS SDK's exact error text
		}
	case "web_search_call":
		result.Result = mapWebSearchAction(item.Action)
	default:
		result.Result = map[string]interface{}{}
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &result})
}

// bareXAIToolName resolves the SDK name of a provider-executed item like TS:
// the registered name when the tool was declared, otherwise the bare tool
// name ("web_search", "x_search", "code_execution", the item's own name).
func bareXAIToolName(itemType, itemName string, toolNames map[string]string) string {
	webSearchSubTools := map[string]bool{"web_search": true, "web_search_with_snippets": true, "browse_page": true}
	xSearchSubTools := map[string]bool{"x_user_search": true, "x_keyword_search": true, "x_semantic_search": true, "x_thread_fetch": true}
	pick := func(id, bare string) string {
		if n := toolNames[id]; n != "" {
			return n
		}
		return bare
	}
	switch {
	case webSearchSubTools[itemName] || itemType == "web_search_call":
		return pick("xai.web_search", "web_search")
	case xSearchSubTools[itemName] || itemType == "x_search_call":
		return pick("xai.x_search", "x_search")
	case itemName == "code_execution" || itemType == "code_interpreter_call" || itemType == "code_execution_call":
		return pick("xai.code_execution", "code_execution")
	case itemType == "mcp_call":
		if n := toolNames["xai.mcp"]; n != "" {
			return n
		}
		if itemName != "" {
			return itemName
		}
		return "mcp"
	}
	return itemName
}
