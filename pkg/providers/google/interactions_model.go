package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

const (
	defaultInteractionsPollInitialDelay = time.Second
	defaultInteractionsPollMaxDelay     = 10 * time.Second
	defaultInteractionsPollTimeout      = 30 * time.Minute
)

// InteractionsConfig carries everything InteractionsLanguageModel needs to
// make HTTP requests, generalized (TS `GoogleInteractionsConfig`) so both the
// google and googlevertex packages can construct one: google.Provider points
// it at its own API-key-authenticated client, and googlevertex.Provider
// points it at a location-scoped, OAuth-authenticated client (the OAuth
// token is injected by Client's underlying *http.Client transport, not by
// this config) — see googlevertex's Interactions() constructor.
type InteractionsConfig struct {
	// ProviderName is the LanguageModel.Provider() string, e.g.
	// "google.generative-ai.interactions" or "google.vertex.interactions".
	ProviderName string
	// Client issues the actual /interactions HTTP requests.
	Client *internalhttp.Client
	// GenerateID generates fallback tool-call/source IDs (TS
	// `GoogleInteractionsConfig.generateId`). Reserved for future use; the
	// current implementation derives fallback IDs from the response/step
	// shape instead (matching existing Go behavior).
	GenerateID func() string
}

// InteractionsLanguageModel targets the Gemini Interactions API
// (POST /interactions) instead of the Gemini generateContent API.
type InteractionsLanguageModel struct {
	cfg     InteractionsConfig
	modelID string
	agent   string
}

// interactionsConfigFromProvider builds the InteractionsConfig for the base
// (API-key) Google provider: the Interactions API is served from the same
// host as generateContent, so it reuses the provider's own client verbatim.
func interactionsConfigFromProvider(p *Provider) InteractionsConfig {
	return InteractionsConfig{ProviderName: p.Name() + ".interactions", Client: p.client}
}

func NewInteractionsLanguageModel(p *Provider, modelID string) *InteractionsLanguageModel {
	return NewInteractionsLanguageModelWithConfig(interactionsConfigFromProvider(p), modelID)
}

func NewInteractionsAgentModel(p *Provider, agent string) *InteractionsLanguageModel {
	return NewInteractionsAgentModelWithConfig(interactionsConfigFromProvider(p), agent)
}

// NewInteractionsManagedAgentModel creates an Interactions API model for a
// user-defined agent created via the Agent Builder API (TS
// `GoogleInteractionsModelInput`'s `{ managedAgent: string }` shape). On the
// wire it behaves identically to a preset agent: the request body sends
// `agent: <id>` and rejects `generation_config` the same way.
func NewInteractionsManagedAgentModel(p *Provider, id string) *InteractionsLanguageModel {
	return NewInteractionsManagedAgentModelWithConfig(interactionsConfigFromProvider(p), id)
}

// NewInteractionsLanguageModelWithConfig builds an Interactions model from an
// explicit InteractionsConfig, letting other packages (googlevertex) reuse
// this implementation with their own client/base URL/auth.
func NewInteractionsLanguageModelWithConfig(cfg InteractionsConfig, modelID string) *InteractionsLanguageModel {
	return &InteractionsLanguageModel{cfg: cfg, modelID: modelID}
}

// NewInteractionsAgentModelWithConfig is the InteractionsConfig-based
// counterpart of NewInteractionsAgentModel.
func NewInteractionsAgentModelWithConfig(cfg InteractionsConfig, agent string) *InteractionsLanguageModel {
	return &InteractionsLanguageModel{cfg: cfg, modelID: agent, agent: agent}
}

// NewInteractionsManagedAgentModelWithConfig is the InteractionsConfig-based
// counterpart of NewInteractionsManagedAgentModel.
func NewInteractionsManagedAgentModelWithConfig(cfg InteractionsConfig, id string) *InteractionsLanguageModel {
	return &InteractionsLanguageModel{cfg: cfg, modelID: id, agent: id}
}

func (m *InteractionsLanguageModel) SpecificationVersion() string { return "v4" }

func (m *InteractionsLanguageModel) Provider() string {
	return m.cfg.ProviderName
}

func (m *InteractionsLanguageModel) ModelID() string { return m.modelID }

