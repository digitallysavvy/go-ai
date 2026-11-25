package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// CompletionModel implements the OpenAI Completions API language model.
type CompletionModel struct {
	provider *Provider
	modelID  string
	// requestModelID, when set, is sent as `model` instead of modelID (TS
	// config.requestModelId; Azure reports the model ID but routes by deployment).
	requestModelID string
}

// WithRequestModelID sets the model ID sent to the API, keeping ModelID() as
// the reported identity. It returns m for chaining.
func (m *CompletionModel) WithRequestModelID(id string) *CompletionModel {
	m.requestModelID = id
	return m
}

func (m *CompletionModel) requestModel() string {
	if m.requestModelID != "" {
		return m.requestModelID
	}
	return m.modelID
}

// NewCompletionModel creates a new OpenAI completion model.
func NewCompletionModel(provider *Provider, modelID string) *CompletionModel {
	return &CompletionModel{provider: provider, modelID: modelID}
}

func (m *CompletionModel) SpecificationVersion() string { return "v4" }

func (m *CompletionModel) Provider() string { return m.provider.completionProviderName() }

func (m *CompletionModel) ModelID() string { return m.modelID }

func (m *CompletionModel) SupportsTools() bool { return false }

func (m *CompletionModel) SupportsStructuredOutput() bool { return false }

func (m *CompletionModel) SupportsImageInput() bool { return false }

func (m *CompletionModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	opts = normalizedGenerateOptions(opts)
	body, warnings, err := m.buildCompletionRequest(opts, false)
	if err != nil {
		return nil, err
	}

	var response openAICompletionResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/completions",
		Query:            m.provider.completionQuery(),
		Body:             body,
		Headers:          opts.Headers,
		ValidateResponse: providerutils.ChatResponseValidator(m.Provider(), m.responseKind()),
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	if len(response.Choices) == 0 {
		// TS doGenerate reads response.choices[0] without a guard: the first
		// property read (openai choice.logprobs, openai-compatible choice.text)
		// is a TypeError.
		field := "logprobs"
		if m.provider.config.OpenAICompatible {
			field = "text"
		}
		return nil, fmt.Errorf("Cannot read properties of undefined (reading '%s')", field) //nolint:staticcheck // ST1005: TypeError text from TS
	}
	result := m.convertCompletionResponse(response, warnings)
	result.RawRequest = body
	if len(resp.Body) > 0 {
		result.RawResponse = providerutils.RawJSONBody(resp.Body) // TS response.body is the raw JSON
	}
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	result.ResponseMetadata = completionResponseMetadata(response.ID, response.Created, response.Model, result.ResponseHeaders)
	return result, nil
}

func (m *CompletionModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	opts = normalizedGenerateOptions(opts)
	body, warnings, err := m.buildCompletionRequest(opts, true)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/completions",
		Query:  m.provider.completionQuery(),
		Body:   body,
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Accept": "text/event-stream",
		}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	inner := newCompletionStreamWithMetadata(httpResp.Body, opts.IncludeRawChunks, m.Provider(), httpResp.Header)
	inner.compat = m.provider.config.OpenAICompatible
	if inner.compat {
		// TS OpenAICompatibleCompletionLanguageModel starts from
		// {unified: 'other'} (the OpenAI model from 'interrupted').
		inner.finishReason = types.FinishReasonOther
	}
	inner.requestBody = body
	return streaming.NewWarningsStream(inner, warnings), nil
}

