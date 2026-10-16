package groq

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements the provider.LanguageModel interface for Groq
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Groq language model
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
	return "groq.chat"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	return false
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	if err := validateChatProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	if err := prompt.RejectProviderReferenceFiles(opts.Prompt.Messages); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, false)
	var response groqResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result, err := m.convertResponse(response)
	if err != nil {
		return nil, err
	}
	providerutils.ApplyWireUsageRawSchema(result, resp.Body, providerutils.UsageRawSchema{
		Keys: []string{"prompt_tokens", "completion_tokens", "total_tokens"},
		Nested: map[string][]string{
			"prompt_tokens_details":     {"cached_tokens"},
			"completion_tokens_details": {"reasoning_tokens"},
		},
	})
	result.RawRequest = reqBody
	applyJSONResponseToolResult(result, response, m.jsonResponseToolName(opts))
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	// TS response.body is the raw parsed JSON, not the schema-filtered value.
	if len(resp.Body) > 0 {
		result.RawResponse = providerutils.RawJSONBody(resp.Body)
	}
	responseMetadata := providerutils.BuildResponseMetadata(response.ID, response.Model, response.Created)
	responseMetadata.Headers = result.ResponseHeaders
	result.ResponseMetadata = responseMetadata
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	if err := validateChatProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	if err := prompt.RejectProviderReferenceFiles(opts.Prompt.Messages); err != nil {
		return nil, err
	}
	reqBody, warnings := m.buildRequestBodyWithWarnings(opts, true)
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/chat/completions",
		Body:    reqBody,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	inner := newGroqStream(httpResp.Body, opts.IncludeRawChunks)
	inner.requestBody = reqBody
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	var textStream provider.TextStream = inner
	if name := m.jsonResponseToolName(opts); name != "" {
		textStream = streaming.NewJSONResponseToolStream(textStream, name)
	}
	return streaming.NewWarningsStream(textStream, warnings), nil
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body, _ := m.buildRequestBodyWithWarnings(opts, stream)
	return body
}

