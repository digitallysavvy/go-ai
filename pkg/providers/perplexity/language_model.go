package perplexity

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// This file mirrors TS perplexity-language-model.ts: the Perplexity Agent API
// (/v1/agent) language model, replacing the pre-migration Sonar Chat
// Completions implementation (/chat/completions). See agent_api.go for the
// wire types, convert_input.go for prompt conversion, provider_options.go for
// providerOptions.perplexity, prepare_tools.go for AI SDK function tool
// conversion, finish_reason.go / convert_usage.go for response mapping, and
// errors.go for HTTP-level error parsing.

// LanguageModel implements provider.LanguageModel for the Perplexity Agent API.
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Perplexity language model.
func NewLanguageModel(provider *Provider, modelID string) *LanguageModel {
	return &LanguageModel{provider: provider, modelID: modelID}
}

// SpecificationVersion returns the specification version. The Agent API
// migration ports the TS LanguageModelV4 shape (content-part output,
// text-start/delta/end and reasoning-start/delta/end streaming blocks),
// matching other v4-ported providers in this SDK (gemini, openai responses,
// anthropic, openresponses).
func (m *LanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *LanguageModel) Provider() string { return "perplexity" }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports tool-calling support. The Agent API supports both
// native tools and AI SDK function tools (unlike the pre-migration Sonar Chat
// Completions API, which had no tool-calling support at all).
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports structured JSON output support via the
// Agent API's response_format.json_schema.
func (m *LanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports image input support.
func (m *LanguageModel) SupportsImageInput() bool { return true }

// SupportedURLs reports the URL patterns this model can receive directly
// without downloading, mirroring TS `supportedUrls`.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^https?://.*$`},
	}
}

// getArgs builds the Agent API request body and accompanying warnings,
// mirroring TS PerplexityLanguageModel#getArgs.
func (m *LanguageModel) getArgs(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning

	var rawOptions map[string]interface{}
	if raw, ok := opts.ProviderOptions["perplexity"]; ok && raw != nil {
		asMap, ok := raw.(map[string]interface{})
		if !ok {
			return nil, nil, invalidPerplexityProviderOptions("perplexity", fmt.Sprintf("must be an object, got %T", raw))
		}
		rawOptions = asMap
	}
	parsedOptions, extras, err := parsePerplexityAgentOptions(rawOptions)
	if err != nil {
		return nil, nil, err
	}

	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "stopSequences"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}

	var messages []types.Message
	if opts.Prompt.IsMessages() {
		messages = opts.Prompt.Messages
	} else if opts.Prompt.IsSimple() {
		messages = prompt.SimpleTextToMessages(opts.Prompt.Text)
	}
	if opts.Prompt.System != "" {
		systemMsg := types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}}}
		messages = append([]types.Message{systemMsg}, messages...)
	}
	input, inputWarnings, err := convertToPerplexityInput(messages)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, inputWarnings...)

	functionTools, toolWarnings := preparePerplexityTools(opts.Tools, opts.ToolChoice)
	warnings = append(warnings, toolWarnings...)

	var agentTools []map[string]interface{}
	if parsedOptions != nil {
		agentTools = append(agentTools, parsedOptions.Tools...)
	}
	agentTools = append(agentTools, functionTools...)

	model, preset := getModelSelection(m.modelID)

	var reasoningConfig map[string]interface{}
	if parsedOptions != nil && parsedOptions.Reasoning != nil {
		reasoningConfig = parsedOptions.Reasoning
	} else if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		if *opts.Reasoning == types.ReasoningNone {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: `reasoning "none"`})
		} else {
			reasoningConfig = map[string]interface{}{"effort": string(*opts.Reasoning)}
		}
	}

	body := map[string]interface{}{}
	if parsedOptions != nil {
		for k, v := range parsedOptions.toBodyMap() {
			body[k] = v
		}
	}
	for k, v := range extras {
		body[k] = v
	}
	if model != "" {
		body["model"] = model
	}
	if preset != "" {
		body["preset"] = preset
	}
	body["input"] = input
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if reasoningConfig != nil {
		body["reasoning"] = reasoningConfig
	}

	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		if opts.ResponseFormat.Schema != nil {
			name := opts.ResponseFormat.Name
			if name == "" {
				name = "response"
			}
			jsonSchema := map[string]interface{}{
				"name":   name,
				"schema": opts.ResponseFormat.Schema,
				"strict": true,
			}
			if opts.ResponseFormat.Description != "" {
				jsonSchema["description"] = opts.ResponseFormat.Description
			}
			body["response_format"] = map[string]interface{}{
				"type":        "json_schema",
				"json_schema": jsonSchema,
			}
		} else {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "JSON response format without a schema"})
		}
	}

	if len(agentTools) > 0 {
		body["tools"] = agentTools
	}

	return body, warnings, nil
}