func (m *CompletionModel) buildCompletionRequest(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning, error) {
	opts = normalizedGenerateOptions(opts)
	// TS parseProviderOptions runs before the prompt conversion.
	if err := m.validateCompletionProviderOptions(opts.ProviderOptions); err != nil {
		return nil, nil, err
	}
	warnings := completionWarnings(opts)
	completionPrompt, stopSequences, err := convertToCompletionPrompt(opts.Prompt)
	if err != nil {
		return nil, nil, err
	}

	body := map[string]interface{}{
		"model":  m.requestModel(),
		"prompt": completionPrompt,
	}
	stop := append([]string{}, stopSequences...)
	stop = append(stop, opts.StopSequences...)
	if len(stop) > 0 {
		body["stop"] = stop
	}

	if opts.MaxTokens != nil {
		body["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	for key, value := range m.completionProviderOptions(opts.ProviderOptions) {
		switch key {
		case "echo":
			body["echo"] = value
		case "logitBias":
			body["logit_bias"] = value
		case "logit_bias":
			body["logit_bias"] = value
		case "logprobs":
			if enabled, ok := value.(bool); ok {
				if enabled {
					body["logprobs"] = 0
				}
				continue
			}
			body["logprobs"] = value
		case "suffix":
			body["suffix"] = value
		case "user":
			body["user"] = value
		}
	}

	if m.provider.config.OpenAICompatible {
		// OpenAICompatibleCompletionLanguageModel spreads the provider's own
		// options (raw name, then camelCase) into the body, before prompt/stop.
		name := m.provider.completionProviderOptionsName()
		camel := providerutils.ToOpenAICompatibleCamelCase(name)
		if camel != name {
			if v, ok := opts.ProviderOptions[name]; ok && v != nil {
				msg := "Use '" + camel + "' instead."
				warnings = append(warnings, types.Warning{Type: "deprecated", Setting: "providerOptions key '" + name + "'", Feature: "providerOptions key '" + name + "'", Message: msg, Details: msg})
			}
		}
		for _, key := range []string{name, camel} {
			if raw, ok := providerutils.NormalizeProviderOptionsObject(opts.ProviderOptions[key]); ok {
				for k, v := range raw {
					if k == "prompt" || k == "stop" {
						continue
					}
					body[k] = v
				}
			}
		}
	}

	if stream {
		body["stream"] = true
		// The compat model asks for usage only when config.includeUsage is set.
		if !m.provider.config.OpenAICompatible || m.provider.config.IncludeUsage {
			body["stream_options"] = map[string]interface{}{"include_usage": true}
		}
	}

	return body, warnings, nil
}

func (m *CompletionModel) completionProviderOptions(providerOptions map[string]interface{}) map[string]interface{} {
	merged := map[string]interface{}{}
	copyCompletionOptions := func(name string) {
		raw, ok := providerOptions[name]
		if !ok {
			return
		}
		if opts, ok := providerutils.NormalizeProviderOptionsObject(raw); ok {
			for k, v := range opts {
				merged[k] = v
			}
		}
	}
	copyCompletionOptions("openai")
	if name := m.provider.completionProviderOptionsName(); name != "openai" {
		copyCompletionOptions(name)
	}
	return merged
}

func completionWarnings(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if len(opts.Tools) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "tools"})
	}
	if opts.ToolChoice.Type != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "toolChoice"})
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "" && opts.ResponseFormat.Type != "text" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "responseFormat",
			Details: "JSON response format is not supported.",
		})
	}
	return warnings
}

func convertToCompletionPrompt(prompt types.Prompt) (string, []string, error) {
	messages := prompt.Messages
	if prompt.IsSimple() {
		messages = []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentPart{types.TextContent{Text: prompt.Text}},
		}}
	}
	if prompt.System != "" {
		messages = append([]types.Message{{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: prompt.System}},
		}}, messages...)
	}

	text := ""
	if len(messages) > 0 && messages[0].Role == types.RoleSystem {
		text += completionTextContent(messages[0].Content) + "\n\n"
		messages = messages[1:]
	}

	for _, message := range messages {
		switch message.Role {
		case types.RoleSystem:
			return "", nil, &providererrors.InvalidPromptError{Message: "Unexpected system message in prompt: " + completionTextContent(message.Content)}
		case types.RoleUser:
			text += "user:\n" + completionTextContent(message.Content) + "\n\n"
		case types.RoleAssistant:
			if len(message.ToolCalls) > 0 {
				return "", nil, &providererrors.UnsupportedFunctionalityError{Functionality: "tool-call messages"}
			}
			assistantText, err := completionAssistantTextContent(message.Content)
			if err != nil {
				return "", nil, err
			}
			text += "assistant:\n" + assistantText + "\n\n"
		case types.RoleTool:
			return "", nil, &providererrors.UnsupportedFunctionalityError{Functionality: "tool messages"}
		default:
			return "", nil, fmt.Errorf("unsupported role: %s", message.Role)
		}
	}

	text += "assistant:\n"
	return text, []string{"\nuser:"}, nil
}