func (m *InteractionsLanguageModel) SupportsTools() bool { return true }

func (m *InteractionsLanguageModel) SupportsStructuredOutput() bool { return true }

func (m *InteractionsLanguageModel) SupportsImageInput() bool { return true }

// SupportedURLs returns the URL media patterns accepted directly by the
// Interactions API, mirroring the TypeScript SDK supportedUrls capability.
func (m *InteractionsLanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*":         {`^https?://.+`},
		"application/pdf": {`^https?://.+`},
		"audio/*":         {`^https?://.+`},
		"video/*": {
			`^https?://(www\.)?youtube\.com/watch\?v=.+`,
			`^https?://youtu\.be/.+`,
			`^gs://.+`,
		},
	}
}

func (m *InteractionsLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	args, warnings, interactionsOpts, err := m.buildArgs(opts, false)
	if err != nil {
		return nil, err
	}

	var response interactionsResponse
	resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/interactions",
		Body:    args,
		Headers: requestHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if m.agent != "" && !isTerminalInteractionStatus(response.Status) {
		response, resp, err = m.pollUntilTerminal(ctx, response.ID, requestHeaders(opts), interactionsOpts.PollingTimeoutMs)
		if err != nil {
			return nil, err
		}
	}

	result := m.convertResponse(response, resp, args, warnings)
	return result, nil
}

func (m *InteractionsLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	args, warnings, interactionsOpts, err := m.buildArgs(opts, true)
	if err != nil {
		return nil, err
	}
	headers := requestHeaders(opts)

	if m.agent != "" {
		var response interactionsResponse
		resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    "/interactions",
			Body:    args,
			Headers: headers,
		}, &response)
		if err != nil {
			return nil, m.handleError(err)
		}
		if response.ID == "" {
			return nil, fmt.Errorf("google.interactions: background POST response did not include an interaction id; cannot stream the result")
		}
		if isTerminalInteractionStatus(response.Status) {
			return newSynthesizedInteractionsStream(response, warnings, resp.Headers), nil
		}
		return newInteractionsStream(ctx, m.cfg.Client, response.ID, headers, warnings, providerutils.ExtractHeaders(resp.Headers), interactionsOpts.PollingTimeoutMs)
	}

	streamArgs := args
	streamArgs.Stream = true
	httpResp, err := m.cfg.Client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/interactions",
		Body:   streamArgs,
		Headers: internalhttp.MergeHeaders(headers, map[string]string{
			"Accept": "text/event-stream",
		}),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return newInteractionsEventStream(ctx, m.cfg.Client, httpResp.Body, "", headers, warnings, providerutils.ExtractHeaders(httpResp.Header), 0), nil
}

