package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// requestModelID, when set, is sent as `model` instead of modelID (TS
	// config.requestModelId; Azure reports the model ID but routes by deployment).
	requestModelID string
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

// WithRequestModelID sets the model ID sent to the API, keeping ModelID() (and
// capability detection) on the reported model ID. It returns m for chaining.
func (m *ResponsesLanguageModel) WithRequestModelID(id string) *ResponsesLanguageModel {
	m.requestModelID = id
	return m
}

func (m *ResponsesLanguageModel) requestModel() string {
	if m.requestModelID != "" {
		return m.requestModelID
	}
	return m.modelID
}

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
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Query:   m.provider.responsesQuery(),
		Body:    body,
		Headers: opts.Headers,
		// TS createJsonResponseHandler(openaiResponsesResponseSchema).
		ValidateResponse: providerutils.ResponsesResponseValidator(m.Provider()),
	}, &resp)
	if err != nil {
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
	if responsesLogprobsRequested(opts.ProviderOptions, m.provider.responsesProviderOptionsName()) {
		if logprobs := responsesOutputLogprobs(resp.Output); len(logprobs) > 0 {
			if pm, ok := result.ProviderMetadata[m.provider.responsesProviderOptionsName()].(map[string]interface{}); ok {
				pm["logprobs"] = logprobs
			}
		}
	}
	result.RawRequest = body
	var rawResponse interface{}
	if json.Unmarshal(httpResp.Body, &rawResponse) == nil {
		result.RawResponse = rawResponse
	}
	result.ResponseHeaders = providerutils.ExtractHeaders(httpResp.Headers)
	result.ResponseMetadata = providerutils.BuildResponseMetadata(resp.ID, resp.Model, resp.CreatedAt)
	result.ResponseMetadata.Headers = result.ResponseHeaders
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
	stream.requestBody = body
	stream.tools = opts.Tools
	stream.logprobsEnabled = responsesLogprobsRequested(opts.ProviderOptions, m.provider.responsesProviderOptionsName())
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
				if err := validateResponsesPromptCacheOptions(v, providerOptionsName); err != nil {
					return nil, store, nil, err
				}
				// zod's object schema drops unknown keys before TS sends it.
				if m, isMap := v.(map[string]interface{}); isMap {
					known := make(map[string]interface{}, len(m))
					for _, k := range []string{"mode", "ttl", "prewarm"} {
						if val, has := m[k]; has {
							known[k] = val
						}
					}
					v = known
				}
				promptCacheOptions = v
			}
			if v, ok := openaiOpts["promptCacheKey"].(string); ok {
				promptCacheKey = v
			}
			if v, ok := openaiOpts["reasoningEffort"].(string); ok {
				reasoningEffort = v
			}
			if v, ok := openaiOpts["reasoningEffortUpdate"].(string); ok {
				// TS validates reasoningEffortUpdate against a fixed schema
				// enum (z.enum(['none','low','medium','high','xhigh','max']),
				// row 94d5d6d3e6) independently of, and prior to, the
				// per-model SupportedReasoningEfforts check below -- an
				// out-of-enum value like "minimal" is rejected the same way
				// on every model, including ones (like gpt-6-luna) that
				// support "none". Go has no schema layer, so validate
				// manually here, before any model-specific handling.
				if !slices.Contains(responses.ValidReasoningEffortUpdateValues, v) {
					return nil, store, nil, &providererrors.InvalidArgumentError{
						Field:   "providerOptions." + providerOptionsName + ".reasoningEffortUpdate",
						Message: fmt.Sprintf("must be one of %s", strings.Join(responses.ValidReasoningEffortUpdateValues, ", ")),
					}
				}
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

	// configurationUpdateUnsupportedReason mirrors TS
	// getConfigurationUpdateUnsupportedReason: the reason (if any)
	// reasoningEffortUpdate configuration_update items -- whether positioned
	// mid-conversation via a message-level providerOptions.openai.
	// reasoningEffortUpdate, or prepended via the request-level option below
	// -- cannot be emitted for this request. Computed once and applied
	// uniformly to both.
	configurationUpdateUnsupportedReason := ""
	if !modelCapabilities.SupportsConfigurationUpdate {
		configurationUpdateUnsupportedReason = "reasoningEffortUpdate is only supported by GPT-6 and later models"
	} else if reasoningMode == "pro" || contextManagementExplicit || truncation == "auto" {
		configurationUpdateUnsupportedReason = "reasoningEffortUpdate requires standard reasoning mode without automatic compaction or automatic truncation"
	}

	// getUpdateEffortUnsupportedReason mirrors TS: the schema accepts update
	// efforts supported by any supported model, so check whether this
	// specific model supports the requested effort.
	getUpdateEffortUnsupportedReason := func(effort string) string {
		if effort != "" && modelCapabilities.SupportedReasoningEfforts != nil &&
			!slices.Contains(modelCapabilities.SupportedReasoningEfforts, effort) {
			return fmt.Sprintf("%s only supports the following reasoning efforts: %s", m.modelID, strings.Join(modelCapabilities.SupportedReasoningEfforts, ", "))
		}
		return ""
	}

	isReasoning := isReasoningModel(m.modelID)
	if forceReasoning != nil {
		isReasoning = *forceReasoning
	}

	// TS: resolvedReasoningEffort = openaiOptions.reasoningEffort ??
	// (isCustomReasoning(reasoning) ? reasoning : undefined), resolved once so
	// the top-level `reasoning` value goes through the same GPT-6+ check.
	reasoningEffortOption := reasoningEffort
	if reasoningEffort == "" && opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		reasoningEffort = string(*opts.Reasoning)
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
	includeWebSearchSources := m.provider.config.SupportsWebSearchSourcesInclude == nil || *m.provider.config.SupportsWebSearchSourcesInclude
	if v, ok := openaiOpts["includeWebSearchSources"].(bool); ok && !v {
		includeWebSearchSources = false
	}

	// Convert prompt to Responses API input format.
	input, inputWarnings, err := responses.ConvertPromptToInputWithOptions(opts.Prompt, systemMsgMode, responses.ConvertOptions{
		PassThroughUnsupportedFiles:          passThroughUnsupportedFiles,
		HasPreviousResponseID:                previousResponseID != "",
		HasConversation:                      conversation != "",
		Store:                                store,
		CustomToolNames:                      customToolNames(opts.Tools),
		HasLocalShellTool:                    hasTool(opts.Tools, "openai.local_shell"),
		HasShellTool:                         hasTool(opts.Tools, "openai.shell"),
		HasApplyPatchTool:                    hasTool(opts.Tools, "openai.apply_patch"),
		HasComputerTool:                      hasTool(opts.Tools, "openai.computer"),
		FileIDPrefixes:                       m.provider.responsesFileIDPrefixes(),
		ProviderOptionsName:                  providerOptionsName,
		ToolSearchToolName:                   toolSearchToolName(opts.Tools),
		ProviderToolNames:                    openAIProviderToolNames(opts.Tools),
		OutputSchemaToolNames:                outputSchemaToolNames(responses.ScopeFunctionToolOptions(opts.Tools, providerOptionsName)),
		ExplicitMessageItemType:              m.provider.explicitMessageItemType(),
		IncludePromptCacheBreakpoint:         true,
		ConfigurationUpdateUnsupportedReason: configurationUpdateUnsupportedReason,
		IsReasoningModel:                     isReasoning,
		WebSearchToolName:                    responsesWebSearchToolName(opts.Tools),
	})
	if err != nil {
		return nil, store, warnings, err
	}
	warnings = append(warnings, inputWarnings...)

	// Reject configuration updates (positioned by message-level
	// reasoningEffortUpdate above) whose effort value is unsupported by the
	// selected model.
	for _, item := range input {
		m0, ok := item.(map[string]interface{})
		if !ok || m0["type"] != "configuration_update" {
			continue
		}
		reasoning, _ := m0["reasoning"].(map[string]interface{})
		effort, _ := reasoning["effort"].(string)
		if unsupportedReason := getUpdateEffortUnsupportedReason(effort); unsupportedReason != "" {
			return nil, store, warnings, &providererrors.UnsupportedFunctionalityError{
				Functionality: "Message-level reasoningEffortUpdate",
				Message:       unsupportedReason,
			}
		}
	}

	// reasoningEffortUpdate (GPT-6+): prepend a configuration_update item so
	// the model's reasoning effort can change mid-conversation without a new
	// response chain. Requires standard reasoning mode (no auto-compaction,
	// no auto-truncation) and a model that supports the requested effort;
	// either failing is a warning (not an error) for the request-level
	// option, unlike the message-level one above.
	if reasoningEffortUpdate != "" {
		requestUpdateUnsupportedReason := configurationUpdateUnsupportedReason
		if requestUpdateUnsupportedReason == "" {
			requestUpdateUnsupportedReason = getUpdateEffortUnsupportedReason(reasoningEffortUpdate)
		}
		if requestUpdateUnsupportedReason != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoningEffortUpdate",
				Details: requestUpdateUnsupportedReason,
			})
		} else {
			// If the first item already sets this effort (e.g. a
			// message-level update positioned at the start of history),
			// prepending another update would create an adjacent pair that
			// OpenAI rejects -- skip the duplicate instead.
			firstIsSameUpdate := false
			if len(input) > 0 {
				if first, ok := input[0].(map[string]interface{}); ok && first["type"] == "configuration_update" {
					if reasoning, ok := first["reasoning"].(map[string]interface{}); ok {
						if effort, _ := reasoning["effort"].(string); effort == reasoningEffortUpdate {
							firstIsSameUpdate = true
						}
					}
				}
			}
			if !firstIsSameUpdate {
				input = append([]interface{}{map[string]interface{}{
					"type":      "configuration_update",
					"reasoning": map[string]interface{}{"effort": reasoningEffortUpdate},
				}}, input...)
			}
		}
	}

	// Conversion and prepending can make updates adjacent, so check
	// afterward to catch combinations that OpenAI would reject.
	for i := 1; i < len(input); i++ {
		prev, ok1 := input[i-1].(map[string]interface{})
		curr, ok2 := input[i].(map[string]interface{})
		if ok1 && ok2 && prev["type"] == "configuration_update" && curr["type"] == "configuration_update" {
			return nil, store, warnings, &providererrors.UnsupportedFunctionalityError{
				Functionality: "Adjacent reasoning effort configuration updates",
			}
		}
	}

	// A compaction trigger is a request control, not conversation history.
	// OpenAI requires it to be the final input item, so append it only
	// after the complete prompt (and any configuration updates) has been
	// assembled.
	if v, ok := openaiOpts["compactionTrigger"].(bool); ok && v {
		input = append(input, map[string]interface{}{"type": "compaction_trigger"})
	}

	body := map[string]interface{}{
		"model": m.requestModel(),
		"input": input,
	}
	if stream {
		body["stream"] = true
	}

	effort := reasoningEffort
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
		if reasoningEffortOption != "" {
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

	// Build the include list in TS order (user include, logprobs, web search
	// sources, code interpreter outputs, then reasoning.encrypted_content when
	// store=false on a reasoning model so multi-turn works without server-side
	// persistence).
	if responsesTopLogprobsIncluded(topLogprobs) {
		includeFields = appendUnique(includeFields, "message.output_text.logprobs")
	}
	if includeWebSearchSources && (hasTool(opts.Tools, "openai.web_search") || hasTool(opts.Tools, "openai.web_search_preview")) {
		includeFields = appendUnique(includeFields, "web_search_call.action.sources")
	}
	if hasTool(opts.Tools, "openai.code_interpreter") {
		includeFields = appendUnique(includeFields, "code_interpreter_call.outputs")
	}
	if !store && isReasoning {
		includeFields = appendUnique(includeFields, "reasoning.encrypted_content")
	}

	// Temperature and top_p: forbidden for reasoning models except GPT-5.1 and
	// later families when reasoning effort is disabled.
	// Input configuration updates change the effort used to validate sampling
	// parameters (TS #22304).
	effectiveEffort := effort
	for _, item := range input {
		if m0, ok := item.(map[string]interface{}); ok && m0["type"] == "configuration_update" {
			if r, ok := m0["reasoning"].(map[string]interface{}); ok {
				if e, ok := r["effort"].(string); ok {
					effectiveEffort = e
				}
			}
		}
	}
	supportsNonReasoningParams := !isReasoning || (effectiveEffort == "none" && supportsNonReasoningParameters(m.modelID))
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
		if modelCapabilities.SupportedReasoningEfforts != nil && (topLogprobs != nil || sliceHas(includeFields, "message.output_text.logprobs")) {
			topLogprobs = nil
			filtered := includeFields[:0:0]
			for _, v := range includeFields {
				if v != "message.output_text.logprobs" {
					filtered = append(filtered, v)
				}
			}
			includeFields = filtered
			if len(filtered) == 0 {
				// TS: an emptied include list becomes undefined.
				includeExplicit = false
			}
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
				if rawSchema := providerutils.ResponseFormatJSONSchemaOrdered(responseSchema); rawSchema != nil {
					{
						normalizedSchema, schemaWarnings, normErr := NormalizeOpenAISchemaValue(rawSchema)
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
		preparedTools, err := responses.PrepareToolsWithOptionsName(opts.Tools, providerOptionsName)
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
			allowedTools, allowedToolWarnings, err := responses.ResolveAllowedToolsWithOptionsName(opts.Tools, allowedToolNames, allowedToolsMode, providerOptionsName)
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

// responsesTopLogprobs mirrors TS: a number is sent verbatim as top_logprobs
// (including 0), true maps to TOP_LOGPROBS_MAX (20). Only a non-zero value
// also adds the message.output_text.logprobs include (see
// responsesTopLogprobsIncluded).
func responsesTopLogprobs(value interface{}) (interface{}, bool) {
	switch v := value.(type) {
	case bool:
		if v {
			return 20, true
		}
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
	return nil, false
}

// responsesTopLogprobsIncluded is TS's `if (topLogprobs)` truthiness check.
func responsesTopLogprobsIncluded(v interface{}) bool {
	n, ok := v.(int)
	return ok && n != 0
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
		case "code_interpreter", "file_search", "image_generation", "web_search_preview", "web_search", "mcp", "apply_patch", "computer", "programmatic_tool_calling":
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
	if fn.Parameters != nil {
		normalized, schemaWarnings, err := NormalizeOpenAISchemaValue(fn.Parameters)
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
		"openai.code_interpreter":          "code_interpreter",
		"openai.file_search":               "file_search",
		"openai.image_generation":          "image_generation",
		"openai.web_search_preview":        "web_search_preview",
		"openai.web_search":                "web_search",
		"openai.mcp":                       "mcp",
		"openai.apply_patch":               "apply_patch",
		"openai.computer":                  "computer",
		"openai.programmatic_tool_calling": "programmatic_tool_calling",
		"code_interpreter":                 "code_interpreter",
		"file_search":                      "file_search",
		"image_generation":                 "image_generation",
		"web_search_preview":               "web_search_preview",
		"web_search":                       "web_search",
		"mcp":                              "mcp",
		"apply_patch":                      "apply_patch",
		"computer":                         "computer",
		"programmatic_tool_calling":        "programmatic_tool_calling",
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

// responsesLogprobsRequested mirrors TS's truthiness check of
// providerOptions[providerOptionsName].logprobs (true or a non-zero number).
func responsesLogprobsRequested(providerOptions map[string]interface{}, providerName string) bool {
	opts, _ := providerOptions[providerName].(map[string]interface{})
	switch v := opts["logprobs"].(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

// openAIProviderToolNames maps each declared OpenAI provider tool's SDK name
// to its bare provider tool name (TS createToolNameMapping providerToolNames).
func openAIProviderToolNames(tools []types.Tool) map[string]string {
	var m map[string]string
	for _, tool := range tools {
		if tool.Name == "" || !strings.HasPrefix(tool.ProviderID, "openai.") {
			continue
		}
		if m == nil {
			m = map[string]string{}
		}
		m[tool.Name] = strings.TrimPrefix(tool.ProviderID, "openai.")
	}
	return m
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
// openAIDeclaredToolName resolves the SDK-facing name of the declared OpenAI
// provider tool with the given id: a tool identified by ProviderID, or by a
// Name equal to the id (the openai.* constructors), reports its own Name (TS
// toolNameMapping.toCustomToolName); an undeclared tool reports the bare
// provider name.
func openAIDeclaredToolName(tools []types.Tool, providerID, bareName string) string {
	for _, tool := range tools {
		if tool.ProviderID == providerID || (tool.ProviderID == "" && tool.Name == providerID) {
			return tool.Name
		}
	}
	return bareName
}

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
	// hasFunctionCall tracks client-side tool calls for the finish reason
	// (TS hasFunctionCall); provider-executed tools don't count.
	hasFunctionCall := false
	hasWebSearchActionSources, hasFileSearchResults := openAIResponseRetrievalFlags(resp.Output)
	retrievedFileIDs := map[string]bool{}

	for _, rawItem := range resp.Output {
		// Peek at type field.
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(rawItem, &peek); err != nil {
			continue
		}
		if responsesItemIsFunctionCall(peek.Type, rawItem, false) {
			hasFunctionCall = true
		}

		switch peek.Type {
		case "message":
			var item responses.AssistantMessageItem
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			for _, part := range item.Content {
				result.Text += part.Text
				msgMeta := openAIResponsesMessageProviderOptions(providerName, item.ID, item.Phase, part.Annotations)
				text := types.TextContent{
					Text:            part.Text,
					Citations:       openAIAnnotationCitations(providerName, part.Annotations),
					ProviderOptions: msgMeta,
				}
				// TS exposes the same data as the part's providerMetadata.
				if msgMeta != nil {
					text.ProviderMetadata, _ = json.Marshal(msgMeta)
				}
				result.Content = append(result.Content, text)
				for _, ann := range part.Annotations {
					// Use citation-derived sources only when retrieval data
					// is unavailable.
					if openAIAnnotationHasRetrievedSource(ann, hasWebSearchActionSources, hasFileSearchResults) {
						continue
					}
					if src, ok := openAIAnnotationToSource(providerName, ann, streaming.GenerateID()); ok {
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
				RawArguments:     item.Arguments,
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
			rawArgs := marshalNoEscape(map[string]interface{}{
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
				ToolName:         openAIDeclaredToolName(tools, "openai.apply_patch", "apply_patch"),
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
					ToolName:         "computer_use",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				}
				toolCalls = append(toolCalls, tc)
				result.Content = append(result.Content,
					types.ToolCallContent{
						ToolCallID:       computerItemID,
						ToolName:         "computer_use",
						Input:            "",
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
					types.ToolResultContent{
						ToolCallID: computerItemID,
						ToolName:   "computer_use",
						Result:     map[string]interface{}{"type": "computer_use_tool_result", "status": item.Status},
						// TS never sets providerExecuted on this tool-result,
						// only on the tool-call.
					},
				)
				continue
			}
			args, rawArgs := computerCallArguments(item)
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               *item.CallID,
				ToolName:         openAIDeclaredToolName(tools, "openai.computer", "computer"),
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
					ProviderMetadata: openAIResponsesReasoningMetadataWithEncrypted(providerName, item.ID, item.EncryptedContent),
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
				ToolName:         openAIDeclaredToolName(tools, "openai.programmatic_tool_calling", "programmatic_tool_calling"),
				Arguments:        args,
				RawArguments:     rawArgs,
				ProviderExecuted: true,
				ProviderMetadata: openAIResponsesToolCallMetadata(providerName, item.ID, "", nil, nil),
			}
			toolCalls = append(toolCalls, tc)
			result.Content = append(result.Content, types.ToolCallContent{
				ToolCallID:       item.CallID,
				ToolName:         openAIDeclaredToolName(tools, "openai.programmatic_tool_calling", "programmatic_tool_calling"),
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
				ToolCallID: item.CallID,
				ToolName:   openAIDeclaredToolName(tools, "openai.programmatic_tool_calling", "programmatic_tool_calling"),
				Result:     map[string]interface{}{"result": item.Result, "status": item.Status},
				// TS never sets providerExecuted on this tool-result, only
				// on the tool-call.
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
			webSearchResult := types.ToolResultContent{
				ToolCallID: item.ID,
				ToolName:   toolName,
				Result:     mapWebSearchOutput(item.Action),
				// TS never sets providerExecuted on this tool-result,
				// only on the tool-call.
			}
			if isFailedWebSearchStatus(item.Status) {
				// TS #22330: failed/incomplete searches are tool errors.
				webSearchResult.Result = map[string]interface{}{"status": item.Status}
				webSearchResult.Error = item.Status
			}
			result.Content = append(result.Content,
				types.ToolCallContent{
					ToolCallID:       item.ID,
					ToolName:         toolName,
					Input:            "{}",
					Arguments:        map[string]interface{}{},
					ProviderExecuted: true,
				},
				webSearchResult,
			)
			for _, src := range openAIWebSearchActionSources(item.Action, streaming.GenerateID) {
				result.Content = append(result.Content, src)
			}

		case "local_shell_call":
			if tc, ok := openAILocalShellToolCall(providerName, tools, rawItem, false); ok {
				toolCalls = append(toolCalls, tc)
			}

		case "shell_call":
			if tc, ok := openAIShellToolCall(providerName, tools, rawItem); ok {
				toolCalls = append(toolCalls, tc)
				if tc.ProviderExecuted {
					// Hosted-container shell calls are provider-executed:
					// TS reports the call in content next to its output.
					result.Content = append(result.Content, types.ToolCallContent{
						ToolCallID:       tc.ID,
						ToolName:         tc.ToolName,
						Input:            tc.RawArguments,
						Arguments:        tc.Arguments,
						ProviderExecuted: true,
						ProviderMetadata: marshalOrNil(tc.ProviderMetadata),
					})
				}
			}

		case "shell_call_output":
			if tr, ok := openAIShellToolResult(tools, rawItem); ok {
				result.Content = append(result.Content, tr)
			}

		case "compaction":
			var item responses.CompactionEvent
			if err := json.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			chunk := responses.CompactionEventToChunkForProvider(item, providerName)
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
			inputRaw := marshalNoEscape(map[string]interface{}{
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
			for _, r := range item.Results {
				if src, ok := openAIFileSearchResultSource(providerName, r, retrievedFileIDs, streaming.GenerateID()); ok {
					result.Content = append(result.Content, src)
				}
			}

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
			rawCodeArgs := marshalNoEscape(codeArgs)
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
	}
	result.FinishReason = mapResponsesFinishReason(resp.IncompleteDetails, hasFunctionCall)
	if resp.IncompleteDetails != nil {
		result.RawFinishReason = resp.IncompleteDetails.Reason
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

// normalizeResponsesLogprobs keeps only the fields the TS logprobs schema
// declares (token, logprob, top_logprobs[].token/logprob); zod strips the
// wire's bytes arrays from providerMetadata.
func normalizeResponsesLogprobs(raw json.RawMessage) json.RawMessage {
	var items []struct {
		Token       string  `json:"token"`
		Logprob     float64 `json:"logprob"`
		TopLogprobs []struct {
			Token   string  `json:"token"`
			Logprob float64 `json:"logprob"`
		} `json:"top_logprobs"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return raw
	}
	out, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return out
}

// responsesOutputLogprobs collects the per-content-part logprobs of every
// message output item (TS: logprobs.push(contentPart.logprobs)).
func responsesOutputLogprobs(output []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, raw := range output {
		var item responses.AssistantMessageItem
		if json.Unmarshal(raw, &item) != nil || item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if len(part.Logprobs) > 0 && string(part.Logprobs) != "null" {
				out = append(out, normalizeResponsesLogprobs(part.Logprobs))
			}
		}
	}
	return out
}

// programCallArguments builds the SDK-facing {code, fingerprint} arguments
// for a decoded "program" item, mirroring TS's
// programmaticToolCallingInputSchema (row 1f6dd3a).
func programCallArguments(item responses.ProgramItem) (map[string]interface{}, string) {
	args := map[string]interface{}{
		"code":        item.Code,
		"fingerprint": item.Fingerprint,
	}
	raw := marshalNoEscape(args)
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
	raw := marshalNoEscape(args)
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

// openAIResponsesReasoningMetadataWithEncrypted mirrors the TS reasoning
// providerMetadata {itemId, reasoningEncryptedContent: encrypted_content ?? null}.
func openAIResponsesReasoningMetadataWithEncrypted(providerName, itemID, encrypted string) json.RawMessage {
	if itemID == "" {
		return nil
	}
	var enc interface{}
	if encrypted != "" {
		enc = encrypted
	}
	raw, _ := json.Marshal(map[string]interface{}{
		providerName: map[string]interface{}{"itemId": itemID, "reasoningEncryptedContent": enc},
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
func openAIAnnotationToSource(providerName string, ann responses.TextAnnotation, id string) (types.SourceContent, bool) {
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

// openAIAnnotationCitations maps text annotations to normalized citations,
// mirroring TS mapOpenAIResponsesCitations: file_path annotations are not
// citations, a url source's id is its URL and a file source's id is its file
// ID, and url_citation / container_file_citation carry output offsets. Returns
// nil when there is nothing to cite.
func openAIAnnotationCitations(providerName string, annotations []responses.TextAnnotation) []types.Citation {
	var citations []types.Citation
	for _, ann := range annotations {
		if ann.Type == "file_path" {
			continue
		}
		id := ann.FileID
		if ann.Type == "url_citation" {
			id = ann.URL
		}
		src, ok := openAIAnnotationToSource(providerName, ann, id)
		if !ok {
			continue
		}
		c := types.Citation{Source: src}
		if ann.Type == "url_citation" || ann.Type == "container_file_citation" {
			start, end := ann.StartIndex, ann.EndIndex
			c.StartIndex, c.EndIndex = &start, &end
		}
		citations = append(citations, c)
	}
	return citations
}

// openAIAnnotationHasRetrievedSource reports whether a citation-derived source
// should be skipped because the response already carries retrieval data for
// it (web-search action sources or file-search results).
func openAIAnnotationHasRetrievedSource(ann responses.TextAnnotation, hasWebSearchActionSources, hasFileSearchResults bool) bool {
	return (ann.Type == "url_citation" && hasWebSearchActionSources) ||
		(ann.Type == "file_citation" && hasFileSearchResults)
}

// openAIResponseRetrievalFlags scans response output items for web-search
// action URL sources and file-search results.
func openAIResponseRetrievalFlags(output []json.RawMessage) (webSearchSources, fileSearchResults bool) {
	for _, raw := range output {
		var peek struct {
			Type    string            `json:"type"`
			Action  *WebSearchAction  `json:"action"`
			Results []json.RawMessage `json:"results"`
		}
		if json.Unmarshal(raw, &peek) != nil {
			continue
		}
		switch peek.Type {
		case "web_search_call":
			if peek.Action != nil && peek.Action.Type == "search" {
				for _, s := range peek.Action.Sources {
					if s.Type == "url" {
						webSearchSources = true
					}
				}
			}
		case "file_search_call":
			if len(peek.Results) > 0 {
				fileSearchResults = true
			}
		}
	}
	return webSearchSources, fileSearchResults
}

// openAIWebSearchActionSources returns the URL sources retrieved by a
// web_search_call "search" action (TS: one source per `url` entry).
func openAIWebSearchActionSources(action *WebSearchAction, newID func() string) []types.SourceContent {
	if action == nil || action.Type != "search" {
		return nil
	}
	var out []types.SourceContent
	for _, s := range action.Sources {
		if s.Type == "url" {
			out = append(out, types.SourceContent{SourceType: "url", ID: newID(), URL: s.URL})
		}
	}
	return out
}

// openAIFileSearchResultSource returns the document source for a retrieved
// file-search result, deduplicated by file ID (seen is updated).
func openAIFileSearchResultSource(providerName string, r responses.FileSearchResult, seen map[string]bool, id string) (types.SourceContent, bool) {
	if seen[r.FileID] {
		return types.SourceContent{}, false
	}
	seen[r.FileID] = true
	meta, _ := json.Marshal(map[string]interface{}{
		providerName: map[string]interface{}{"type": "file_search", "fileId": r.FileID},
	})
	return types.SourceContent{
		SourceType:       "document",
		ID:               id,
		MediaType:        "text/plain",
		Title:            r.Filename,
		Filename:         r.Filename,
		ProviderMetadata: meta,
	}, true
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

// responsesItemIsFunctionCall reports whether a Responses output item is a
// client-side tool call, which TS counts as hasFunctionCall. A computer_call
// without a call_id is executed server-side and does not count; streaming
// only counts an apply_patch_call once its status is "completed" (doGenerate
// counts every apply_patch_call).
func responsesItemIsFunctionCall(itemType string, raw json.RawMessage, streaming bool) bool {
	switch itemType {
	case "function_call", "custom_tool_call":
		return true
	case "computer_call":
		var item struct {
			CallID *string `json:"call_id"`
		}
		return json.Unmarshal(raw, &item) == nil && item.CallID != nil
	case "apply_patch_call":
		if !streaming {
			return true
		}
		var item struct {
			Status string `json:"status"`
		}
		return json.Unmarshal(raw, &item) == nil && item.Status == "completed"
	}
	return false
}

// mapResponsesFinishReason maps Responses API incomplete_details to a
// FinishReason. Mirrors TS mapOpenAIResponseFinishReason: a length or
// content-filter stop wins over tool calls, and an unknown reason is
// "tool-calls" when the model called a client-side tool, else "other".
func mapResponsesFinishReason(details *responses.IncompleteDetails, hasFunctionCall bool) types.FinishReason {
	reason := ""
	if details != nil {
		reason = details.Reason
	}
	switch reason {
	case "":
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonStop
	case "max_output_tokens":
		return types.FinishReasonLength
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonOther
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

	// TS convertOpenAIResponsesUsage always reports the breakdown (cached and
	// reasoning default to 0; cacheWrite stays undefined unless sent).
	var cached int64
	var cacheWritePtr *int64
	if u.InputTokensDetails != nil {
		cached = int64(u.InputTokensDetails.CachedTokens)
		if u.InputTokensDetails.CacheWriteTokens != nil {
			cw := int64(*u.InputTokensDetails.CacheWriteTokens)
			cacheWritePtr = &cw
		}
	}
	noCached := inputTokens - cached
	if cacheWritePtr != nil {
		noCached -= *cacheWritePtr
	}
	result.InputDetails = &types.InputTokenDetails{
		CacheReadTokens:  &cached,
		CacheWriteTokens: cacheWritePtr,
		NoCacheTokens:    &noCached,
	}

	var reasoning int64
	if u.OutputTokensDetails != nil {
		reasoning = int64(u.OutputTokensDetails.ReasoningTokens)
	}
	textOut := outputTokens - reasoning
	result.OutputDetails = &types.OutputTokenDetails{
		ReasoningTokens: &reasoning,
		TextTokens:      &textOut,
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
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure
	}
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
	async     *bool
	// suppressInputStreaming is set for an undeclared "parallel" wrapper call:
	// its input deltas are buffered and replayed (TS isUndeclaredParallelToolCall).
	suppressInputStreaming bool
	bufferedInputDeltas    []string
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
	// curEventData is the SSE data of the frame being processed (for typed
	// validation errors).
	curEventData string
	reader       io.ReadCloser
	parser       *streaming.SSEParser
	err          error

	// hasFunctionCall is set once a client-side tool call item finishes; it
	// makes the finish reason "tool-calls" (TS hasFunctionCall).
	hasFunctionCall bool

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
	logprobsEnabled   bool              // providerOptions.<name>.logprobs is truthy
	logprobs          []json.RawMessage // per-delta logprobs collected for finish metadata
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
	// sawErrorEvent records a provider "error" event (TS sets the finish
	// reason's raw value to 'error' for it).
	sawErrorEvent bool

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

	// Retrieval-vs-citation source tracking (TS #21797): url_citation and
	// file_citation annotations only become fallback sources at the end of
	// the stream, and only when no web-search action sources / file-search
	// results were retrieved.
	hasWebSearchActionSources bool
	hasFileSearchResults      bool
	retrievedFileIDs          map[string]bool
	fallbackAnnotations       []responses.TextAnnotation

	// activeMessagePhase captures the "message" item's phase at
	// output_item.added, used as a fallback for text-end's providerMetadata
	// when the output_item.done event's own item omits phase (TS
	// `activeMessagePhase`: `phase = value.item.phase ?? activeMessagePhase`).
	activeMessagePhase *string

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *responsesStream) RequestBody() interface{} { return s.requestBody }

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
//
// Returns (chunk, true) when the caller should return chunk immediately, or
// (nil, false) when the caller should continue its Next() loop instead (see
// emitParsedChunk below for why this no longer recurses into Next()).
func (s *responsesStream) emitDecodeError(eventType string, err error) (*provider.StreamChunk, bool) {
	s.hadDecodeError = true
	// The frame is valid JSON with a string `type` but a member that does not
	// fit that event's typed decode: TS's chunk schema rejects it as a
	// TypeValidationError.
	msg, perr := providerutils.ChunkParseFailure(s.curEventData, fmt.Errorf("%s event: %w", eventType, err))
	return s.emitParsedChunk(&provider.StreamChunk{
		Type: provider.ChunkTypeError,
		Text: msg,
		Err:  perr,
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

// queueFallbackSources enqueues citation-derived sources for url_citation /
// file_citation annotations once the stream's complete source set is known,
// skipping those superseded by retrieval data (TS flush()).
// queueEOFFinish is TS flush() for a stream that ended without a terminal
// response event: the source fallbacks, then `finish` with the default
// finish reason (other, or error after a decode failure) and responseId
// metadata, and the end of the stream.
func (s *responsesStream) queueEOFFinish() {
	s.queueFallbackSources()
	finishReason := types.FinishReasonOther
	if s.hadDecodeError {
		finishReason = types.FinishReasonError
	}
	fields := map[string]interface{}{}
	if s.responseID != "" {
		fields["responseId"] = s.responseID
	}
	if len(s.logprobs) > 0 {
		fields["logprobs"] = s.logprobs
	}
	var meta json.RawMessage
	if len(fields) > 0 {
		meta, _ = json.Marshal(map[string]interface{}{s.providerName: fields})
	}
	usage := convertResponsesUsage(nil)
	rawFinishReason := ""
	if s.sawErrorEvent {
		rawFinishReason = "error"
	}
	s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     finishReason,
		RawFinishReason:  rawFinishReason,
		Usage:            &usage,
		ProviderMetadata: meta,
	})
	s.err = io.EOF
}

func (s *responsesStream) queueFallbackSources() {
	for _, ann := range s.fallbackAnnotations {
		if openAIAnnotationHasRetrievedSource(ann, s.hasWebSearchActionSources, s.hasFileSearchResults) {
			continue
		}
		if src, ok := openAIAnnotationToSource(s.providerName, ann, streaming.GenerateID()); ok {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src})
		}
	}
	s.fallbackAnnotations = nil

	// TS flush(): a suppressed (undeclared "parallel") tool call that never
	// reached output_item.done still surfaces its buffered input.
	indices := make([]int, 0, len(s.toolAccum))
	for idx, accum := range s.toolAccum {
		if accum.suppressInputStreaming {
			indices = append(indices, idx)
		}
	}
	sort.Ints(indices)
	for _, idx := range indices {
		accum := s.toolAccum[idx]
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
			Type:     provider.ChunkTypeToolInputStart,
			ToolCall: &types.ToolCall{ID: accum.id, ToolName: accum.name},
		})
		for _, d := range accum.bufferedInputDeltas {
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: accum.id, Text: d})
		}
		delete(s.toolAccum, idx)
	}
}

// emitParsedChunk decides how a freshly built chunk should be delivered: if
// nothing is queued ahead of it, it is returned immediately (chunk, true);
// otherwise it is appended behind the pending flushQueue and the caller
// should continue its Next() loop to drain the queue (nil, false). A nil
// chunk likewise means "nothing to deliver, continue the loop".
//
// This used to recurse by calling s.Next() directly in both of those cases.
// Go does not eliminate that tail call, and Next()'s own loop can call this
// method on almost every SSE event, so a long run of skip-worthy events
// could grow the goroutine stack without bound. Returning a "continue"
// signal instead lets Next()'s single top-level loop (and
// handleOutputItemDone's, which shares this same signal shape) do the
// looping without adding stack frames.
func (s *responsesStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, bool) {
	if chunk == nil {
		return nil, false
	}
	if len(s.flushQueue) == 0 {
		return chunk, true
	}
	s.flushQueue = append(s.flushQueue, chunk)
	return nil, false
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
	for {
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
			if err == io.EOF {
				// End of stream without a terminal event: TS flush() still
				// decides the citation-derived source fallbacks and enqueues
				// `finish` (other, no usage).
				s.queueEOFFinish()
				continue
			}
			s.err = err
			return nil, err
		}

		// Standard [DONE] terminator (some OpenAI SSE streams still send it);
		// TS skips it and flush() runs at the end of the stream.
		if streaming.IsStreamDone(event) {
			s.queueEOFFinish()
			continue
		}
		// Parse the "type" discriminator.
		s.curEventData = event.Data
		var peek responses.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(event.Data), &peek); err != nil {
			s.queueRawChunk(event.Data)
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
		// TS openaiResponsesChunkSchema: a modeled event whose members fail its
		// schema, or a frame without a string `type`, is a parse failure (error
		// part + finish reason "error"); unmodeled event types pass as
		// unknown chunks.
		if msg, perr := providerutils.CheckResponsesChunk(event.Data); perr != nil {
			s.queueRawChunk(event.Data)
			s.hadDecodeError = true
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
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			if e.Response.ID != "" {
				s.responseID = e.Response.ID
			}
			metadata := &provider.ResponseMetadata{
				ID:      e.Response.ID,
				ModelID: e.Response.Model,
				Headers: providerutils.ExtractHeaders(s.responseHeaders),
			}
			if e.Response.CreatedAt != 0 {
				metadata.Timestamp = time.Unix(e.Response.CreatedAt, 0)
			}
			s.pendingMetadata = &provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: metadata,
			}
			continue

		case "response.output_item.added":
			var e responses.OutputItemAddedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
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
				suppress := responses.IsUndeclaredParallelToolCall(e.Item.Name, s.tools)
				s.toolAccum[e.OutputIndex] = &responsesToolAccum{
					id:                     e.Item.CallID,
					itemID:                 e.Item.ID,
					name:                   e.Item.Name,
					namespace:              e.Item.Namespace,
					async:                  e.Item.Async,
					suppressInputStreaming: suppress,
				}
				if !suppress {
					// TS streams the tool input: tool-input-start here, a
					// tool-input-delta per arguments delta, tool-input-end at
					// output_item.done.
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
						Type:     provider.ChunkTypeToolInputStart,
						ToolCall: &types.ToolCall{ID: e.Item.CallID, ToolName: e.Item.Name},
					})
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
				continue
			case "message":
				// TS: `ongoingAnnotations.splice(0)` then emit text-start with
				// providerMetadata {itemId, phase?} (no annotations yet).
				s.ongoingAnnotations = nil
				s.activeMessagePhase = e.Item.Phase
				var meta json.RawMessage
				if m := openAIResponsesMessageProviderOptions(s.providerName, e.Item.ID, e.Item.Phase, nil); m != nil {
					meta, _ = json.Marshal(m)
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:             provider.ChunkTypeTextStart,
					ID:               e.Item.ID,
					ProviderMetadata: meta,
				})
				continue

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
				continue
			case "computer_call":
				// TS: tool-input-start under call_id ?? id; the input and
				// tool-input-end follow at output_item.done.
				computerCallID := e.Item.ID
				if e.Item.CallID != "" {
					computerCallID = e.Item.CallID
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:       computerCallID,
						ToolName: openAIDeclaredToolName(s.tools, "openai.computer", "computer"),
					},
				})
				continue
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
				continue
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
				continue
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
				continue
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
				continue
			case "custom_tool_call":
				s.ongoingToolCalls[e.OutputIndex] = &responsesOngoingToolCall{
					toolName:   e.Item.Name,
					toolCallID: e.Item.CallID,
				}
				s.flushQueue = append(s.flushQueue,
					&provider.StreamChunk{
						Type: provider.ChunkTypeToolInputStart,
						ToolCall: &types.ToolCall{
							ID:       e.Item.CallID,
							ToolName: e.Item.Name,
						},
					},
					// Custom tool input is a string, so stream its JSON
					// encoding consistently with the final tool call input
					// (TS #22535).
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: e.Item.CallID, Text: `"`},
				)
				continue
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
					toolName:   openAIDeclaredToolName(s.tools, "openai.apply_patch", "apply_patch"),
					toolCallID: callID,
					applyPatch: &applyPatchStreamState{hasDiff: isDelete, endEmitted: isDelete},
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:       callID,
						ToolName: openAIDeclaredToolName(s.tools, "openai.apply_patch", "apply_patch"),
					},
				})
				if isDelete {
					inputStr := marshalNoEscape(struct {
						CallID    string `json:"callId"`
						Operation struct {
							Type string `json:"type"`
							Path string `json:"path"`
						} `json:"operation"`
					}{CallID: callID, Operation: struct {
						Type string `json:"type"`
						Path string `json:"path"`
					}{opType, opPath}})
					s.flushQueue = append(s.flushQueue,
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: string(inputStr)},
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: callID},
					)
				} else {
					// Marshal the whole object with an empty diff (fields in
					// streaming order, diff last), then drop the closing `"}}`
					// so the prefix ends inside the open diff string.
					type applyPatchOp struct {
						Type string `json:"type"`
						Path string `json:"path"`
						Diff string `json:"diff"`
					}
					prefixJSON := marshalNoEscape(struct {
						CallID    string       `json:"callId"`
						Operation applyPatchOp `json:"operation"`
					}{CallID: callID, Operation: applyPatchOp{Type: opType, Path: opPath}})
					prefix := strings.TrimSuffix(string(prefixJSON), `"}}`)
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: prefix})
				}
			}
			continue

		case "response.output_text.delta":
			var e responses.OutputTextDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			if s.logprobsEnabled && len(e.Logprobs) > 0 && string(e.Logprobs) != "null" {
				s.logprobs = append(s.logprobs, normalizeResponsesLogprobs(e.Logprobs))
			}
			// TS enqueues text-delta unconditionally (even for ""), keyed by
			// resolveOutputItemId(item_id, output_index).
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeText,
				ID:   s.resolveOutputItemID(e.OutputIndex, e.ItemID),
				Text: e.Delta,
			})
			if ok {
				return chunk, nil
			}
			continue

		case "response.function_call_arguments.delta":
			var e responses.FunctionCallArgumentsDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			if accum, ok := s.toolAccum[e.OutputIndex]; ok {
				accum.arguments += e.Delta
				if accum.suppressInputStreaming {
					accum.bufferedInputDeltas = append(accum.bufferedInputDeltas, e.Delta)
				} else {
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
						Type: provider.ChunkTypeToolInputDelta,
						ID:   accum.id,
						Text: e.Delta,
					})
				}
			}
			continue

		case "response.reasoning_summary_text.delta":
			var e responses.ReasoningSummaryTextDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			// TS unconditionally enqueues reasoning-delta here, even for an
			// empty-string delta -- no guard.
			itemID := s.resolveOutputItemID(e.OutputIndex, e.ItemID)
			meta, _ := json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"itemId": itemID}})
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeReasoning,
				ID:               fmt.Sprintf("%s:%d", itemID, e.SummaryIndex),
				Reasoning:        e.Delta,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

		case "response.reasoning_summary_part.added":
			var e responses.ReasoningSummaryPartAddedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
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
			continue

		case "response.reasoning_summary_part.done":
			var e responses.ReasoningSummaryPartDoneEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
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
					chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
						Type:             provider.ChunkTypeReasoningEnd,
						ID:               fmt.Sprintf("%s:%d", itemID, e.SummaryIndex),
						ProviderMetadata: meta,
					})
					if ok {
						return chunk, nil
					}
					continue
				}
				// store=false: keep the block open until output_item.done, which
				// carries the final encrypted_content.
				accum.summaryParts[e.SummaryIndex] = "can-conclude"
			}
			continue

		case "response.output_text.annotation.added":
			var e responses.OutputTextAnnotationAddedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			// TS: `ongoingAnnotations.push(value.annotation)` -- surfaced on the
			// owning message item's text-end providerMetadata.openai.annotations.
			s.ongoingAnnotations = append(s.ongoingAnnotations, e.Annotation)
			if e.Annotation.Type == "url_citation" || e.Annotation.Type == "file_citation" {
				// Retrieval events may arrive after the annotation. Decide the
				// compatibility fallback once the complete source set is known.
				s.fallbackAnnotations = append(s.fallbackAnnotations, e.Annotation)
				continue
			}
			if src, ok := openAIAnnotationToSource(s.providerName, e.Annotation, streaming.GenerateID()); ok {
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type:          provider.ChunkTypeSource,
					SourceContent: &src,
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		case "response.custom_tool_call_input.delta":
			var e responses.CustomToolCallInputDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok {
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   toolCall.toolCallID,
					Text: escapeJSONDelta(e.Delta),
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		case "response.apply_patch_call_operation_diff.delta":
			var e responses.ApplyPatchCallOperationDiffDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok && toolCall.applyPatch != nil {
				toolCall.applyPatch.hasDiff = true
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   toolCall.toolCallID,
					Text: escapeJSONDelta(e.Delta),
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		case "response.apply_patch_call_operation_diff.done":
			var e responses.ApplyPatchCallOperationDiffDoneEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
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
			continue

		case "response.image_generation_call.partial_image":
			var e responses.ImageGenerationPartialImageEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID:  e.ItemID,
					ToolName:    openAIProviderToolDisplayName(s.tools, "openai.image_generation", "image_generation"),
					Result:      map[string]interface{}{"result": e.PartialImageB64},
					Preliminary: true,
				},
			})
			if ok {
				return chunk, nil
			}
			continue

		case "response.code_interpreter_call_code.delta":
			var e responses.CodeInterpreterCallCodeDeltaEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			if toolCall, ok := s.ongoingToolCalls[e.OutputIndex]; ok {
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type: provider.ChunkTypeToolInputDelta,
					ID:   toolCall.toolCallID,
					Text: escapeJSONDelta(e.Delta),
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		case "response.code_interpreter_call_code.done":
			var e responses.CodeInterpreterCallCodeDoneEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
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
				inputStr := marshalNoEscape(map[string]interface{}{"code": e.Code, "containerId": containerID})
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
			continue

		case "response.output_item.done":
			var e responses.OutputItemDoneEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			s.markOutputStarted()
			if eventRawChunk != nil {
				s.flushQueue = append(s.flushQueue, eventRawChunk)
			}
			chunk, ok := s.handleOutputItemDone(e)
			if ok {
				return chunk, nil
			}
			continue

		case "response.completed", "response.incomplete":
			var e responses.ResponseCompletedEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				chunk, ok := s.emitDecodeError(peek.Type, err)
				if ok {
					return chunk, nil
				}
				continue
			}
			usage := convertResponsesUsage(e.Response.Usage)
			finishReason := mapResponsesFinishReason(e.Response.IncompleteDetails, s.hasFunctionCall)
			rawFinishReason := ""
			if e.Response.IncompleteDetails != nil {
				rawFinishReason = e.Response.IncompleteDetails.Reason
			}
			// Row eee6200: an earlier known-event decode failure forces the
			// finish reason to "error", regardless of what this event reports.
			if s.hadDecodeError {
				finishReason = types.FinishReasonError
				rawFinishReason = ""
				if s.sawErrorEvent {
					rawFinishReason = "error" // TS: raw 'error' after an error event
				}
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
			{
				// TS: { responseId, logprobs?, serviceTier?, reasoningContext? }.
				fields := map[string]interface{}{}
				if responseID != "" {
					fields["responseId"] = responseID
				}
				if len(s.logprobs) > 0 {
					fields["logprobs"] = s.logprobs
				}
				if e.Response.ServiceTier != "" {
					fields["serviceTier"] = e.Response.ServiceTier
				}
				if e.Response.Reasoning != nil && e.Response.Reasoning.Context != "" {
					fields["reasoningContext"] = e.Response.Reasoning.Context
				}
				if len(fields) > 0 {
					meta, _ = json.Marshal(map[string]interface{}{s.providerName: fields})
				}
			}

			s.err = io.EOF
			s.queueFallbackSources()
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeFinish,
				FinishReason:     finishReason,
				RawFinishReason:  rawFinishReason,
				Usage:            &usage,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

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
				finishReason = mapResponsesFinishReason(e.Response.IncompleteDetails, s.hasFunctionCall)
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
			if len(s.logprobs) > 0 {
				metaMap["logprobs"] = s.logprobs
			}
			if e.Response.Reasoning != nil && e.Response.Reasoning.Context != "" {
				metaMap["reasoningContext"] = e.Response.Reasoning.Context
			}
			var meta json.RawMessage
			if len(metaMap) > 0 {
				meta, _ = json.Marshal(map[string]interface{}{s.providerName: metaMap})
			}

			// Output already started (encounteredStreamError branch, TS
			// openai-responses-language-model.ts ~line 2737): a response.failed
			// with a response.error surfaces as a ChunkTypeError chunk (built via
			// createOpenAIProviderStreamError from the same synthetic
			// {type:'response.failed', response:{error,...}} frame TS
			// constructs), enqueued before the terminal finish chunk. Previously
			// Go only emitted the finish chunk here and silently dropped the
			// error entirely (P1-1c part 2).
			if e.Response.Error != nil && !s.hadDecodeError {
				s.hadDecodeError = true
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: e.Response.Error.Message,
					Err:  newOpenAIStreamProviderErrorChunk(s.providerName, json.RawMessage(event.Data)),
				})
			}

			s.err = io.EOF
			s.queueFallbackSources()
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type:             provider.ChunkTypeFinish,
				FinishReason:     finishReason,
				RawFinishReason:  rawFinishReason,
				Usage:            &usage,
				ProviderMetadata: meta,
			})
			if ok {
				return chunk, nil
			}
			continue

		case "error":
			var e responses.ResponsesStreamErrorEvent
			if err := json.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if !s.outputStarted {
				s.flushQueue = nil
				s.pendingRaw = nil
				s.pendingMetadata = nil
				s.err = newOpenAIStreamProviderError(s.providerName, json.RawMessage(event.Data), s.responseHeaders)
				return nil, s.err
			}
			// TS encounteredStreamError: a later response.completed must not
			// replace the error finish reason.
			s.hadDecodeError = true
			s.sawErrorEvent = true
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: e.Message,
				// P1-1c part 2: attach the structured StreamProviderError (TS
				// createOpenAIProviderStreamError(value)) instead of only Text.
				Err: newOpenAIStreamProviderErrorChunk(s.providerName, json.RawMessage(event.Data)),
			})
			if ok {
				return chunk, nil
			}
			continue

		default:
			// Unknown event types (web_search_call, code_interpreter_call, etc.) —
			// skip silently; they are provider-internal and require no client action.
			continue
		}

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
func (s *responsesStream) handleOutputItemDone(e responses.OutputItemDoneEvent) (*provider.StreamChunk, bool) {
	itemType := s.itemTypes[e.OutputIndex]
	if responsesItemIsFunctionCall(itemType, e.Item, true) {
		s.hasFunctionCall = true
	}

	switch itemType {
	case "function_call":
		accum, ok := s.toolAccum[e.OutputIndex]
		if !ok {
			return nil, false
		}
		delete(s.toolAccum, e.OutputIndex)
		delete(s.itemTypes, e.OutputIndex)

		// TS builds the tool-input-end / tool-call parts from the done item
		// (call_id, name, arguments), not from the accumulated deltas.
		callID, name, rawArguments := accum.id, accum.name, accum.arguments
		var item responses.FunctionCallItem
		if err := json.Unmarshal(e.Item, &item); err == nil {
			if item.ID != "" {
				accum.itemID = item.ID
			}
			if item.Namespace != "" {
				accum.namespace = item.Namespace
			}
			if item.CallID != "" {
				callID = item.CallID
			}
			if item.Name != "" {
				name = item.Name
			}
			if item.Arguments != "" || accum.arguments == "" {
				rawArguments = item.Arguments
			}
		}
		async := item.Async
		if async == nil {
			async = accum.async
		}
		newToolCall := func(id, toolName, input string, metadata map[string]interface{}) *provider.StreamChunk {
			var args map[string]interface{}
			if input != "" {
				json.Unmarshal([]byte(input), &args) //nolint:errcheck
			}
			return &provider.StreamChunk{
				Type: provider.ChunkTypeToolCall,
				ToolCall: &types.ToolCall{
					ID:               id,
					ToolName:         toolName,
					Arguments:        args,
					RawArguments:     input,
					ProviderMetadata: metadata,
				},
			}
		}
		emitUnexpanded := func() {
			if accum.suppressInputStreaming {
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:     provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{ID: callID, ToolName: name},
				})
				if len(accum.bufferedInputDeltas) > 0 {
					for _, d := range accum.bufferedInputDeltas {
						s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: d})
					}
				} else if len(rawArguments) > 0 {
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: rawArguments})
				}
			}
			end := &provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: callID, ToolCall: &types.ToolCall{ID: callID, ToolName: name}}
			if accum.namespace != "" {
				end.ProviderMetadata, _ = json.Marshal(map[string]interface{}{s.providerName: map[string]interface{}{"namespace": accum.namespace}})
			}
			s.flushQueue = append(s.flushQueue, end,
				newToolCall(callID, name, rawArguments, openAIResponsesToolCallMetadata(s.providerName, accum.itemID, accum.namespace, async, item.Caller)))
		}
		if !accum.suppressInputStreaming {
			emitUnexpanded()
			return nil, false
		}
		// Row 6be0f51: expand an internal "parallel" wrapper call into one
		// tool-call chunk per nested recipient, when every recipient names a
		// declared function tool.
		if expanded, ok := responses.ExpandParallelToolCall(callID, name, rawArguments, accum.itemID, s.tools, s.providerName); ok {
			for _, tc := range expanded {
				tc := tc
				s.flushQueue = append(s.flushQueue,
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: tc.ID, ToolName: tc.ToolName}},
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: tc.ID, Text: tc.RawArguments},
					&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: tc.ID, ToolCall: &types.ToolCall{ID: tc.ID, ToolName: tc.ToolName}},
					&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &tc},
				)
			}
			return nil, false
		}
		emitUnexpanded()
		return nil, false

	case "local_shell_call":
		delete(s.itemTypes, e.OutputIndex)
		if tc, ok := openAILocalShellToolCall(s.providerName, s.tools, e.Item, true); ok {
			return s.emitParsedChunk(&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &tc})
		}
		return nil, false

	case "shell_call":
		delete(s.itemTypes, e.OutputIndex)
		if tc, ok := openAIShellToolCall(s.providerName, s.tools, e.Item); ok {
			return s.emitParsedChunk(&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &tc})
		}
		return nil, false

	case "shell_call_output":
		delete(s.itemTypes, e.OutputIndex)
		if tr, ok := openAIShellToolResult(s.tools, e.Item); ok {
			return s.emitParsedChunk(&provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID: tr.ToolCallID,
				ToolName:   tr.ToolName,
				Result:     tr.Result,
			}})
		}
		return nil, false

	case "compaction":
		var item responses.CompactionEvent
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return nil, false
		}
		chunk := responses.CompactionEventToChunkForProvider(item, s.providerName)
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
			return nil, false
		}
		itemID := s.firstItemIDByOutputIndex[e.OutputIndex]
		if itemID == "" {
			itemID = item.ID
		}
		delete(s.firstItemIDByOutputIndex, e.OutputIndex)
		annotations := s.ongoingAnnotations
		s.ongoingAnnotations = nil
		// TS: `phase = value.item.phase ?? activeMessagePhase` -- the done
		// event's own item can omit phase even when output_item.added carried
		// one, so fall back to the phase captured at text-start.
		phase := item.Phase
		if phase == nil {
			phase = s.activeMessagePhase
		}
		s.activeMessagePhase = nil
		var meta json.RawMessage
		if m := openAIResponsesMessageProviderOptions(s.providerName, itemID, phase, annotations); m != nil {
			meta, _ = json.Marshal(m)
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type:             provider.ChunkTypeTextEnd,
			ID:               itemID,
			Citations:        openAIAnnotationCitations(s.providerName, annotations),
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
			return nil, false
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
		}
		sort.Ints(indices)
		if len(indices) == 0 {
			return nil, false
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
		rawArgs := marshalNoEscape(map[string]interface{}{
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
				ToolName:         openAIDeclaredToolName(s.tools, "openai.apply_patch", "apply_patch"),
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
						ToolName:         "computer_use",
						ProviderExecuted: true,
					},
				},
				&provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               computerItemID,
						ToolName:         "computer_use",
						Arguments:        map[string]interface{}{},
						ProviderExecuted: true,
					},
				},
			)
			return s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeToolResult,
				ToolResult: &types.ToolResult{
					ToolCallID: computerItemID,
					ToolName:   "computer_use",
					Result:     map[string]interface{}{"type": "computer_use_tool_result", "status": item.Status},
				},
			})
		}
		computerArgs, computerRawArgs := computerCallArguments(item)
		s.flushQueue = append(s.flushQueue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: *item.CallID, Text: computerRawArgs},
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: *item.CallID, ToolCall: &types.ToolCall{
				ID:       *item.CallID,
				ToolName: openAIDeclaredToolName(s.tools, "openai.computer", "computer"),
			}},
		)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               *item.CallID,
				ToolName:         openAIDeclaredToolName(s.tools, "openai.computer", "computer"),
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
			return nil, false
		}
		s.flushQueue = append(s.flushQueue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: item.CallID, Text: `"`},
			&provider.StreamChunk{
				Type:     provider.ChunkTypeToolInputEnd,
				ToolCall: &types.ToolCall{ID: item.CallID},
			},
		)
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
			return nil, false
		}
		webSearchResult := &types.ToolResult{
			ToolCallID: item.ID,
			ToolName:   s.webSearchToolName,
			Result:     mapWebSearchOutput(item.Action),
		}
		if isFailedWebSearchStatus(item.Status) {
			// TS #22330: failed/incomplete searches are tool errors.
			webSearchResult.Result = map[string]interface{}{"status": item.Status}
			webSearchResult.Error = errors.New(item.Status)
		}
		chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
			Type:       provider.ChunkTypeToolResult,
			ToolResult: webSearchResult,
		})
		// Retrieved URL sources follow the tool result (TS order).
		for _, src := range openAIWebSearchActionSources(item.Action, streaming.GenerateID) {
			s.hasWebSearchActionSources = true
			src := src
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src})
		}
		return chunk, ok

	case "program":
		// Row 1f6dd3a: the hosted programmatic-tool-calling sandbox generated
		// and is executing this JavaScript program; the result arrives as a
		// separate "program_output" item's own output_item.done event.
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ProgramItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return nil, false
		}
		args, rawArgs := programCallArguments(item)
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolCall,
			ToolCall: &types.ToolCall{
				ID:               item.CallID,
				ToolName:         openAIDeclaredToolName(s.tools, "openai.programmatic_tool_calling", "programmatic_tool_calling"),
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
			return nil, false
		}
		return s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.CallID,
				ToolName:   openAIDeclaredToolName(s.tools, "openai.programmatic_tool_calling", "programmatic_tool_calling"),
				Result:     map[string]interface{}{"result": item.Result, "status": item.Status},
			},
		})

	case "image_generation_call":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.ImageGenerationCallItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return nil, false
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
			return nil, false
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
		chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
			Type: provider.ChunkTypeToolResult,
			ToolResult: &types.ToolResult{
				ToolCallID: item.ID,
				ToolName:   openAIProviderToolDisplayName(s.tools, "openai.file_search", "file_search"),
				Result:     map[string]interface{}{"queries": item.Queries, "results": results},
			},
		})
		// Retrieved documents follow the tool result, deduplicated by file.
		if s.retrievedFileIDs == nil {
			s.retrievedFileIDs = map[string]bool{}
		}
		for _, r := range item.Results {
			s.hasFileSearchResults = true
			if src, ok := openAIFileSearchResultSource(s.providerName, r, s.retrievedFileIDs, streaming.GenerateID()); ok {
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src})
			}
		}
		return chunk, ok

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
			return nil, false
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
			return nil, false
		}
		if toolCall == nil {
			return nil, false
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
		inputRaw := marshalNoEscape(map[string]interface{}{"arguments": item.Arguments, "call_id": callIDForInput})
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
			return nil, false
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
			return nil, false
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
		return nil, false

	case "mcp_approval_request":
		delete(s.itemTypes, e.OutputIndex)
		var item responses.McpApprovalRequestItem
		if err := json.Unmarshal(e.Item, &item); err != nil {
			return nil, false
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
		return nil, false
	}
}