// DoGenerate performs non-streaming Agent API generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	body, warnings, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/agent",
		Body:    body,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	if resp.StatusCode >= 400 {
		return nil, m.handleError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
	}

	responseHeaders := providerutils.ExtractHeaders(resp.Headers)

	if err := validatePerplexityAgentResponse(resp.Body); err != nil {
		return nil, &providererrors.ProviderError{
			Provider:        "perplexity",
			StatusCode:      200,
			Message:         "Invalid JSON response",
			Cause:           err,
			ResponseHeaders: responseHeaders,
			ResponseBody:    string(resp.Body),
		}
	}

	var response perplexityAgentResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, &providererrors.ProviderError{
			Provider:        "perplexity",
			StatusCode:      200,
			Message:         "Invalid JSON response",
			Cause:           err,
			ResponseHeaders: responseHeaders,
			ResponseBody:    string(resp.Body),
		}
	}

	if response.Error != nil || response.Status == "failed" {
		message := "Perplexity response failed"
		if response.Error != nil && response.Error.Message != "" {
			message = response.Error.Message
		}
		return nil, &providererrors.ProviderError{
			Provider:        "perplexity",
			StatusCode:      400,
			Message:         message,
			ResponseHeaders: responseHeaders,
			ResponseBody:    string(resp.Body),
		}
	}

	result := m.convertResponse(&response, resp.Body)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = responseHeaders
	result.RawRequest = body

	var bodyGeneric interface{}
	_ = json.Unmarshal(resp.Body, &bodyGeneric)
	result.ResponseMetadata = &types.ResponseMetadata{
		ID:        response.ID,
		Timestamp: time.Unix(response.CreatedAt, 0),
		ModelID:   response.Model,
		Headers:   responseHeaders,
		Body:      bodyGeneric,
	}

	return result, nil
}

// DoStream performs streaming Agent API generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, warnings, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	body["stream"] = true

	headers := map[string]string{"Accept": "text/event-stream"}
	for k, v := range opts.Headers {
		headers[k] = v
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/agent",
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	responseHeaders := providerutils.ExtractHeaders(httpResp.Header)
	return newPerplexityAgentStream(httpResp.Body, warnings, opts.IncludeRawChunks, responseHeaders), nil
}

// handleError converts an HTTP-level error into a ProviderError, resolving
// the Agent API's error envelope when present (see errors.go).
func (m *LanguageModel) handleError(err error) error {
	if parsed := parsePerplexityHTTPError(err); parsed != nil {
		if pe, ok := parsed.(*providererrors.ProviderError); ok {
			return pe
		}
	}
	return providererrors.NewProviderError("perplexity", 0, "", err.Error(), err)
}

