package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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

	// Row 75f86f4: a 200 response with an embedded error object maps to a
	// 400 ProviderError.
	if resp.Error != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 400, resp.Error.Code, resp.Error.Message, nil)
	}
	// Row 75f86f4: a 200 response with no `output` field means the API
	// returned nothing to work with; raise a descriptive error instead of
	// silently producing an empty result.
	if resp.Output == nil {
		message := "Responses API returned no output"
		if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
			message = fmt.Sprintf("Responses API returned no output (%s)", resp.IncompleteDetails.Reason)
		}
		return nil, providererrors.NewProviderError(m.Provider(), 500, "", message, nil)
	}

	approvalFromPrompt := extractApprovalRequestIDToToolCallIDFromPrompt(opts.Prompt)
	result, err := m.convertResponse(resp, store, webSearchToolName, opts.Tools, approvalFromPrompt, m.provider.responsesProviderOptionsName())
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

	stream := newResponsesStreamWithMetadata(httpResp.Body, opts.IncludeRawChunks, webSearchToolName, m.provider.responsesProviderOptionsName(), httpResp.Header)
	stream.tools = opts.Tools
	stream.store = responsesExplicitStore(body)
	stream.approvalFromPrompt = extractApprovalRequestIDToToolCallIDFromPrompt(opts.Prompt)
	if name := toolSearchToolName(opts.Tools); name != "" {
		stream.toolSearchToolName = name
	}
	return streaming.NewWarningsStream(stream, warnings), nil
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

// responsesExplicitStore reports whether the caller explicitly set
// providerOptions.openai.store to a boolean, by inspecting the request body
// buildRequest already assembled. This is TS's raw `openaiOptions?.store`
// (openai-responses-language-model.ts line 511: `boolean | undefined`,
// truthy-checked as `if (store)`) -- distinct from buildRequest's own
// `store` return value, which defaults to true (matching TS's separate
// `openaiOptions?.store ?? true` at line 399/386, used for the request
// body's "store" field and for prompt-conversion item_reference decisions).
// buildRequest only ever writes a bool into body["store"] when the option
// was explicitly set (storeExplicit); it is otherwise left unset (or set to
// nil for an explicit `store: null`), so a failed type assertion here
// correctly yields false for every case TS's `if (store)` also treats as
// falsy: unset, or explicit null.
func responsesExplicitStore(body map[string]interface{}) bool {
	v, _ := body["store"].(bool)
	return v
}