func completionTextContent(parts []types.ContentPart) string {
	text := ""
	for _, part := range parts {
		if t, ok := part.(types.TextContent); ok {
			text += t.Text
		}
	}
	return text
}

func completionAssistantTextContent(parts []types.ContentPart) (string, error) {
	text := ""
	for _, part := range parts {
		switch p := part.(type) {
		case types.TextContent:
			text += p.Text
		case *types.TextContent:
			if p != nil {
				text += p.Text
			}
		case types.ToolCallContent, *types.ToolCallContent:
			return "", &providererrors.UnsupportedFunctionalityError{Functionality: "tool-call messages"}
		}
	}
	return text, nil
}

func (m *CompletionModel) convertCompletionResponse(response openAICompletionResponse, warnings []types.Warning) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:            convertOpenAICompletionUsage(response.Usage, m.provider.config.OpenAICompatible),
		RawResponse:      response,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"openai": map[string]interface{}{}},
	}
	if len(response.Choices) == 0 {
		return result
	}

	choice := response.Choices[0]
	result.Text = choice.Text
	// TS openai always returns the text part; openai-compatible only a
	// non-empty one.
	if choice.Text != "" || !m.provider.config.OpenAICompatible {
		result.Content = []types.ContentPart{types.TextContent{Text: choice.Text}}
	}
	result.FinishReason = providerutils.MapOpenAIFinishReason(choice.FinishReason)
	result.RawFinishReason = choice.FinishReason
	if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
		result.ProviderMetadata["openai"].(map[string]interface{})["logprobs"] = completionLogprobs(choice.Logprobs)
	}
	if m.provider.config.OpenAICompatible {
		// TS OpenAICompatibleCompletionLanguageModel returns no providerMetadata.
		result.ProviderMetadata = nil
	}
	return result
}

// completionLogprobs is the TS openaiCompletion{Response,Chunk}Schema
// logprobs output: the zod object keeps tokens, token_logprobs and
// top_logprobs (null kept, absent left out) and drops the other wire keys,
// such as the text_offset the Completions API returns.
func completionLogprobs(raw json.RawMessage) json.RawMessage {
	var picked struct {
		Tokens        json.RawMessage `json:"tokens"`
		TokenLogprobs json.RawMessage `json:"token_logprobs"`
		TopLogprobs   json.RawMessage `json:"top_logprobs,omitempty"`
	}
	if jsonexact.Unmarshal(raw, &picked) != nil || len(picked.Tokens) == 0 || len(picked.TokenLogprobs) == 0 {
		return raw // the response schema check rejects this shape first
	}
	out, err := jsonstr.Marshal(picked)
	if err != nil {
		return raw
	}
	return out
}

func convertOpenAICompletionUsage(usage *openAICompletionUsage, compat bool) types.Usage {
	if usage == nil {
		return types.Usage{}
	}
	// TS raw: usage, the schema output: the OpenAI z.object keeps the three
	// counts, the openai-compatible z.looseObject every key, each number as
	// sent (a fraction included).
	raw := map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	if usage.rawWire != nil {
		if compat {
			raw = usage.rawWire
		} else {
			raw = providerutils.PickSchemaKeys(usage.rawWire, "prompt_tokens", "completion_tokens", "total_tokens")
		}
	}

	input := int64(usage.PromptTokens)
	output := int64(usage.CompletionTokens)
	total := input + output // TS core: input total + output total
	return types.Usage{
		InputTokens:  &input,
		OutputTokens: &output,
		TotalTokens:  &total,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens: &input,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens: &output,
		},
		Raw: raw,
	}
}

func completionResponseMetadata(id string, created *float64, model string, headers map[string]string) *types.ResponseMetadata {
	metadata := &types.ResponseMetadata{
		ID:      id,
		ModelID: model,
		Headers: headers,
	}
	if created != nil {
		if t, ok := unixSecondsTime(*created); ok {
			metadata.Timestamp = t.UTC()
		}
	}
	return metadata
}

