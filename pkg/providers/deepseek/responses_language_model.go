package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// The Responses API model IDs (TS DeepSeekResponsesModelId) are
// ModelDeepSeekFlash and ModelDeepSeekV4Pro.

// deepseekResponsesOptionsSchema mirrors TS
// deepseekLanguageModelResponsesOptions.
var deepseekResponsesOptionsSchema = providerutils.OptionSchema{
	"thinking": providerutils.OptObject(providerutils.OptionSchema{
		"type": providerutils.OptEnum("enabled", "disabled"),
	}),
	"reasoningEffort": providerutils.OptEnum("low", "high", "max"),
	"topLogprobs":     providerutils.OptInt(&zero, &twenty),
	"userId": providerutils.OptAnd(
		providerutils.OptMatch(deepseekUserIDPattern, "userId must match /^[a-zA-Z0-9_-]+$/"),
		providerutils.OptStringLen(0, 512),
	),
}

// deepseekResponsesEffortMap is TS's effortMap for the top-level reasoning
// (DeepSeek's server maps medium to high and xhigh to max).
var deepseekResponsesEffortMap = map[types.ReasoningLevel]string{
	types.ReasoningMinimal: "low",
	types.ReasoningLow:     "low",
	types.ReasoningMedium:  "high",
	types.ReasoningHigh:    "high",
	types.ReasoningXHigh:   "max",
	types.ReasoningMax:     "max",
}

// ResponsesLanguageModel is DeepSeek's Responses API model (TS
// DeepSeekResponsesLanguageModel, POST /responses). It supports DeepSeek's
// server-side web search (WebSearch), reasoning replay by item id, JSON
// schema output, logprobs and the server tool counts DeepSeek bills by.
type ResponsesLanguageModel struct {
	provider *Provider
	modelID  string
}

// NewResponsesLanguageModel creates a DeepSeek Responses model.
func NewResponsesLanguageModel(p *Provider, modelID string) *ResponsesLanguageModel {
	return &ResponsesLanguageModel{provider: p, modelID: modelID}
}

// ResponsesModel returns a DeepSeek model that uses the Responses API (TS
// provider.responses(modelId)).
func (p *Provider) ResponsesModel(modelID string) (provider.LanguageModel, error) {
	return NewResponsesLanguageModel(p, modelID), nil
}