func (m *ResponsesLanguageModel) buildRequest(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, bool, []types.Warning, error) {
	// Extract provider options early (store needed before input conversion).
	store := true
	storeExplicit := false
	conversation := ""
	previousResponseID := ""
	promptCacheRetention := ""
	var promptCacheOptions interface{}
	promptCacheKey := ""
	reasoningEffort := ""
	reasoningEffortUpdate := ""
	reasoningMode := ""
	reasoningContext := ""
	reasoningSummary := ""
	reasoningSummarySet := false
	strictJSONSchema := true
	textVerbosity := ""
	serviceTier := ""
	user := ""
	instructions := ""
	safetyIdentifier := ""
	passThroughUnsupportedFiles := false
	forceReasoning := (*bool)(nil)
	maxToolCalls := (*int)(nil)
	parallelToolCalls := (*bool)(nil)
	truncation := ""
	includeFields := []string(nil)
	includeExplicit := false
	var metadata interface{}
	var topLogprobs interface{}
	var contextManagement []map[string]interface{}
	contextManagementExplicit := false
	var allowedToolNames []string
	var allowedToolsMode string
	providerOptionsName := m.provider.responsesProviderOptionsName()
	var openaiOpts map[string]interface{}

	if opts.ProviderOptions != nil {
		var ok bool
		openaiOpts, ok = opts.ProviderOptions[providerOptionsName].(map[string]interface{})
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
			if v, ok := openaiOpts["promptCacheOptions"]; ok {
				promptCacheOptions = v
			}
			if v, ok := openaiOpts["promptCacheKey"].(string); ok {
				promptCacheKey = v
			}
			if v, ok := openaiOpts["reasoningEffort"].(string); ok {
				reasoningEffort = v
			}
			if v, ok := openaiOpts["reasoningEffortUpdate"].(string); ok {
				reasoningEffortUpdate = v
			}
			if v, ok := openaiOpts["reasoningMode"].(string); ok {
				reasoningMode = v
			}
			if v, ok := openaiOpts["reasoningContext"].(string); ok {
				reasoningContext = v
			}
			if v, ok := openaiOpts["reasoningSummary"]; ok {
				reasoningSummarySet = true
				if summary, ok := v.(string); ok {
					reasoningSummary = summary
				}
			}
			if v, ok := openaiOpts["strictJsonSchema"].(bool); ok {
				strictJSONSchema = v
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
			if v, ok := openaiOpts["instructions"].(string); ok {
				instructions = v
			}
			if v, ok := openaiOpts["safetyIdentifier"].(string); ok {
				safetyIdentifier = v
			}
			if v, ok := openaiOpts["passThroughUnsupportedFiles"].(bool); ok {
				passThroughUnsupportedFiles = v
			}
			if v, ok := intFromInterface(openaiOpts["maxToolCalls"]); ok {
				maxToolCalls = &v
			}
			if v, ok := openaiOpts["parallelToolCalls"].(bool); ok {
				parallelToolCalls = &v
			}
			if v, ok := openaiOpts["forceReasoning"].(bool); ok {
				forceReasoning = &v
			}
			if v, ok := openaiOpts["truncation"].(string); ok {
				truncation = v
			}
			if _, ok := openaiOpts["include"]; ok {
				includeExplicit = true
			}
			if v := stringSliceFromInterface(openaiOpts["include"]); includeExplicit {
				includeFields = v
			}
			metadata = openaiOpts["metadata"]
			topLogprobs, _ = responsesTopLogprobs(openaiOpts["logprobs"])
			if _, ok := openaiOpts["contextManagement"]; ok && openaiOpts["contextManagement"] != nil {
				contextManagementExplicit = true
			}
			if contextManagementExplicit {
				contextManagement = responsesContextManagement(openaiOpts["contextManagement"])
			}
			if raw, ok := openaiOpts["allowedTools"].(map[string]interface{}); ok {
				allowedToolNames = stringSliceFromInterface(raw["toolNames"])
				allowedToolsMode, _ = raw["mode"].(string)
			}
		}
	}

	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.StopSequences != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "stopSequences"})
	}
	if conversation != "" && previousResponseID != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "conversation",
			Details: "conversation and previousResponseId cannot be used together",
		})
	}

	modelCapabilities := GetLanguageModelCapabilities(m.modelID)

	isReasoning := isReasoningModel(m.modelID)
	if forceReasoning != nil {
		isReasoning = *forceReasoning
	}

	// GPT-6+ models restrict reasoning effort to a fixed set; drop and warn
	// on anything else (row 17e489e).
	if reasoningEffort != "" && modelCapabilities.SupportedReasoningEfforts != nil &&
		!slices.Contains(modelCapabilities.SupportedReasoningEfforts, reasoningEffort) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "reasoningEffort",
			Details: fmt.Sprintf("%s only supports the following reasoning efforts: %s", m.modelID, strings.Join(modelCapabilities.SupportedReasoningEfforts, ", ")),
		})
		reasoningEffort = ""
	}

	// Determine system message mode based on model type.
	systemMsgMode := "system"
	if isReasoning {
		systemMsgMode = "developer"
	}
	if v, ok := openaiOpts["systemMessageMode"].(string); ok && v != "" {
		systemMsgMode = v
	}

	// Whether to include "web_search_call.action.sources" when a web_search
	// tool is present. Ports TS openai-responses-language-model.ts:500-503:
	// `config.supportsWebSearchSourcesInclude !== false &&
	// openaiOptions?.includeWebSearchSources !== false` — an AND of two
	// "not explicitly false" checks, so EITHER one being explicitly false
	// disables the include and cannot be overridden back on by the other.
	// In particular a backend that sets Config.SupportsWebSearchSourcesInclude
	// = false (e.g. Amazon Bedrock Mantle, which rejects this include value)
	// cannot have it re-enabled by a per-call providerOptions.openai.
	// includeWebSearchSources = true.
	includeWebSearchSources := true
	if m.provider.config.SupportsWebSearchSourcesInclude != nil && !*m.provider.config.SupportsWebSearchSourcesInclude {
		includeWebSearchSources = false
	}
	if v, ok := openaiOpts["includeWebSearchSources"].(bool); ok && !v {
		includeWebSearchSources = false
	}

	// Convert prompt to Responses API input format.
	input, inputWarnings, err := responses.ConvertPromptToInputWithOptions(opts.Prompt, systemMsgMode, responses.ConvertOptions{
		PassThroughUnsupportedFiles: passThroughUnsupportedFiles,
		HasPreviousResponseID:       previousResponseID != "",
		HasConversation:             conversation != "",
		Store:                       store,
		CustomToolNames:             customToolNames(opts.Tools),
		HasLocalShellTool:           hasTool(opts.Tools, "openai.local_shell"),
		HasShellTool:                hasTool(opts.Tools, "openai.shell"),
		HasApplyPatchTool:           hasTool(opts.Tools, "openai.apply_patch"),
		HasComputerTool:             hasTool(opts.Tools, "openai.computer"),
		FileIDPrefixes:              m.provider.responsesFileIDPrefixes(),
		ProviderOptionsName:         providerOptionsName,
		ToolSearchToolName:          toolSearchToolName(opts.Tools),
		OutputSchemaToolNames:       outputSchemaToolNames(opts.Tools),
		ExplicitMessageItemType:     m.provider.explicitMessageItemType(),
	})
	if err != nil {
		return nil, store, warnings, err
	}
	warnings = append(warnings, inputWarnings...)

	// reasoningEffortUpdate (GPT-6+): prepend a configuration_update item so
	// the model's reasoning effort can change mid-conversation without a new
	// response chain. Requires standard reasoning mode (no auto-compaction,
	// no auto-truncation).
	if reasoningEffortUpdate != "" {
		configurationUpdateSupported := modelCapabilities.SupportsConfigurationUpdate &&
			reasoningMode != "pro" && !contextManagementExplicit && truncation != "auto"
		if !configurationUpdateSupported {
			details := "reasoningEffortUpdate requires standard reasoning mode without automatic compaction or automatic truncation"
			if !modelCapabilities.SupportsConfigurationUpdate {
				details = "reasoningEffortUpdate is only supported by GPT-6 and later models"
			}
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningEffortUpdate",
				Details: details,
			})
		} else {
			input = append([]interface{}{map[string]interface{}{
				"type":      "configuration_update",
				"reasoning": map[string]interface{}{"effort": reasoningEffortUpdate},
			}}, input...)
		}
	}

	// compactionTrigger: append a compaction_trigger item to the end of input.
	if v, ok := openaiOpts["compactionTrigger"].(bool); ok && v {
		input = append(input, map[string]interface{}{"type": "compaction_trigger"})
	}

	body := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}
	if stream {
		body["stream"] = true
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
	resolvedReasoningSummary := reasoningSummary
	if !reasoningSummarySet && resolvedReasoningSummary == "" && effort != "" && effort != "none" {
		resolvedReasoningSummary = "detailed"
	}
	if isReasoning && (effort != "" || resolvedReasoningSummary != "" || reasoningMode != "" || reasoningContext != "") {
		reasoning := map[string]interface{}{}
		if effort != "" {
			reasoning["effort"] = effort
		}
		if resolvedReasoningSummary != "" {
			reasoning["summary"] = resolvedReasoningSummary
		}
		// Row b2b1bb9 (Responses half): GPT-5.6 reasoningMode ("standard"/
		// "pro") and reasoningContext ("auto"/"current_turn"/"all_turns").
		if reasoningMode != "" {
			reasoning["mode"] = reasoningMode
		}
		if reasoningContext != "" {
			reasoning["context"] = reasoningContext
		}
		body["reasoning"] = reasoning
	} else if !isReasoning {
		if reasoningEffort != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningEffort",
				Details: "reasoningEffort is not supported for non-reasoning models",
			})
		}
		if reasoningSummary != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningSummary",
				Details: "reasoningSummary is not supported for non-reasoning models",
			})
		}
		if reasoningMode != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningMode",
				Details: "reasoningMode is not supported for non-reasoning models",
			})
		}
		if reasoningContext != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningContext",
				Details: "reasoningContext is not supported for non-reasoning models",
			})
		}
	}

	// Temperature and top_p: forbidden for reasoning models except GPT-5.1 and
	// later families when reasoning effort is disabled.
	supportsNonReasoningParams := !isReasoning || (effort == "none" && supportsNonReasoningParameters(m.modelID))
	if supportsNonReasoningParams {
		if opts.Temperature != nil {
			body["temperature"] = *opts.Temperature
		}
		if opts.TopP != nil {
			body["top_p"] = *opts.TopP
		}
	} else {
		if opts.Temperature != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "temperature",
				Details: "temperature is not supported for reasoning models",
			})
		}
		if opts.TopP != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "topP",
				Details: "topP is not supported for reasoning models",
			})
		}
		// GPT-6+ models (which have a fixed SupportedReasoningEfforts list)
		// do not support logprobs while reasoning is active.
		if modelCapabilities.SupportedReasoningEfforts != nil && topLogprobs != nil {
			topLogprobs = nil
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "logprobs",
				Details: "logprobs is not supported for reasoning models",
			})
		}
	}

	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}

	// Response format and/or text verbosity → "text" object.
	// TS only creates text.format for responseFormat.type === "json".
	hasJSONFormat := opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json"
	if hasJSONFormat || textVerbosity != "" {
		textObj := map[string]interface{}{}
		if hasJSONFormat {
			if opts.ResponseFormat.Schema != nil {
				// Normalize the response schema for OpenAI structured outputs
				// (d5e3024, 411b3f2: drop propertyNames / lookaround
				// patterns), matching the chat model's handling.
				responseSchema := opts.ResponseFormat.Schema
				if rawSchema := providerutils.ResponseFormatJSONSchema(responseSchema); rawSchema != nil {
					if schemaMap, ok := rawSchema.(map[string]interface{}); ok {
						normalizedSchema, schemaWarnings, normErr := NormalizeOpenAIJSONSchema(schemaMap)
						if normErr != nil {
							return nil, false, warnings, normErr
						}
						warnings = append(warnings, schemaWarnings...)
						responseSchema = normalizedSchema
					}
				}
				format := map[string]interface{}{
					"type":   "json_schema",
					"strict": strictJSONSchema,
					"name":   opts.ResponseFormat.Name,
					"schema": responseSchema,
				}
				if format["name"] == "" {
					format["name"] = "response"
				}
				if opts.ResponseFormat.Description != "" {
					format["description"] = opts.ResponseFormat.Description
				}
				textObj["format"] = format
			} else {
				textObj["format"] = map[string]interface{}{"type": "json_object"}
			}
		}
		if textVerbosity != "" {
			textObj["verbosity"] = textVerbosity
		}
		body["text"] = textObj
	}

	// Tools.
	if len(opts.Tools) > 0 {
		preparedTools, err := responses.PrepareToolsWithError(opts.Tools)
		if err != nil {
			return nil, false, nil, err
		}
		toolSchemaWarnings, err := normalizeResponsesToolSchemas(preparedTools, modelCapabilities.SupportsAsyncToolCalling)
		if err != nil {
			return nil, false, nil, err
		}
		warnings = append(warnings, toolSchemaWarnings...)
		body["tools"] = preparedTools
		if len(allowedToolNames) > 0 {
			allowedTools, allowedToolWarnings, err := responses.ResolveAllowedTools(opts.Tools, allowedToolNames, allowedToolsMode)
			warnings = append(warnings, allowedToolWarnings...)
			if err != nil {
				return nil, false, warnings, err
			}
			if allowedTools != nil {
				body["tool_choice"] = *allowedTools
			}
		} else if opts.ToolChoice.Type != "" {
			body["tool_choice"] = convertResponsesToolChoice(opts.ToolChoice, opts.Tools)
		}
	}

	// Build include list — always add reasoning.encrypted_content when
	// store=false and we have a reasoning model, so multi-turn works without
	// server-side persistence.
	if !store && isReasoning {
		includeFields = appendUnique(includeFields, "reasoning.encrypted_content")
	}
	if includeWebSearchSources && (hasTool(opts.Tools, "openai.web_search") || hasTool(opts.Tools, "openai.web_search_preview")) {
		includeFields = appendUnique(includeFields, "web_search_call.action.sources")
	}
	if hasTool(opts.Tools, "openai.code_interpreter") {
		includeFields = appendUnique(includeFields, "code_interpreter_call.outputs")
	}
	if topLogprobs != nil {
		includeFields = appendUnique(includeFields, "message.output_text.logprobs")
	}
	if len(includeFields) > 0 {
		body["include"] = includeFields
	} else if hasNilProviderOption(openaiOpts, "include") {
		body["include"] = nil
	} else if includeExplicit {
		body["include"] = []string{}
	}
	if topLogprobs != nil {
		body["top_logprobs"] = topLogprobs
	}

	// Provider options fields.
	if conversation != "" {
		body["conversation"] = conversation
	} else if hasNilProviderOption(openaiOpts, "conversation") {
		body["conversation"] = nil
	}
	if metadata != nil || hasNilProviderOption(openaiOpts, "metadata") {
		body["metadata"] = metadata
	}
	if storeExplicit {
		body["store"] = store
	} else if hasNilProviderOption(openaiOpts, "store") {
		body["store"] = nil
	}
	if previousResponseID != "" {
		body["previous_response_id"] = previousResponseID
	} else if hasNilProviderOption(openaiOpts, "previousResponseId") {
		body["previous_response_id"] = nil
	}
	if promptCacheRetention != "" && modelCapabilities.SupportsConfigurationUpdate {
		// GPT-6+ models do not support promptCacheRetention; use
		// promptCacheOptions instead.
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "promptCacheRetention",
			Details: "promptCacheRetention is not supported by GPT-6 and later models; use promptCacheOptions instead",
		})
	} else if promptCacheRetention != "" {
		body["prompt_cache_retention"] = promptCacheRetention
	} else if hasNilProviderOption(openaiOpts, "promptCacheRetention") {
		body["prompt_cache_retention"] = nil
	}
	if promptCacheKey != "" {
		body["prompt_cache_key"] = promptCacheKey
	} else if hasNilProviderOption(openaiOpts, "promptCacheKey") {
		body["prompt_cache_key"] = nil
	}
	if promptCacheOptions != nil {
		body["prompt_cache_options"] = promptCacheOptions
	}
	if serviceTier != "" {
		switch serviceTier {
		case "flex":
			if supportsFlexProcessing(m.modelID) {
				body["service_tier"] = serviceTier
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "serviceTier",
					Details: "flex processing is only available for o3, o4-mini, and gpt-5 models",
				})
			}
		case "priority", "fast":
			// "fast" is an alias for priority processing (row 4cd4548) and
			// is gated the same way.
			if supportsPriorityProcessing(m.modelID) {
				body["service_tier"] = serviceTier
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "serviceTier",
					Details: "priority processing is only available for supported models (gpt-4, gpt-5, gpt-5-mini, o3, o4-mini) and requires Enterprise access. gpt-5-nano is not supported",
				})
			}
		default:
			body["service_tier"] = serviceTier
		}
	} else if hasNilProviderOption(openaiOpts, "serviceTier") {
		body["service_tier"] = nil
	}
	if user != "" {
		body["user"] = user
	} else if hasNilProviderOption(openaiOpts, "user") {
		body["user"] = nil
	}
	if instructions != "" {
		body["instructions"] = instructions
	} else if hasNilProviderOption(openaiOpts, "instructions") {
		body["instructions"] = nil
	}
	if safetyIdentifier != "" {
		body["safety_identifier"] = safetyIdentifier
	} else if hasNilProviderOption(openaiOpts, "safetyIdentifier") {
		body["safety_identifier"] = nil
	}
	if maxToolCalls != nil {
		body["max_tool_calls"] = *maxToolCalls
	} else if hasNilProviderOption(openaiOpts, "maxToolCalls") {
		body["max_tool_calls"] = nil
	}
	if parallelToolCalls != nil {
		body["parallel_tool_calls"] = *parallelToolCalls
	} else if hasNilProviderOption(openaiOpts, "parallelToolCalls") {
		body["parallel_tool_calls"] = nil
	}
	if truncation != "" {
		body["truncation"] = truncation
	} else if hasNilProviderOption(openaiOpts, "truncation") {
		body["truncation"] = nil
	}
	if len(contextManagement) > 0 || contextManagementExplicit {
		body["context_management"] = contextManagement
	}

	return body, store, warnings, nil
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

func intFromInterface(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func responsesTopLogprobs(value interface{}) (interface{}, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return 20, true
		}
	case int:
		if v > 0 {
			return v, true
		}
	case int64:
		if v > 0 {
			return v, true
		}
	case float64:
		if v > 0 {
			return v, true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil {
			if i > 0 {
				return i, true
			}
		}
	}
	return nil, false
}

func responsesContextManagement(value interface{}) []map[string]interface{} {
	var entries []map[string]interface{}
	switch v := value.(type) {
	case []map[string]interface{}:
		entries = v
	case []interface{}:
		entries = make([]map[string]interface{}, 0, len(v))
		for _, entry := range v {
			if m, ok := entry.(map[string]interface{}); ok {
				entries = append(entries, m)
			}
		}
	default:
		return nil
	}
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
	return out
}

func hasNilProviderOption(options map[string]interface{}, key string) bool {
	if options == nil {
		return false
	}
	value, ok := options[key]
	return ok && value == nil
}

// supportsFlexProcessing delegates to GetLanguageModelCapabilities (34c53c0),
// which ports TypeScript's regex-based GPT/o-series version parsing instead
// of the previous ad-hoc prefix list.
func supportsFlexProcessing(modelID string) bool {
	return GetLanguageModelCapabilities(modelID).SupportsFlexProcessing
}