func (m *CompletionModel) handleError(err error) error {
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
	if pe, ok := m.provider.apiError(m.Provider(), err); ok {
		return pe // TS openaiFailedResponseHandler / defaultOpenAICompatibleErrorStructure
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

type openAICompletionResponse struct {
	ID      string                   `json:"id"`
	Created *float64                 `json:"created"`
	Model   string                   `json:"model"`
	Choices []openAICompletionChoice `json:"choices"`
	Usage   *openAICompletionUsage   `json:"usage"`
}

type openAICompletionChoice struct {
	Text         string          `json:"text"`
	FinishReason string          `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs"`
}

type openAICompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// rawWire is the usage object as sent (set by UnmarshalJSON).
	rawWire map[string]interface{}
}

// UnmarshalJSON decodes the counts (a fraction truncated into the int
// fields, TS z.number()) and keeps the wire object for Usage.Raw.
func (u *openAICompletionUsage) UnmarshalJSON(b []byte) error {
	type plain openAICompletionUsage
	var p plain
	if err := providerutils.UnmarshalChunk(b, &p); err != nil {
		return err
	}
	*u = openAICompletionUsage(p)
	var raw map[string]interface{}
	if jsonexact.Unmarshal(b, &raw) == nil && raw != nil {
		u.rawWire = raw
	}
	return nil
}

type completionStream struct {
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	includeRawChunks bool
	err              error
	flushQueue       []*provider.StreamChunk
	metadataEmitted  bool
	pendingRaw       []*provider.StreamChunk
	pendingMetadata  *provider.StreamChunk
	textStarted      bool
	outputStarted    bool
	finished         bool
	finishReason     types.FinishReason
	rawFinishReason  string
	usage            *types.Usage
	providerMetadata map[string]interface{}
	// compat is set for OpenAI-compatible hosts, whose TS completion model
	// reports no providerMetadata.
	compat          bool
	providerName    string
	responseHeaders http.Header

	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *completionStream) RequestBody() interface{} { return s.requestBody }

// ResponseHeaders implements provider.StreamResponseHeaders (TS doStream
// response.headers).
func (s *completionStream) ResponseHeaders() map[string]string { return s.headerMap() }

func newCompletionStream(reader io.ReadCloser, includeRawChunks bool) *completionStream {
	return newCompletionStreamWithMetadata(reader, includeRawChunks, "openai.completion", nil)
}

func newCompletionStreamWithMetadata(reader io.ReadCloser, includeRawChunks bool, providerName string, headers http.Header) *completionStream {
	if providerName == "" {
		providerName = "openai.completion"
	}
	return &completionStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		includeRawChunks: includeRawChunks,
		finishReason:     types.FinishReasonInterrupted,
		providerMetadata: map[string]interface{}{"openai": map[string]interface{}{}},
		providerName:     providerName,
		responseHeaders:  headers,
	}
}