// SpecificationVersion returns the specification version.
func (m *ResponsesLanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns "deepseek.responses" (TS config.provider).
func (m *ResponsesLanguageModel) Provider() string {
	return m.provider.providerOptionsName() + ".responses"
}

// ModelID returns the model ID.
func (m *ResponsesLanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports tool support.
func (m *ResponsesLanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports JSON schema output support.
func (m *ResponsesLanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports image input support.
func (m *ResponsesLanguageModel) SupportsImageInput() bool { return true }

// SupportedURLs is TS supportedUrls: image URLs are sent as given.
func (m *ResponsesLanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{"image/*": {`^https?://.*$`}}
}

// getArgs is TS getArgs: the request body, the call warnings and the name
// of the web search tool.
func (m *ResponsesLanguageModel) getArgs(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, string, error) {
	var warnings []types.Warning
	name := m.provider.providerOptionsName()
	providerOptions := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)
	dsOpts, err := providerutils.ParseProviderOptions(name, providerOptions, deepseekResponsesOptionsSchema)
	if err != nil {
		return nil, nil, "", err
	}

	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
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

	var messages []types.Message
	if opts.Prompt.System != "" {
		messages = append(messages, types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}}})
	}
	if opts.Prompt.IsMessages() {
		messages = append(messages, opts.Prompt.Messages...)
	} else if opts.Prompt.IsSimple() {
		messages = append(messages, prompt.SimpleTextToMessages(opts.Prompt.Text)...)
	}
	input, inputWarnings, err := convertToDeepSeekResponsesInput(messages, name)
	if err != nil {
		return nil, nil, "", err
	}
	warnings = append(warnings, inputWarnings...)

	tools, toolChoice, toolWarnings := prepareDeepSeekResponsesTools(opts.Tools, opts.ToolChoice)
	warnings = append(warnings, toolWarnings...)

	webSearchToolName := ""
	for _, t := range opts.Tools {
		if t.Type == types.ToolTypeProviderDefined && t.ProviderID == WebSearchToolID {
			webSearchToolName = t.Name
			break
		}
	}

	// TS: thinking disabled is effort none; else the provider option, else
	// the top-level reasoning (none stays none).
	var effort interface{}
	thinking, _ := dsOpts["thinking"].(map[string]interface{})
	switch {
	case thinking != nil && thinking["type"] == "disabled":
		effort = "none"
	case dsOpts["reasoningEffort"] != nil:
		effort = dsOpts["reasoningEffort"]
	case providerutils.IsCustomReasoning(opts.Reasoning):
		if *opts.Reasoning == types.ReasoningNone {
			effort = "none"
		} else if e, ok := providerutils.MapReasoningToProviderEffort(opts.Reasoning, deepseekResponsesEffortMap, nil, &warnings); ok {
			effort = e
		}
	}

	body := map[string]interface{}{
		"model": m.modelID,
		"input": input,
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
	if v, ok := dsOpts["topLogprobs"]; ok && v != nil {
		body["top_logprobs"] = v
	}
	if effort != nil {
		body["reasoning"] = map[string]interface{}{"effort": effort}
	}
	if opts.ResponseFormat != nil && providerutils.IsJSONResponseFormat(opts.ResponseFormat) {
		var format map[string]interface{}
		if schema := providerutils.ResponseFormatJSONSchemaOrdered(opts.ResponseFormat.Schema); schema != nil && opts.ResponseFormat.Type != "json_object" {
			formatName := opts.ResponseFormat.Name
			if formatName == "" {
				formatName = "response"
			}
			format = map[string]interface{}{"type": "json_schema", "name": formatName, "schema": schema}
		} else {
			format = map[string]interface{}{"type": "json_object"}
		}
		body["text"] = map[string]interface{}{"format": format}
	}
	if tools != nil {
		body["tools"] = tools
	}
	if toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	if v, ok := dsOpts["userId"]; ok && v != nil {
		body["user"] = v
	}
	return body, warnings, webSearchToolName, nil
}

// DoGenerate is TS doGenerate.
func (m *ResponsesLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	body, warnings, webSearchToolName, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	var response deepseekResponsesResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	headers := providerutils.ExtractHeaders(resp.Headers)
	name := m.provider.providerOptionsName()

	if response.Status == "failed" || response.Error != nil {
		message := "DeepSeek response failed"
		var code string
		if response.Error != nil {
			if response.Error.Message != nil {
				message = *response.Error.Message
			}
			if response.Error.Code != nil {
				code = *response.Error.Code
			}
		}
		status, retryable := deepseekFailedResponseStatus(code)
		pe := providererrors.NewProviderError(m.Provider(), status, code, message, nil)
		pe.Message = message
		pe.ResponseBody = string(resp.Body)
		pe.ResponseHeaders = headers
		pe.Data = providerutils.RawJSONBody(resp.Body)
		pe.Retryable = &retryable
		return nil, pe
	}

	result := &types.GenerateResult{
		Usage:       convertDeepSeekResponsesUsage(response.Usage),
		RawRequest:  body,
		RawResponse: providerutils.RawJSONBody(resp.Body),
		Warnings:    warnings,
	}
	var logprobs []json.RawMessage
	hasFunctionCall := false
	toolName := webSearchToolName
	if toolName == "" {
		toolName = "web_search"
	}
	for _, rawItem := range response.Output {
		var item deepseekResponsesOutputItem
		if json.Unmarshal(rawItem, &item) != nil {
			continue
		}
		switch item.Type {
		case "reasoning":
			part := types.ReasoningContent{Text: deepseekJoinText(item.Content)}
			if item.ID != nil {
				part.ProviderMetadata = deepseekItemIDMetadata(name, *item.ID)
			}
			result.Content = append(result.Content, part)
		case "message":
			for _, c := range item.Content {
				logprobs = append(logprobs, deepseekResponsesLogprobs(c.Logprobs)...)
				if c.Text != nil && *c.Text != "" {
					result.Content = append(result.Content, types.TextContent{Text: *c.Text})
					result.Text += *c.Text
				}
			}
		case "function_call":
			hasFunctionCall = true
			arguments := ""
			if item.Arguments != nil {
				arguments = *item.Arguments
			}
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(arguments), &args)
			tc := types.ToolCall{ID: item.CallID, ToolName: item.Name, Arguments: args, RawArguments: arguments}
			result.ToolCalls = append(result.ToolCalls, tc)
			result.Content = append(result.Content, types.ToolCallContent{ToolCallID: item.CallID, ToolName: item.Name, Input: arguments, Arguments: args})
		case "web_search_call":
			call, res := deepseekWebSearchCallContent(rawItem, toolName, name)
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{ID: call.ToolCallID, ToolName: toolName, Arguments: map[string]interface{}{}, RawArguments: "{}", ProviderExecuted: true, ProviderMetadata: deepseekRawMetadataMap(call.ProviderMetadata)})
			result.Content = append(result.Content, call, res)
		}
	}

	incompleteReason := ""
	if response.IncompleteDetails != nil && response.IncompleteDetails.Reason != nil {
		incompleteReason = *response.IncompleteDetails.Reason
	}
	result.FinishReason = mapDeepSeekResponsesFinishReason(response.Status, incompleteReason, hasFunctionCall)
	result.RawFinishReason = incompleteReason
	if result.RawFinishReason == "" {
		result.RawFinishReason = response.Status
	}
	result.ProviderMetadata = deepseekResponsesProviderMetadata(name, response.ID, response.Usage, logprobs)
	result.ResponseHeaders = headers
	id := ""
	if response.ID != nil {
		id = *response.ID
	}
	model := ""
	if response.Model != nil {
		model = *response.Model
	}
	meta := providerutils.BuildResponseMetadata(id, model, 0)
	if response.CreatedAt != nil {
		meta.Timestamp = deepseekCreatedTimestamp(*response.CreatedAt)
	}
	meta.Headers = headers
	result.ResponseMetadata = meta
	return result, nil
}