func (m *InteractionsLanguageModel) buildArgs(opts *provider.GenerateOptions, _ bool) (interactionsRequest, []types.Warning, GoogleInteractionsProviderOptions, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	allowSystem := opts.AllowSystemMessages || opts.AllowSystemInMessages
	normalizedPrompt, err := prompt.NormalizePrompt(opts.Prompt, allowSystem)
	if err != nil {
		return interactionsRequest{}, nil, GoogleInteractionsProviderOptions{}, err
	}
	// Defensive, idempotent: pkg/ai's per-step NormalizePromptWithDownloadSupport
	// already merges consecutive tool messages before DoGenerate is called, but
	// buildArgs can also be reached directly (bypassing pkg/ai), and this model
	// builds one Interactions API turn per source message, so unmerged
	// consecutive tool-role messages would otherwise produce separate turns.
	normalizedPrompt.Messages = prompt.MergeConsecutiveToolMessages(normalizedPrompt.Messages)
	normalizedOpts := *opts
	normalizedOpts.Prompt = normalizedPrompt

	interactionsOpts, err := parseInteractionsProviderOptions(opts.ProviderOptions)
	if err != nil {
		return interactionsRequest{}, nil, GoogleInteractionsProviderOptions{}, err
	}
	isAgent := m.agent != ""
	warnings := make([]types.Warning, 0)

	// Agents keep tools (TS `hasTools` gate is unconditional on isAgent): the
	// Interactions API accepts `tools`/`tool_choice` on agent requests too
	// (e.g. `google.file_search` for Deep Research agents).
	toolsForBody, toolChoice, toolWarnings := prepareInteractionsTools(opts.Tools, opts.ToolChoice)
	warnings = append(warnings, toolWarnings...)
	if !isAgent {
		if opts.FrequencyPenalty != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
		}
		if opts.PresencePenalty != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
		}
	}

	input, systemInstruction, convWarnings, err := m.convertPrompt(normalizedOpts.Prompt, interactionsOpts)
	if err != nil {
		return interactionsRequest{}, nil, GoogleInteractionsProviderOptions{}, err
	}
	warnings = append(warnings, convWarnings...)
	if systemInstruction != "" && interactionsOpts.SystemInstruction != "" {
		warnings = append(warnings, types.Warning{
			Type:    "other",
			Message: "google.interactions: both AI SDK system message and providerOptions.google.systemInstruction were set; using the AI SDK system message.",
			Details: "google.interactions: both AI SDK system message and providerOptions.google.systemInstruction were set; using the AI SDK system message.",
		})
	} else if systemInstruction == "" {
		systemInstruction = interactionsOpts.SystemInstruction
	}

	/*
	 * response_format is a polymorphic array of entries. Three sources
	 * contribute, in order:
	 *
	 *  1. AI SDK call-level responseFormat:{type:'json',schema} ->
	 *     {type:'text', mime_type:'application/json', schema?} (agent calls
	 *     drop this with a warning; the Interactions API cannot combine
	 *     structured output with an agent).
	 *  2. providerOptions.google.responseFormat entries (camelCase ->
	 *     snake_case), sent for both model and agent calls.
	 *  3. providerOptions.google.imageConfig (deprecated fallback), model
	 *     calls only: contributes an image entry only when responseFormat
	 *     didn't already supply one.
	 */
	var responseFormatEntries []map[string]interface{}
	if opts.ResponseFormat != nil && (opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema" || opts.ResponseFormat.Type == "json_object") {
		if isAgent {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "google.interactions: structured output (responseFormat) is not supported when an agent is set; responseFormat will be ignored.",
				Details: "google.interactions: structured output (responseFormat) is not supported when an agent is set; responseFormat will be ignored.",
			})
		} else {
			entry := map[string]interface{}{"type": "text", "mime_type": "application/json"}
			if opts.ResponseFormat.Schema != nil {
				entry["schema"] = opts.ResponseFormat.Schema
			}
			responseFormatEntries = append(responseFormatEntries, entry)
		}
	}
	hasImageEntry := false
	for _, entry := range interactionsOpts.ResponseFormat {
		switch stringValue(entry["type"]) {
		case "text":
			responseFormatEntries = append(responseFormatEntries, pruneMap(map[string]interface{}{
				"type":      "text",
				"mime_type": entry["mimeType"],
				"schema":    entry["schema"],
			}))
		case "image":
			hasImageEntry = true
			responseFormatEntries = append(responseFormatEntries, pruneMap(map[string]interface{}{
				"type":         "image",
				"mime_type":    entry["mimeType"],
				"aspect_ratio": entry["aspectRatio"],
				"image_size":   entry["imageSize"],
			}))
		case "audio":
			responseFormatEntries = append(responseFormatEntries, pruneMap(map[string]interface{}{
				"type":      "audio",
				"mime_type": entry["mimeType"],
			}))
		case "video":
			responseFormatEntries = append(responseFormatEntries, pruneMap(map[string]interface{}{
				"type":         "video",
				"aspect_ratio": entry["aspectRatio"],
				"resolution":   entry["resolution"],
				"duration":     entry["duration"],
				"delivery":     entry["delivery"],
				"gcs_uri":      entry["gcsUri"],
			}))
		}
	}

	var generationConfig map[string]interface{}
	if isAgent {
		dropped := droppedAgentGenerationFields(opts, interactionsOpts)
		if len(dropped) > 0 {
			msg := fmt.Sprintf("google.interactions: %s %s not supported when an agent is set; use providerOptions.google.agentConfig instead. Dropped from the request body.", strings.Join(dropped, ", "), pluralVerb(len(dropped)))
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
		}
	} else {
		generationConfig = pruneMap(map[string]interface{}{
			"temperature":        derefFloat(opts.Temperature),
			"top_p":              derefFloat(opts.TopP),
			"top_k":              derefInt(opts.TopK),
			"seed":               derefInt(opts.Seed),
			"stop_sequences":     nonEmptyStrings(opts.StopSequences),
			"max_output_tokens":  derefInt(opts.MaxTokens),
			"thinking_level":     emptyToNil(interactionsOpts.ThinkingLevel),
			"thinking_summaries": emptyToNil(interactionsOpts.ThinkingSummaries),
			"tool_choice":        toolChoice,
		})

		// Deprecated fallback: imageConfig contributes an image entry only
		// when responseFormat didn't already supply one. Always warn.
		if len(interactionsOpts.ImageConfig) > 0 {
			msg := "google.interactions: providerOptions.google.imageConfig is deprecated. Use providerOptions.google.responseFormat with a { type: \"image\", ... } entry instead."
			if hasImageEntry {
				msg = "google.interactions: providerOptions.google.imageConfig is deprecated and was ignored because providerOptions.google.responseFormat already supplies an image entry. Use responseFormat exclusively."
			}
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
			if !hasImageEntry {
				snake := snakeImageConfig(interactionsOpts.ImageConfig)
				entry := map[string]interface{}{"type": "image", "mime_type": "image/png"}
				if v, ok := snake["aspect_ratio"]; ok {
					entry["aspect_ratio"] = v
				}
				if v, ok := snake["image_size"]; ok {
					entry["image_size"] = v
				}
				responseFormatEntries = append(responseFormatEntries, entry)
			}
		}
	}

	body := interactionsRequest{
		Input:                 input,
		SystemInstruction:     systemInstruction,
		Tools:                 toolsForBody,
		ResponseFormat:        responseFormatEntries,
		ResponseModalities:    interactionsOpts.ResponseModalities,
		PreviousInteractionID: interactionsOpts.PreviousInteractionID,
		ServiceTier:           interactionsOpts.ServiceTier,
		Store:                 interactionsOpts.Store,
		GenerationConfig:      generationConfig,
		AgentConfig:           emptyMapToNil(interactionsOpts.AgentConfig),
		Environment:           interactionsOpts.Environment,
		Background:            interactionsOpts.Background,
	}
	if isAgent {
		body.Agent = m.agent
		if body.Background == nil {
			background := true
			body.Background = &background
		}
	} else if interactionsOpts.Agent != "" {
		body.Agent = interactionsOpts.Agent
	} else {
		body.Model = m.modelID
	}
	return body, warnings, interactionsOpts, nil
}