func (s *completionStream) Next() (*provider.StreamChunk, error) {
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
			if err == io.EOF && !s.finished {
				s.finish()
				continue
			}
			s.err = err
			return nil, err
		}
		// TS parseJsonEventStream drops the [DONE] frame and keeps reading;
		// flush() runs at the end of the body (the io.EOF branch above).
		if streaming.IsStreamDone(event) {
			continue
		}
		var chunk openAICompletionChunk
		// TS openaiCompletionChunkSchema: invalid JSON or a schema mismatch is
		// an error part carrying a typed error; the error envelope is routed
		// below.
		msg, perr := providerutils.CheckChatChunk(event.Data, providerutils.ChatChunkShape{Kind: providerutils.ChunkKindOpenAICompletion})
		if perr == nil {
			if derr := providerutils.UnmarshalChunk([]byte(event.Data), &chunk); derr != nil {
				msg, perr = providerutils.ChunkParseFailure(event.Data, derr)
			}
		}
		if perr != nil {
			// TS throwIfOpenAIStreamErrorBeforeOutput stops peeking at a
			// chunk that fails to parse: the stream is handed out, the parts
			// read before it come first and a later error frame is an error
			// part.
			s.outputStarted = true
			s.flushPendingMetadata()
			s.queueRawChunk(event.Data)
			// TS finishReason = {unified: 'error', raw: undefined}.
			s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: msg,
				Err:  perr,
			})
			continue
		}
		rawChunk := s.rawChunk(event.Data)
		// TS: the completion chunk union's data member is a z.object (also
		// for openai-compatible), which strips an `error` key.
		if len(chunk.Error) > 0 && chatChunkMatchesDataMember(event.Data, providerutils.ChatChunkShape{Kind: providerutils.ChunkKindOpenAICompletion}) {
			chunk.Error = nil
		}
		if len(chunk.Error) > 0 {
			s.finishReason, s.rawFinishReason = types.FinishReasonError, ""
			// TS throwIfOpenAIStreamErrorBeforeOutput wraps only the OpenAI
			// completion model; the openai-compatible one emits an error part.
			if !s.outputStarted && !s.compat {
				s.flushQueue = nil
				s.pendingRaw = nil
				s.pendingMetadata = nil
				s.err = newOpenAIStreamProviderError(s.providerName, openAIErrorField(chunk.Error), s.responseHeaders) // TS getError: chunk.error
				return nil, s.err
			}
			if rawChunk != nil {
				s.flushQueue = append(s.flushQueue, rawChunk)
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: openAIStreamErrorText(chunk.Error),
				// TS createOpenAIProviderStreamError(value.error): attach the
				// structured StreamProviderError so retries see its
				// type/code/statusCode/isRetryable.
				Err: newOpenAIStreamProviderErrorChunk(s.providerName, openAIErrorField(chunk.Error)),
			})
			continue
		}
		// TS order: the raw chunk, then (first valid chunk) response-metadata
		// and text-start, then the delta
		// (openai-completion-language-model.ts:321-333). Before the first
		// output they wait in arrival order (the early-error peek).
		if rawChunk != nil {
			s.queueOrHold(rawChunk)
		}
		if !s.metadataEmitted {
			s.metadataEmitted = true
			s.textStarted = true
			s.queueOrHold(&provider.StreamChunk{
				Type: provider.ChunkTypeResponseMetadata,
				ResponseMetadata: &provider.ResponseMetadata{
					ID:        chunk.ID,
					ModelID:   chunk.Model,
					Timestamp: completionStreamTimestamp(chunk.Created),
					// TS doStream returns response: { headers }; Go carries
					// them on the response-metadata part.
					Headers: s.headerMap(),
				},
			})
			s.queueOrHold(&provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "0"})
		}
		if chunk.Usage != nil {
			usage := convertOpenAICompletionUsage(chunk.Usage, s.compat)
			s.usage = &usage
		}
		// TS isOpenAICompletionOutputChunk: any choice with text ends the
		// early-error peek.
		for _, c := range chunk.Choices {
			if c.Text != "" {
				s.outputStarted = true
				s.flushPendingMetadata()
				break
			}
		}
		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			if choice.FinishReason != nil {
				s.finishReason = providerutils.MapOpenAIFinishReason(*choice.FinishReason)
				s.rawFinishReason = *choice.FinishReason
			}
			if len(choice.Logprobs) > 0 && string(choice.Logprobs) != "null" {
				s.providerMetadata["openai"].(map[string]interface{})["logprobs"] = completionLogprobs(choice.Logprobs)
			}
			// TS openai: `text.length > 0`; openai-compatible: `text != null`
			// (the schema requires the string, so an empty one is a delta).
			if choice.Text != "" || (s.compat && choice.TextPresent) {
				s.outputStarted = true
				s.flushPendingMetadata()
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					ID:   "0",
					Text: choice.Text,
				})
			}
		}
		continue

	}
}

// queueOrHold queues c, or holds it in arrival order until the first output
// (the early-error peek drops held chunks). An OpenAI-compatible stream has
// no peek (TS OpenAICompatibleCompletionLanguageModel), so nothing is held.
func (s *completionStream) queueOrHold(c *provider.StreamChunk) {
	if s.outputStarted || s.compat {
		s.flushQueue = append(s.flushQueue, c)
		return
	}
	s.pendingRaw = append(s.pendingRaw, c)
}