// DoStream is TS doStream.
func (m *ResponsesLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, warnings, webSearchToolName, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	body["stream"] = true
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	toolName := webSearchToolName
	if toolName == "" {
		toolName = "web_search"
	}
	s := &deepseekResponsesStream{
		reader:            httpResp.Body,
		parser:            streaming.NewSSEParser(httpResp.Body),
		warnings:          warnings,
		providerName:      m.Provider(),
		optionsName:       m.provider.providerOptionsName(),
		webSearchToolName: toolName,
		includeRawChunks:  opts.IncludeRawChunks,
		headers:           providerutils.ExtractHeaders(httpResp.Header),
		requestBody:       body,
		finishReason:      types.FinishReasonInterrupted,
		openText:          map[string]bool{},
		openReasoning:     map[string]bool{},
		functionCalls:     map[string]*deepseekResponsesCallState{},
	}
	return s, nil
}

// handleError is TS's failedResponseHandler (createJsonErrorResponseHandler
// with deepSeekErrorSchema); a failure without an HTTP response is returned
// unchanged (TS handleFetchError).
func (m *ResponsesLanguageModel) handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) || providerutils.IsInvalidJSONResponse(err) {
		return err
	}
	if pe, ok := providerutils.OpenAICompatibleAPIError(m.Provider(), err); ok {
		return pe
	}
	var status *internalhttp.HTTPStatusError
	if errors.As(err, &status) {
		pe := providererrors.NewProviderErrorFromResponse(m.Provider(), 0, "", http.StatusText(status.StatusCode), status)
		pe.Message = http.StatusText(status.StatusCode)
		pe.Data = nil
		_, h, _ := status.HTTPStatusDetails()
		pe.ResponseHeaders = h
		return pe
	}
	return err
}

// deepseekFailedResponseStatus is TS getFailedResponseStatus.
func deepseekFailedResponseStatus(code string) (int, bool) {
	switch code {
	case "rate_limit_exceeded", "rate_limit_error":
		return 429, true
	case "insufficient_quota":
		return 429, false
	case "authentication_error", "invalid_api_key":
		return 401, false
	case "permission_error":
		return 403, false
	case "bad_request", "context_length_exceeded", "invalid_request_error":
		return 400, false
	case "timeout", "timeout_error":
		return 504, true
	default:
		return 500, true
	}
}

// mapDeepSeekResponsesFinishReason is TS mapDeepSeekResponsesFinishReason.
func mapDeepSeekResponsesFinishReason(status, incompleteReason string, hasFunctionCall bool) types.FinishReason {
	switch status {
	case "completed":
		if hasFunctionCall {
			return types.FinishReasonToolCalls
		}
		return types.FinishReasonStop
	case "incomplete":
		switch incompleteReason {
		case "max_output_tokens":
			return types.FinishReasonLength
		case "content_filter":
			return types.FinishReasonContentFilter
		}
		return types.FinishReasonOther
	case "failed":
		return types.FinishReasonError
	}
	return types.FinishReasonOther
}