func (m *InteractionsLanguageModel) convertResponse(response interactionsResponse, httpResp *internalhttp.Response, request interactionsRequest, warnings []types.Warning) *types.GenerateResult {
	content, toolCalls, hasFunctionCall := m.parseOutputs(response.Steps, normalizedInteractionID(response.ID))
	text, _, _, _ := splitContent(content)
	finishReason := mapInteractionsFinishReason(response.Status, hasFunctionCall)
	headers := map[string]string(nil)
	var rawBody interface{}
	if httpResp != nil {
		headers = providerutils.ExtractHeaders(httpResp.Headers)
		rawBody = json.RawMessage(httpResp.Body)
	}
	serviceTier := response.ServiceTier
	if serviceTier == "" && headers != nil {
		serviceTier = headers["x-gemini-service-tier"]
	}
	googleMeta := pruneMap(map[string]interface{}{
		"interactionId":          emptyToNil(normalizedInteractionID(response.ID)),
		"serviceTier":            emptyToNil(serviceTier),
		"outputTokensByModality": outputTokensByModalityMap(response.Usage),
	})
	if googleMeta == nil {
		googleMeta = map[string]interface{}{}
	}
	providerMeta := map[string]interface{}{"google": googleMeta}
	created, _ := time.Parse(time.RFC3339, response.Created)
	respMeta := &types.ResponseMetadata{
		ID:        normalizedInteractionID(response.ID),
		Timestamp: created,
		ModelID:   response.Model,
		Headers:   headers,
		Body:      rawBody,
	}
	return &types.GenerateResult{
		Text:             text,
		Content:          content,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		Usage:            convertInteractionsUsage(response.Usage),
		RawRequest:       request,
		RawResponse:      rawBody,
		Warnings:         warnings,
		ProviderMetadata: providerMeta,
		ResponseHeaders:  headers,
		ResponseMetadata: respMeta,
	}
}

