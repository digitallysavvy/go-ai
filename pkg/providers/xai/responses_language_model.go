package xai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"

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

// modelsWithoutNoneReasoningEffortRe matches Grok 4.5 and later, which
// always reason and reject reasoning effort "none" (TS xai-model-catalog.ts
// modelsWithoutNoneReasoningEffort,
// /^grok-(?:4\.[5-9](?!\d)|(?:[5-9]|[1-9]\d+)(?:[.-]|$))/; the lookahead is
// written as "a non-digit or the end"). 4.20 predates 4.5 despite its
// version number.
var modelsWithoutNoneReasoningEffortRe = regexp.MustCompile(`^grok-(?:4\.[5-9](?:[^0-9]|$)|(?:[5-9]|[1-9][0-9]+)(?:[.-]|$))`)

// supportsNoneReasoningEffort ports TS supportsNoneReasoningEffort.
func supportsNoneReasoningEffort(modelID string) bool {
	return !modelsWithoutNoneReasoningEffortRe.MatchString(modelID)
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
func (m *ResponsesLanguageModel) SpecificationVersion() string { return "v4" }

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

	var wire xaiResponseWire
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: opts.Headers,
		// TS createJsonResponseHandler(xaiResponsesResponseSchema).
		ValidateResponse: providerutils.XAIResponsesResponseValidator("xai.responses"),
	}, &wire)
	if err != nil {
		return nil, m.wrapErr(err)
	}
	resp := wire.response()

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
	// TS finishRaw = incomplete_details?.reason ?? status: a present ""
	// reason is kept (raw "", unified "other" unless a function call ran).
	if reason, ok := xaiIncompleteReason(httpResp.Body, false); ok && reason == "" {
		result.RawFinishReason = ""
		if !xaiHasFunctionCall(result.ToolCalls) {
			result.FinishReason = mapXAIResponsesFinishReason("", nil)
		}
	}
	result.ResponseMetadata = providerutils.BuildResponseMetadata(resp.ID, resp.Model, resp.CreatedAt)
	if ts, ok := xaiResponsesCreatedAt(httpResp.Body, false); ok {
		result.ResponseMetadata.Timestamp = ts // TS getResponseMetadata: created_at 0 is the epoch
	}
	result.ResponseMetadata.Headers = result.ResponseHeaders
	// TS reports service_tier, prompt_cache_key and safety_identifier when
	// `!= null`, so a present empty string is reported too.
	if echoes := xaiResponseEchoes(httpResp.Body, false); len(echoes) > 0 {
		if result.ProviderMetadata == nil {
			result.ProviderMetadata = map[string]interface{}{}
		}
		xaiMeta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
		if xaiMeta == nil {
			xaiMeta = map[string]interface{}{}
			result.ProviderMetadata["xai"] = xaiMeta
		}
		for k, v := range echoes {
			xaiMeta[k] = v
		}
	}
	return result, nil
}

// xaiResponseEchoes reads service_tier, prompt_cache_key and
// safety_identifier (of the body, or of its response member for a stream
// event) as TS's `!= null`: each present string, "" included, is returned
// under its providerMetadata key; an absent or null one is left out.
func xaiResponseEchoes(data []byte, event bool) map[string]string {
	type echoes struct {
		ServiceTier      *string `json:"service_tier"`
		PromptCacheKey   *string `json:"prompt_cache_key"`
		SafetyIdentifier *string `json:"safety_identifier"`
	}
	var v struct {
		echoes
		Response echoes `json:"response"`
	}
	if jsonexact.Unmarshal(data, &v) != nil {
		return nil
	}
	e := v.echoes
	if event {
		e = v.Response
	}
	out := map[string]string{}
	if e.ServiceTier != nil {
		out["serviceTier"] = *e.ServiceTier
	}
	if e.PromptCacheKey != nil {
		out["promptCacheKey"] = *e.PromptCacheKey
	}
	if e.SafetyIdentifier != nil {
		out["safetyIdentifier"] = *e.SafetyIdentifier
	}
	return out
}