// convertDeepSeekResponsesUsage is TS convertDeepSeekResponsesUsage.
func convertDeepSeekResponsesUsage(raw json.RawMessage) types.Usage {
	if len(raw) == 0 || string(raw) == "null" {
		return types.Usage{}
	}
	var u deepseekResponsesUsage
	_ = providerutils.UnmarshalChunk(raw, &u)
	input := int64(derefFloat(u.InputTokens))
	output := int64(derefFloat(u.OutputTokens))
	var cached, reasoning int64
	if u.InputTokensDetails != nil {
		cached = int64(derefFloat(u.InputTokensDetails.CachedTokens))
	}
	if u.OutputTokensDetails != nil {
		reasoning = int64(derefFloat(u.OutputTokensDetails.ReasoningTokens))
	}
	noCache := input - cached
	text := output - reasoning
	if text < 0 {
		text = 0
	}
	total := input + output // TS core: input total + output total
	var rawMap map[string]interface{}
	_ = json.Unmarshal(raw, &rawMap)
	return types.Usage{
		InputTokens:   &input,
		OutputTokens:  &output,
		TotalTokens:   &total,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &noCache, CacheReadTokens: &cached},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning},
		Raw:           rawMap,
	}
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// deepseekResponsesProviderMetadata is TS buildProviderMetadata:
// {responseId?, serverToolUse?: {toolSearch, toolFetch}, logprobs?}.
func deepseekResponsesProviderMetadata(name string, responseID *string, usageRaw json.RawMessage, logprobs []json.RawMessage) map[string]interface{} {
	meta := map[string]interface{}{}
	if responseID != nil {
		meta["responseId"] = *responseID
	}
	if len(usageRaw) > 0 && string(usageRaw) != "null" {
		var u deepseekResponsesUsage
		if providerutils.UnmarshalChunk(usageRaw, &u) == nil && u.ServerToolUse != nil {
			meta["serverToolUse"] = map[string]interface{}{
				"toolSearch": derefFloat(u.ServerToolUse.ToolSearch),
				"toolFetch":  derefFloat(u.ServerToolUse.ToolFetch),
			}
		}
	}
	if len(logprobs) > 0 {
		meta["logprobs"] = logprobs
	}
	return map[string]interface{}{name: meta}
}

// deepseekItemIDMetadata is {[name]: {itemId}}.
func deepseekItemIDMetadata(name, itemID string) json.RawMessage {
	b, _ := json.Marshal(map[string]interface{}{name: map[string]interface{}{"itemId": itemID}})
	return b
}