func (m *InteractionsLanguageModel) pollUntilTerminal(ctx context.Context, interactionID string, headers map[string]string, timeoutMs int) (interactionsResponse, *internalhttp.Response, error) {
	if interactionID == "" {
		return interactionsResponse{}, nil, fmt.Errorf("google.interactions: background POST response did not include an interaction id")
	}
	timeout := defaultInteractionsPollTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	nextDelay := defaultInteractionsPollInitialDelay
	startedAt := time.Now()
	for {
		if time.Since(startedAt) > timeout {
			_ = m.cancelInteraction(context.Background(), interactionID, headers)
			return interactionsResponse{}, nil, fmt.Errorf("google.interactions: timed out polling interaction %s after %dms", interactionID, timeout.Milliseconds())
		}
		timer := time.NewTimer(nextDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = m.cancelInteraction(context.Background(), interactionID, headers)
			return interactionsResponse{}, nil, ctx.Err()
		case <-timer.C:
		}

		var response interactionsResponse
		resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
			Method:  http.MethodGet,
			Path:    "/interactions/" + url.PathEscape(interactionID),
			Headers: headers,
		}, &response)
		if err != nil {
			if ctx.Err() != nil {
				_ = m.cancelInteraction(context.Background(), interactionID, headers)
			}
			return interactionsResponse{}, nil, m.handleError(err)
		}
		if isTerminalInteractionStatus(response.Status) {
			return response, resp, nil
		}
		nextDelay *= 2
		if nextDelay > defaultInteractionsPollMaxDelay {
			nextDelay = defaultInteractionsPollMaxDelay
		}
	}
}

func (m *InteractionsLanguageModel) cancelInteraction(ctx context.Context, interactionID string, headers map[string]string) error {
	if interactionID == "" {
		return nil
	}
	_, err := m.cfg.Client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/interactions/" + url.PathEscape(interactionID) + "/cancel",
		Headers: headers,
		Body:    map[string]interface{}{},
	})
	return err
}