// supportsPriorityProcessing delegates to GetLanguageModelCapabilities
// (34c53c0).
func supportsPriorityProcessing(modelID string) bool {
	return GetLanguageModelCapabilities(modelID).SupportsPriorityProcessing
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
		case "code_interpreter", "file_search", "image_generation", "web_search_preview", "web_search", "mcp", "apply_patch", "computer":
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

// normalizeResponsesToolSchemas normalizes the "parameters" JSON Schema of
// every function tool (including tools grouped under a namespace) for
// OpenAI structured outputs (d5e3024, 411b3f2: drop propertyNames /
// lookaround patterns), mirroring the chat model's
// normalizeOpenAIChatToolSchemas. It mutates tools in place.
func normalizeResponsesToolSchemas(tools []interface{}, supportsAsync bool) ([]types.Warning, error) {
	var warnings []types.Warning
	for i, t := range tools {
		switch v := t.(type) {
		case responses.FunctionToolDef:
			normalized, toolWarnings, err := normalizeResponsesFunctionToolDef(v, supportsAsync)
			if err != nil {
				return nil, err
			}
			warnings = append(warnings, toolWarnings...)
			tools[i] = normalized
		case responses.CustomToolDef:
			toolWarnings := gateResponsesToolAsync(&v.Async, v.Name, supportsAsync)
			warnings = append(warnings, toolWarnings...)
			tools[i] = v
		case *responses.NamespaceToolDef:
			// PrepareToolsWithError always stores namespaces as pointers, so
			// mutating v.Tools mutates the shared underlying struct.
			for j, fn := range v.Tools {
				normalized, toolWarnings, err := normalizeResponsesFunctionToolDef(fn, supportsAsync)
				if err != nil {
					return nil, err
				}
				warnings = append(warnings, toolWarnings...)
				v.Tools[j] = normalized
			}
		}
	}
	return warnings, nil
}

func normalizeResponsesFunctionToolDef(fn responses.FunctionToolDef, supportsAsync bool) (responses.FunctionToolDef, []types.Warning, error) {
	var warnings []types.Warning
	schemaMap, ok := fn.Parameters.(map[string]interface{})
	if ok && schemaMap != nil {
		normalized, schemaWarnings, err := NormalizeOpenAIJSONSchema(schemaMap)
		if err != nil {
			return fn, nil, err
		}
		fn.Parameters = normalized
		warnings = append(warnings, schemaWarnings...)
	}
	warnings = append(warnings, gateResponsesToolAsync(&fn.Async, fn.Name, supportsAsync)...)
	return fn, warnings, nil
}

// gateResponsesToolAsync implements row 4a09793's model gating: async tool
// calling is only supported by GPT-6 and later models. If async=true was
// requested on an unsupported model, drop it and warn, mirroring TS
// resolveAsyncToolOption.
func gateResponsesToolAsync(async **bool, toolName string, supportsAsync bool) []types.Warning {
	if *async == nil || !**async || supportsAsync {
		return nil
	}
	*async = nil
	return []types.Warning{{
		Type:    "unsupported",
		Feature: fmt.Sprintf("async tool calling for %q", toolName),
		Details: "Async tool calling is only supported by GPT-6 and later models.",
	}}
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
		"openai.computer":           "computer",
		"code_interpreter":          "code_interpreter",
		"file_search":               "file_search",
		"image_generation":          "image_generation",
		"web_search_preview":        "web_search_preview",
		"web_search":                "web_search",
		"mcp":                       "mcp",
		"apply_patch":               "apply_patch",
		"computer":                  "computer",
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

// toolSearchToolName returns the SDK tool name of the tool whose ProviderID
// is "openai.tool_search", if any is present in this request. Matches TS
// `getOpenAIToolName('openai.tool_search')`. Returns "" when absent -- some
// callers need that "not present" signal, so this does not itself apply the
// bare-name fallback (see openAIProviderToolDisplayName for that).
func toolSearchToolName(tools []types.Tool) string {
	for _, tool := range tools {
		if tool.ProviderID == "openai.tool_search" {
			return tool.Name
		}
	}
	return ""
}

// openAIProviderToolDisplayName resolves the tool name to surface on
// tool-call/tool-result content for a hosted provider tool, mirroring TS's
// `toolNameMapping.toCustomToolName(bareName)`: a "provider" tool with the
// given ProviderID present in the request lends its custom SDK Name (e.g.
// registering openai.image_generation as name "generateImage" surfaces
// "generateImage"); otherwise the bare, unprefixed provider tool identity
// (e.g. "image_generation", not "openai.image_generation") is used, matching
// TS's untouched fallback -- TS's providerToolNames table maps every one of
// these ids to its bare identity string, and toCustomToolName returns that
// input unchanged when no custom mapping exists.
func openAIProviderToolDisplayName(tools []types.Tool, providerID, bareName string) string {
	for _, tool := range tools {
		if tool.ProviderID == providerID {
			return tool.Name
		}
	}
	return bareName
}

// outputSchemaToolNames returns the set of function tool names that declared
// providerOptions.openai.outputSchema, so the input converter can JSON-encode
// their text-like results.
func outputSchemaToolNames(tools []types.Tool) map[string]bool {
	if len(tools) == 0 {
		return nil
	}
	names := map[string]bool{}
	for _, tool := range tools {
		options, ok := tool.ProviderOptions.(map[string]interface{})
		if !ok {
			continue
		}
		openaiOptions, ok := options["openai"].(map[string]interface{})
		if !ok {
			continue
		}
		if openaiOptions["outputSchema"] != nil {
			names[tool.Name] = true
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// extractApprovalRequestIDToToolCallIDFromPrompt extracts a mapping from MCP
// approval request IDs to their corresponding tool call IDs from the prompt.
// When an MCP tool requires approval, the SDK generates a dummy tool call ID
// to track the pending approval. When the caller responds to the approval
// (and the conversation continues in a later turn), this maps the approval
// request ID back to that dummy tool call ID so a later mcp_call for the
// same approval references the correct tool call. Mirrors TS's
// extractApprovalRequestIdToToolCallIdMapping.
func extractApprovalRequestIDToToolCallIDFromPrompt(prompt types.Prompt) map[string]string {
	mapping := map[string]string{}
	for _, message := range prompt.Messages {
		if message.Role != types.RoleAssistant {
			continue
		}
		for _, part := range message.Content {
			toolCall, ok := part.(types.ToolCallContent)
			if !ok {
				continue
			}
			openaiOpts, ok := toolCall.ProviderOptions["openai"].(map[string]interface{})
			if !ok {
				continue
			}
			approvalRequestID, ok := openaiOpts["approvalRequestId"].(string)
			if !ok || approvalRequestID == "" {
				continue
			}
			mapping[approvalRequestID] = toolCall.ToolCallID
		}
	}
	return mapping
}

func responsesWebSearchToolName(tools []types.Tool) string {
	for _, tool := range tools {
		switch {
		case tool.ProviderID == "openai.web_search_preview":
			if tool.Name != "" && tool.Name != "openai.web_search_preview" {
				return tool.Name
			}
			return "web_search_preview"
		case tool.Name == "openai.web_search_preview":
			return "web_search_preview"
		case tool.ProviderID == "openai.web_search":
			if tool.Name != "" && tool.Name != "openai.web_search" {
				return tool.Name
			}
			return "web_search"
		case tool.Name == "openai.web_search":
			return "web_search"
		}
	}
	return "web_search"
}

// ─────────────────────────────────────────────────────────────────────────────
// Non-streaming response conversion
// ─────────────────────────────────────────────────────────────────────────────

func (m *ResponsesLanguageModel) convertResponse(resp responses.ResponsesAPIResponse, store bool, webSearchToolName string, tools []types.Tool, approvalFromPrompt map[string]string, providerOptionsName ...string) (*types.GenerateResult, error) {
	providerName := "openai"
	if len(providerOptionsName) > 0 && providerOptionsName[0] != "" {
		providerName = providerOptionsName[0]
	}
	result := &types.GenerateResult{
		Usage:       convertResponsesUsage(resp.Usage),
		RawResponse: resp,
	}

	var toolCalls []types.ToolCall
	// hostedToolSearchCallIds pairs a server-executed tool_search_call with
	// its following tool_search_output in FIFO order (row e6a2992 area;
	// mirrors TS `hostedToolSearchCallIds`), since hosted tool_search_output
	// items report call_id: null.
	var hostedToolSearchCallIds []string

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
					ProviderOptions: openAIResponsesMessageProviderOptions(providerName, item.ID, item.Phase, part.Annotations),
				})
				for _, ann := range part.Annotations {
					if src, ok := openAIAnnotationToSource(providerName, ann); ok {
						result.Content = append(result.Content, src)
					}
				}
			}

		case "function_call":
			var item responses.FunctionCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			// Row 6be0f51: expand an internal "parallel" wrapper call into
			// one tool call per nested recipient, when every recipient names
			// a declared function tool.
			if expanded, ok := responses.ExpandParallelToolCall(item.CallID, item.Name, item.Arguments, item.ID, tools, providerName); ok {
				toolCalls = append(toolCalls, expanded...)
				continue
			}
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			tc := types.ToolCall{
				ID:               item.CallID,
				ToolName:         item.Name,
				Arguments:        args,
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, item.Namespace, item.Async, item.Caller),
			}
			toolCalls = append(toolCalls, tc)

		case "apply_patch_call":
			// Row 45f2b6a: decode into a tool call ({callId, operation}) and
			// drive a tool-calls finish reason like any other tool call.
			var item responses.ApplyPatchCall
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			rawArgs, _ := json.Marshal(map[string]interface{}{
				"callId":    item.CallID,
				"operation": item.Operation,
			})
			var args map[string]interface{}
			json.Unmarshal(rawArgs, &args) //nolint:errcheck
			itemID := ""
			if item.ID != nil {
				itemID = *item.ID
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.CallID,
				ToolName:         "openai.apply_patch",
				Arguments:        args,
				RawArguments:     string(rawArgs),
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, itemID, "", nil, nil),
			})

		case "computer_call":
			// Row 0063c2d: decode a batch of UI actions into a tool call.
			var item responses.ComputerCall
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			computerItemID := ""
			if item.ID != nil {
				computerItemID = *item.ID
			}
			if item.CallID == nil {
				// A null call_id means this call is fully server-executed
				// with no client round-trip: emit the immediate
				// "computer_use" tool-call/tool-result pair TS produces,
				// instead of a client-executable "computer" tool call.
				tc := types.ToolCall{
					ID:               computerItemID,
					ToolName:         "openai.computer_use",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				}
				toolCalls = append(toolCalls, tc)
				result.Content = append(result.Content,
					types.ToolCallContent{
						ToolCallID:       computerItemID,
						ToolName:         "openai.computer_use",
						Input:            "",
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
					types.ToolResultContent{
						ToolCallID:       computerItemID,
						ToolName:         "openai.computer_use",
						Result:           map[string]interface{}{"type": "computer_use_tool_result", "status": item.Status},
						ProviderExecuted: true,
					},
				)
				continue
			}
			args, rawArgs := computerCallArguments(item)
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               *item.CallID,
				ToolName:         "openai.computer",
				Arguments:        args,
				RawArguments:     rawArgs,
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, computerItemID, "", nil, nil),
			})

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
			// TS pushes one `reasoning` content entry per summary array
			// element (openai-responses-language-model.ts: "when there are
			// no summary parts, we need to add an empty reasoning part"),
			// not one entry with every part's text concatenated -- push an
			// empty summary_text part when there are none, matching TS's
			// fallback exactly.
			summaries := item.Summary
			if len(summaries) == 0 {
				summaries = []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}{{Type: "summary_text", Text: ""}}
			}
			for _, s := range summaries {
				result.Content = append(result.Content, types.ReasoningContent{
					Text:             s.Text,
					EncryptedContent: item.EncryptedContent,
					ProviderMetadata: openAIResponsesReasoningMetadata(providerName, item.ID),
				})
			}

		case "custom_tool_call":
			var item responses.CustomToolCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.CallID,
				ToolName:         item.Name,
				Arguments:        map[string]interface{}{"input": item.Input},
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", item.Async, item.Caller),
			})

		case "program":
			// Row 1f6dd3a: the hosted programmatic-tool-calling sandbox
			// generated and is executing this JavaScript program; the result
			// arrives as a separate "program_output" item below.
			var item responses.ProgramItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			args, rawArgs := programCallArguments(item)
			tc := types.ToolCall{
				ID:               item.CallID,
				ToolName:         "openai.programmatic_tool_calling",
				Arguments:        args,
				RawArguments:     rawArgs,
				ProviderExecuted: true,
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil),
			}
			toolCalls = append(toolCalls, tc)
			result.Content = append(result.Content, types.ToolCallContent{
				ToolCallID:       item.CallID,
				ToolName:         "openai.programmatic_tool_calling",
				Input:            rawArgs,
				Arguments:        args,
				ProviderExecuted: true,
			})

		case "program_output":
			// Row 1f6dd3a: the result of a "program" item's hosted execution.
			var item responses.ProgramOutputItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			result.Content = append(result.Content, types.ToolResultContent{
				ToolCallID:       item.CallID,
				ToolName:         "openai.programmatic_tool_calling",
				Result:           map[string]interface{}{"result": item.Result, "status": item.Status},
				ProviderExecuted: true,
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

		case "image_generation_call":
			var item responses.ImageGenerationCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			imageGenToolName := openAIProviderToolDisplayName(tools, "openai.image_generation", "image_generation")
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         imageGenToolName,
				Arguments:        map[string]interface{}{},
				ProviderExecuted: true,
			})
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       item.ID,
					ToolName:         imageGenToolName,
					Input:            "{}",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
				types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   imageGenToolName,
					Result:     map[string]interface{}{"result": item.Result},
					// TS never sets providerExecuted on this tool-result,
					// only on the tool-call.
				},
			)

		case "tool_search_call":
			var item responses.ToolSearchCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := openAIProviderToolDisplayName(tools, "openai.tool_search", "tool_search")
			isHosted := item.Execution == "server"
			toolCallID := item.ID
			if item.CallID != nil && *item.CallID != "" {
				toolCallID = *item.CallID
			}
			if isHosted {
				hostedToolSearchCallIds = append(hostedToolSearchCallIds, toolCallID)
			}
			inputRaw, _ := json.Marshal(map[string]interface{}{
				"arguments": item.Arguments,
				"call_id":   item.CallID,
			})
			tc := types.ToolCall{
				ID:               toolCallID,
				ToolName:         toolName,
				RawArguments:     string(inputRaw),
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil),
			}
			json.Unmarshal(inputRaw, &tc.Arguments) //nolint:errcheck
			if isHosted {
				tc.ProviderExecuted = true
			}
			toolCalls = append(toolCalls, tc)

		case "tool_search_output":
			var item responses.ToolSearchOutputItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolCallID := item.ID
			if item.CallID != nil && *item.CallID != "" {
				toolCallID = *item.CallID
			} else if len(hostedToolSearchCallIds) > 0 {
				toolCallID = hostedToolSearchCallIds[0]
				hostedToolSearchCallIds = hostedToolSearchCallIds[1:]
			}
			matchedTools := make([]interface{}, 0, len(item.Tools))
			for _, t := range item.Tools {
				var v interface{}
				json.Unmarshal(t, &v) //nolint:errcheck
				matchedTools = append(matchedTools, v)
			}
			result.Content = append(result.Content, types.ToolResultContent{
				ToolCallID:       toolCallID,
				ToolName:         openAIProviderToolDisplayName(tools, "openai.tool_search", "tool_search"),
				Result:           map[string]interface{}{"tools": matchedTools},
				ProviderMetadata: toRawMetadata(openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil)),
			})

		case "file_search_call":
			var item responses.FileSearchCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			fileSearchToolName := openAIProviderToolDisplayName(tools, "openai.file_search", "file_search")
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         fileSearchToolName,
				Arguments:        map[string]interface{}{},
				ProviderExecuted: true,
			})
			var results interface{}
			if item.Results != nil {
				mapped := make([]map[string]interface{}, 0, len(item.Results))
				for _, r := range item.Results {
					mapped = append(mapped, map[string]interface{}{
						"attributes": r.Attributes,
						"fileId":     r.FileID,
						"filename":   r.Filename,
						"score":      r.Score,
						"text":       r.Text,
					})
				}
				results = mapped
			}
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       item.ID,
					ToolName:         fileSearchToolName,
					Input:            "{}",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
				types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   fileSearchToolName,
					Result:     map[string]interface{}{"queries": item.Queries, "results": results},
					// TS never sets providerExecuted on this tool-result,
					// only on the tool-call.
				},
			)

		case "code_interpreter_call":
			var item responses.CodeInterpreterCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			// TS's schema is `code: z.string().nullable()`, always present
			// (null when absent) since JSON.stringify still includes a
			// null-valued key -- include "code" explicitly here too, not
			// only when non-nil.
			var codeValue interface{}
			if item.Code != nil {
				codeValue = *item.Code
			}
			codeArgs := map[string]interface{}{"containerId": item.ContainerID, "code": codeValue}
			rawCodeArgs, _ := json.Marshal(codeArgs)
			codeInterpreterToolName := openAIProviderToolDisplayName(tools, "openai.code_interpreter", "code_interpreter")
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         codeInterpreterToolName,
				Arguments:        codeArgs,
				RawArguments:     string(rawCodeArgs),
				ProviderExecuted: true,
			})
			outputs := make([]map[string]interface{}, 0, len(item.Outputs))
			for _, o := range item.Outputs {
				switch o.Type {
				case "logs":
					outputs = append(outputs, map[string]interface{}{"type": "logs", "logs": o.Logs})
				case "image":
					outputs = append(outputs, map[string]interface{}{"type": "image", "url": o.URL})
				}
			}
			var outputsValue interface{}
			if item.Outputs != nil {
				outputsValue = outputs
			}
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       item.ID,
					ToolName:         codeInterpreterToolName,
					Input:            string(rawCodeArgs),
					Arguments:        codeArgs,
					ProviderExecuted: true,
				},
				types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   codeInterpreterToolName,
					Result:     map[string]interface{}{"outputs": outputsValue},
					// TS never sets providerExecuted on this tool-result,
					// only on the tool-call.
				},
			)

		case "mcp_call":
			var item responses.McpCallItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolCallID := item.ID
			if item.ApprovalRequestID != nil {
				// Matches TS: `approvalRequestIdToDummyToolCallIdFromPrompt[part.approval_request_id] ?? part.id`.
				// When this mcp_call was already approved in a previous turn
				// (an mcp_approval_request whose dummy tool-call id was
				// carried in the prompt via providerOptions.openai.approvalRequestId),
				// reuse that same tool-call id so the approval and its
				// resulting call/result share one id across turns.
				if alias, ok := approvalFromPrompt[*item.ApprovalRequestID]; ok {
					toolCallID = alias
				}
			}
			toolName := "mcp." + item.Name
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			mcpResult := map[string]interface{}{
				"type":        "call",
				"serverLabel": item.ServerLabel,
				"name":        item.Name,
				"arguments":   item.Arguments,
			}
			if item.Output != nil {
				mcpResult["output"] = *item.Output
			}
			if item.Error != nil {
				mcpResult["error"] = item.Error.AsJSONValue()
			}
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               toolCallID,
				ToolName:         toolName,
				Arguments:        args,
				RawArguments:     item.Arguments,
				ProviderExecuted: true,
				Dynamic:          true,
			})
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       toolCallID,
					ToolName:         toolName,
					Input:            item.Arguments,
					Arguments:        args,
					ProviderExecuted: true,
					Dynamic:          true,
				},
				types.ToolResultContent{
					ToolCallID: toolCallID,
					ToolName:   toolName,
					Result:     mcpResult,
					// TS never sets providerExecuted/dynamic on this
					// tool-result, only on the tool-call (matches the
					// streaming path's output_item.done handler below).
					ProviderMetadata: toRawMetadata(openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil)),
				},
			)

		case "mcp_list_tools":
			// TS: skipped entirely -- not exposed to the caller or replayed.
			continue

		case "mcp_approval_request":
			var item responses.McpApprovalRequestItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			approvalRequestID := item.ID
			if item.ApprovalRequestID != nil && *item.ApprovalRequestID != "" {
				approvalRequestID = *item.ApprovalRequestID
			}
			dummyToolCallID := streaming.GenerateID()
			toolName := "mcp." + item.Name
			var args map[string]interface{}
			json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               dummyToolCallID,
				ToolName:         toolName,
				Arguments:        args,
				RawArguments:     item.Arguments,
				ProviderExecuted: true,
				Dynamic:          true,
			})
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       dummyToolCallID,
					ToolName:         toolName,
					Input:            item.Arguments,
					Arguments:        args,
					ProviderExecuted: true,
					Dynamic:          true,
				},
				types.ToolApprovalRequestContent{
					ApprovalID: approvalRequestID,
					ToolCallID: dummyToolCallID,
				},
			)
		}
	}

	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
		result.FinishReason = types.FinishReasonToolCalls
	} else {
		result.FinishReason = mapResponsesFinishReason(resp.IncompleteDetails, false)
	}

	// Row b2b1bb9 (Responses half): surface the response id, echoed
	// service tier, and effective reasoning context (GPT-5.6
	// reasoningContext) in provider metadata, mirroring TS's
	// doGenerate providerMetadata assembly.
	meta := map[string]interface{}{"responseId": resp.ID}
	if resp.ServiceTier != "" {
		meta["serviceTier"] = resp.ServiceTier
	}
	if resp.Reasoning != nil && resp.Reasoning.Context != "" {
		meta["reasoningContext"] = resp.Reasoning.Context
	}
	result.ProviderMetadata = map[string]interface{}{providerName: meta}

	return result, nil
}