func deepseekRawMetadataMap(raw json.RawMessage) map[string]interface{} {
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func deepseekJoinText(parts []deepseekResponsesContentPart) string {
	out := ""
	for _, p := range parts {
		if p.Text != nil {
			out += *p.Text
		}
	}
	return out
}

// deepseekWebSearchCallContent is TS webSearchCallContent: the
// provider-executed tool call (input "{}", the raw item in
// providerMetadata[name].webSearchCall, so a later turn replays it) and its
// tool result {status?, action?}.
func deepseekWebSearchCallContent(rawItem json.RawMessage, toolName, name string) (types.ToolCallContent, types.ToolResultContent) {
	var item map[string]interface{}
	_ = json.Unmarshal(rawItem, &item)
	id, _ := item["id"].(string)
	meta, _ := json.Marshal(map[string]interface{}{name: map[string]interface{}{"webSearchCall": item}})
	result := map[string]interface{}{}
	if v, ok := item["status"]; ok && v != nil {
		result["status"] = v
	}
	if v, ok := item["action"]; ok && v != nil {
		result["action"] = v
	}
	return types.ToolCallContent{
			ToolCallID:       id,
			ToolName:         toolName,
			Input:            "{}",
			Arguments:        map[string]interface{}{},
			ProviderExecuted: true,
			ProviderMetadata: meta,
		}, types.ToolResultContent{
			ToolCallID: id,
			ToolName:   toolName,
			Result:     result,
		}
}

// Wire types (TS deepseek-responses-api.ts).

type deepseekResponsesResponse struct {
	ID        *string  `json:"id"`
	CreatedAt *float64 `json:"created_at"`
	Model     *string  `json:"model"`
	Status    string   `json:"status"`
	Error     *struct {
		Code    *string `json:"code"`
		Message *string `json:"message"`
	} `json:"error"`
	IncompleteDetails *struct {
		Reason *string `json:"reason"`
	} `json:"incomplete_details"`
	Output []json.RawMessage `json:"output"`
	Usage  json.RawMessage   `json:"usage"`
}

// deepseekResponsesLogprobs is TS createLogprobSchema's output for each
// entry: token, logprob, bytes? and top_logprobs? ({token, logprob, bytes?}),
// a present null kept and other wire keys dropped like the zod object.
func deepseekResponsesLogprobs(items []json.RawMessage) []json.RawMessage {
	type top struct {
		Token   json.RawMessage `json:"token"`
		Logprob json.RawMessage `json:"logprob"`
		Bytes   json.RawMessage `json:"bytes,omitempty"`
	}
	type entry struct {
		Token       json.RawMessage `json:"token"`
		Logprob     json.RawMessage `json:"logprob"`
		Bytes       json.RawMessage `json:"bytes,omitempty"`
		TopLogprobs *[]top          `json:"top_logprobs,omitempty"`
	}
	out := make([]json.RawMessage, len(items))
	for i, raw := range items {
		out[i] = raw
		var e entry
		var probe map[string]json.RawMessage
		if json.Unmarshal(raw, &probe) != nil || json.Unmarshal(raw, &e) != nil || len(e.Token) == 0 || len(e.Logprob) == 0 {
			continue // the response schema check rejects this shape first
		}
		if tl, ok := probe["top_logprobs"]; ok && string(tl) == "null" {
			if b, err := json.Marshal(struct {
				entry
				TopLogprobs json.RawMessage `json:"top_logprobs"`
			}{entry: e, TopLogprobs: tl}); err == nil {
				out[i] = b
			}
			continue
		}
		if b, err := json.Marshal(e); err == nil {
			out[i] = b
		}
	}
	return out
}

type deepseekResponsesContentPart struct {
	Type     string            `json:"type"`
	Text     *string           `json:"text"`
	Logprobs []json.RawMessage `json:"logprobs"`
}

type deepseekResponsesOutputItem struct {
	Type      string                         `json:"type"`
	ID        *string                        `json:"id"`
	Content   []deepseekResponsesContentPart `json:"content"`
	CallID    string                         `json:"call_id"`
	Name      string                         `json:"name"`
	Arguments *string                        `json:"arguments"`
}

type deepseekResponsesUsage struct {
	InputTokens        *float64 `json:"input_tokens"`
	InputTokensDetails *struct {
		CachedTokens *float64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens        *float64 `json:"output_tokens"`
	OutputTokensDetails *struct {
		ReasoningTokens *float64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	ServerToolUse *struct {
		ToolSearch *float64 `json:"tool_search"`
		ToolFetch  *float64 `json:"tool_fetch"`
	} `json:"server_tool_use"`
}

// Streaming (TS doStream's TransformStream).

type deepseekResponsesCallState struct {
	callID    string
	toolName  string
	arguments string
}

type deepseekResponsesStream struct {
	reader            io.ReadCloser
	parser            *streaming.SSEParser
	warnings          []types.Warning
	providerName      string
	optionsName       string
	webSearchToolName string
	includeRawChunks  bool
	headers           map[string]string
	requestBody       interface{}

	started  bool
	finished bool
	err      error
	pending  []*provider.StreamChunk

	finishReason    types.FinishReason
	rawFinishReason string
	usage           json.RawMessage
	responseID      *string
	hasFunctionCall bool
	logprobs        []json.RawMessage
	openText        map[string]bool
	openTextOrder   []string
	openReasoning   map[string]bool
	openReasonOrder []string
	functionCalls   map[string]*deepseekResponsesCallState
}

// RequestBody implements provider.StreamRequestBody.
func (s *deepseekResponsesStream) RequestBody() interface{} { return s.requestBody }

// Close implements provider.TextStream.
func (s *deepseekResponsesStream) Close() error { return s.reader.Close() }

// Err implements provider.TextStream.
func (s *deepseekResponsesStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *deepseekResponsesStream) push(c *provider.StreamChunk) { s.pending = append(s.pending, c) }

// Next implements provider.TextStream.
func (s *deepseekResponsesStream) Next() (*provider.StreamChunk, error) {
	if !s.started {
		s.started = true
		return &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: s.warnings}, nil
	}
	for {
		if len(s.pending) > 0 {
			c := s.pending[0]
			s.pending = s.pending[1:]
			return c, nil
		}
		if s.finished {
			return nil, io.EOF
		}
		event, err := s.parser.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.flush()
				continue
			}
			s.err = err
			return nil, err
		}
		if streaming.IsStreamDone(event) {
			continue
		}
		s.transform(event.Data)
	}
}