func (m *InteractionsLanguageModel) handleError(err error) error {
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

func requestHeaders(opts *provider.GenerateOptions) map[string]string {
	if opts == nil {
		return nil
	}
	return opts.Headers
}

func parseInteractionsProviderOptions(providerOptions map[string]interface{}) (GoogleInteractionsProviderOptions, error) {
	var out GoogleInteractionsProviderOptions
	raw, ok := providerOptions["google"]
	if !ok {
		return out, nil
	}
	switch v := raw.(type) {
	case GoogleInteractionsProviderOptions:
		if err := validateInteractionsProviderOptions(v); err != nil {
			return out, err
		}
		return v, nil
	case *GoogleInteractionsProviderOptions:
		if v != nil {
			if err := validateInteractionsProviderOptions(*v); err != nil {
				return out, err
			}
			return *v, nil
		}
	case map[string]interface{}:
		out.PreviousInteractionID, _ = v["previousInteractionId"].(string)
		out.MediaResolution, _ = v["mediaResolution"].(string)
		out.SystemInstruction, _ = v["systemInstruction"].(string)
		out.ServiceTier, _ = v["serviceTier"].(string)
		out.ThinkingLevel, _ = v["thinkingLevel"].(string)
		out.ThinkingSummaries, _ = v["thinkingSummaries"].(string)
		out.Agent, _ = v["agent"].(string)
		out.Environment = v["environment"]
		if background, ok := v["background"].(bool); ok {
			out.Background = &background
		}
		if store, ok := v["store"].(bool); ok {
			out.Store = &store
		}
		if modalities, ok := v["responseModalities"].([]string); ok {
			out.ResponseModalities = modalities
		} else if modalities, ok := v["responseModalities"].([]interface{}); ok {
			for _, item := range modalities {
				if s, ok := item.(string); ok {
					out.ResponseModalities = append(out.ResponseModalities, s)
				}
			}
		}
		if imageConfig, ok := v["imageConfig"].(map[string]interface{}); ok {
			out.ImageConfig = snakeImageConfig(imageConfig)
		}
		if rf, ok := v["responseFormat"].([]interface{}); ok {
			for _, item := range rf {
				if m, ok := item.(map[string]interface{}); ok {
					out.ResponseFormat = append(out.ResponseFormat, m)
				}
			}
		} else if rf, ok := v["responseFormat"].([]map[string]interface{}); ok {
			out.ResponseFormat = rf
		}
		if agentConfig, ok := v["agentConfig"].(map[string]interface{}); ok {
			out.AgentConfig = snakeAgentConfig(agentConfig)
		}
		if n, ok := numberToInt(v["pollingTimeoutMs"]); ok {
			out.PollingTimeoutMs = n
		}
	default:
		return out, fmt.Errorf("google interactions provider options must be GoogleInteractionsProviderOptions or map[string]interface{}, got %T", raw)
	}
	if err := validateInteractionsProviderOptions(out); err != nil {
		return out, err
	}
	return out, nil
}

func validateInteractionsProviderOptions(opts GoogleInteractionsProviderOptions) error {
	if opts.MediaResolution != "" && !containsString([]string{"low", "medium", "high", "ultra_high"}, opts.MediaResolution) {
		return fmt.Errorf("google.interactions: mediaResolution %q is not supported", opts.MediaResolution)
	}
	if opts.ServiceTier != "" && !containsString([]string{"flex", "standard", "priority"}, opts.ServiceTier) {
		return fmt.Errorf("google.interactions: serviceTier %q is not supported", opts.ServiceTier)
	}
	if opts.ThinkingLevel != "" && !containsString([]string{"minimal", "low", "medium", "high"}, opts.ThinkingLevel) {
		return fmt.Errorf("google.interactions: thinkingLevel %q is not supported", opts.ThinkingLevel)
	}
	if opts.ThinkingSummaries != "" && !containsString([]string{"auto", "none"}, opts.ThinkingSummaries) {
		return fmt.Errorf("google.interactions: thinkingSummaries %q is not supported", opts.ThinkingSummaries)
	}
	for _, modality := range opts.ResponseModalities {
		if !containsString([]string{"text", "image", "audio", "video", "document"}, modality) {
			return fmt.Errorf("google.interactions: responseModalities value %q is not supported", modality)
		}
	}
	if aspectRatio, _ := firstMapString(opts.ImageConfig, "aspectRatio", "aspect_ratio"); aspectRatio != "" && !containsString([]string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "1:8", "8:1", "1:4", "4:1"}, aspectRatio) {
		return fmt.Errorf("google.interactions: imageConfig.aspectRatio %q is not supported", aspectRatio)
	}
	if imageSize, _ := firstMapString(opts.ImageConfig, "imageSize", "image_size"); imageSize != "" && !containsString([]string{"1K", "2K", "4K", "512"}, imageSize) {
		return fmt.Errorf("google.interactions: imageConfig.imageSize %q is not supported", imageSize)
	}
	if typ, _ := opts.AgentConfig["type"].(string); typ != "" {
		switch typ {
		case "dynamic", "deep-research":
		default:
			return fmt.Errorf("google.interactions: agentConfig.type %q is not supported", typ)
		}
	}
	if summaries, _ := firstMapString(opts.AgentConfig, "thinkingSummaries", "thinking_summaries"); summaries != "" && !containsString([]string{"auto", "none"}, summaries) {
		return fmt.Errorf("google.interactions: agentConfig.thinkingSummaries %q is not supported", summaries)
	}
	if visualization, _ := opts.AgentConfig["visualization"].(string); visualization != "" && !containsString([]string{"off", "auto"}, visualization) {
		return fmt.Errorf("google.interactions: agentConfig.visualization %q is not supported", visualization)
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func snakeImageConfig(in map[string]interface{}) map[string]interface{} {
	aspectRatio := firstMapValue(in, "aspectRatio", "aspect_ratio")
	imageSize := firstMapValue(in, "imageSize", "image_size")
	return pruneMap(map[string]interface{}{
		"aspect_ratio": aspectRatio,
		"image_size":   imageSize,
	})
}

func snakeAgentConfig(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		switch k {
		case "thinkingSummaries":
			out["thinking_summaries"] = v
		case "collaborativePlanning":
			out["collaborative_planning"] = v
		default:
			out[k] = v
		}
	}
	return out
}

func firstMapValue(in map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if v, ok := in[key]; ok {
			return v
		}
	}
	return nil
}

