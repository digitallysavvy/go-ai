package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
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
	// GenerateID generates fallback tool-call, processing and source ids (TS
	// `GoogleInteractionsConfig.generateId`). Defaults to a random id.
	GenerateID func() string

	// SerializableConfig returns a JSON-friendly copy of the constructing
	// provider's Config struct (credentials stripped), used for workflow
	// serialization (SER2). Set by the constructing provider
	// (interactionsConfigFromProvider here, or googlevertex.Provider's
	// interactionsConfig); nil if built without a provider back-reference,
	// in which case Serialize() returns an empty config.
	SerializableConfig func() map[string]interface{}
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
	return InteractionsConfig{
		ProviderName: p.Name() + ".interactions",
		Client:       p.client,
		GenerateID:   p.config.GenerateID,
		SerializableConfig: func() map[string]interface{} {
			return provider.SerializableConfig(p.config)
		},
	}
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
		ValidateResponse: providerutils.NodeResponseValidator(m.Provider(), zGoogleInteractions),
		Method:           http.MethodPost,
		Path:             "/interactions",
		Body:             args,
		Headers:          requestHeaders(opts),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Agent calls and background: true calls (e.g. Gemini 4 long decode, where
	// the server runs every hop) may return a non-terminal status. Poll until
	// terminal so the result matches a synchronous call (TS #23261).
	isBackground := interactionsOpts.Background != nil && *interactionsOpts.Background
	if (m.agent != "" || isBackground) && !isTerminalInteractionStatus(response.Status) {
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

	// `background: true` is incompatible with `stream: true` on POST, so
	// background calls (TS isBackground) POST first and then stream the GET.
	if interactionsOpts.Background != nil && *interactionsOpts.Background {
		var response interactionsResponse
		resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
			ValidateResponse: providerutils.NodeResponseValidator(m.Provider(), zGoogleInteractions),
			Method:           http.MethodPost,
			Path:             "/interactions",
			Body:             args,
			Headers:          headers,
		}, &response)
		if err != nil {
			return nil, m.handleError(err)
		}
		if response.ID == "" {
			return nil, fmt.Errorf("google.interactions: background POST response did not include an interaction id; cannot stream the result.") //nolint:staticcheck // matches TS SDK exact error text
		}
		if isTerminalInteractionStatus(response.Status) {
			synth := newSynthesizedInteractionsStream(m, response, warnings, resp.Headers, opts.IncludeRawChunks)
			if s, ok := synth.(*sliceTextStream); ok {
				s.requestBody = args
			}
			return synth, nil
		}
		pollStream, err := newInteractionsStream(ctx, m.cfg.Client, response.ID, headers, warnings, providerutils.ExtractHeaders(resp.Headers), interactionsOpts.PollingTimeoutMs)
		if err != nil {
			return nil, err
		}
		if s, ok := pollStream.(*interactionsStream); ok {
			s.requestBody = args
			s.generateID = m.cfg.GenerateID
			s.includeRawChunks = opts.IncludeRawChunks
		}
		return pollStream, nil
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
	eventStream := newInteractionsEventStream(ctx, m.cfg.Client, httpResp.Body, "", headers, warnings, providerutils.ExtractHeaders(httpResp.Header), 0)
	if s, ok := eventStream.(*interactionsStream); ok {
		s.requestBody = streamArgs
		s.generateID = m.cfg.GenerateID
		s.includeRawChunks = opts.IncludeRawChunks
	}
	return eventStream, nil
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
		// TS parseProviderOptions: InvalidArgumentError "invalid google
		// provider options" wrapping the schema failure.
		return interactionsRequest{}, nil, GoogleInteractionsProviderOptions{}, invalidGoogleFilesOptions(err)
	}
	isAgent := m.agent != ""
	warnings := make([]types.Warning, 0)

	// Agents keep tools (TS `hasTools` gate is unconditional on isAgent): the
	// Interactions API accepts `tools`/`tool_choice` on agent requests too
	// (e.g. `google.file_search` for Deep Research agents).
	// Warning order follows TS getArgs: penalties, tools, agent responseFormat,
	// prompt conversion, system instruction, dropped fields, imageConfig,
	// environment.
	if !isAgent {
		if opts.FrequencyPenalty != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
		}
		if opts.PresencePenalty != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
		}
	}
	toolsForBody, toolChoice, toolWarnings := prepareInteractionsTools(opts.Tools, opts.ToolChoice)
	warnings = append(warnings, toolWarnings...)
	rfIsJSON := opts.ResponseFormat != nil && (opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema" || opts.ResponseFormat.Type == "json_object")
	if rfIsJSON && isAgent {
		const msg = "google.interactions: structured output (responseFormat) is not supported when an agent is set; responseFormat will be ignored."
		warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
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
	if rfIsJSON && !isAgent {
		entry := map[string]interface{}{"type": "text", "mime_type": "application/json"}
		if opts.ResponseFormat.Schema != nil {
			entry["schema"] = providerutils.ResponseFormatJSONSchemaOrdered(opts.ResponseFormat.Schema)
		}
		responseFormatEntries = append(responseFormatEntries, entry)
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
		AgentConfig:           interactionsAgentConfig(isAgent, interactionsOpts.AgentConfig),
		Background:            interactionsOpts.Background,
	}
	// TS sends `background` only when providerOptions.google.background is
	// set: some agents require it, others reject it.
	if interactionsOpts.Environment != nil {
		if !isAgent {
			const msg = "google.interactions: environment is only supported when an agent is set; environment will be omitted from the request body."
			warnings = append(warnings, types.Warning{Type: "other", Message: msg, Details: msg})
		} else {
			body.Environment = interactionsEnvironment(interactionsOpts.Environment)
		}
	}
	// TS sends `agent` only for agent models; providerOptions.google.agent is
	// accepted by the schema but never read.
	if isAgent {
		body.Agent = m.agent
	} else {
		body.Model = m.modelID
	}
	return body, warnings, interactionsOpts, nil
}

// interactionsAgentConfig mirrors TS getArgs: agent_config is built only when
// an agent is set, from the deep-research fields or {type: 'dynamic'}.
func interactionsAgentConfig(isAgent bool, cfg map[string]interface{}) map[string]interface{} {
	if !isAgent || cfg == nil {
		return nil
	}
	switch cfg["type"] {
	case "deep-research":
		return dropNilValues(map[string]interface{}{
			"type":                   "deep-research",
			"thinking_summaries":     cfg["thinking_summaries"],
			"visualization":          cfg["visualization"],
			"collaborative_planning": cfg["collaborative_planning"],
		})
	case "dynamic":
		return map[string]interface{}{"type": "dynamic"}
	}
	return nil
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
	if serviceTier == "" {
		serviceTier = headerServiceTier(headers)
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
		RawFinishReason:  response.Status,
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
		return interactionsResponse{}, nil, fmt.Errorf("google.interactions: cannot poll a background interaction without an id. The POST response did not include an interaction id.") //nolint:staticcheck // matches TS SDK exact error text
	}
	timeout := defaultInteractionsPollTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}
	nextDelay := defaultInteractionsPollInitialDelay
	startedAt := time.Now()
	for {
		if time.Since(startedAt) > timeout {
			// TS only cancels on abort; a timeout leaves the run going so the
			// caller can still retrieve it by id.
			return interactionsResponse{}, nil, fmt.Errorf("google.interactions: timed out polling interaction %s after %dms.", interactionID, timeout.Milliseconds()) //nolint:staticcheck // matches TS SDK's exact error text
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
			ValidateResponse: providerutils.NodeResponseValidator(m.Provider(), zGoogleInteractions),
			Method:           http.MethodGet,
			Path:             "/interactions/" + url.PathEscape(interactionID),
			Headers:          headers,
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
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure is an APICallError as is
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return gemini.GoogleHTTPError(m.Provider(), statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
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
		if err := checkInteractionsOptionTypes(v); err != nil {
			return out, err
		}
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

// checkInteractionsOptionTypes mirrors the zod type checks of
// googleInteractionsLanguageModelOptions for a raw providerOptions.google map:
// a present, non-null value of the wrong type fails the call.
func checkInteractionsOptionTypes(v map[string]interface{}) error {
	for _, k := range []string{"previousInteractionId", "mediaResolution", "systemInstruction", "serviceTier", "thinkingLevel", "thinkingSummaries", "agent", "signature", "interactionId"} {
		if x, ok := v[k]; ok && x != nil {
			if _, isStr := x.(string); !isStr {
				return fmt.Errorf("google.interactions: %s: expected string, received %T", k, x)
			}
		}
	}
	for _, k := range []string{"background", "store"} {
		if x, ok := v[k]; ok && x != nil {
			if _, isBool := x.(bool); !isBool {
				return fmt.Errorf("google.interactions: %s: expected boolean, received %T", k, x)
			}
		}
	}
	if x, ok := v["pollingTimeoutMs"]; ok && x != nil {
		n, isNum := numberToInt(x)
		if f, isF := x.(float64); isNum && isF && f != float64(n) {
			isNum = false
		}
		if !isNum || n <= 0 {
			return fmt.Errorf("google.interactions: pollingTimeoutMs: expected a positive integer, received %v", x)
		}
	}
	if x, ok := v["responseModalities"]; ok && x != nil {
		switch x.(type) {
		case []string, []interface{}:
		default:
			return fmt.Errorf("google.interactions: responseModalities: expected array, received %T", x)
		}
	}
	if x, ok := v["imageConfig"]; ok && x != nil {
		if _, isMap := x.(map[string]interface{}); !isMap {
			return fmt.Errorf("google.interactions: imageConfig: expected object, received %T", x)
		}
	}
	if x, ok := v["agentConfig"]; ok && x != nil {
		m, isMap := x.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("google.interactions: agentConfig: expected object, received %T", x)
		}
		if _, hasType := m["type"].(string); !hasType {
			return fmt.Errorf("google.interactions: agentConfig.type: invalid input")
		}
		if c, ok := m["collaborativePlanning"]; ok && c != nil {
			if _, isBool := c.(bool); !isBool {
				return fmt.Errorf("google.interactions: agentConfig.collaborativePlanning: expected boolean, received %T", c)
			}
		}
	}
	if x, ok := v["responseFormat"]; ok && x != nil {
		var entries []map[string]interface{}
		switch list := x.(type) {
		case []interface{}:
			for i, item := range list {
				m, isMap := item.(map[string]interface{})
				if !isMap {
					return fmt.Errorf("google.interactions: responseFormat[%d]: expected object, received %T", i, item)
				}
				entries = append(entries, m)
			}
		case []map[string]interface{}:
			entries = list
		default:
			return fmt.Errorf("google.interactions: responseFormat: expected array, received %T", x)
		}
		for i, e := range entries {
			if err := checkInteractionsResponseFormatEntry(i, e); err != nil {
				return err
			}
		}
	}
	if x, ok := v["environment"]; ok && x != nil {
		if err := checkInteractionsEnvironment(x); err != nil {
			return err
		}
	}
	return nil
}

func checkInteractionsResponseFormatEntry(i int, e map[string]interface{}) error {
	typ, _ := e["type"].(string)
	enum := func(key string, allowed ...string) error {
		x, ok := e[key]
		if !ok || x == nil {
			return nil
		}
		if s, isStr := x.(string); isStr && containsString(allowed, s) {
			return nil
		}
		return fmt.Errorf("google.interactions: responseFormat[%d].%s: invalid option %v", i, key, x)
	}
	str := func(key string) error {
		if x, ok := e[key]; ok && x != nil {
			if _, isStr := x.(string); !isStr {
				return fmt.Errorf("google.interactions: responseFormat[%d].%s: expected string, received %T", i, key, x)
			}
		}
		return nil
	}
	ratios := []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9", "1:8", "8:1", "1:4", "4:1"}
	var errs []error
	switch typ {
	case "text", "audio":
		errs = append(errs, str("mimeType"))
	case "image":
		errs = append(errs, str("mimeType"), enum("aspectRatio", ratios...), enum("imageSize", "1K", "2K", "4K", "512"))
	case "video":
		errs = append(errs, enum("aspectRatio", "16:9", "9:16"), enum("resolution", "360p", "720p", "1080p", "4k"),
			str("duration"), enum("delivery", "inline", "uri"), str("gcsUri"))
	default:
		return fmt.Errorf("google.interactions: responseFormat[%d].type: invalid input", i)
	}
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func checkInteractionsEnvironment(x interface{}) error {
	if _, isStr := x.(string); isStr {
		return nil
	}
	env, isMap := x.(map[string]interface{})
	if !isMap {
		return fmt.Errorf("google.interactions: environment: expected string or object, received %T", x)
	}
	if env["type"] != "remote" {
		return fmt.Errorf("google.interactions: environment.type: invalid input")
	}
	needStr := func(path string, m map[string]interface{}, key string, nullable bool) error {
		val, ok := m[key]
		if !ok || val == nil {
			if nullable {
				return nil
			}
			return fmt.Errorf("google.interactions: %s.%s: required", path, key)
		}
		if _, isStr := val.(string); !isStr {
			return fmt.Errorf("google.interactions: %s.%s: expected string, received %T", path, key, val)
		}
		return nil
	}
	if srcs, ok := env["sources"]; ok && srcs != nil {
		list, isList := srcs.([]interface{})
		if !isList {
			return fmt.Errorf("google.interactions: environment.sources: expected array, received %T", srcs)
		}
		for i, item := range list {
			path := fmt.Sprintf("environment.sources[%d]", i)
			m, isMap := item.(map[string]interface{})
			if !isMap {
				return fmt.Errorf("google.interactions: %s: expected object, received %T", path, item)
			}
			switch m["type"] {
			case "gcs", "repository":
				if err := needStr(path, m, "source", false); err != nil {
					return err
				}
				if err := needStr(path, m, "target", true); err != nil {
					return err
				}
			case "inline":
				if err := needStr(path, m, "content", false); err != nil {
					return err
				}
				if err := needStr(path, m, "target", false); err != nil {
					return err
				}
			default:
				return fmt.Errorf("google.interactions: %s.type: invalid input", path)
			}
		}
	}
	if net, ok := env["network"]; ok && net != nil {
		switch n := net.(type) {
		case string:
			if n != "disabled" {
				return fmt.Errorf("google.interactions: environment.network: invalid input")
			}
		case map[string]interface{}:
			list, isList := n["allowlist"].([]interface{})
			if !isList {
				return fmt.Errorf("google.interactions: environment.network.allowlist: expected array")
			}
			for i, item := range list {
				path := fmt.Sprintf("environment.network.allowlist[%d]", i)
				m, isMap := item.(map[string]interface{})
				if !isMap {
					return fmt.Errorf("google.interactions: %s: expected object, received %T", path, item)
				}
				if err := needStr(path, m, "domain", false); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("google.interactions: environment.network: invalid input")
		}
	}
	return nil
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

// dropNilValues is TS pruneUndefined: only nil values are dropped.
func dropNilValues(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		if v != nil {
			out[k] = v
		}
	}
	return out
}

// interactionsEnvironment maps providerOptions.google.environment to the wire
// value (TS getArgs): a string is sent as is; an object becomes
// {type:"remote", sources?, network?}.
func interactionsEnvironment(raw interface{}) interface{} {
	if s, ok := raw.(string); ok {
		return s
	}
	var env map[string]interface{}
	switch v := raw.(type) {
	case map[string]interface{}:
		env = v
	default:
		b, err := json.Marshal(raw)
		if err != nil || json.Unmarshal(b, &env) != nil {
			return raw
		}
	}
	out := map[string]interface{}{"type": "remote"}
	if sources, ok := env["sources"].([]interface{}); ok && len(sources) > 0 {
		mapped := make([]interface{}, 0, len(sources))
		for _, src := range sources {
			m, _ := src.(map[string]interface{})
			if m == nil {
				mapped = append(mapped, src)
				continue
			}
			if m["type"] == "inline" {
				mapped = append(mapped, map[string]interface{}{"type": "inline", "content": m["content"], "target": m["target"]})
				continue
			}
			mapped = append(mapped, dropNilValues(map[string]interface{}{"type": m["type"], "source": m["source"], "target": m["target"]}))
		}
		out["sources"] = mapped
	}
	switch n := env["network"].(type) {
	case string:
		if n == "disabled" {
			out["network"] = "disabled"
		}
	case map[string]interface{}:
		var allow []interface{}
		entries, _ := n["allowlist"].([]interface{})
		for _, e := range entries {
			m, _ := e.(map[string]interface{})
			if m == nil {
				allow = append(allow, e)
				continue
			}
			allow = append(allow, dropNilValues(map[string]interface{}{"domain": m["domain"], "transform": m["transform"]}))
		}
		if allow == nil {
			allow = []interface{}{}
		}
		out["network"] = map[string]interface{}{"allowlist": allow}
	}
	return out
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
	// TS convertGoogleInteractionsUsage: output total is output + thought
	// tokens, text is total_output_tokens, noCache is input - cached, raw is
	// the API's usage object untouched.
	var totalInput, totalCached int64
	if usage.TotalInputTokens != nil {
		totalInput = *usage.TotalInputTokens
	}
	if usage.TotalCachedTokens != nil {
		totalCached = *usage.TotalCachedTokens
	}
	out := types.Usage{
		InputTokens: usage.TotalInputTokens,
		TotalTokens: usage.TotalTokens,
		Raw:         usage.raw,
	}
	if usage.TotalOutputTokens != nil || usage.TotalThoughtTokens != nil {
		var total int64
		if usage.TotalOutputTokens != nil {
			total += *usage.TotalOutputTokens
		}
		if usage.TotalThoughtTokens != nil {
			total += *usage.TotalThoughtTokens
		}
		out.OutputTokens = &total
	}
	if out.Raw == nil {
		out.Raw = map[string]interface{}{
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
		}
	}
	if usage.TotalInputTokens != nil || usage.TotalCachedTokens != nil || len(usage.InputTokensByModality) > 0 {
		details := &types.InputTokenDetails{
			CacheReadTokens: usage.TotalCachedTokens,
			TextTokens:      modalityToken(usage.InputTokensByModality, "text"),
			ImageTokens:     modalityToken(usage.InputTokensByModality, "image"),
		}
		if usage.TotalInputTokens != nil {
			noCache := totalInput - totalCached
			details.NoCacheTokens = &noCache
		}
		out.InputDetails = details
	}
	if usage.TotalThoughtTokens != nil || usage.TotalOutputTokens != nil {
		out.OutputDetails = &types.OutputTokenDetails{
			ReasoningTokens: usage.TotalThoughtTokens,
			TextTokens:      usage.TotalOutputTokens,
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