// programCallArguments builds the SDK-facing {code, fingerprint} arguments
// for a decoded "program" item, mirroring TS's
// programmaticToolCallingInputSchema (row 1f6dd3a).
func programCallArguments(item responses.ProgramItem) (map[string]interface{}, string) {
	args := map[string]interface{}{
		"code":        item.Code,
		"fingerprint": item.Fingerprint,
	}
	raw, _ := json.Marshal(args)
	return args, string(raw)
}

// computerCallArguments builds the SDK-facing {actions, pendingSafetyChecks,
// status} arguments for a decoded computer_call item, mirroring TS
// mapComputerCallInput (row 0063c2d): actions are translated from wire
// (scroll_x/scroll_y) to SDK (scrollX/scrollY) field names.
func computerCallArguments(item responses.ComputerCall) (map[string]interface{}, string) {
	// TS: `actions ?? (action != null ? [action] : [])` -- fall back to the
	// singular `action` field when `actions` is empty/absent.
	rawActions := item.Actions
	if len(rawActions) == 0 && item.Action != nil {
		rawActions = []map[string]interface{}{item.Action}
	}
	actions := make([]map[string]interface{}, 0, len(rawActions))
	for _, action := range rawActions {
		actions = append(actions, responses.MapComputerActionToSDK(action))
	}
	checks := make([]map[string]interface{}, 0, len(item.PendingSafetyChecks))
	for _, c := range item.PendingSafetyChecks {
		check := map[string]interface{}{"id": c.ID}
		if c.Code != "" {
			check["code"] = c.Code
		}
		if c.Message != "" {
			check["message"] = c.Message
		}
		checks = append(checks, check)
	}
	status := item.Status
	if status == "" {
		status = "completed"
	}
	args := map[string]interface{}{
		"actions":             actions,
		"pendingSafetyChecks": checks,
		"status":              status,
	}
	raw, _ := json.Marshal(args)
	return args, string(raw)
}