// convertResponse mirrors TS doGenerate's response handling: it builds the
// ordered content array (text, sources, tool calls), deduping sources by URL
// with search-result IDs taking priority over annotation/fetch sources.
func (m *LanguageModel) convertResponse(response *perplexityAgentResponse, rawBody []byte) *types.GenerateResult {
	var content []types.ContentPart
	var toolCalls []types.ToolCall
	var textParts []string
	hasFunctionCall := false

	sourceIndexesByURL := map[string]int{}
	sourceHasResultID := map[string]bool{}
	addSource := func(url string, hasResultID bool, build func() types.SourceContent) {
		if idx, ok := sourceIndexesByURL[url]; ok {
			if hasResultID && !sourceHasResultID[url] {
				content[idx] = build()
				sourceHasResultID[url] = true
			}
			return
		}
		sourceIndexesByURL[url] = len(content)
		sourceHasResultID[url] = hasResultID
		content = append(content, build())
	}

	for _, item := range response.Output {
		item := item
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != nil {
					content = append(content, types.TextContent{Text: *part.Text})
					textParts = append(textParts, *part.Text)
				}
				for _, ann := range part.Annotations {
					if ann.URL == nil {
						continue
					}
					url := *ann.URL
					title := ""
					if ann.Title != nil {
						title = *ann.Title
					}
					addSource(url, false, func() types.SourceContent {
						return types.SourceContent{SourceType: "url", ID: streaming.GenerateID(), URL: url, Title: title}
					})
				}
			}
		case "search_results":
			for _, result := range item.Results {
				result := result
				addSource(result.URL, result.ID != nil, func() types.SourceContent {
					return perplexitySearchResultSource(result)
				})
			}
		case "fetch_url_results":
			for _, fc := range item.Contents {
				fc := fc
				addSource(fc.URL, false, func() types.SourceContent {
					return perplexityFetchedContentSource(fc)
				})
			}
		case "function_call":
			if item.CallID != nil && item.Name != nil && item.Arguments != nil {
				hasFunctionCall = true
				callID := *item.CallID
				name := *item.Name
				args := *item.Arguments
				var parsedArgs map[string]interface{}
				_ = json.Unmarshal([]byte(args), &parsedArgs)
				content = append(content, types.ToolCallContent{
					ToolCallID:       callID,
					ToolName:         name,
					Input:            args,
					Arguments:        parsedArgs,
					ProviderMetadata: perplexityToolCallMetadata(item),
				})
				toolCalls = append(toolCalls, types.ToolCall{
					ID:               callID,
					ToolName:         name,
					Arguments:        parsedArgs,
					RawArguments:     args,
					ProviderMetadata: perplexityToolCallMetadataMap(item),
				})
			}
		}
	}

	incompleteReason := ""
	if response.IncompleteDetails != nil {
		incompleteReason = response.IncompleteDetails.Reason
	}
	rawFinishReason := incompleteReason
	if rawFinishReason == "" {
		rawFinishReason = response.Status
	}

	result := &types.GenerateResult{
		Content:          content,
		ToolCalls:        toolCalls,
		FinishReason:     mapPerplexityFinishReason(response.Status, incompleteReason, hasFunctionCall),
		RawFinishReason:  rawFinishReason,
		Usage:            convertPerplexityUsage(response.Usage, perplexityRawUsageBytes(rawBody)),
		RawResponse:      response,
		ProviderMetadata: getPerplexityProviderMetadata(response.Usage),
	}
	if len(textParts) > 0 {
		var b strings.Builder
		for _, t := range textParts {
			b.WriteString(t)
		}
		result.Text = b.String()
	}
	return result
}