func (m *LanguageModel) buildRequestBodyWithWarnings(opts *provider.GenerateOptions, stream bool) (map[string]interface{}, []types.Warning) {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
	}
	// AssistantToolCallContentMode: Text -- Groq's own TS converter
	// (convert-to-groq-chat-messages.ts) always sets `content: text`
	// unconditionally, never null, even when tool calls are present and no
	// text was generated.
	toOpenAIMessagesOpts := prompt.ToOpenAIMessagesOptions{AssistantToolCallContentMode: prompt.AssistantToolCallContentText}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages, toOpenAIMessagesOpts)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), toOpenAIMessagesOpts)
	}
	if opts.Prompt.System != "" {
		messages := body["messages"].([]map[string]interface{})
		systemMsg := map[string]interface{}{
			"role":    "system",
			"content": opts.Prompt.System,
		}
		body["messages"] = append([]map[string]interface{}{systemMsg}, messages...)
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
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	// TS sends frequency_penalty / presence_penalty and warns that topK is
	// unsupported.
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	var topKWarnings []types.Warning
	if opts.TopK != nil {
		topKWarnings = append(topKWarnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	// Groq rejects JSON response formatting together with function tools, so
	// structured output is requested through a synthetic JSON response tool
	// (TS #21989).
	tools := opts.Tools
	toolChoice := opts.ToolChoice
	var jsonResponseTool *types.Tool
	if providerutils.NeedsJSONResponseTool(opts.ResponseFormat, opts.Tools) {
		t := providerutils.NewJSONResponseTool(opts.ResponseFormat, opts.Tools)
		jsonResponseTool = &t
		tools = append(append([]types.Tool(nil), opts.Tools...), t)
		toolChoice = providerutils.JSONResponseToolChoice(opts.ToolChoice, t.Name)
	}
	var toolPrepWarnings []types.Warning
	if len(tools) > 0 {
		groqTools, groqToolChoice, toolWarnings := prepareTools(m.modelID, tools, toolChoice)
		toolPrepWarnings = toolWarnings
		body["tools"] = groqTools
		if groqToolChoice != nil {
			body["tool_choice"] = groqToolChoice
		}
	}
	compatibleOptions, warnings := providerutils.ResolveOpenAICompatibleProviderOptions("groq", opts.ProviderOptions)
	warnings = append(topKWarnings, warnings...)
	warnings = append(warnings, providerutils.OpenAICompatibleCommonOptionWarnings(compatibleOptions)...)
	// Map top-level Reasoning to Groq reasoning_effort. An explicit
	// providerOptions.groq.reasoningEffort always wins (applied below via
	// ApplyOpenAICompatibleCommonRequestOptions, which runs after this).
	// provider-default → omit. none → "none" for qwen/qwen3.6-27b (the only
	// model that accepts it), else an "unsupported" warning (Groq otherwise
	// has no way to disable reasoning on a reasoning model).
	// minimal/low → "low", medium → "medium", high/xhigh → "high".
	if _, hasExplicitReasoningEffort := providerutils.OpenAICompatibleStringOption(compatibleOptions, "reasoningEffort", "reasoning_effort", "reasoning-effort"); !hasExplicitReasoningEffort && opts.Reasoning != nil {
		switch *opts.Reasoning {
		case types.ReasoningNone:
			if m.modelID == "qwen/qwen3.6-27b" {
				body["reasoning_effort"] = "none"
			} else {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "reasoning",
					Details: `reasoning "none" is not supported by this model.`,
				})
			}
		case types.ReasoningDefault:
			// provider-default: omit
		default:
			if effort, ok := providerutils.MapReasoningToProviderEffort(*opts.Reasoning, map[types.ReasoningLevel]string{
				types.ReasoningMinimal: "low", types.ReasoningLow: "low",
				types.ReasoningMedium: "medium", types.ReasoningHigh: "high",
				types.ReasoningXHigh: "high", types.ReasoningMax: "high",
			}, &warnings); ok {
				body["reasoning_effort"] = effort
			}
		}
	}
	// Response format (TS groq-chat-language-model.ts): structuredOutputs and
	// strictJsonSchema both default to true; a dropped schema is warned about.
	responseFormat, formatWarnings := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs:         providerutils.BoolOption(compatibleOptions, "structuredOutputs", true),
		StrictJSONSchema:          providerutils.BoolOption(compatibleOptions, "strictJsonSchema", true),
		WarnWhenSchemaUnsupported: true,
	})
	if jsonResponseTool == nil {
		if responseFormat != nil {
			body["response_format"] = responseFormat
		}
		warnings = append(warnings, formatWarnings...)
	}
	providerutils.ApplyOpenAICompatibleCommonRequestOptions(body, compatibleOptions)
	// Groq performance service tier: on_demand/flex/auto/performance.
	if serviceTier, ok := providerutils.OpenAICompatibleStringOption(compatibleOptions, "serviceTier", "service_tier", "service-tier"); ok {
		body["service_tier"] = serviceTier
	}
	if parallelToolCalls, ok := providerutils.OpenAICompatibleBoolOption(compatibleOptions, "parallelToolCalls", "parallel_tool_calls", "parallel-tool-calls"); ok {
		body["parallel_tool_calls"] = parallelToolCalls
	}
	if jsonResponseTool != nil {
		body["parallel_tool_calls"] = false
	}
	if reasoningFormat, ok := providerutils.OpenAICompatibleStringOption(compatibleOptions, "reasoningFormat", "reasoning_format", "reasoning-format"); ok {
		body["reasoning_format"] = reasoningFormat
	}
	// TS: warnings: [...warnings, ...toolWarnings].
	return body, append(warnings, toolPrepWarnings...)
}

// jsonResponseToolName returns the name of the synthetic JSON response tool
// added for this request, or "" when none is needed.
func (m *LanguageModel) jsonResponseToolName(opts *provider.GenerateOptions) string {
	if !providerutils.NeedsJSONResponseTool(opts.ResponseFormat, opts.Tools) {
		return ""
	}
	return providerutils.NewJSONResponseTool(opts.ResponseFormat, opts.Tools).Name
}

// applyJSONResponseToolResult turns the JSON response tool call into the
// result text, drops conversational text, and reports "stop" when the model
// only called the response tool (TS #21989).
func applyJSONResponseToolResult(result *types.GenerateResult, response groqResponse, toolName string) {
	if toolName == "" {
		return
	}
	result.Text = ""
	// Prefer the wire argument string over a re-encoding of the parsed map so
	// the structured text keeps the model's exact output.
	rawByIndex := map[int]string{}
	if len(response.Choices) > 0 {
		for i, tc := range response.Choices[0].Message.ToolCalls {
			rawByIndex[i] = tc.Function.Arguments
		}
	}
	var remaining []types.ToolCall
	for i, call := range result.ToolCalls {
		if call.ToolName == toolName {
			if call.RawArguments == "" {
				call.RawArguments = rawByIndex[i]
			}
			text := providerutils.JSONResponseToolText(call)
			result.Text += text
			result.Content = append(result.Content, types.TextContent{Text: text})
			continue
		}
		remaining = append(remaining, call)
	}
	result.ToolCalls = remaining
	if result.Text != "" && len(remaining) == 0 && result.RawFinishReason == "tool_calls" {
		result.FinishReason = types.FinishReasonStop
	}
}