func (s *completionStream) finish() {
	if s.finished {
		s.err = io.EOF
		return
	}
	s.finished = true
	s.flushPendingMetadata()
	// No valid chunk carried response metadata: TS emits no
	// response-metadata part; the headers reach the caller through
	// ResponseHeaders (TS doStream response.headers, review round 87e).
	// TS opens the text block on the first valid chunk, whether or not it
	// carries text, and closes it in flush().
	if s.metadataEmitted && !s.textStarted {
		s.textStarted = true
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextStart, ID: "0"})
	}
	if s.textStarted {
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "0"})
	}
	finish := &provider.StreamChunk{
		Type:            provider.ChunkTypeFinish,
		FinishReason:    s.finishReason,
		RawFinishReason: s.rawFinishReason,
		Usage:           s.usage,
	}
	if !s.compat {
		finish.ProviderMetadata = mustMarshalCompletionProviderMetadata(s.providerMetadata)
	}
	s.flushQueue = append(s.flushQueue, finish)
	s.err = io.EOF
}

// headerMap is the HTTP response headers as a map, nil when there are none.
func (s *completionStream) headerMap() map[string]string {
	if len(s.responseHeaders) == 0 {
		return nil
	}
	return providerutils.ExtractHeaders(s.responseHeaders)
}

func (s *completionStream) flushPendingMetadata() {
	if len(s.pendingRaw) > 0 {
		s.flushQueue = append(s.flushQueue, s.pendingRaw...)
		s.pendingRaw = nil
	}
	if s.pendingMetadata != nil {
		s.flushQueue = append(s.flushQueue, s.pendingMetadata)
		s.pendingMetadata = nil
	}
}

func (s *completionStream) queueRawChunk(data string) {
	if raw := s.rawChunk(data); raw != nil {
		s.flushQueue = append(s.flushQueue, raw)
	}
}

func (s *completionStream) rawChunk(data string) *provider.StreamChunk {
	if !s.includeRawChunks {
		return nil
	}
	raw := streaming.RawChunkValue(data)
	return &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw}
}

func mustMarshalCompletionProviderMetadata(metadata map[string]interface{}) json.RawMessage {
	raw, err := jsonstr.Marshal(metadata)
	if err != nil {
		return nil
	}
	return raw
}

func completionStreamTimestamp(created *float64) time.Time {
	if created == nil {
		return time.Time{}
	}
	if t, ok := unixSecondsTime(*created); ok {
		return t.UTC()
	}
	return time.Time{}
}

func (s *completionStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *completionStream) Close() error {
	return s.reader.Close()
}

type openAICompletionChunk struct {
	ID      string                        `json:"id"`
	Created *float64                      `json:"created"`
	Model   string                        `json:"model"`
	Choices []openAICompletionChunkChoice `json:"choices"`
	Usage   *openAICompletionUsage        `json:"usage"`
	Error   json.RawMessage               `json:"error,omitempty"`
}

type openAICompletionChunkChoice struct {
	Text         string          `json:"text"`
	FinishReason *string         `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs"`
	// TextPresent reports a non-null `text` (set by UnmarshalJSON).
	TextPresent bool `json:"-"`
}

// UnmarshalJSON decodes the choice and records whether `text` was a string.
func (c *openAICompletionChunkChoice) UnmarshalJSON(b []byte) error {
	type plain openAICompletionChunkChoice
	var aux struct {
		plain
		Text *string `json:"text"`
	}
	if err := jsonexact.Unmarshal(b, &aux); err != nil {
		return err
	}
	*c = openAICompletionChunkChoice(aux.plain)
	if aux.Text != nil {
		c.Text, c.TextPresent = *aux.Text, true
	}
	return nil
}

var _ provider.LanguageModel = (*CompletionModel)(nil)

// responseKind selects the TS response schema of a non-streaming completion
// call: openaiCompletionResponseSchema, or
// openaiCompatibleCompletionResponseSchema for the openai-compatible wrappers.
func (m *CompletionModel) responseKind() providerutils.ChatResponseKind {
	if m.provider.config.OpenAICompatible {
		return providerutils.ResponseKindCompatCompletion
	}
	return providerutils.ResponseKindOpenAICompletion
}