// perplexityRawUsageBytes extracts the raw "usage" sub-object bytes from the
// response body, preserving fields not modeled by perplexityUsage. Mirrors
// TS's `raw: usage as JSONObject`, where `usage` is the zod-loose-object-
// parsed value that retains every original field.
func perplexityRawUsageBytes(rawBody []byte) []byte {
	var probe struct {
		Usage json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(rawBody, &probe); err != nil {
		return nil
	}
	return probe.Usage
}

type perplexitySearchSourceMeta struct {
	ResultID    *float64 `json:"resultId"`
	Snippet     *string  `json:"snippet"`
	Date        *string  `json:"date"`
	LastUpdated *string  `json:"lastUpdated"`
	Source      *string  `json:"source"`
}

type perplexityFetchedSourceMeta struct {
	Snippet *string `json:"snippet"`
}

func perplexitySearchResultSource(result perplexitySearchResult) types.SourceContent {
	id := streaming.GenerateID()
	if result.ID != nil {
		id = strconv.FormatFloat(*result.ID, 'f', -1, 64)
	}
	meta, _ := json.Marshal(map[string]interface{}{
		"perplexity": perplexitySearchSourceMeta{
			ResultID:    result.ID,
			Snippet:     result.Snippet,
			Date:        result.Date,
			LastUpdated: result.LastUpdated,
			Source:      result.Source,
		},
	})
	return types.SourceContent{
		SourceType:       "url",
		ID:               id,
		URL:              result.URL,
		Title:            result.Title,
		ProviderMetadata: meta,
	}
}

func perplexityFetchedContentSource(fc perplexityFetchedContent) types.SourceContent {
	meta, _ := json.Marshal(map[string]interface{}{
		"perplexity": perplexityFetchedSourceMeta{Snippet: fc.Snippet},
	})
	return types.SourceContent{
		SourceType:       "url",
		ID:               streaming.GenerateID(),
		URL:              fc.URL,
		Title:            fc.Title,
		ProviderMetadata: meta,
	}
}

type perplexityToolCallMeta struct {
	ItemID           *string `json:"itemId"`
	ThoughtSignature string  `json:"thoughtSignature,omitempty"`
}

func perplexityToolCallMetadata(item perplexityOutputItem) json.RawMessage {
	m := perplexityToolCallMeta{ItemID: item.ID}
	if item.ThoughtSignature != nil {
		m.ThoughtSignature = *item.ThoughtSignature
	}
	raw, _ := json.Marshal(map[string]interface{}{"perplexity": m})
	return raw
}

func perplexityToolCallMetadataMap(item perplexityOutputItem) map[string]interface{} {
	inner := map[string]interface{}{"itemId": nil}
	if item.ID != nil {
		inner["itemId"] = *item.ID
	}
	if item.ThoughtSignature != nil {
		inner["thoughtSignature"] = *item.ThoughtSignature
	}
	return map[string]interface{}{"perplexity": inner}
}

// perplexityTextID mirrors TS getTextId: a text block's identity is the
// item's own id, or its output index, or "text" as a last resort; a nonzero
// content_index is appended so multiple content parts on one item get
// distinct blocks.
func perplexityTextID(itemID *string, outputIndex *int64, contentIndex *int64) string {
	id := "text"
	switch {
	case itemID != nil:
		id = *itemID
	case outputIndex != nil:
		id = strconv.FormatInt(*outputIndex, 10)
	}
	if contentIndex == nil || *contentIndex == 0 {
		return id
	}
	return fmt.Sprintf("%s:%d", id, *contentIndex)
}

// perplexityTextState tracks one text block's accumulated text and whether
// it has already been closed with a text-end chunk.
type perplexityTextState struct {
	text  string
	ended bool
}

// perplexityAgentStream implements provider.TextStream for the Agent API's
// typed SSE events, mirroring TS doStream's TransformStream. content_index
// (and every other nullish chunk field) tolerates both an absent key and an
// explicit JSON null, folding in 6da8aa06d6 for free via Go's pointer/slice
// field types (see agent_api.go's doc comment).
type perplexityAgentStream struct {
	reader          io.ReadCloser
	parser          *streaming.SSEParser
	warnings        []types.Warning
	includeRaw      bool
	responseHeaders map[string]string

	started bool
	done    bool
	err     error

	pending []*provider.StreamChunk

	hasResponseMetadata bool
	activeReasoningID   string
	hasFunctionCall     bool
	seenFunctionCalls   map[string]bool

	textStates map[string]*perplexityTextState
	textOrder  []string

	emittedSourceURLs  map[string]bool
	pendingSources     map[string]types.SourceContent
	pendingSourceOrder []string

	finishReason    types.FinishReason
	rawFinishReason string
	usage           *perplexityUsage
	usageRawBytes   []byte
}

func newPerplexityAgentStream(reader io.ReadCloser, warnings []types.Warning, includeRaw bool, responseHeaders map[string]string) *perplexityAgentStream {
	return &perplexityAgentStream{
		reader:            reader,
		parser:            streaming.NewSSEParser(reader),
		warnings:          warnings,
		includeRaw:        includeRaw,
		responseHeaders:   responseHeaders,
		seenFunctionCalls: map[string]bool{},
		textStates:        map[string]*perplexityTextState{},
		emittedSourceURLs: map[string]bool{},
		pendingSources:    map[string]types.SourceContent{},
		finishReason:      types.FinishReasonOther,
	}
}

func (s *perplexityAgentStream) Next() (*provider.StreamChunk, error) {
	for {
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			return chunk, nil
		}
		if s.done {
			return nil, io.EOF
		}
		if !s.started {
			s.started = true
			return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings}, nil
		}

		event, err := s.parser.Next()
		if err != nil {
			s.done = true
			if err != io.EOF {
				s.err = err
				return nil, err
			}
			s.flush()
			continue
		}
		if streaming.IsStreamDone(event) {
			s.done = true
			s.flush()
			continue
		}
		s.processEvent(event.Data)
	}
}

func (s *perplexityAgentStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *perplexityAgentStream) Close() error {
	return s.reader.Close()
}