func openAIResponsesToolCallMetadata(providerName, itemID, namespace string, async *bool, caller *responses.ToolCaller) map[string]interface{} {
	openai := map[string]interface{}{}
	if itemID != "" {
		openai["itemId"] = itemID
	}
	if namespace != "" {
		openai["namespace"] = namespace
	}
	// Row 4a09793: forward async on tool-call replay/decode metadata.
	if async != nil {
		openai["async"] = *async
	}
	// Row 1f6dd3a: forward the caller (direct vs. a programmatic-tool-calling
	// "program") on tool-call replay/decode metadata, mirroring TS's
	// caller.type === 'program' ? {type:'program', callerId} : caller.
	if caller != nil {
		if caller.Type == "program" {
			openai["caller"] = map[string]interface{}{"type": "program", "callerId": caller.CallerID}
		} else {
			openai["caller"] = map[string]interface{}{"type": caller.Type}
		}
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

func openAIResponsesMessageProviderOptions(providerName, itemID string, phase *string, annotations []responses.TextAnnotation) map[string]interface{} {
	openai := map[string]interface{}{}
	if itemID != "" {
		openai["itemId"] = itemID
	}
	if phase != nil && *phase != "" {
		openai["phase"] = *phase
	}
	if len(annotations) > 0 {
		openai["annotations"] = annotations
	}
	if len(openai) == 0 {
		return nil
	}
	return map[string]interface{}{providerName: openai}
}

// openAIAnnotationToSource converts a single Responses API text annotation
// into a SourceContent part, mirroring TS's per-annotation switch in the
// "message" case of convertResponse. Not every annotation type produces a
// source ("compaction" etc. never reach this function). The id is generated
// the same way in both the non-streaming and streaming paths -- TS uses
// `this.config.generateId?.() ?? generateId()` for each source there;
// streaming.GenerateID() is this file's existing stand-in for that (also
// used for the mcp_approval_request dummy tool-call id below).
func openAIAnnotationToSource(providerName string, ann responses.TextAnnotation) (types.SourceContent, bool) {
	id := streaming.GenerateID()
	switch ann.Type {
	case "url_citation":
		return types.SourceContent{
			SourceType: "url",
			ID:         id,
			URL:        ann.URL,
			Title:      ann.Title,
		}, true
	case "file_citation":
		meta, _ := json.Marshal(map[string]interface{}{
			providerName: map[string]interface{}{
				"type":   ann.Type,
				"fileId": ann.FileID,
				"index":  ann.Index,
			},
		})
		return types.SourceContent{
			SourceType:       "document",
			ID:               id,
			MediaType:        "text/plain",
			Title:            ann.Filename,
			Filename:         ann.Filename,
			ProviderMetadata: meta,
		}, true
	case "container_file_citation":
		meta, _ := json.Marshal(map[string]interface{}{
			providerName: map[string]interface{}{
				"type":        ann.Type,
				"fileId":      ann.FileID,
				"containerId": ann.ContainerID,
			},
		})
		return types.SourceContent{
			SourceType:       "document",
			ID:               id,
			MediaType:        "text/plain",
			Title:            ann.Filename,
			Filename:         ann.Filename,
			ProviderMetadata: meta,
		}, true
	case "file_path":
		meta, _ := json.Marshal(map[string]interface{}{
			providerName: map[string]interface{}{
				"type":   ann.Type,
				"fileId": ann.FileID,
				"index":  ann.Index,
			},
		})
		return types.SourceContent{
			SourceType:       "document",
			ID:               id,
			MediaType:        "application/octet-stream",
			Title:            ann.FileID,
			Filename:         ann.FileID,
			ProviderMetadata: meta,
		}, true
	}
	return types.SourceContent{}, false
}

// toRawMetadata marshals a provider metadata map into json.RawMessage, for
// the types.ToolResultContent.ProviderMetadata field (unlike
// types.ToolCall/types.ToolResult, which take the map directly).
func toRawMetadata(m map[string]interface{}) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	raw, _ := json.Marshal(m)
	return raw
}

// nonEmptyOrNil returns nil for an empty string so it marshals as JSON null
// (matching TS's `encryptedContent ?? null`), or the string itself otherwise.
func nonEmptyOrNil(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// escapeJSONDelta returns delta's JSON string-escaped form without the
// surrounding quotes, for splicing into a hand-built JSON string during
// progressive tool-input-delta streaming. Mirrors TS's escapeJSONDelta
// (`JSON.stringify(delta).slice(1, -1)`).
func escapeJSONDelta(delta string) string {
	raw, err := json.Marshal(delta)
	if err != nil || len(raw) < 2 {
		return delta
	}
	return string(raw[1 : len(raw)-1])
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
		// TS only ever includes `sources` on the "search" action
		// (mapWebSearchOutput's `case 'search'` branch); open_page/
		// find_in_page never carry a sources key even if the API returned
		// one.
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

// convertResponsesUsage converts Responses API usage to types.Usage. Row
// f6fac50: a nil usage (JSON null/absent) yields an all-nil-fields
// types.Usage rather than a zero-token usage that looks like a real
// (empty) response.
func convertResponsesUsage(u *responses.ResponsesAPIUsage) types.Usage {
	if u == nil {
		return types.Usage{}
	}
	inputTokens := int64(u.InputTokens)
	outputTokens := int64(u.OutputTokens)
	total := inputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &total,
	}

	if u.InputTokensDetails != nil && (u.InputTokensDetails.CachedTokens > 0 || u.InputTokensDetails.CacheWriteTokens != nil) {
		cached := int64(u.InputTokensDetails.CachedTokens)
		var cacheWrite int64
		var cacheWritePtr *int64
		if u.InputTokensDetails.CacheWriteTokens != nil {
			cacheWrite = int64(*u.InputTokensDetails.CacheWriteTokens)
			cacheWritePtr = &cacheWrite
		}
		noCached := inputTokens - cached - cacheWrite
		result.InputDetails = &types.InputTokenDetails{
			CacheReadTokens:  &cached,
			CacheWriteTokens: cacheWritePtr,
			NoCacheTokens:    &noCached,
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

	// Row 7243530/2c4767d: keep the complete raw usage object (captured by
	// ResponsesAPIUsage's UnmarshalJSON), so fields not modeled above --
	// e.g. orchestration_input_tokens/orchestration_input_cached_tokens/
	// orchestration_output_tokens -- are still available to callers.
	if u.Raw != nil {
		result.Raw = u.Raw
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

// responsesReasoningAccum tracks the lifecycle of a streaming reasoning
// item's summary parts (row a0d2e8c/6fe187f/3b9f025 area), mirroring TS's
// `activeReasoning[itemId]`. Each summary index can be one of "active"
// (still streaming or never explicitly closed), "can-conclude" (its
// response.reasoning_summary_part.done arrived but store=false, so the
// final encrypted_content on output_item.done must still be attached
// before closing), or "concluded" (a reasoning-end chunk was already
// emitted for it).
type responsesReasoningAccum struct {
	encryptedContent string
	summaryParts     map[int]string
}

// responsesStream implements provider.TextStream for the Responses API SSE stream.
type responsesStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error

	// Accumulated tool calls keyed by output_index.
	toolAccum map[int]*responsesToolAccum
	// Accumulated reasoning summary-part state keyed by item id (not
	// output_index: response.reasoning_summary_part.added/.done carry a
	// nullable output_index in the API schema, so item id is the only
	// reliable correlation key -- mirrors TS's `activeReasoning[itemId]`).
	reasoningAccum map[string]*responsesReasoningAccum
	// Item type by output_index, set on output_item.added.
	itemTypes map[int]string
	// firstItemIDByOutputIndex records the item id first seen for a given
	// output_index (row 73d48d0): OpenAI can rotate/reuse item ids across
	// events sharing the same output_index mid-stream, so the id observed at
	// output_item.added -- not whatever the later done event reports -- is
	// used consistently for reasoning-end ids and itemId metadata.
	firstItemIDByOutputIndex map[int]string
	// Chunks ready to emit without reading more SSE events.
	flushQueue        []*provider.StreamChunk
	includeRawChunks  bool
	webSearchToolName string
	providerName      string
	responseID        string
	outputStarted     bool
	pendingRaw        []*provider.StreamChunk
	pendingMetadata   *provider.StreamChunk
	responseHeaders   http.Header

	// hadDecodeError is set when a known event type (one with a dedicated
	// case below) fails schema decode (row eee6200). The stream keeps
	// running (a single malformed event doesn't necessarily invalidate the
	// whole response), but the eventual finish reason is forced to "error"
	// instead of whatever response.completed/incomplete would otherwise
	// report, so callers don't see a false "stop"/"length" outcome.
	hadDecodeError bool

	// tools holds the request's declared tools, used to expand an internal
	// "parallel" tool call wrapper into its nested recipients (row 6be0f51).
	// Left nil in most tests; only DoStream and tests exercising parallel
	// expansion set it.
	tools []types.Tool

	// ongoingToolCalls tracks per-output-index state for tool calls whose
	// input streams progressively across multiple SSE events (apply_patch,
	// custom_tool_call, code_interpreter, tool_search), mirroring TS's
	// `ongoingToolCalls` array.
	ongoingToolCalls map[int]*responsesOngoingToolCall

	// toolSearchToolName is the SDK tool name registered for
	// "openai.tool_search", or the bare "tool_search" if none is registered
	// under a custom name -- matches TS's toolNameMapping.toCustomToolName
	// fallback (the unprefixed provider tool identity, not the internal
	// "openai.tool_search" id).
	toolSearchToolName string

	// hostedToolSearchIDs pairs a hosted (server-executed) tool_search_call
	// with its following tool_search_output in FIFO order, since hosted
	// tool_search_output items report call_id: null.
	hostedToolSearchIDs []string

	// store mirrors TS's raw `openaiOptions?.store` (boolean | undefined,
	// truthy-checked as `if (store)`) used by doStream's reasoning-summary
	// state machine -- NOT the request-body-default-true `store` value used
	// elsewhere (the field actually sent to the API, and the value used for
	// prompt-conversion item_reference decisions). It controls whether a
	// reasoning summary part can be concluded immediately on
	// response.reasoning_summary_part.done, or must wait for
	// output_item.done to carry the final encrypted_content: unset/false
	// defers to output_item.done (matching TS's undefined-is-falsy
	// default); only an explicit `store: true` concludes immediately. See
	// responsesExplicitStore.
	store bool

	// mcpApprovalAlias maps an mcp_approval_request's approval_request_id
	// (seen earlier in this same stream) to the dummy tool-call id emitted
	// for it, so a later mcp_call sharing that approval_request_id reuses
	// the same tool-call id the approval was requested/answered under.
	// Mirrors TS's approvalRequestIdToDummyToolCallIdFromStream.
	mcpApprovalAlias map[string]string

	// approvalFromPrompt maps an MCP approval_request_id to the dummy
	// tool-call id it was assigned in a PREVIOUS turn, extracted from the
	// prompt's assistant tool-call parts (providerOptions.openai.approvalRequestId).
	// Used as a fallback when mcpApprovalAlias has no entry for the current
	// stream (the approval request happened in an earlier turn, not this
	// one). Mirrors TS's approvalRequestIdToDummyToolCallIdFromPrompt.
	approvalFromPrompt map[string]string

	// ongoingAnnotations accumulates response.output_text.annotation.added
	// annotations for the currently-open "message" item, reset when a new
	// message item starts (output_item.added) and attached to that item's
	// text-end providerMetadata.openai.annotations (TS `ongoingAnnotations`).
	ongoingAnnotations []responses.TextAnnotation
}

// responsesOngoingToolCall tracks per-output-index state for a tool call
// whose input streams progressively across multiple SSE events. Mirrors
// TS's `ongoingToolCalls[output_index]` entry shape.
type responsesOngoingToolCall struct {
	toolName   string
	toolCallID string

	applyPatch      *applyPatchStreamState
	codeInterpreter *codeInterpreterStreamState
	// toolSearchExecution is "server" or "client" for a tool_search_call.
	toolSearchExecution string
}

// applyPatchStreamState tracks whether an apply_patch_call's diff has
// started streaming and whether its tool-input-end has already been sent.
type applyPatchStreamState struct {
	hasDiff    bool
	endEmitted bool
}

// codeInterpreterStreamState carries the container id needed to close out
// the code_interpreter_call's synthetic input JSON.
type codeInterpreterStreamState struct {
	containerID string
}

// resolveOutputItemID mirrors TS's `resolveOutputItemId`: an event carrying
// both an output_index and an item id resolves to the id first seen for
// that output_index at output_item.added (s.firstItemIDByOutputIndex),
// falling back to the event's own id if none was recorded yet. This handles
// OpenAI rotating/reusing item ids across events sharing the same
// output_index mid-stream, and must be applied at every reasoning-summary
// event carrying item_id -- response.reasoning_summary_part.added,
// response.reasoning_summary_text.delta, and
// response.reasoning_summary_part.done -- not just output_item.done.
func (s *responsesStream) resolveOutputItemID(outputIndex int, itemID string) string {
	if first, ok := s.firstItemIDByOutputIndex[outputIndex]; ok {
		return first
	}
	return itemID
}

// emitDecodeError reports a decode failure for a known Responses API SSE
// event type: emits a ChunkTypeError chunk (instead of silently skipping the
// event) and marks the eventual finish reason to be forced to "error".
func (s *responsesStream) emitDecodeError(eventType string, err error) (*provider.StreamChunk, error) {
	s.hadDecodeError = true
	return s.emitParsedChunk(&provider.StreamChunk{
		Type: provider.ChunkTypeError,
		Text: fmt.Sprintf("failed to parse %s event: %v", eventType, err),
	})
}

// chatCompletionsMismatchMessage adapts TS
// createOpenAIResponsesChatCompletionsMismatchError's message (row 1ead90c)
// to this SDK's Go API surface.
const chatCompletionsMismatchMessage = "Received a Chat Completions stream while using the OpenAI Responses API. " +
	"The default OpenAI provider model uses the Responses API. If your custom baseURL targets a Chat Completions-compatible endpoint, use Provider.ChatModel(\"model-id\") instead of Provider.ResponsesModel/LanguageModel. " +
	"You can also use one of this SDK's OpenAI-compatible providers."

func newResponsesStream(r io.ReadCloser, includeRawChunks bool, args ...string) *responsesStream {
	return newResponsesStreamWithMetadata(r, includeRawChunks, argOrDefault(args, 0, "web_search"), argOrDefault(args, 1, "openai"), nil)
}

func newResponsesStreamWithMetadata(r io.ReadCloser, includeRawChunks bool, toolName, providerName string, headers http.Header) *responsesStream {
	if toolName == "" {
		toolName = "web_search"
	}
	if providerName == "" {
		providerName = "openai"
	}
	return &responsesStream{
		reader:                   r,
		parser:                   streaming.NewSSEParser(r),
		toolAccum:                make(map[int]*responsesToolAccum),
		reasoningAccum:           make(map[string]*responsesReasoningAccum),
		itemTypes:                make(map[int]string),
		firstItemIDByOutputIndex: make(map[int]string),
		includeRawChunks:         includeRawChunks,
		webSearchToolName:        toolName,
		providerName:             providerName,
		responseHeaders:          headers,
		ongoingToolCalls:         make(map[int]*responsesOngoingToolCall),
		toolSearchToolName:       "tool_search",
	}
}

func argOrDefault(args []string, index int, fallback string) string {
	if len(args) > index && args[index] != "" {
		return args[index]
	}
	return fallback
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
	s.flushQueue = append(s.flushQueue, chunk)
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
	// Parse the "type" discriminator.
	var peek responses.ResponsesStreamEvent
	if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
		s.queueRawChunk(event.Data)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeError,
			Text: fmt.Sprintf("failed to parse stream chunk: %v", err),
		})
	}
	// Row 1ead90c: a Chat Completions-shaped chunk (top-level "choices"
	// array, no "type" discriminator) means the configured baseURL points
	// at a Chat Completions-compatible endpoint instead of the Responses
	// API. Surface a helpful error instead of silently skipping it.
	if peek.Type == "" && len(peek.Choices) > 0 {
		var isArray bool
		trimmed := bytes.TrimSpace(peek.Choices)
		isArray = len(trimmed) > 0 && trimmed[0] == '['
		if isArray {
			s.err = providererrors.NewProviderError(s.providerName, 0, "", chatCompletionsMismatchMessage, nil)
			return nil, s.err
		}
	}
	var eventRawChunk *provider.StreamChunk
	if s.includeRawChunks {
		eventRawChunk = openAIResponsesRawChunk(event.Data)
		if !s.outputStarted && peek.Type == "response.created" {
			s.pendingRaw = append(s.pendingRaw, eventRawChunk)
		} else if !s.outputStarted && (peek.Type == "error" || peek.Type == "response.failed") {
			// TS checks early errors before exposing raw chunks.
		} else if !s.outputStarted {
			// First output raw is queued after pending metadata in the handler.
		} else {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
	}

	switch peek.Type {

	case "response.created":
		var e responses.ResponseCreatedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
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
		s.pendingMetadata = &provider.StreamChunk{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: metadata,
		}
		return s.Next()

	case "response.output_item.added":
		var e responses.OutputItemAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.itemTypes[e.OutputIndex] = e.Item.Type
		if e.Item.ID != "" {
			if _, seen := s.firstItemIDByOutputIndex[e.OutputIndex]; !seen {
				s.firstItemIDByOutputIndex[e.OutputIndex] = e.Item.ID
			}
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
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
		case "message":
			// TS: `ongoingAnnotations.splice(0)` then emit text-start with
			// providerMetadata {itemId, phase?} (no annotations yet).
			s.ongoingAnnotations = nil
			var meta json.RawMessage
			if m := openAIResponsesMessageProviderOptions(s.providerName, e.Item.ID, e.Item.Phase, nil); m != nil {
				meta, _ = json.Marshal(m)
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeTextStart,
				ID:               e.Item.ID,
				ProviderMetadata: meta,
			})
			return s.Next()

		case "reasoning":
			accum := &responsesReasoningAccum{
				encryptedContent: e.Item.EncryptedContent,
				summaryParts:     map[int]string{0: "active"},
			}
			s.reasoningAccum[e.Item.ID] = accum
			var encMeta interface{}
			if e.Item.EncryptedContent != "" {
				encMeta = e.Item.EncryptedContent
			}
			meta, _ := json.Marshal(map[string]interface{}{
				s.providerName: map[string]interface{}{
					"itemId":                    e.Item.ID,
					"reasoningEncryptedContent": encMeta,
				},
			})
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningStart,
				ID:               e.Item.ID + ":0",
				ProviderMetadata: meta,
			})
			return s.Next()
		case "file_search_call":
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               e.Item.ID,
					ToolName:         openAIProviderToolDisplayName(s.tools, "openai.file_search", "file_search"),
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
			})
			return s.Next()
		case "image_generation_call":
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               e.Item.ID,
					ToolName:         openAIProviderToolDisplayName(s.tools, "openai.image_generation", "image_generation"),
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
			})
			return s.Next()
		case "code_interpreter_call":
			codeInterpreterName := openAIProviderToolDisplayName(s.tools, "openai.code_interpreter", "code_interpreter")
			s.ongoingToolCalls[e.OutputIndex] = &responsesOngoingToolCall{
				toolName:        codeInterpreterName,
				toolCallID:      e.Item.ID,
				codeInterpreter: &codeInterpreterStreamState{containerID: e.Item.ContainerID},
			}
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:               e.Item.ID,
						ToolName:         codeInterpreterName,
						ProviderExecuted: true,
					},
				},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   e.Item.ID,
					Text: `{"containerId":"` + e.Item.ContainerID + `","code":"`,
				},
			)
			return s.Next()
		case "tool_search_call":
			execution := e.Item.Execution
			if execution == "" {
				execution = "server"
			}
			s.ongoingToolCalls[e.OutputIndex] = &responsesOngoingToolCall{
				toolName:            s.toolSearchToolName,
				toolCallID:          e.Item.ID,
				toolSearchExecution: execution,
			}
			if execution == "server" {
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:               e.Item.ID,
						ToolName:         s.toolSearchToolName,
						ProviderExecuted: true,
					},
				})
			}
			return s.Next()
		case "custom_tool_call":
			s.ongoingToolCalls[e.OutputIndex] = &responsesOngoingToolCall{
				toolName:   e.Item.Name,
				toolCallID: e.Item.CallID,
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{
					ID:       e.Item.CallID,
					ToolName: e.Item.Name,
				},
			})
			return s.Next()
		case "apply_patch_call":
			// Row 45f2b6a / item 9: progressive tool-input streaming, mirroring
			// TS's ongoingToolCalls[output_index].applyPatch tracking.
			callID := e.Item.CallID
			opType, opPath := "", ""
			if e.Item.Operation != nil {
				opType = e.Item.Operation.Type
				opPath = e.Item.Operation.Path
			}
			isDelete := opType == "delete_file"
			s.ongoingToolCalls[e.OutputIndex] = &responsesOngoingToolCall{
				toolName:   "openai.apply_patch",
				toolCallID: callID,
				applyPatch: &applyPatchStreamState{hasDiff: isDelete, endEmitted: isDelete},
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{
					ID:       callID,
					ToolName: "openai.apply_patch",
				},
			})
			if isDelete {
				inputStr, _ := json.Marshal(map[string]interface{}{
					"callId":    callID,
					"operation": map[string]interface{}{"type": opType, "path": opPath},
				})
				s.flushQueue = append(s.flushQueue,
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: string(inputStr)},
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: callID},
				)
			} else {
				prefix := `{"callId":"` + escapeJSONDelta(callID) + `","operation":{"type":"` + escapeJSONDelta(opType) + `","path":"` + escapeJSONDelta(opPath) + `","diff":"`
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: prefix})
			}
		}
		return s.Next()

	case "response.output_text.delta":
		var e responses.OutputTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		if e.Delta == "" {
			return s.Next()
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeText,
			ID:   s.firstItemIDByOutputIndex[e.OutputIndex],
			Text: e.Delta,
		})

	case "response.function_call_arguments.delta":
		var e responses.FunctionCallArgumentsDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if accum, ok := s.toolAccum[e.OutputIndex]; ok {
			accum.arguments += e.Delta
		}
		return s.Next()

	case "response.reasoning_summary_text.delta":
		var e responses.ReasoningSummaryTextDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		// TS unconditionally enqueues reasoning-delta here, even for an
		// empty-string delta -- no guard.
		itemID := s.resolveOutputItemID(e.OutputIndex, e.ItemID)
		meta, _ := json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"itemId": itemID}})
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeReasoning,
			ID:               fmt.Sprintf("%s:%d", itemID, e.SummaryIndex),
			Reasoning:        e.Delta,
			ProviderMetadata: meta,
		})

	case "response.reasoning_summary_part.added":
		var e responses.ReasoningSummaryPartAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		// The first summary part's reasoning-start was already emitted from
		// output_item.added; only summary_index > 0 needs boundary handling
		// here (row a0d2e8c/6fe187f area).
		if e.SummaryIndex > 0 {
			itemID := s.resolveOutputItemID(e.OutputIndex, e.ItemID)
			if accum := s.reasoningAccum[itemID]; accum != nil {
				accum.summaryParts[e.SummaryIndex] = "active"
				// A new active summary part means every "can-conclude" part
				// (its own .done arrived under store=false, waiting for the
				// final encrypted_content) can now be concluded. Close them
				// in ascending index order, matching TS's Object.keys
				// (ascending for numeric-string keys).
				var canConclude []int
				for idx, status := range accum.summaryParts {
					if status == "can-conclude" {
						canConclude = append(canConclude, idx)
					}
				}
				sort.Ints(canConclude)
				for _, idx := range canConclude {
					endMeta, _ := json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"itemId": itemID}})
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
						Type:             provider.ChunkTypeReasoningEnd,
						ID:               fmt.Sprintf("%s:%d", itemID, idx),
						ProviderMetadata: endMeta,
					})
					accum.summaryParts[idx] = "concluded"
				}
				startMeta, _ := json.Marshal(map[string]interface{}{
					s.providerName: map[string]interface{}{
						"itemId":                    itemID,
						"reasoningEncryptedContent": nonEmptyOrNil(accum.encryptedContent),
					},
				})
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:             provider.ChunkTypeReasoningStart,
					ID:               fmt.Sprintf("%s:%d", itemID, e.SummaryIndex),
					ProviderMetadata: startMeta,
				})
			}
		}
		return s.Next()

	case "response.reasoning_summary_part.done":
		var e responses.ReasoningSummaryPartDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		itemID := s.resolveOutputItemID(e.OutputIndex, e.ItemID)
		if accum := s.reasoningAccum[itemID]; accum != nil {
			if s.store {
				// The response is stored server-side, so no encrypted_content
				// needs to be attached: the reasoning block can close now.
				meta, _ := json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"itemId": itemID}})
				accum.summaryParts[e.SummaryIndex] = "concluded"
				return s.emitParsedChunk(&provider.StreamChunk{
					Type:             provider.ChunkTypeReasoningEnd,
					ID:               fmt.Sprintf("%s:%d", itemID, e.SummaryIndex),
					ProviderMetadata: meta,
				})
			}
			// store=false: keep the block open until output_item.done, which
			// carries the final encrypted_content.
			accum.summaryParts[e.SummaryIndex] = "can-conclude"
		}
		return s.Next()

	case "response.output_text.annotation.added":
		var e responses.OutputTextAnnotationAddedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		// TS: `ongoingAnnotations.push(value.annotation)` -- surfaced on the
		// owning message item's text-end providerMetadata.openai.annotations.
		s.ongoingAnnotations = append(s.ongoingAnnotations, e.Annotation)
		if src, ok := openAIAnnotationToSource(s.providerName, e.Annotation); ok {
			return s.emitParsedChunk(&provider.StreamChunk{
				Type:          provider.ChunkTypeSource,
				SourceContent: &src,
			})
		}
		return s.Next()

	case "response.custom_tool_call_input.delta":
		var e responses.CustomToolCallInputDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok {
			return s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolInputDelta,
				ID:   toolCall.toolCallID,
				Text: e.Delta,
			})
		}
		return s.Next()

	case "response.apply_patch_call_operation_diff.delta":
		var e responses.ApplyPatchCallOperationDiffDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok && toolCall.applyPatch != nil {
			toolCall.applyPatch.hasDiff = true
			return s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolInputDelta,
				ID:   toolCall.toolCallID,
				Text: escapeJSONDelta(e.Delta),
			})
		}
		return s.Next()

	case "response.apply_patch_call_operation_diff.done":
		var e responses.ApplyPatchCallOperationDiffDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok && toolCall.applyPatch != nil && !toolCall.applyPatch.endEmitted {
			if !toolCall.applyPatch.hasDiff {
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   toolCall.toolCallID,
					Text: escapeJSONDelta(e.Diff),
				})
				toolCall.applyPatch.hasDiff = true
			}
			toolCall.applyPatch.endEmitted = true
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: toolCall.toolCallID, Text: `"}}`},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: toolCall.toolCallID},
			)
		}
		return s.Next()

	case "response.image_generation_call.partial_image":
		var e responses.ImageGenerationPartialImageEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID:  e.ItemID,
				ToolName:    openAIProviderToolDisplayName(s.tools, "openai.image_generation", "image_generation"),
				Result:      map[string]interface{}{"result": e.PartialImageB64},
				Preliminary: true,
			},
		})

	case "response.code_interpreter_call_code.delta":
		var e responses.CodeInterpreterCallCodeDeltaEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok {
			return s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolInputDelta,
				ID:   toolCall.toolCallID,
				Text: escapeJSONDelta(e.Delta),
			})
		}
		return s.Next()

	case "response.code_interpreter_call_code.done":
		var e responses.CodeInterpreterCallCodeDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok {
			delete(s.ongoingToolCalls, e.OutputIndex)
			containerID := ""
			if toolCall.codeInterpreter != nil {
				containerID = toolCall.codeInterpreter.containerID
			}
			inputStr, _ := json.Marshal(map[string]interface{}{"code": e.Code, "containerId": containerID})
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: toolCall.toolCallID, Text: `"}`},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: toolCall.toolCallID},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               toolCall.toolCallID,
						ToolName:         toolCall.toolName,
						RawArguments:     string(inputStr),
						ProviderExecuted: true,
					},
				},
			)
		}
		return s.Next()

	case "response.output_item.done":
		var e responses.OutputItemDoneEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}
		return s.handleOutputItemDone(e)

	case "response.completed", "response.incomplete":
		var e responses.ResponseCompletedEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.emitDecodeError(peek.Type, err)
		}
		usage := convertResponsesUsage(e.Response.Usage)
		finishReason := mapResponsesFinishReason(e.Response.IncompleteDetails, false)
		// Row eee6200: an earlier known-event decode failure forces the
		// finish reason to "error", regardless of what this event reports.
		if s.hadDecodeError {
			finishReason = types.FinishReasonError
		}
		s.markOutputStarted()
		if eventRawChunk != nil {
			s.flushQueue = append(s.flushQueue, eventRawChunk)
		}

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
		if !s.outputStarted && e.Response.Error != nil {
			s.flushQueue = nil
			s.pendingRaw = nil
			s.pendingMetadata = nil
			s.err = newOpenAIStreamProviderError(s.providerName, json.RawMessage(event.Data), s.responseHeaders)
			return nil, s.err
		}
		usage := convertResponsesUsage(e.Response.Usage)
		finishReason := types.FinishReason("error")
		rawFinishReason := "error"
		if e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason != "" {
			finishReason = mapResponsesFinishReason(e.Response.IncompleteDetails, false)
			rawFinishReason = e.Response.IncompleteDetails.Reason
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
			RawFinishReason:  rawFinishReason,
			Usage:            &usage,
			ProviderMetadata: meta,
		})

	case "error":
		var e responses.ResponsesStreamErrorEvent
		if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
			return s.Next()
		}
		if !s.outputStarted {
			s.flushQueue = nil
			s.pendingRaw = nil
			s.pendingMetadata = nil
			s.err = newOpenAIStreamProviderError(s.providerName, json.RawMessage(event.Data), s.responseHeaders)
			return nil, s.err
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

func (s *responsesStream) markOutputStarted() {
	if s.outputStarted {
		return
	}
	s.outputStarted = true
	if len(s.pendingRaw) > 0 {
		s.flushQueue = append(s.flushQueue, s.pendingRaw...)
		s.pendingRaw = nil
	}
	if s.pendingMetadata != nil {
		s.flushQueue = append(s.flushQueue, s.pendingMetadata)
		s.pendingMetadata = nil
	}
}

func (s *responsesStream) queueRawChunk(data string) {
	if s.includeRawChunks {
		s.flushQueue = append(s.flushQueue, openAIResponsesRawChunk(data))
	}
}

func openAIResponsesRawChunk(data string) *provider.StreamChunk {
	var raw interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		raw = data
	}
	return &provider.StreamChunk{
		Type: provider.ChunkTypeRaw,
		Raw:  raw,
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
		rawArguments := accum.arguments
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
				rawArguments = item.Arguments
			}
		}
		// Row 6be0f51: expand an internal "parallel" wrapper call into one
		// tool-call chunk per nested recipient, when every recipient names a
		// declared function tool.
		if expanded, ok := responses.ExpandParallelToolCall(accum.id, accum.name, rawArguments, accum.itemID, s.tools, s.providerName); ok {
			for _, tc := range expanded {
				tc := tc
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:     provider.ChunkTypeToolCall,
					ToolCall: &tc,
				})
			}
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               accum.id,
				ToolName:         accum.name,
				Arguments:        args,
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, accum.itemID, accum.namespace, item.Async, item.Caller),
			},
		})

	case "compaction":
		var item responses.CompactionEvent
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		chunk := responses.CompactionEventToChunk(item)
		return s.emitParsedChunk(chunk)

	case "message":
		// text-end carries the accumulated annotations from every
		// response.output_text.annotation.added event seen for this item
		// (TS output_item.done "message" case: ongoingAnnotations).
		delete(s.itemTypes, e.OutputIndex)
		var item struct {
			ID    string  `json:"id,omitempty"`
			Phase *string `json:"phase,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		itemID := s.firstItemIDByOutputIndex[e.OutputIndex]
		if itemID == "" {
			itemID = item.ID
		}
		delete(s.firstItemIDByOutputIndex, e.OutputIndex)
		annotations := s.ongoingAnnotations
		s.ongoingAnnotations = nil
		var meta json.RawMessage
		if m := openAIResponsesMessageProviderOptions(s.providerName, itemID, item.Phase, annotations); m != nil {
			meta, _ = json.Marshal(m)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeTextEnd,
			ID:               itemID,
			ProviderMetadata: meta,
		})

	case "reasoning":
		// Reasoning text was already emitted as ChunkTypeReasoning deltas.
		// Forward encrypted_content so callers can round-trip it for multi-turn
		// reasoning when store=false.
		// Row 73d48d0: use the id first seen for this output_index (at
		// output_item.added) rather than this done event's own id, in case
		// OpenAI rotated the item id mid-stream.
		firstID := s.firstItemIDByOutputIndex[e.OutputIndex]
		delete(s.itemTypes, e.OutputIndex)
		delete(s.firstItemIDByOutputIndex, e.OutputIndex)
		var item struct {
			ID               string `json:"id,omitempty"`
			EncryptedContent string `json:"encrypted_content,omitempty"`
		}
		if err := json.Unmarshal(e.Item, &item); err != nil || (item.ID == "" && item.EncryptedContent == "") {
			return s.Next()
		}
		id := firstID
		if id == "" {
			id = item.ID
		}
		accum := s.reasoningAccum[id]
		delete(s.reasoningAccum, id)

		// TS always sets reasoningEncryptedContent (falling back to `null`),
		// not only when non-empty -- matches the key name/shape used at the
		// other two reasoning-end sites in this file (output_item.added and
		// reasoning_summary_part.added).
		meta := map[string]interface{}{"reasoningEncryptedContent": nonEmptyOrNil(item.EncryptedContent)}
		if id != "" {
			meta["itemId"] = id
		}
		providerMeta, _ := json.Marshal(map[string]interface{}{s.providerName: meta})

		// Row 3b9f025/6fe187f: close every summary part that is still open
		// (active, i.e. never got its own response.reasoning_summary_part.done,
		// or can-conclude, i.e. store=false and was waiting on this event's
		// encrypted_content) with the same final metadata.
		var indices []int
		if accum != nil {
			for idx, status := range accum.summaryParts {
				if status == "active" || status == "can-conclude" {
					indices = append(indices, idx)
				}
			}
		} else {
			indices = []int{0}
		}
		sort.Ints(indices)
		if len(indices) == 0 {
			return s.Next()
		}
		for _, idx := range indices[:len(indices)-1] {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeReasoningEnd,
				ID:               fmt.Sprintf("%s:%d", id, idx),
				ProviderMetadata: providerMeta,
			})
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeReasoningEnd,
			ID:               fmt.Sprintf("%s:%d", id, indices[len(indices)-1]),
			ProviderMetadata: providerMeta,
		})

	case "apply_patch_call":
		// Row 45f2b6a / item 9: close out progressive input streaming (as a
		// safety net, in case the dedicated diff.delta/.done events didn't
		// already do so) before emitting the final tool-call.
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ApplyPatchCall
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.emitDecodeError("apply_patch_call", err)
		}
		toolCall := s.ongoingToolCalls[e.OutputIndex]
		delete(s.ongoingToolCalls, e.OutputIndex)
		if toolCall != nil && toolCall.applyPatch != nil && !toolCall.applyPatch.endEmitted && item.Operation.Type != "delete_file" {
			if !toolCall.applyPatch.hasDiff {
				diff := ""
				if item.Operation.Diff != nil {
					diff = *item.Operation.Diff
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta, ID: toolCall.toolCallID, Text: escapeJSONDelta(diff),
				})
			}
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: toolCall.toolCallID, Text: `"}}`},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: toolCall.toolCallID},
			)
		}
		rawArgs, _ := json.Marshal(map[string]interface{}{
			"callId":    item.CallID,
			"operation": item.Operation,
		})
		var args map[string]interface{}
		json.Unmarshal(rawArgs, &args) //nolint:errcheck
		itemID := ""
		if item.ID != nil {
			itemID = *item.ID
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.CallID,
				ToolName:         "openai.apply_patch",
				Arguments:        args,
				RawArguments:     string(rawArgs),
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, itemID, "", nil, nil),
			},
		})

	case "computer_call":
		// Row 0063c2d.
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ComputerCall
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.emitDecodeError("computer_call", err)
		}
		computerItemID := ""
		if item.ID != nil {
			computerItemID = *item.ID
		}
		if item.CallID == nil {
			// A null call_id means this call is fully server-executed with
			// no client round-trip: emit the immediate "computer_use"
			// tool-input-end/tool-call/tool-result sequence TS produces,
			// instead of a client-executable "computer" tool call.
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputEnd,
					ToolCall: &types.ToolCall{
						ID:               computerItemID,
						ToolName:         "openai.computer_use",
						ProviderExecuted: true,
					},
				},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               computerItemID,
						ToolName:         "openai.computer_use",
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
				},
			)
			return s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: computerItemID,
					ToolName:   "openai.computer_use",
					Result:     map[string]interface{}{"type": "computer_use_tool_result", "status": item.Status},
				},
			})
		}
		computerArgs, computerRawArgs := computerCallArguments(item)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               *item.CallID,
				ToolName:         "openai.computer",
				Arguments:        computerArgs,
				RawArguments:     computerRawArgs,
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, computerItemID, "", nil, nil),
			},
		})

	case "custom_tool_call":
		delete(s.itemTypes, e.OutputIndex)
		delete(s.ongoingToolCalls, e.OutputIndex)
		var item responses.CustomToolCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputEnd,
			ToolCall: &types.ToolCall{ID: item.CallID},
		})
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.CallID,
				ToolName:         item.Name,
				Arguments:        map[string]interface{}{"input": item.Input},
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, item.ID, "", item.Async, item.Caller),
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

	case "program":
		// Row 1f6dd3a: the hosted programmatic-tool-calling sandbox generated
		// and is executing this JavaScript program; the result arrives as a
		// separate "program_output" item's own output_item.done event.
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ProgramItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		args, rawArgs := programCallArguments(item)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.CallID,
				ToolName:         "openai.programmatic_tool_calling",
				Arguments:        args,
				RawArguments:     rawArgs,
				ProviderExecuted: true,
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, item.ID, "", nil, nil),
			},
		})

	case "program_output":
		// Row 1f6dd3a: the result of a "program" item's hosted execution.
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ProgramOutputItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.CallID,
				ToolName:   "openai.programmatic_tool_calling",
				Result:     map[string]interface{}{"result": item.Result, "status": item.Status},
			},
		})

	case "image_generation_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ImageGenerationCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.ID,
				ToolName:   openAIProviderToolDisplayName(s.tools, "openai.image_generation", "image_generation"),
				Result:     map[string]interface{}{"result": item.Result},
			},
		})

	case "file_search_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.FileSearchCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		var results interface{}
		if item.Results != nil {
			mapped := make([]map[string]interface{}, 0, len(item.Results))
			for _, r := range item.Results {
				mapped = append(mapped, map[string]interface{}{
					"attributes": r.Attributes,
					"fileId":     r.FileID,
					"filename":   r.Filename,
					"score":      r.Score,
					"text":       r.Text,
				})
			}
			results = mapped
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.ID,
				ToolName:   openAIProviderToolDisplayName(s.tools, "openai.file_search", "file_search"),
				Result:     map[string]interface{}{"queries": item.Queries, "results": results},
			},
		})

	case "code_interpreter_call":
		// The tool-call itself was already emitted by the
		// response.code_interpreter_call_code.done handler; this only
		// carries the final outputs.
		delete(s.itemTypes, e.OutputIndex)
		codeInterpreterName := openAIProviderToolDisplayName(s.tools, "openai.code_interpreter", "code_interpreter")
		if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok && toolCall.toolName != "" {
			codeInterpreterName = toolCall.toolName
		}
		delete(s.ongoingToolCalls, e.OutputIndex)
		var item responses.CodeInterpreterCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		outputs := make([]map[string]interface{}, 0, len(item.Outputs))
		for _, o := range item.Outputs {
			switch o.Type {
			case "logs":
				outputs = append(outputs, map[string]interface{}{"type": "logs", "logs": o.Logs})
			case "image":
				outputs = append(outputs, map[string]interface{}{"type": "image", "url": o.URL})
			}
		}
		var outputsValue interface{}
		if item.Outputs != nil {
			outputsValue = outputs
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.ID,
				ToolName:   codeInterpreterName,
				Result:     map[string]interface{}{"outputs": outputsValue},
			},
		})

	case "tool_search_call":
		delete(s.itemTypes, e.OutputIndex)
		toolCall := s.ongoingToolCalls[e.OutputIndex]
		delete(s.ongoingToolCalls, e.OutputIndex)
		var item responses.ToolSearchCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		if toolCall == nil {
			return s.Next()
		}
		isHosted := item.Execution == "server"
		toolCallID := toolCall.toolCallID
		if isHosted {
			s.hostedToolSearchIDs = append(s.hostedToolSearchIDs, toolCallID)
		} else if item.CallID != nil && *item.CallID != "" {
			toolCallID = *item.CallID
		} else {
			toolCallID = item.ID
		}
		if !isHosted {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:     provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{ID: toolCallID, ToolName: toolCall.toolName},
			})
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputEnd,
			ToolCall: &types.ToolCall{ID: toolCallID},
		})
		var callIDForInput interface{}
		if isHosted {
			callIDForInput = nil
		} else {
			callIDForInput = toolCallID
		}
		inputRaw, _ := json.Marshal(map[string]interface{}{"arguments": item.Arguments, "call_id": callIDForInput})
		tc := types.ToolCall{
			ID:               toolCallID,
			ToolName:         toolCall.toolName,
			RawArguments:     string(inputRaw),
			ProviderExecuted: isHosted,
			ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, item.ID, "", nil, nil),
		}
		json.Unmarshal(inputRaw, &tc.Arguments) //nolint:errcheck
		return s.emitParsedChunk(&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &tc})

	case "tool_search_output":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ToolSearchOutputItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		toolCallID := item.ID
		if item.CallID != nil && *item.CallID != "" {
			toolCallID = *item.CallID
		} else if len(s.hostedToolSearchIDs) > 0 {
			toolCallID = s.hostedToolSearchIDs[0]
			s.hostedToolSearchIDs = s.hostedToolSearchIDs[1:]
		}
		matchedTools := make([]interface{}, 0, len(item.Tools))
		for _, t := range item.Tools {
			var v interface{}
			json.Unmarshal(t, &v) //nolint:errcheck
			matchedTools = append(matchedTools, v)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID:       toolCallID,
				ToolName:         s.toolSearchToolName,
				Result:           map[string]interface{}{"tools": matchedTools},
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, item.ID, "", nil, nil),
			},
		})

	case "mcp_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.McpCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		toolCallID := item.ID
		if item.ApprovalRequestID != nil {
			// Matches TS precedence: check this stream's own alias map
			// first (an mcp_approval_request seen earlier in the same
			// stream), then the prompt-derived map (approved in a
			// previous turn), then fall back to the item's own id.
			if alias, ok := s.mcpApprovalAlias[*item.ApprovalRequestID]; ok {
				toolCallID = alias
			} else if alias, ok := s.approvalFromPrompt[*item.ApprovalRequestID]; ok {
				toolCallID = alias
			}
		}
		toolName := "mcp." + item.Name
		var args map[string]interface{}
		json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
		mcpResult := map[string]interface{}{
			"type":        "call",
			"serverLabel": item.ServerLabel,
			"name":        item.Name,
			"arguments":   item.Arguments,
		}
		if item.Output != nil {
			mcpResult["output"] = *item.Output
		}
		if item.Error != nil {
			mcpResult["error"] = item.Error.AsJSONValue()
		}
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               toolCallID,
				ToolName:         toolName,
				Arguments:        args,
				RawArguments:     item.Arguments,
				ProviderExecuted: true,
				Dynamic:          true,
			},
		})
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID:       toolCallID,
				ToolName:         toolName,
				Result:           mcpResult,
				ProviderMetadata: openAIResponsesToolCallMetadata(s.providerName, item.ID, "", nil, nil),
			},
		})

	case "mcp_list_tools":
		// TS: skipped entirely -- not exposed to the caller or replayed.
		delete(s.itemTypes, e.OutputIndex)
		return s.Next()

	case "mcp_approval_request":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.McpApprovalRequestItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return s.Next()
		}
		approvalRequestID := item.ID
		if item.ApprovalRequestID != nil && *item.ApprovalRequestID != "" {
			approvalRequestID = *item.ApprovalRequestID
		}
		dummyToolCallID := streaming.GenerateID()
		if s.mcpApprovalAlias == nil {
			s.mcpApprovalAlias = map[string]string{}
		}
		s.mcpApprovalAlias[approvalRequestID] = dummyToolCallID
		toolName := "mcp." + item.Name
		var args map[string]interface{}
		json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               dummyToolCallID,
				ToolName:         toolName,
				Arguments:        args,
				RawArguments:     item.Arguments,
				ProviderExecuted: true,
				Dynamic:          true,
			},
		})
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolApprovalRequest,
			ToolApprovalRequest: &types.ToolApprovalRequestContent{
				ApprovalID: approvalRequestID,
				ToolCallID: dummyToolCallID,
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