func (s *deepseekResponsesStream) transform(data string) {
	if s.includeRawChunks {
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: streaming.RawChunkValue(data)})
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
		msg, perr := providerutils.ChunkParseFailure(data, err)
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: msg, Err: perr})
		return
	}
	if _, ok := raw["type"].(string); !ok {
		msg, perr := providerutils.CheckTypedFrame(data)
		s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: msg, Err: perr})
		return
	}
	var ev struct {
		Type     string                     `json:"type"`
		Response *deepseekResponsesResponse `json:"response"`
		Item     json.RawMessage            `json:"item"`
		ItemID   string                     `json:"item_id"`
		Delta    string                     `json:"delta"`
		Logprobs []json.RawMessage          `json:"logprobs"`
		Code     *string                    `json:"code"`
		Message  *string                    `json:"message"`
	}
	_ = json.Unmarshal([]byte(data), &ev)

	switch ev.Type {
	case "response.created":
		if ev.Response == nil {
			return
		}
		s.responseID = ev.Response.ID
		meta := &provider.ResponseMetadata{Headers: s.headers}
		if ev.Response.ID != nil {
			meta.ID = *ev.Response.ID
		}
		if ev.Response.Model != nil {
			meta.ModelID = *ev.Response.Model
		}
		if ev.Response.CreatedAt != nil {
			meta.Timestamp = deepseekCreatedTimestamp(*ev.Response.CreatedAt)
		}
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: meta})

	case "response.output_item.added":
		var item deepseekResponsesOutputItem
		if json.Unmarshal(ev.Item, &item) != nil {
			return
		}
		switch {
		case item.Type == "message" && item.ID != nil:
			s.openTextPart(*item.ID)
		case item.Type == "reasoning" && item.ID != nil:
			s.openReasoningPart(*item.ID)
		case item.Type == "function_call":
			key := item.CallID
			if item.ID != nil {
				key = *item.ID
			}
			arguments := ""
			if item.Arguments != nil {
				arguments = *item.Arguments
			}
			s.functionCalls[key] = &deepseekResponsesCallState{callID: item.CallID, toolName: item.Name, arguments: arguments}
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: item.CallID, ToolCall: &types.ToolCall{ID: item.CallID, ToolName: item.Name}})
		}

	case "response.output_text.delta":
		s.logprobs = append(s.logprobs, deepseekResponsesLogprobs(ev.Logprobs)...)
		s.openTextPart(ev.ItemID)
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeText, ID: ev.ItemID, Text: ev.Delta})

	case "response.reasoning_text.delta":
		s.openReasoningPart(ev.ItemID)
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: ev.ItemID, Reasoning: ev.Delta})

	case "response.function_call_arguments.delta":
		call := s.functionCalls[ev.ItemID]
		if call == nil {
			return
		}
		call.arguments += ev.Delta
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: call.callID, Text: ev.Delta})

	case "response.output_item.done":
		var item deepseekResponsesOutputItem
		if json.Unmarshal(ev.Item, &item) != nil {
			return
		}
		switch {
		case item.Type == "message" && item.ID != nil:
			if s.openText[*item.ID] {
				s.closeText(*item.ID)
				s.push(&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: *item.ID})
			}
		case item.Type == "reasoning" && item.ID != nil:
			if s.openReasoning[*item.ID] {
				s.closeReasoning(*item.ID)
				s.push(&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: *item.ID, ProviderMetadata: deepseekItemIDMetadata(s.optionsName, *item.ID)})
			}
		case item.Type == "function_call":
			key := item.CallID
			if item.ID != nil {
				key = *item.ID
			}
			tracked := s.functionCalls[key]
			delete(s.functionCalls, key)
			s.hasFunctionCall = true
			if tracked == nil {
				s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ID: item.CallID, ToolCall: &types.ToolCall{ID: item.CallID, ToolName: item.Name}})
			}
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: item.CallID, ToolCall: &types.ToolCall{ID: item.CallID}})
			arguments := ""
			switch {
			case item.Arguments != nil:
				arguments = *item.Arguments
			case tracked != nil:
				arguments = tracked.arguments
			}
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(arguments), &args)
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: item.CallID, ToolName: item.Name, Arguments: args, RawArguments: arguments}})
		case item.Type == "web_search_call":
			call, res := deepseekWebSearchCallContent(ev.Item, s.webSearchToolName, s.optionsName)
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
				ID: call.ToolCallID, ToolName: call.ToolName, Arguments: map[string]interface{}{}, RawArguments: "{}",
				ProviderExecuted: true, ProviderMetadata: deepseekRawMetadataMap(call.ProviderMetadata),
			}})
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: res.ToolCallID, ToolName: res.ToolName, Result: res.Result}})
		}

	case "response.completed", "response.incomplete", "response.failed":
		if ev.Response == nil {
			return
		}
		done := ev.Response
		s.usage = done.Usage
		if done.ID != nil {
			s.responseID = done.ID
		}
		incomplete := ""
		if done.IncompleteDetails != nil && done.IncompleteDetails.Reason != nil {
			incomplete = *done.IncompleteDetails.Reason
		}
		s.finishReason = mapDeepSeekResponsesFinishReason(done.Status, incomplete, s.hasFunctionCall)
		s.rawFinishReason = incomplete
		if s.rawFinishReason == "" {
			s.rawFinishReason = done.Status
		}
		if ev.Type == "response.failed" {
			message := "DeepSeek response failed"
			var code interface{}
			codeText := ""
			if done.Error != nil {
				if done.Error.Message != nil {
					message = *done.Error.Message
				}
				if done.Error.Code != nil {
					code, codeText = *done.Error.Code, *done.Error.Code
				}
			}
			status, retryable := deepseekFailedResponseStatus(codeText)
			s.push(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: message,
				Err: providererrors.NewStreamProviderError(message, s.providerName, "response.failed", code, &status, &retryable, raw)})
		}

	case "error":
		s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
		message := "DeepSeek stream error"
		if ev.Message != nil {
			message = *ev.Message
		}
		var code interface{}
		if ev.Code != nil {
			code = *ev.Code
		}
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeError, Text: message,
			Err: providererrors.NewStreamProviderError(message, s.providerName, "error", code, nil, nil, raw)})
	}
}