func (s *perplexityAgentStream) processEvent(data string) {
	if s.includeRaw {
		var rawValue interface{}
		if err := json.Unmarshal([]byte(data), &rawValue); err == nil {
			s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: rawValue})
		}
	}

	var chunk perplexityAgentChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		s.finishReason = types.FinishReasonError
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: err.Error()})
		return
	}

	switch chunk.Type {
	case "response.created", "response.in_progress":
		s.emitResponseMetadataOnce(chunk.Response)

	case "response.output_text.delta":
		if chunk.Delta != nil {
			s.emitTextDelta(perplexityTextID(chunk.ItemID, chunk.OutputIndex, chunk.ContentIndex), *chunk.Delta)
		}

	case "response.output_text.done":
		s.finishText(perplexityTextID(chunk.ItemID, chunk.OutputIndex, chunk.ContentIndex), chunk.Text)

	case "response.reasoning.started":
		if s.activeReasoningID != "" {
			s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: s.activeReasoningID})
		}
		seq := streaming.GenerateID()
		if chunk.SequenceNumber != nil {
			seq = strconv.FormatInt(*chunk.SequenceNumber, 10)
		}
		s.activeReasoningID = "reasoning-" + seq
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: s.activeReasoningID})
		s.emitReasoningThought(chunk.Thought)

	case "response.reasoning.search_queries", "response.reasoning.fetch_url_queries":
		s.emitReasoningThought(chunk.Thought)

	case "response.reasoning.search_results":
		s.emitReasoningThought(chunk.Thought)
		for _, result := range chunk.Results {
			result := result
			s.emitSource(result.URL, result.ID != nil, func() types.SourceContent { return perplexitySearchResultSource(result) })
		}

	case "response.reasoning.fetch_url_results":
		s.emitReasoningThought(chunk.Thought)
		for _, fc := range chunk.Contents {
			fc := fc
			s.emitSource(fc.URL, false, func() types.SourceContent { return perplexityFetchedContentSource(fc) })
		}

	case "response.reasoning.stopped":
		s.emitReasoningThought(chunk.Thought)
		if s.activeReasoningID != "" {
			s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: s.activeReasoningID})
			s.activeReasoningID = ""
		}

	case "response.output_item.done":
		if chunk.Item != nil {
			s.finishOutputText(chunk.Item, chunk.OutputIndex)
			s.emitOutputSources(chunk.Item)
			s.emitFunctionCall(chunk.Item)
		}

	case "response.completed", "response.incomplete":
		if chunk.Response != nil {
			s.emitResponseMetadataOnce(chunk.Response)
			for i := range chunk.Response.Output {
				item := chunk.Response.Output[i]
				outputIndex := int64(i)
				s.finishOutputText(&item, &outputIndex)
				s.emitOutputSources(&item)
				s.emitFunctionCall(&item)
			}
			s.usage = chunk.Response.Usage
			s.usageRawBytes = perplexityExtractNestedUsageBytes([]byte(data))
			incompleteReason := ""
			if chunk.Response.IncompleteDetails != nil {
				incompleteReason = chunk.Response.IncompleteDetails.Reason
			}
			raw := incompleteReason
			if raw == "" {
				raw = chunk.Response.Status
			}
			s.finishReason = mapPerplexityFinishReason(chunk.Response.Status, incompleteReason, s.hasFunctionCall)
			s.rawFinishReason = raw
		}

	case "response.failed":
		s.finishReason = types.FinishReasonError
		s.rawFinishReason = "failed"
		message := "Perplexity response failed"
		if chunk.Error != nil && chunk.Error.Message != "" {
			message = chunk.Error.Message
		}
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: message})
	}
}

func (s *perplexityAgentStream) emitResponseMetadataOnce(response *perplexityAgentResponse) {
	if s.hasResponseMetadata || response == nil {
		return
	}
	s.hasResponseMetadata = true
	s.pending = append(s.pending, &provider.StreamChunk{
		Type: provider.ChunkTypeResponseMetadata,
		ResponseMetadata: &provider.ResponseMetadata{
			ID:        response.ID,
			Timestamp: time.Unix(response.CreatedAt, 0),
			ModelID:   response.Model,
			Headers:   s.responseHeaders,
		},
	})
}

func (s *perplexityAgentStream) emitTextDelta(id, delta string) {
	state, ok := s.textStates[id]
	if ok && state.ended {
		return
	}
	if !ok {
		state = &perplexityTextState{}
		s.textStates[id] = state
		s.textOrder = append(s.textOrder, id)
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
	}
	state.text += delta
	s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: delta})
}

func (s *perplexityAgentStream) finishText(id string, text *string) {
	state, ok := s.textStates[id]
	if ok && state.ended {
		return
	}
	emitted := ""
	if ok {
		emitted = state.text
	}
	if text != nil && strings.HasPrefix(*text, emitted) && len(*text) > len(emitted) {
		s.emitTextDelta(id, (*text)[len(emitted):])
	}
	if final, ok := s.textStates[id]; ok {
		final.ended = true
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id})
	}
}