// xaiIncompleteReason reads incomplete_details.reason (of the body, or of
// its response member for a stream event) as TS's `??`: ok for a present,
// non-null string, "" included.
func xaiIncompleteReason(data []byte, event bool) (string, bool) {
	type details struct {
		IncompleteDetails *struct {
			Reason *string `json:"reason"`
		} `json:"incomplete_details"`
	}
	var v struct {
		details
		Response details `json:"response"`
	}
	if jsonexact.Unmarshal(data, &v) != nil {
		return "", false
	}
	d := v.details
	if event {
		d = v.Response
	}
	if d.IncompleteDetails == nil || d.IncompleteDetails.Reason == nil {
		return "", false
	}
	return *d.IncompleteDetails.Reason, true
}

// xaiHasFunctionCall reports whether a function_call output item produced
// one of the calls (TS hasFunctionCall): only those are not provider-executed.
func xaiHasFunctionCall(calls []types.ToolCall) bool {
	for _, c := range calls {
		if !c.ProviderExecuted {
			return true
		}
	}
	return false
}

// xaiResponseWire decodes a Responses body as TS's schema does:
// created_at is z.number(), so a fractional value is accepted (the exported
// int64 ResponsesAPIResponse.CreatedAt gets whole seconds; the timestamp is
// read from the body with milliseconds). A wrapper, not an UnmarshalJSON on
// the exported type, so structs that embed ResponsesAPIResponse keep
// decoding their own fields.
type xaiResponseWire struct {
	responses.ResponsesAPIResponse
	CreatedAt *float64 `json:"created_at"`
}

func (w *xaiResponseWire) response() responses.ResponsesAPIResponse {
	r := w.ResponsesAPIResponse
	r.CreatedAt = xaiWholeSeconds(w.CreatedAt)
	return r
}

// xaiCreatedEventWire is responses.ResponseCreatedEvent with a z.number()
// created_at (see xaiResponseWire).
type xaiCreatedEventWire struct {
	Type     string `json:"type"`
	Response struct {
		ID        string   `json:"id"`
		CreatedAt *float64 `json:"created_at"`
		Model     string   `json:"model"`
	} `json:"response"`
}

// xaiWholeSeconds truncates a wire unix-seconds number toward zero; nil,
// NaN, ±Inf or out of the int64 range is 0.
func xaiWholeSeconds(v *float64) int64 {
	if v == nil || math.IsNaN(*v) || math.IsInf(*v, 0) || math.Abs(*v) > 9e18 {
		return 0
	}
	return int64(*v)
}

// xaiResponsesCreatedAt reads created_at (of the body, or of its response
// member for a stream event) as TS getResponseMetadata /
// createLanguageModelResponseMetadata: a present value, 0 included, is
// new Date(created_at * 1000); an absent or null one is no timestamp.
func xaiResponsesCreatedAt(data []byte, event bool) (time.Time, bool) {
	var v struct {
		CreatedAt *float64 `json:"created_at"`
		Response  struct {
			CreatedAt *float64 `json:"created_at"`
		} `json:"response"`
	}
	if jsonexact.Unmarshal(data, &v) != nil {
		return time.Time{}, false
	}
	c := v.CreatedAt
	if event {
		c = v.Response.CreatedAt
	}
	// TS TimeClip: |t| > 8.64e15 ms is an Invalid Date.
	if c == nil || math.IsNaN(*c) || math.IsInf(*c, 0) || math.Abs(*c*1000) > 8.64e15 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(*c * 1000)), true
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
	// Row dc2f851: xAI Responses does not support frequencyPenalty/
	// presencePenalty (topK IS supported and forwarded as top_k, see
	// buildRequestBody).
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	// TS warns frequencyPenalty, presencePenalty, then stopSequences.
	// TS: stopSequences != null (an empty array warns too).
	if opts.StopSequences != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "stopSequences",
		})
	}
	return warnings
}