func (m *LanguageModel) convertResponse(response groqResponse) (*types.GenerateResult, error) {
	if len(response.Choices) == 0 {
		return nil, providererrors.NewInvalidResponseDataError(response, "Response did not contain any choices.")
	}
	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:            choice.Message.Content,
		FinishReason:    providerutils.MapOpenAIFinishReason(choice.FinishReason),
		RawFinishReason: choice.FinishReason,
		Usage:           convertGroqUsage(response.Usage),
		RawResponse:     response,
	}
	if choice.Message.Reasoning != "" {
		result.Content = append(result.Content, types.ReasoningContent{Text: choice.Message.Reasoning})
	}
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
			}
			id := tc.ID
			if id == "" {
				// e6087c9/f807e45: some Groq-compatible endpoints omit tool
				// call IDs on non-streaming responses; generate one rather
				// than sending an empty tool_call_id back on the next turn.
				id = streaming.GenerateID()
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:        id,
				ToolName:  tc.Function.Name,
				Arguments: args,
				// Wire text keeps the key order TS keeps (JSON.stringify of the parsed input).
				RawArguments: tc.Function.Arguments,
			}
		}
	}
	return result, nil
}

func convertGroqUsage(usage groqUsage) types.Usage {
	promptTokens := int64(usage.PromptTokens)
	completionTokens := int64(usage.CompletionTokens)
	totalTokens := int64(usage.TotalTokens)
	result := types.Usage{
		InputTokens:  &promptTokens,
		OutputTokens: &completionTokens,
		TotalTokens:  &totalTokens,
	}

	// TS convertGroqUsage: cacheRead and reasoning stay undefined when the
	// wire omits them (a reported 0 stays 0), noCache subtracts only a
	// reported cache count, and text is the full completion count unless
	// reasoning tokens were reported.
	var cacheRead *int64
	var textTokens, imageTokens *int64
	if usage.PromptTokensDetails != nil {
		if usage.PromptTokensDetails.CachedTokens != nil {
			v := int64(*usage.PromptTokensDetails.CachedTokens)
			cacheRead = &v
		}
		if usage.PromptTokensDetails.TextTokens != nil {
			textVal := int64(*usage.PromptTokensDetails.TextTokens)
			textTokens = &textVal
		}
		if usage.PromptTokensDetails.ImageTokens != nil {
			imageVal := int64(*usage.PromptTokensDetails.ImageTokens)
			imageTokens = &imageVal
		}
	}
	noCacheTokens := promptTokens
	if cacheRead != nil {
		noCacheTokens = promptTokens - *cacheRead
	}
	result.InputDetails = &types.InputTokenDetails{
		NoCacheTokens:    &noCacheTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: nil,
		TextTokens:       textTokens,
		ImageTokens:      imageTokens,
	}

	var reasoning *int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		v := int64(*usage.CompletionTokensDetails.ReasoningTokens)
		reasoning = &v
	}
	textOutputTokens := completionTokens
	if reasoning != nil {
		textOutputTokens = completionTokens - *reasoning
		if textOutputTokens < 0 {
			textOutputTokens = 0
		}
	}
	result.OutputDetails = &types.OutputTokenDetails{
		TextTokens:      &textOutputTokens,
		ReasoningTokens: reasoning,
	}

	// Store raw usage
	result.Raw = map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"total_tokens":      usage.TotalTokens,
	}
	if usage.PromptTokensDetails != nil {
		result.Raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		result.Raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}

	return result
}

func (m *LanguageModel) handleError(err error) error {
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if parsed := parseGroqProviderError(err); parsed != nil {
		return parsed
	}
	statusCode := 0
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		statusCode = statusErr.StatusCode
	}
	return providererrors.NewProviderError("groq", statusCode, "", err.Error(), err)
}

type groqResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"` // reasoning mode
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage groqUsage `json:"usage"`
}

type groqUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`

	// Detailed token breakdown (OpenAI-compatible)
	PromptTokensDetails *struct {
		CachedTokens *int `json:"cached_tokens,omitempty"`
		AudioTokens  *int `json:"audio_tokens,omitempty"`
		TextTokens   *int `json:"text_tokens,omitempty"`
		ImageTokens  *int `json:"image_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`

	CompletionTokensDetails *struct {
		ReasoningTokens          *int `json:"reasoning_tokens,omitempty"`
		AcceptedPredictionTokens *int `json:"accepted_prediction_tokens,omitempty"`
		RejectedPredictionTokens *int `json:"rejected_prediction_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

type groqStreamChunk struct {
	ID      string          `json:"id"`
	Object  string          `json:"object"`
	Created int64           `json:"created"`
	Model   string          `json:"model"`
	Error   json.RawMessage `json:"error,omitempty"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"` // Groq uses "reasoning" field
			ToolCalls []struct {
				Index    *int   `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	// XGroq carries Groq-specific metadata including streaming usage.
	// Groq does not use stream_options; usage arrives in this envelope instead.
	XGroq *struct {
		Usage *groqUsage `json:"usage,omitempty"`
	} `json:"x_groq,omitempty"`
}

type groqStream struct {
	reader            io.ReadCloser
	parser            *streaming.SSEParser
	err               error
	toolCallTracker   *streaming.StreamingToolCallTracker
	flushQueue        []*provider.StreamChunk
	isActiveReasoning bool
	pendingUsage      *groqUsage // captured from x_groq.usage
	includeRawChunks  bool
	responseHeaders   map[string]string
	metadataEmitted   bool
	finishEmitted     bool   // a finish chunk has been queued
	finishErrored     bool   // TS finishReason={unified:"error"} after a parse/error chunk
	finishRaw         string // finish_reason seen on the wire; the finish chunk waits for stream end
	// requestBody is the raw request body this stream was opened with,
	// exposed via RequestBody() (provider.StreamRequestBody, hand-off:
	// "stream request body field").
	requestBody interface{}
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that was sent to open this stream.
func (s *groqStream) RequestBody() interface{} { return s.requestBody }

func newGroqStream(reader io.ReadCloser, includeRawChunks ...bool) *groqStream {
	emitRaw := len(includeRawChunks) > 0 && includeRawChunks[0]
	return &groqStream{
		reader:           reader,
		parser:           streaming.NewSSEParser(reader),
		toolCallTracker:  streaming.NewStreamingToolCallTracker(),
		includeRawChunks: emitRaw,
	}
}

func (s *groqStream) Close() error { return s.reader.Close() }

// emitParsedChunk decides how a freshly built chunk should be delivered:
// returned immediately (chunk, true) if nothing is queued ahead of it, or
// appended behind the pending flushQueue (nil, false) for the caller's
// Next() loop to continue draining. This used to recurse by calling
// s.Next() directly in the queued case; see the comment on Next() for why
// that could grow the goroutine stack without bound.
func (s *groqStream) emitParsedChunk(chunk *provider.StreamChunk) (*provider.StreamChunk, bool) {
	if len(s.flushQueue) == 0 {
		return chunk, true
	}
	s.flushQueue = append(s.flushQueue, chunk)
	return nil, false
}

func (s *groqStream) Next() (*provider.StreamChunk, error) {
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
			if err == io.EOF && s.queueFinishAtEnd() {
				continue
			}
			s.err = err
			return nil, err
		}
		if streaming.IsStreamDone(event) {
			if s.queueFinishAtEnd() {
				continue
			}
			s.err = io.EOF
			return nil, io.EOF
		}
		rawQueued := false
		if s.includeRawChunks {
			var raw interface{}
			if err := json.Unmarshal([]byte(event.Data), &raw); err != nil {
				raw = event.Data
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type: provider.ChunkTypeRaw,
				Raw:  raw,
			})
			rawQueued = true
		}
		var chunkData groqStreamChunk
		// TS groqChatChunkSchema: invalid JSON or a schema mismatch is an error part
		// carrying a typed error; the error envelope is routed below.
		msg, perr := providerutils.CheckChatChunk(event.Data, providerutils.ChatChunkShape{Kind: providerutils.ChunkKindGroq})
		if perr == nil {
			if derr := providerutils.UnmarshalChunk([]byte(event.Data), &chunkData); derr != nil {
				msg, perr = providerutils.ChunkParseFailure(event.Data, derr)
			}
		}
		if perr != nil {
			s.finishErrored = true
			errorChunk := &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: msg,
				Err:  perr,
			}
			if rawQueued {
				s.flushQueue = append(s.flushQueue, errorChunk)
				continue
			}
			return errorChunk, nil
		}
		if len(chunkData.Error) > 0 {
			s.finishErrored = true
			chunk, ok := s.emitParsedChunk(&provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: groqStreamErrorText(chunkData.Error),
				// P1-1c part 2: attach the structured StreamProviderError (TS
				// createGroqStreamError) so streamRetries/IsRetryable see the
				// real type/statusCode/isRetryable instead of generic inference.
				Err: newGroqStreamProviderErrorChunk(chunkData.Error, json.RawMessage(event.Data)),
			})
			if ok {
				return chunk, nil
			}
			continue
		}
		if !s.metadataEmitted && (chunkData.ID != "" || chunkData.Model != "" || chunkData.Created != 0 || len(s.responseHeaders) > 0) {
			metadata := &provider.ResponseMetadata{
				ID:      chunkData.ID,
				ModelID: chunkData.Model,
				Headers: s.responseHeaders,
			}
			if chunkData.Created != 0 {
				metadata.Timestamp = time.Unix(chunkData.Created, 0)
			}
			s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
				Type:             provider.ChunkTypeResponseMetadata,
				ResponseMetadata: metadata,
			})
			s.metadataEmitted = true
		}
		// Capture usage from x_groq envelope (Groq's streaming usage mechanism).
		if chunkData.XGroq != nil && chunkData.XGroq.Usage != nil {
			s.pendingUsage = chunkData.XGroq.Usage
		}
		if len(chunkData.Choices) > 0 {
			choice := chunkData.Choices[0]
			// TS records finish_reason before looking at the delta, so a
			// finish_reason that shares a chunk with content still counts.
			if choice.FinishReason != "" {
				s.recordGroqFinish(choice.FinishReason)
			}

			// TS handles reasoning, content and tool calls independently, in that
			// order, so one chunk may carry several. Everything is appended
			// behind chunks already queued for this event (response-metadata).
			if choice.Delta.Reasoning != "" {
				if !s.isActiveReasoning {
					s.isActiveReasoning = true
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningStart, ID: "reasoning-0"})
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{
					Type:      provider.ChunkTypeReasoning,
					Reasoning: choice.Delta.Reasoning,
					ID:        "reasoning-0",
				})
			}
			if choice.Delta.Content != "" {
				if s.isActiveReasoning {
					s.isActiveReasoning = false
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
				}
				s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeText, Text: choice.Delta.Content})
			}
			// Tool call delta: accumulate partial arguments by index; the
			// tracker finalizes at stream end (TS flush()).
			if len(choice.Delta.ToolCalls) > 0 {
				if s.isActiveReasoning {
					s.isActiveReasoning = false
					s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
				}
				for _, tc := range choice.Delta.ToolCalls {
					for _, chunk := range s.toolCallTracker.Track(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments) {
						c := chunk
						s.flushQueue = append(s.flushQueue, &c)
					}
				}
			}
		}
		continue

	}
}

func groqStreamErrorText(raw json.RawMessage) string {
	var withMessage struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &withMessage); err == nil && withMessage.Message != "" {
		return withMessage.Message
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

// queueFinishAtEnd mirrors TS's flush(): when the stream ends without a
// finish_reason the terminal finish part is still emitted (reason "other", or
// "error" after a parse/error chunk). It reports whether it queued anything.
func (s *groqStream) queueFinishAtEnd() bool {
	if s.finishEmitted {
		return false
	}
	s.finishEmitted = true
	if s.isActiveReasoning {
		s.isActiveReasoning = false
		s.flushQueue = append(s.flushQueue, &provider.StreamChunk{Type: provider.ChunkTypeReasoningEnd, ID: "reasoning-0"})
	}
	for _, chunk := range s.toolCallTracker.Flush() {
		c := chunk
		s.flushQueue = append(s.flushQueue, &c)
	}
	finish := &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonOther}
	if s.finishRaw != "" {
		finish.FinishReason = providerutils.MapOpenAIFinishReason(s.finishRaw)
		finish.RawFinishReason = s.finishRaw
	}
	if s.finishErrored {
		finish.FinishReason = types.FinishReasonError
		finish.RawFinishReason = ""
	}
	if s.pendingUsage != nil {
		u := convertGroqUsage(*s.pendingUsage)
		finish.Usage = &u
	}
	s.flushQueue = append(s.flushQueue, finish)
	return true
}

// recordGroqFinish only remembers the finish_reason: TS sets finishReason in
// transform() and closes blocks, finalizes tool calls and enqueues `finish` in
// flush(), so later deltas and the trailing x_groq usage still count.
func (s *groqStream) recordGroqFinish(finishReason string) {
	s.finishRaw = finishReason
	s.finishErrored = false
}

func (s *groqStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}