func firstMapString(in map[string]interface{}, keys ...string) (string, bool) {
	for _, key := range keys {
		if v, ok := in[key]; ok {
			s, ok := v.(string)
			return s, ok
		}
	}
	return "", false
}

func droppedAgentGenerationFields(opts *provider.GenerateOptions, googleOpts GoogleInteractionsProviderOptions) []string {
	var dropped []string
	if opts.Temperature != nil {
		dropped = append(dropped, "temperature")
	}
	if opts.TopP != nil {
		dropped = append(dropped, "topP")
	}
	if opts.TopK != nil {
		dropped = append(dropped, "topK")
	}
	if opts.FrequencyPenalty != nil {
		dropped = append(dropped, "frequencyPenalty")
	}
	if opts.PresencePenalty != nil {
		dropped = append(dropped, "presencePenalty")
	}
	if opts.Seed != nil {
		dropped = append(dropped, "seed")
	}
	if len(opts.StopSequences) > 0 {
		dropped = append(dropped, "stopSequences")
	}
	if opts.MaxTokens != nil {
		dropped = append(dropped, "maxOutputTokens")
	}
	if googleOpts.ThinkingLevel != "" {
		dropped = append(dropped, "thinkingLevel")
	}
	if googleOpts.ThinkingSummaries != "" {
		dropped = append(dropped, "thinkingSummaries")
	}
	if len(googleOpts.ImageConfig) > 0 {
		dropped = append(dropped, "imageConfig")
	}
	return dropped
}