// buildRequestBody constructs the Responses API request body.
func (m *ResponsesLanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning

	if err := validateResponsesOptions(opts.ProviderOptions); err != nil {
		return nil, warnings, err
	}

	// Extract XAI-specific provider options.
	var xaiOpts XAIResponsesProviderOptions
	var rawXAIOpts map[string]interface{}
	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["xai"]; ok {
			if jsonData, err := json.Marshal(raw); err == nil {
				json.Unmarshal(jsonData, &xaiOpts)    //nolint:errcheck
				json.Unmarshal(jsonData, &rawXAIOpts) //nolint:errcheck
			}
		}
	}
	// TS forwards every string option that is not undefined, so an empty
	// string is sent; the typed struct cannot tell "" from absent.
	xaiString := func(key string) (string, bool) {
		v, ok := rawXAIOpts[key].(string)
		return v, ok
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
	if v, ok := xaiString("promptCacheKey"); ok {
		body["prompt_cache_key"] = v
	}
	if v, ok := xaiString("safetyIdentifier"); ok {
		body["safety_identifier"] = v
	}
	if v, ok := xaiString("user"); ok {
		body["user"] = v
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
		// TS appends reasoning.encrypted_content without a duplicate check.
		include = append(include, "reasoning.encrypted_content")
	}
	// TS: `options.include ? [...options.include] : undefined` -- an empty
	// array is truthy, so `include: []` is sent when the option is present.
	if _, includePresent := rawXAIOpts["include"].([]interface{}); len(include) > 0 || includePresent {
		body["include"] = include
	}

	// Previous response ID for stateful multi-turn conversations.
	if v, ok := xaiString("previousResponseId"); ok {
		body["previous_response_id"] = v
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
		// TS sets tools only when at least one tool survived preparation.
		if len(preparedTools) > 0 {
			body["tools"] = preparedTools
		}
		// TS prepareResponsesTools throws for an unknown tool choice type.
		if err := prompt.ValidateToolChoiceType(opts.Tools, opts.ToolChoice); err != nil {
			return nil, nil, err
		}
		if opts.ToolChoice.Type != "" {
			choice, choiceWarnings := resolveXAIResponsesToolChoice(opts.ToolChoice, opts.Tools)
			warnings = append(warnings, choiceWarnings...)
			if choice != nil {
				body["tool_choice"] = choice
			}
		}
	}

	// TS resolves reasoning after the input and tool warnings. A provider
	// option effort is sent unchanged; otherwise the top-level reasoning is
	// mapped (TS 61a315e333) onto {none (models that accept it), low,
	// medium, high, xhigh (grok-4.6 only)} with
	// providerutils.MapReasoningToProviderEffort: Grok 4.5+ map "none" to
	// "low" with a compatibility warning instead of sending a value they
	// reject. grok-4.20-(non-)reasoning models reject the reasoning effort
	// parameter entirely (row 8e006de).
	effort := xaiOpts.ReasoningEffort
	if effort == "" && providerutils.IsCustomReasoning(opts.Reasoning) {
		if !supportsReasoningEffort(m.modelID) {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning \"%s\" is not supported by this model.", string(*opts.Reasoning)),
			})
		} else {
			effortMap := map[types.ReasoningLevel]string{
				types.ReasoningLow: "low", types.ReasoningMedium: "medium", types.ReasoningHigh: "high",
			}
			if supportsNoneReasoningEffort(m.modelID) {
				effortMap[types.ReasoningNone] = "none"
			}
			if m.modelID == "grok-4.6" {
				effortMap[types.ReasoningXHigh] = "xhigh"
			}
			effort, _ = providerutils.MapReasoningToProviderEffort(opts.Reasoning, effortMap, nil, &warnings)
		}
	}

	// Row 0a5dd0f9c3: xAI ignores the requested reasoning summary level and
	// always returns a detailed summary, so reasoning.summary is no longer
	// sent (xaiOpts.ReasoningSummary is deprecated and read nowhere here).
	if effort != "" {
		body["reasoning"] = map[string]interface{}{"effort": effort}
	}

	return body, warnings, nil
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
		result.Usage = xaiZeroUsage()
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
	// pushCall records a tool call and puts its tool-call part in the content
	// at the output item's position: TS doGenerate pushes every part in output
	// order, so a function call ahead of a message stays ahead of its text and
	// a provider-executed call comes before its tool-result.
	pushCall := func(tc types.ToolCall) {
		toolCalls = append(toolCalls, tc)
		result.Content = append(result.Content, types.ToolCallContent{
			ToolCallID:       tc.ID,
			ToolName:         tc.ToolName,
			Input:            tc.RawArguments,
			Arguments:        tc.Arguments,
			ProviderExecuted: tc.ProviderExecuted,
		})
	}

	for _, rawItem := range resp.Output {
		var peek struct {
			Type string `json:"type"`
		}
		if err := jsonexact.Unmarshal(rawItem, &peek); err != nil {
			continue
		}

		switch peek.Type {
		case "message":
			// Parse with annotations support for url_citation.
			var item struct {
				Content []struct {
					Type        string            `json:"type"`
					Text        string            `json:"text"`
					Annotations []json.RawMessage `json:"annotations,omitempty"`
				} `json:"content"`
			}
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
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
					if source := xaiURLCitationSource(ann); source != nil {
						result.Content = append(result.Content, *source)
					}
				}
			}

		case "function_call":
			var item responses.FunctionCallItem
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			var args map[string]interface{}
			jsonexact.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			pushCall(types.ToolCall{
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
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
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
				if raw, err := jsonstr.Marshal(map[string]interface{}{"xai": meta}); err == nil {
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
				Name      *string         `json:"name,omitempty"`
				Action    json.RawMessage `json:"action,omitempty"`
				Arguments string          `json:"arguments,omitempty"`
				Input     string          `json:"input,omitempty"`
			}
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
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
			_ = jsonexact.Unmarshal([]byte(toolInput), &toolArgs)
			pushCall(types.ToolCall{
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
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := toolNames["xai.file_search"]
			if toolName == "" {
				toolName = "file_search"
			}
			pushCall(types.ToolCall{
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
			// TS: queries ?? [].
			queries := item.Queries
			if queries == nil {
				queries = []string{}
			}
			resultData := map[string]interface{}{
				"queries": queries,
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
			if err := jsonexact.Unmarshal(rawItem, &item); err != nil {
				continue
			}
			toolName := toolNames["xai.image_generation"]
			if toolName == "" {
				toolName = "image_generation"
			}
			pushCall(types.ToolCall{
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
				// TS: isError with the message as the result value.
				failure := fmt.Sprintf("Image generation failed (status: %s).", item.Status)
				result.Content = append(result.Content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   toolName,
					Result:     failure,
					Error:      failure,
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
	}
	// TS 3d6ac0ae11: finishRaw = incomplete_details?.reason ?? status, used
	// for both the unified and the raw finish reason, so a
	// max_output_tokens cut is "length".
	finishRaw := resp.Status
	if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
		finishRaw = resp.IncompleteDetails.Reason
	}
	if !hasFunctionCall {
		result.FinishReason = mapXAIResponsesFinishReason(finishRaw, nil)
	}
	result.RawFinishReason = finishRaw

	return result, nil
}

// xaiZeroUsage is the usage TS reports when a response carries none (doGenerate
// without usage, a stream with no terminal usage): explicit zeros, cacheWrite
// included, and no raw usage.
func xaiZeroUsage() types.Usage {
	z := func() *int64 { v := int64(0); return &v }
	return types.Usage{
		InputTokens:   z(),
		OutputTokens:  z(),
		TotalTokens:   z(),
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: z(), CacheReadTokens: z(), CacheWriteTokens: z()},
		OutputDetails: &types.OutputTokenDetails{TextTokens: z(), ReasoningTokens: z()},
	}
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
		id := t.ProviderID
		if !knownIDs[id] {
			id = t.Name
		}
		// TS tools.find: the first tool with the id names it.
		if _, seen := names[id]; knownIDs[id] && !seen {
			names[id] = t.Name
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
	// TS webSearchWireActionSchema: a discriminated union on type whose
	// string fields are nullish; a field of the wrong type fails the parse.
	// Only the matched branch's fields are checked (zod strips other keys).
	var head struct {
		Type string `json:"type"`
	}
	if err := jsonexact.Unmarshal(raw, &head); err != nil {
		return result
	}
	var action struct {
		Type    string
		Query   *string
		Queries []string
		URL     *string
		Pattern *string
		Sources []json.RawMessage
	}
	action.Type = head.Type
	var branchErr error
	switch head.Type {
	case "search":
		var b struct {
			Query   *string           `json:"query"`
			Queries []string          `json:"queries"`
			Sources []json.RawMessage `json:"sources"`
		}
		branchErr = jsonexact.Unmarshal(raw, &b)
		action.Query, action.Queries, action.Sources = b.Query, b.Queries, b.Sources
	case "open_page":
		var b struct {
			URL     *string           `json:"url"`
			Sources []json.RawMessage `json:"sources"`
		}
		branchErr = jsonexact.Unmarshal(raw, &b)
		action.URL, action.Sources = b.URL, b.Sources
	case "find_in_page":
		var b struct {
			URL     *string           `json:"url"`
			Pattern *string           `json:"pattern"`
			Sources []json.RawMessage `json:"sources"`
		}
		branchErr = jsonexact.Unmarshal(raw, &b)
		action.URL, action.Pattern, action.Sources = b.URL, b.Pattern, b.Sources
	}
	if branchErr != nil {
		return result
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return result
	}
	// nullable returns a nullish string field as TS passes it on: the value,
	// null when the wire sent null, and absent when it was absent.
	nullable := func(a map[string]interface{}, key string, v *string) {
		if v != nil {
			a[key] = *v
		} else if f, ok := fields[key]; ok && string(f) == "null" {
			a[key] = nil
		}
	}

	var a map[string]interface{}
	switch action.Type {
	case "search":
		a = map[string]interface{}{"type": "search"}
		if action.Query != nil {
			a["query"] = *action.Query
		}
		if action.Queries != nil {
			a["queries"] = action.Queries
		}
	case "open_page":
		a = map[string]interface{}{"type": "openPage"}
		nullable(a, "url", action.URL)
	case "find_in_page":
		a = map[string]interface{}{"type": "findInPage"}
		nullable(a, "url", action.URL)
		nullable(a, "pattern", action.Pattern)
	default:
		// TS: an action that matches no branch of the union yields {}.
		return result
	}
	result["action"] = a

	// TS webSearchWireSourceSchema: {type: 'url', url: string}; other
	// sources are dropped.
	var sources []map[string]interface{}
	for _, rawSource := range action.Sources {
		var source struct {
			Type string  `json:"type"`
			URL  *string `json:"url"`
		}
		if jsonexact.Unmarshal(rawSource, &source) != nil || source.Type != "url" || source.URL == nil {
			continue
		}
		sources = append(sources, map[string]interface{}{"type": "url", "url": *source.URL})
	}
	if len(sources) > 0 {
		result["sources"] = sources
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
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure
	}
	if provErr, ok := xaiStatusError("xai.responses", err); ok {
		return provErr // TS xaiFailedResponseHandler
	}
	return err // TS handleFetchError: no HTTP response, returned unchanged
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
	// usageMeta is the usage-derived metadata (costInUsdTicks, cost). TS only
	// replaces it when a terminal event carries usage, so a later event
	// without usage keeps the earlier value.
	usageMeta map[string]interface{}

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
		finishReason:     types.FinishReasonInterrupted,
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

		// TS parseJsonEventStream skips a [DONE] frame; flush() runs when the
		// stream ends, after any later events.
		if streaming.IsStreamDone(event) {
			continue
		}
		if s.includeRawChunks {
			raw := streaming.RawChunkValue(event.Data)
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeRaw,
				Raw:  raw,
			})
		}

		var peek responses.ResponsesStreamEvent
		if err := jsonexact.Unmarshal([]byte(event.Data), &peek); err != nil {
			msg, perr := providerutils.ChunkParseFailure(event.Data, err)
			s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
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
			s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
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
			var e xaiCreatedEventWire
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
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
			if ts, ok := xaiResponsesCreatedAt([]byte(event.Data), true); ok {
				metadata.Timestamp = ts
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.itemTypes[e.OutputIndex] = e.Item.Type
			if e.Item.Type == "message" {
				var added struct {
					Item json.RawMessage `json:"item"`
				}
				if jsonexact.Unmarshal([]byte(event.Data), &added) == nil {
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
				if jsonexact.Unmarshal([]byte(event.Data), &added) == nil {
					s.queueProviderExecutedItem(e.Item.Type, added.Item, false)
				}
			}
			continue

		case "response.output_text.delta":
			var e struct {
				ItemID string `json:"item_id"`
				Delta  string `json:"delta"`
			}
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			blockID := "reasoning-" + e.ItemID
			if _, alreadyStarted := s.activeReasoning[e.ItemID]; alreadyStarted {
				continue
			}
			s.activeReasoning[e.ItemID] = struct{}{}
			meta, _ := jsonstr.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			blockID := "reasoning-" + e.ItemID
			meta, _ := jsonstr.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			blockID := "reasoning-" + e.ItemID
			meta, _ := jsonstr.Marshal(map[string]interface{}{"xai": map[string]interface{}{"itemId": e.ItemID}})
			if _, started := s.activeReasoning[e.ItemID]; !started {
				// First delta without a prior summary_part.added — emit start now.
				s.activeReasoning[e.ItemID] = struct{}{}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:             provider.ChunkTypeReasoningStart,
					ID:               blockID,
					ProviderMetadata: meta,
				}, &provider.StreamChunk{
					Type:             provider.ChunkTypeReasoning,
					ID:               blockID,
					Reasoning:        e.Delta,
					ProviderMetadata: meta,
				})
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
				Annotations []json.RawMessage `json:"annotations,omitempty"`
			}
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			for _, ann := range e.Annotations {
				if source := xaiURLCitationSource(ann); source != nil {
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: source})
				}
			}
			continue

		// Gap 7: individual annotation added mid-stream.
		case "response.output_text.annotation.added":
			var e struct {
				Annotation json.RawMessage `json:"annotation"`
			}
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if source := xaiURLCitationSource(e.Annotation); source != nil {
				chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
					Type:          provider.ChunkTypeSource,
					SourceContent: source,
				})
				if ok {
					return chunk, nil
				}
				continue
			}
			continue

		// TS announces a hosted image_generation call from its progress events
		// when output_item.added has not been seen for the item.
		case "response.image_generation_call.in_progress", "response.image_generation_call.generating", "response.image_generation_call.completed":
			var e struct {
				ItemID string `json:"item_id"`
			}
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			if s.seenToolCalls == nil {
				s.seenToolCalls = map[string]bool{}
			}
			if s.seenToolCalls[e.ItemID] {
				continue
			}
			s.seenToolCalls[e.ItemID] = true
			toolName := s.toolNames["xai.image_generation"]
			if toolName == "" {
				toolName = "image_generation"
			}
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: e.ItemID, ToolName: toolName}},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: e.ItemID, Text: "{}"},
				&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: e.ItemID, ToolCall: &types.ToolCall{ID: e.ItemID, ToolName: toolName}},
				&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
					ID: e.ItemID, ToolName: toolName, Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true,
				}},
			)
			continue

		case "response.output_item.done":
			var e responses.OutputItemDoneEvent
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
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
					s.finishReason = mapXAIResponsesFinishReason(e.Response.Status, nil)
				}
				s.rawFinishReason = e.Response.Status
			} else if s.finishReason == types.FinishReasonInterrupted {
				// The terminal event arrived, but supplied no recognized reason.
				s.finishReason, s.rawFinishReason = types.FinishReasonOther, ""
			}
			s.recordMetadata(e.Response.Usage, xaiResponseEchoes([]byte(event.Data), true))
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
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			s.recordUsage(e.Response.Usage)
			s.finishReason = mapXAIResponsesFinishReason("incomplete", e.Response.IncompleteDetails)
			// TS raw: reason ?? 'incomplete' (a present "" is kept).
			s.rawFinishReason = "incomplete"
			if reason, ok := xaiIncompleteReason([]byte(event.Data), true); ok {
				s.rawFinishReason = reason
			}
			s.recordMetadata(e.Response.Usage, xaiResponseEchoes([]byte(event.Data), true))
			continue

		case "response.failed":
			var e responses.ResponseFailedEvent
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			// TS: reason ? map(reason) : 'error', raw reason ?? 'error' (a
			// present "" is kept as the raw reason).
			s.finishReason = types.FinishReasonError
			s.rawFinishReason = "error"
			if e.Response.IncompleteDetails != nil && e.Response.IncompleteDetails.Reason != "" {
				s.finishReason = mapXAIResponsesFinishReason("incomplete", e.Response.IncompleteDetails)
			}
			if reason, ok := xaiIncompleteReason([]byte(event.Data), true); ok {
				s.rawFinishReason = reason
			}
			s.recordUsage(e.Response.Usage)
			// Mirrors TS xai-responses-language-model.ts's response.failed
			// branch: response.error != null enqueues a structured error chunk
			// (createXaiResponsesStreamError); the finish chunk follows at the
			// end of the stream.
			if e.Response.Error != nil {
				var data interface{}
				_ = jsonexact.Unmarshal([]byte(event.Data), &data)
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: formatXAIResponseError(e.Response.Error.Code, e.Response.Error.Message),
					Err:  newXAIResponsesStreamError("xai", e.Response.Error.Message, e.Response.Error.Code, xaiWireValuePresent(data, "response", "error", "code"), peek.Type, data),
				})
			}
			continue

		case "error":
			var e responses.ResponsesStreamErrorEvent
			if err := jsonexact.Unmarshal([]byte(event.Data), &e); err != nil {
				continue
			}
			streamErr := &XAIStreamError{Code: e.Code, Message: e.Message}
			s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
			var data interface{}
			_ = jsonexact.Unmarshal([]byte(event.Data), &data)
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: streamErr.Error(),
				Err:  newXAIResponsesStreamError("xai", e.Message, e.Code, xaiWireValuePresent(data, "code"), peek.Type, data),
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