func (s *deepseekResponsesStream) openTextPart(id string) {
	if s.openText[id] {
		return
	}
	s.openText[id] = true
	s.openTextOrder = append(s.openTextOrder, id)
	s.push(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: id})
}

func (s *deepseekResponsesStream) openReasoningPart(id string) {
	if s.openReasoning[id] {
		return
	}
	s.openReasoning[id] = true
	s.openReasonOrder = append(s.openReasonOrder, id)
	s.push(&provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: id, ProviderMetadata: deepseekItemIDMetadata(s.optionsName, id)})
}

func (s *deepseekResponsesStream) closeText(id string) {
	delete(s.openText, id)
	s.openTextOrder = removeString(s.openTextOrder, id)
}

func (s *deepseekResponsesStream) closeReasoning(id string) {
	delete(s.openReasoning, id)
	s.openReasonOrder = removeString(s.openReasonOrder, id)
}

func removeString(list []string, v string) []string {
	out := list[:0]
	for _, x := range list {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// flush is TS flush(): open reasoning and text parts end, then the finish
// part carries the last finish reason, usage and provider metadata.
func (s *deepseekResponsesStream) flush() {
	s.finished = true
	for _, id := range s.openReasonOrder {
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: id})
	}
	for _, id := range s.openTextOrder {
		s.push(&provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: id})
	}
	usage := convertDeepSeekResponsesUsage(s.usage)
	meta, _ := json.Marshal(deepseekResponsesProviderMetadata(s.optionsName, s.responseID, s.usage, s.logprobs))
	s.push(&provider.StreamChunk{
		Type:             provider.ChunkTypeFinish,
		FinishReason:     s.finishReason,
		RawFinishReason:  s.rawFinishReason,
		Usage:            &usage,
		ProviderMetadata: meta,
	})
}