func pluralVerb(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func derefFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func derefInt(v *int) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nonEmptyStrings(v []string) interface{} {
	if len(v) == 0 {
		return nil
	}
	return v
}

func emptyToNil(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

// stringValue extracts a string from an interface{}, returning "" for any
// other type (used to read the "type" discriminator out of a raw
// responseFormat entry map).
func stringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

func emptyMapToNil(v map[string]interface{}) map[string]interface{} {
	if len(v) == 0 {
		return nil
	}
	return v
}

func pruneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		if m, ok := v.(map[string]interface{}); ok && len(m) == 0 {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func numberToInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func normalizedInteractionID(id string) string {
	if id == "" {
		return ""
	}
	return id
}

func convertInteractionsUsage(usage *interactionsUsage) types.Usage {
	if usage == nil {
		return types.Usage{}
	}
	out := types.Usage{
		InputTokens:  usage.TotalInputTokens,
		OutputTokens: usage.TotalOutputTokens,
		TotalTokens:  usage.TotalTokens,
		Raw: map[string]interface{}{
			"total_input_tokens":          usage.TotalInputTokens,
			"total_output_tokens":         usage.TotalOutputTokens,
			"total_thought_tokens":        usage.TotalThoughtTokens,
			"total_cached_tokens":         usage.TotalCachedTokens,
			"total_tool_use_tokens":       usage.TotalToolUseTokens,
			"total_tokens":                usage.TotalTokens,
			"input_tokens_by_modality":    usage.InputTokensByModality,
			"output_tokens_by_modality":   usage.OutputTokensByModality,
			"cached_tokens_by_modality":   usage.CachedTokensByModality,
			"tool_use_tokens_by_modality": usage.ToolUseTokensByModality,
			"grounding_tool_count":        usage.GroundingToolCount,
		},
	}
	if usage.TotalCachedTokens != nil || len(usage.InputTokensByModality) > 0 {
		out.InputDetails = &types.InputTokenDetails{
			CacheReadTokens: usage.TotalCachedTokens,
			TextTokens:      modalityToken(usage.InputTokensByModality, "text"),
			ImageTokens:     modalityToken(usage.InputTokensByModality, "image"),
		}
	}
	if usage.TotalThoughtTokens != nil || len(usage.OutputTokensByModality) > 0 {
		out.OutputDetails = &types.OutputTokenDetails{
			ReasoningTokens: usage.TotalThoughtTokens,
			TextTokens:      modalityToken(usage.OutputTokensByModality, "text"),
		}
	}
	return out
}

func modalityToken(tokens []interactionsModalityTokens, modality string) *int64 {
	var total int64
	found := false
	for _, item := range tokens {
		if item.Modality == modality && item.Tokens != nil {
			total += *item.Tokens
			found = true
		}
	}
	if !found {
		return nil
	}
	return &total
}

func mapInteractionsFinishReason(status string, hasFunctionCall bool) types.FinishReason {
	switch status {
	case InteractionStatusCompleted:
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonStop
	case InteractionStatusRequiresAction:
		return types.FinishReasonToolCalls
	case InteractionStatusFailed:
		return types.FinishReasonError
	case InteractionStatusIncomplete:
		return types.FinishReasonLength
	case InteractionStatusCancelled:
		return types.FinishReasonOther
	default:
		return types.FinishReasonOther
	}
}

func splitContent(content []types.ContentPart) (string, []types.ReasoningContent, []types.GeneratedFileContent, []types.ToolCall) {
	var text strings.Builder
	var reasoning []types.ReasoningContent
	var files []types.GeneratedFileContent
	for _, part := range content {
		switch p := part.(type) {
		case types.TextContent:
			text.WriteString(p.Text)
		case types.ReasoningContent:
			reasoning = append(reasoning, p)
		case types.GeneratedFileContent:
			files = append(files, p)
		}
	}
	_ = reasoning
	_ = files
	return text.String(), reasoning, files, nil
}

// outputTokensByModalityMap extracts the per-modality output token
// breakdown from an Interactions usage record (e.g. {"video":57920,"text":12}),
// mirroring TS getGoogleInteractionsOutputTokensByModality. Returns nil
// (pruned by pruneMap) when there is no usage or no by-modality data.
func outputTokensByModalityMap(usage *interactionsUsage) map[string]interface{} {
	if usage == nil || len(usage.OutputTokensByModality) == 0 {
		return nil
	}
	out := map[string]interface{}{}
	for _, entry := range usage.OutputTokensByModality {
		if entry.Modality != "" && entry.Tokens != nil {
			out[entry.Modality] = *entry.Tokens
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func providerMetaRaw(signature, interactionID string) json.RawMessage {
	google := pruneMap(map[string]interface{}{
		"signature":     emptyToNil(signature),
		"interactionId": emptyToNil(interactionID),
	})
	if len(google) == 0 {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{"google": google})
	return raw
}

func providerMetaMap(signature, interactionID string) map[string]interface{} {
	google := pruneMap(map[string]interface{}{
		"signature":     emptyToNil(signature),
		"interactionId": emptyToNil(interactionID),
	})
	if len(google) == 0 {
		return nil
	}
	return map[string]interface{}{"google": google}
}

// processingMeta builds the providerMetadata.google payload for
// `google.processing_call`/`google.processing_result` custom content parts
// (TS googleProviderMetadata's processingId/processingCallId fields).
func processingMeta(signature, interactionID, processingID, processingCallID string) json.RawMessage {
	google := pruneMap(map[string]interface{}{
		"signature":        emptyToNil(signature),
		"interactionId":    emptyToNil(interactionID),
		"processingId":     emptyToNil(processingID),
		"processingCallId": emptyToNil(processingCallID),
	})
	if len(google) == 0 {
		return nil
	}
	raw, _ := json.Marshal(map[string]interface{}{"google": google})
	return raw
}

func signatureFromRawMetadata(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var envelope map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	google := envelope["google"]
	if google == nil {
		return ""
	}
	if sig, _ := google["signature"].(string); sig != "" {
		return sig
	}
	return ""
}

func extractGoogleMetadata(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	if google, ok := root["google"].(map[string]interface{}); ok {
		return google
	}
	return nil
}

func googleSignature(raw json.RawMessage) string {
	if google := extractGoogleMetadata(raw); google != nil {
		if s, ok := google["signature"].(string); ok {
			return s
		}
	}
	return ""
}

func googleInteractionID(raw json.RawMessage) string {
	if google := extractGoogleMetadata(raw); google != nil {
		if s, ok := google["interactionId"].(string); ok {
			return s
		}
	}
	return ""
}

func encodeData(data []byte, dataString string) string {
	if dataString != "" {
		return dataString
	}
	return base64.StdEncoding.EncodeToString(data)
}