// xaiCitationTitle is TS `annotation.title ?? annotation.url`: only a missing
// or null title falls back to the URL, an empty title stays empty.
func xaiCitationTitle(title *string, url string) string {
	if title == nil {
		return url
	}
	return *title
}

// xaiURLCitationSource is TS's `annotation.type === 'url_citation' && 'url'
// in annotation` over xAI's annotation union: the url_citation branch needs a
// string url (an empty one included) and an absent or string title; an
// annotation that fails it parses as the bare {type} branch and is no source.
func xaiURLCitationSource(raw json.RawMessage) *types.SourceContent {
	var ann struct {
		Type  string  `json:"type"`
		URL   *string `json:"url"`
		Title *string `json:"title"`
	}
	if jsonexact.Unmarshal(raw, &ann) != nil || ann.Type != "url_citation" || ann.URL == nil {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	if title, ok := fields["title"]; ok && string(title) == "null" {
		return nil
	}
	return &types.SourceContent{SourceType: "url", ID: streaming.GenerateID(), URL: *ann.URL, Title: xaiCitationTitle(ann.Title, *ann.URL)}
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
// url_citation annotation becomes a source unless the item's text block was
// already streamed (TS 0eb8de60dc: `streamed` is computed once before the
// content loop, so the citations of an output_item.done message whose text
// was streamed are not emitted again).
func (s *xaiResponsesStream) queueMessageItem(raw json.RawMessage) {
	var item struct {
		ID      string `json:"id"`
		Content []struct {
			Text        string            `json:"text"`
			Annotations []json.RawMessage `json:"annotations"`
		} `json:"content"`
	}
	if jsonexact.Unmarshal(raw, &item) != nil {
		return
	}
	blockID := "text-" + item.ID
	streamed := s.textSeen[blockID]
	for _, part := range item.Content {
		if part.Text != "" && !s.textSeen[blockID] {
			s.startTextBlock(blockID)
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: blockID, Text: part.Text})
		}
		if streamed {
			continue
		}
		for _, ann := range part.Annotations {
			if source := xaiURLCitationSource(ann); source != nil {
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: source})
			}
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

// recordMetadata keeps the terminal event's metadata; echoes holds the
// service_tier / prompt_cache_key / safety_identifier the event carried
// (xaiResponseEchoes: TS `?? undefined`, so a present "" is reported).
func (s *xaiResponsesStream) recordMetadata(u *responses.ResponsesAPIUsage, echoes map[string]string) {
	if u != nil {
		s.usageMeta = map[string]interface{}{}
		if u.CostInUsdTicks != nil {
			s.usageMeta["costInUsdTicks"] = *u.CostInUsdTicks
		}
		if u.InputTokensCost != nil || u.OutputTokensCost != nil {
			cost := map[string]interface{}{}
			if u.InputTokensCost != nil {
				cost["inputTokensCost"] = *u.InputTokensCost
			}
			if u.OutputTokensCost != nil {
				cost["outputTokensCost"] = *u.OutputTokensCost
			}
			s.usageMeta["cost"] = cost
		}
	}
	xaiMeta := map[string]interface{}{}
	for k, v := range s.usageMeta {
		xaiMeta[k] = v
	}
	for k, v := range echoes {
		xaiMeta[k] = v
	}
	if len(xaiMeta) == 0 {
		s.finishMeta = nil
		return
	}
	s.finishMeta, _ = jsonstr.Marshal(map[string]interface{}{"xai": xaiMeta})
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
		zero := xaiZeroUsage()
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
	if jsonexact.Unmarshal(e.Item, &doneType) == nil && doneType.Type == "message" {
		delete(s.itemTypes, e.OutputIndex)
		s.queueMessageItem(e.Item)
		return nil, false
	}
	// TS dispatches on the done item's own type, so an item that was never
	// announced by output_item.added is still handled.
	if doneType.Type != "" {
		itemType = doneType.Type
	}

	switch itemType {
	case "function_call":
		s.hasFunctionCall = true
		accum, ok := s.toolAccum[e.OutputIndex]
		if !ok {
			accum = &xaiResponsesToolAccum{}
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
		if jsonexact.Unmarshal(e.Item, &doneItem) == nil {
			// TS: `toolCallId: part.call_id, toolName: part.name, input:
			// part.arguments` -- the done item's values verbatim (required
			// strings in the chunk schema, so a present "" is kept), not the
			// added item's or the accumulated deltas.
			callID, name, rawArgs = doneItem.CallID, doneItem.Name, doneItem.Arguments
		}
		var args map[string]interface{}
		if rawArgs != "" {
			jsonexact.Unmarshal([]byte(rawArgs), &args) //nolint:errcheck
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
		if err := jsonexact.Unmarshal(e.Item, &item); err != nil {
			return nil, false
		}
		// TS emits reasoning-start/-end for every done reasoning item, even
		// one without an id or encrypted content.
		meta := map[string]interface{}{}
		if item.ID != "" {
			meta["itemId"] = item.ID
		}
		if item.EncryptedContent != "" {
			meta["reasoningEncryptedContent"] = item.EncryptedContent
		}
		providerMeta, _ := jsonstr.Marshal(map[string]interface{}{"xai": meta})
		blockID := "reasoning-" + item.ID

		// If no reasoning-start was emitted yet (encrypted reasoning path where
		// reasoning_summary_part.added events are not sent), emit one now.
		if _, started := s.activeReasoning[item.ID]; !started {
			startXAI := map[string]interface{}{}
			if item.ID != "" {
				startXAI["itemId"] = item.ID
			}
			startMeta, _ := jsonstr.Marshal(map[string]interface{}{"xai": startXAI})
			s.flushQueue = append(s.flushQueue,
				&provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: blockID, ProviderMetadata: startMeta},
				&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: blockID, ProviderMetadata: providerMeta},
			)
			return nil, false
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
		Name      *string         `json:"name,omitempty"`
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
	if jsonexact.Unmarshal(raw, &item) != nil {
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
		_ = jsonexact.Unmarshal([]byte(toolInput), &args)
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
			// TS: isError with the message as the result value.
			failure := fmt.Sprintf("Image generation failed (status: %s).", item.Status)
			result.Result = failure
			result.Error = errors.New(failure)
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
// itemName is nil when the item has no (or a null) name: TS `part.name ??`.
func bareXAIToolName(itemType string, namePtr *string, toolNames map[string]string) string {
	itemName := ""
	if namePtr != nil {
		itemName = *namePtr
	}
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
		// TS mcpToolName ?? part.name ?? 'mcp': a present "" name is kept.
		if namePtr != nil {
			return itemName
		}
		return "mcp"
	}
	return itemName
}