// Ensure ResponsesLanguageModel satisfies the provider.LanguageModel interface
// at compile time.
var _ provider.LanguageModel = (*ResponsesLanguageModel)(nil)

func sliceHas(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// isFailedWebSearchStatus reports whether a web_search_call status marks the
// search as a tool error (TS: status === 'failed' || 'incomplete').
func isFailedWebSearchStatus(status string) bool {
	return status == "failed" || status == "incomplete"
}

// validateResponsesPromptCacheOptions mirrors the TS Responses option schema
// z.object({mode: z.enum(['implicit','explicit']).optional(),
// ttl: z.enum(['30m','12h']).optional(), prewarm: z.boolean().optional()}).
func validateResponsesPromptCacheOptions(value interface{}, providerOptionsName string) error {
	field := "providerOptions." + providerOptionsName + ".promptCacheOptions"
	invalid := func(sub, message string) error {
		if sub != "" {
			field += "." + sub
		}
		return &providererrors.InvalidArgumentError{Field: field, Message: message}
	}
	if value == nil {
		return nil
	}
	m, ok := value.(map[string]interface{})
	if !ok {
		return invalid("", "must be an object")
	}
	if v, ok := m["mode"]; ok {
		if s, isString := v.(string); !isString || (s != "implicit" && s != "explicit") {
			return invalid("mode", `must be one of "implicit", "explicit"`)
		}
	}
	if v, ok := m["ttl"]; ok {
		if s, isString := v.(string); !isString || (s != "30m" && s != "12h") {
			return invalid("ttl", `must be one of "30m", "12h"`)
		}
	}
	if v, ok := m["prewarm"]; ok {
		if _, isBool := v.(bool); !isBool {
			return invalid("prewarm", "must be a boolean")
		}
	}
	return nil
}