func (s *perplexityAgentStream) finishOutputText(item *perplexityOutputItem, outputIndex *int64) {
	if item == nil || item.Type != "message" {
		return
	}
	for i, part := range item.Content {
		if part.Type != "output_text" {
			continue
		}
		contentIndex := int64(i)
		s.finishText(perplexityTextID(item.ID, outputIndex, &contentIndex), part.Text)
	}
}

func (s *perplexityAgentStream) emitSource(url string, hasResultID bool, build func() types.SourceContent) {
	if s.emittedSourceURLs[url] {
		return
	}
	if hasResultID {
		delete(s.pendingSources, url)
		s.emittedSourceURLs[url] = true
		source := build()
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
		return
	}
	if _, ok := s.pendingSources[url]; !ok {
		s.pendingSources[url] = build()
		s.pendingSourceOrder = append(s.pendingSourceOrder, url)
	}
}

func (s *perplexityAgentStream) emitReasoningThought(thought *string) {
	if s.activeReasoningID != "" && thought != nil {
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: s.activeReasoningID, Reasoning: *thought})
	}
}

func (s *perplexityAgentStream) emitFunctionCall(item *perplexityOutputItem) {
	if item == nil || item.Type != "function_call" || item.CallID == nil || item.Name == nil || item.Arguments == nil {
		return
	}
	if s.seenFunctionCalls[*item.CallID] {
		return
	}
	s.seenFunctionCalls[*item.CallID] = true
	s.hasFunctionCall = true

	callID := *item.CallID
	name := *item.Name
	args := *item.Arguments

	s.pending = append(s.pending,
		&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: callID, ToolName: name}},
		&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: callID, Text: args},
		&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: callID}},
	)

	var parsedArgs map[string]interface{}
	_ = json.Unmarshal([]byte(args), &parsedArgs)
	s.pending = append(s.pending, &provider.StreamChunk{
		Type: provider.ChunkTypeToolCall,
		ToolCall: &types.ToolCall{
			ID:               callID,
			ToolName:         name,
			Arguments:        parsedArgs,
			RawArguments:     args,
			ProviderMetadata: perplexityToolCallMetadataMap(*item),
		},
	})
}

func (s *perplexityAgentStream) emitOutputSources(item *perplexityOutputItem) {
	if item == nil {
		return
	}
	if item.Type == "message" {
		for _, part := range item.Content {
			for _, ann := range part.Annotations {
				if ann.URL == nil {
					continue
				}
				url := *ann.URL
				title := ""
				if ann.Title != nil {
					title = *ann.Title
				}
				s.emitSource(url, false, func() types.SourceContent {
					return types.SourceContent{SourceType: "url", ID: streaming.GenerateID(), URL: url, Title: title}
				})
			}
		}
	}
	for _, result := range item.Results {
		result := result
		s.emitSource(result.URL, result.ID != nil, func() types.SourceContent { return perplexitySearchResultSource(result) })
	}
	for _, fc := range item.Contents {
		fc := fc
		s.emitSource(fc.URL, false, func() types.SourceContent { return perplexityFetchedContentSource(fc) })
	}
}

func (s *perplexityAgentStream) flush() {
	for _, url := range s.pendingSourceOrder {
		if source, ok := s.pendingSources[url]; ok {
			source := source
			s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &source})
		}
	}
	if s.activeReasoningID != "" {
		s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: s.activeReasoningID})
		s.activeReasoningID = ""
	}
	for _, id := range s.textOrder {
		if state := s.textStates[id]; state != nil && !state.ended {
			state.ended = true
			s.pending = append(s.pending, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id})
		}
	}

	usage := convertPerplexityUsage(s.usage, s.usageRawBytes)
	metaRaw, _ := json.Marshal(getPerplexityProviderMetadata(s.usage))
	s.pending = append(s.pending, &provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.finishReason,
		RawFinishReason:  s.rawFinishReason,
		Usage:            &usage,
		ProviderMetadata: metaRaw,
	})
}

// perplexityExtractNestedUsageBytes extracts the raw "response.usage"
// sub-object bytes from a streaming chunk's data string, mirroring
// perplexityRawUsageBytes for the non-streaming path.
func perplexityExtractNestedUsageBytes(raw []byte) []byte {
	var probe struct {
		Response struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return probe.Response.Usage
}
