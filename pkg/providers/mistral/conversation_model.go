package mistral

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"context"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// conversationReasoningEffortModelIDs mirrors reasoningEffortModelIds in
// ai/packages/mistral/src/mistral-conversation-language-model.ts: models that
// accept reasoning_effort on the Conversations API.
var conversationReasoningEffortModelIDs = map[string]bool{
	ModelGLM52: true, ModelLabsLeanstral15: true, ModelLabsLeanstral151: true,
	ModelMagistralMediumLatest: true, ModelMagistralSmallLatest: true,
	ModelMistralLarge4: true, ModelMistralLarge40: true,
	ModelMistralMediumBare: true, ModelMistralMedium2604: true, ModelMistralMedium3: true,
	ModelMistralMedium35Dashed: true, ModelMistralMedium35: true, ModelMistralMediumLatest: true,
	ModelMistralSmall2603: true, ModelMistralSmallLatest: true,
	ModelMistralVibeCliFast: true, ModelMistralVibeCliLatest: true, ModelMistralVibeCliWithTools: true,
	ModelZaiGLM52: true,
}

// ConversationLanguageModel calls Mistral's Conversations API
// (POST /v1/conversations), which supports the built-in WebSearch and
// WebSearchPremium tools. It mirrors TS MistralConversationLanguageModel.
type ConversationLanguageModel struct {
	provider *Provider
	modelID  string
}

// NewConversationLanguageModel creates a Conversations API language model.
func NewConversationLanguageModel(p *Provider, modelID string) *ConversationLanguageModel {
	return &ConversationLanguageModel{provider: p, modelID: modelID}
}

func (m *ConversationLanguageModel) SpecificationVersion() string { return "v3" }
func (m *ConversationLanguageModel) Provider() string             { return "mistral.conversation" }
func (m *ConversationLanguageModel) ModelID() string              { return m.modelID }
func (m *ConversationLanguageModel) SupportsTools() bool          { return true }
func (m *ConversationLanguageModel) SupportsStructuredOutput() bool {
	return true
}
func (m *ConversationLanguageModel) SupportsImageInput() bool { return false }

// Serialize implements provider.SerializableModel.
func (m *ConversationLanguageModel) Serialize() provider.SerializedModel {
	return provider.SerializedModel{Provider: m.Provider(), ModelID: m.ModelID(), Config: provider.SerializableConfig(m.provider.config)}
}

func (m *ConversationLanguageModel) buildRequest(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	provOpts, err := extractMistralProviderOptions(opts)
	if err != nil {
		return nil, nil, err
	}
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}

	var effort string
	if conversationReasoningEffortModelIDs[m.modelID] {
		if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
			if *opts.Reasoning == types.ReasoningNone {
				effort = "none"
			} else {
				effort = "high"
			}
		}
		if provOpts.ReasoningEffort != "" {
			effort = provOpts.ReasoningEffort
		}
	} else if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		warnings = append(warnings, types.Warning{
			Type: "unsupported", Feature: "reasoning",
			Details: "This model does not support reasoning configuration.",
		})
	}

	messages := opts.Prompt.Messages
	if opts.Prompt.IsSimple() && len(messages) == 0 {
		messages = []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.Text}}}}
	}
	if opts.Prompt.System != "" {
		messages = append([]types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}}}}, messages...)
	}

	mistralOptions, _ := opts.ProviderOptions["mistral"].(map[string]interface{})
	structuredOutputs := providerutils.BoolOption(mistralOptions, "structuredOutputs", true)
	strict := providerutils.BoolOption(mistralOptions, "strictJsonSchema", false)

	completion := map[string]interface{}{}
	if format, _ := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs: structuredOutputs,
		StrictJSONSchema:  strict,
	}); format != nil {
		completion["response_format"] = format
		schema, _ := providerutils.ResponseFormatJSONSchema(opts.ResponseFormat.Schema).(map[string]interface{})
		if !structuredOutputs || schema == nil || opts.ResponseFormat.Type == "json_object" {
			messages = providerutils.InjectJSONInstructionIntoMessages(messages, schema, providerutils.InjectJSONInstructionOptions{})
		}
	}

	names := newConversationToolNames(opts.Tools)
	inputs, instructions, err := convertToMistralConversationInputs(messages, names)
	if err != nil {
		return nil, nil, err
	}
	wireTools, toolChoice, toolWarnings := prepareConversationTools(opts.Tools, opts.ToolChoice)

	if opts.MaxTokens != nil {
		completion["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		completion["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		completion["top_p"] = *opts.TopP
	}
	if opts.FrequencyPenalty != nil {
		completion["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		completion["presence_penalty"] = *opts.PresencePenalty
	}
	if len(opts.StopSequences) > 0 {
		completion["stop"] = opts.StopSequences
	}
	if opts.Seed != nil {
		completion["random_seed"] = *opts.Seed
	}
	if effort != "" {
		completion["reasoning_effort"] = effort
	}
	if toolChoice != nil {
		completion["tool_choice"] = toolChoice
	}

	for _, f := range []struct {
		feature string
		set     bool
	}{
		{"safePrompt", provOpts.SafePrompt != nil},
		{"documentImageLimit", provOpts.DocumentImageLimit != nil},
		{"documentPageLimit", provOpts.DocumentPageLimit != nil},
		{"promptCacheKey", provOpts.PromptCacheKey != ""},
		{"parallelToolCalls", provOpts.ParallelToolCalls != nil},
	} {
		if f.set {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: f.feature, Details: "Not supported by the Mistral Conversations API."})
		}
	}

	body := map[string]interface{}{
		"model":           m.modelID,
		"store":           false,
		"inputs":          inputs,
		"completion_args": completion,
	}
	if instructions != "" {
		body["instructions"] = instructions
	}
	if wireTools != nil {
		body["tools"] = wireTools
	}
	return body, append(warnings, toolWarnings...), nil
}

// conversationContent is one content chunk of a message.output entry.
type conversationContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Thinking []struct {
		Text string `json:"text"`
	} `json:"thinking"`
	Title *string `json:"title"`
	URL   *string `json:"url"`
}

// parseConversationContent decodes a content value that is either a string or
// a content chunk (or an array of chunks).
func parseConversationContent(raw json.RawMessage) []conversationContent {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	switch raw[0] {
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return []conversationContent{{Type: "text", Text: s}}
	case '[':
		var list []conversationContent
		_ = json.Unmarshal(raw, &list)
		return list
	default:
		var one conversationContent
		_ = json.Unmarshal(raw, &one)
		return []conversationContent{one}
	}
}

// validConversationContent reports whether a streamed message.output.delta
// content value matches the TS schema: a string or a text / thinking /
// tool_reference chunk.
func validConversationContent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	switch raw[0] {
	case '"':
		return true
	case '{':
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &head) != nil {
			return false
		}
		switch head.Type {
		case "text", "thinking", "tool_reference":
			return true
		}
	}
	return false
}

type conversationConverter struct {
	names      conversationToolNames
	sourceURLs map[string]bool
}

func newConversationConverter(tools []types.Tool) *conversationConverter {
	return &conversationConverter{names: newConversationToolNames(tools), sourceURLs: map[string]bool{}}
}

type conversationPart struct {
	kind   string // "text", "reasoning", "source"
	text   string
	source *types.SourceContent
}

func (c *conversationConverter) convertChunk(chunk conversationContent) []conversationPart {
	switch chunk.Type {
	case "text":
		if chunk.Text != "" {
			return []conversationPart{{kind: "text", text: chunk.Text}}
		}
	case "thinking":
		text := ""
		for _, t := range chunk.Thinking {
			text += t.Text
		}
		if text != "" {
			return []conversationPart{{kind: "reasoning", text: text}}
		}
	case "tool_reference":
		if chunk.URL == nil || c.sourceURLs[*chunk.URL] {
			return nil
		}
		c.sourceURLs[*chunk.URL] = true
		title := *chunk.URL
		if chunk.Title != nil {
			title = *chunk.Title
		}
		return []conversationPart{{kind: "source", source: &types.SourceContent{
			SourceType: "url", ID: streaming.GenerateID(), URL: *chunk.URL, Title: title,
		}}}
	}
	return nil
}

type conversationExecution struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments string                 `json:"arguments"`
	Function  *string                `json:"function"`
	Info      map[string]interface{} `json:"info"`
}

// convertTool builds the provider-executed tool call and result for a
// completed built-in tool execution.
func (c *conversationConverter) convertTool(e conversationExecution) (types.ToolCallContent, types.ToolResultContent) {
	toolName := c.names.customName(e.Name)
	input, _ := json.Marshal(map[string]interface{}{"arguments": e.Arguments})
	meta := map[string]interface{}{"type": "tool.execution", "name": e.Name}
	if e.Function != nil {
		meta["function"] = *e.Function
	}
	metaJSON, _ := json.Marshal(map[string]interface{}{"mistral": meta})
	result := map[string]interface{}{}
	if e.Info != nil {
		result["info"] = e.Info
	}
	return types.ToolCallContent{
			ToolCallID: e.ID, ToolName: toolName, Input: string(input),
			Arguments:        map[string]interface{}{"arguments": e.Arguments},
			ProviderExecuted: true, ProviderMetadata: metaJSON,
		}, types.ToolResultContent{
			ToolCallID: e.ID, ToolName: toolName, Result: result,
		}
}

type conversationResponse struct {
	ConversationID string `json:"conversation_id"`
	Outputs        []struct {
		Type       string                 `json:"type"`
		ID         string                 `json:"id"`
		Name       string                 `json:"name"`
		Arguments  json.RawMessage        `json:"arguments"`
		Function   *string                `json:"function"`
		Info       map[string]interface{} `json:"info"`
		ToolCallID string                 `json:"tool_call_id"`
		Content    json.RawMessage        `json:"content"`
		Model      *string                `json:"model"`
	} `json:"outputs"`
	Usage json.RawMessage `json:"usage"`
}

func (m *ConversationLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	body, warnings, err := m.buildRequest(opts)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost, Path: "/v1/conversations", Body: body, Headers: opts.Headers,
	}, &raw)
	if err != nil {
		return nil, (&LanguageModel{provider: m.provider, modelID: m.modelID}).handleError(err)
	}
	var response conversationResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("mistral: failed to decode conversation response: %w", err)
	}
	converter := newConversationConverter(opts.Tools)
	result := &types.GenerateResult{Warnings: warnings, RawRequest: body, RawResponse: raw}
	hasFunctionCalls := false
	modelID := ""
	for _, out := range response.Outputs {
		switch out.Type {
		case "tool.execution":
			var args string
			if err := json.Unmarshal(out.Arguments, &args); err != nil {
				args = string(out.Arguments)
			}
			call, res := converter.convertTool(conversationExecution{ID: out.ID, Name: out.Name, Arguments: args, Function: out.Function, Info: out.Info})
			result.Content = append(result.Content, call, res)
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID: call.ToolCallID, ToolName: call.ToolName, Arguments: call.Arguments, ProviderExecuted: true,
				ProviderMetadata: map[string]interface{}{"mistral": map[string]interface{}{"type": "tool.execution", "name": out.Name}},
			})
		case "function.call":
			hasFunctionCalls = true
			var argStr string
			if len(out.Arguments) > 0 && out.Arguments[0] == '"' {
				_ = json.Unmarshal(out.Arguments, &argStr)
			} else {
				argStr = string(out.Arguments)
			}
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(argStr), &args)
			call := types.ToolCall{ID: out.ToolCallID, ToolName: out.Name, Arguments: args, RawArguments: argStr}
			result.ToolCalls = append(result.ToolCalls, call)
			result.Content = append(result.Content, types.ToolCallContent{ToolCallID: call.ID, ToolName: call.ToolName, Input: argStr, Arguments: args})
		case "message.output":
			if out.Model != nil {
				modelID = *out.Model
			}
			for _, chunk := range parseConversationContent(out.Content) {
				for _, part := range converter.convertChunk(chunk) {
					switch part.kind {
					case "text":
						result.Text += part.text
						result.Content = append(result.Content, types.TextContent{Text: part.text})
					case "reasoning":
						result.Content = append(result.Content, types.ReasoningContent{Text: part.text})
					case "source":
						result.Content = append(result.Content, *part.source)
					}
				}
			}
		}
	}
	result.FinishReason = types.FinishReasonStop
	if hasFunctionCalls {
		result.FinishReason = types.FinishReasonToolCalls
	}
	result.Usage = convertMistralUsage(response.Usage)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	result.ResponseMetadata = &types.ResponseMetadata{ID: response.ConversationID, ModelID: modelID, Headers: result.ResponseHeaders, Body: raw}
	return result, nil
}

func (m *ConversationLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	body, warnings, err := m.buildRequest(opts)
	if err != nil {
		return nil, err
	}
	body["stream"] = true
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost, Path: "/v1/conversations", Body: body,
		Headers: internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers),
	})
	if err != nil {
		return nil, (&LanguageModel{provider: m.provider, modelID: m.modelID}).handleError(err)
	}
	s := &conversationStream{
		reader:           httpResp.Body,
		parser:           streaming.NewSSEParser(httpResp.Body),
		converter:        newConversationConverter(opts.Tools),
		tracker:          streaming.NewStreamingToolCallTracker(),
		executions:       map[string]*conversationExecution{},
		includeRawChunks: opts.IncludeRawChunks,
		requestBody:      body,
		headers:          providerutils.ExtractHeaders(httpResp.Header),
	}
	s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeStreamStart, Warnings: warnings})
	return s, nil
}

type conversationStream struct {
	reader           io.ReadCloser
	parser           *streaming.SSEParser
	converter        *conversationConverter
	tracker          *streaming.StreamingToolCallTracker
	executions       map[string]*conversationExecution
	queue            []*provider.StreamChunk
	err              error
	includeRawChunks bool
	requestBody      interface{}
	headers          map[string]string

	active           *conversationActive
	usage            json.RawMessage
	hasFunctionCalls bool
	hasError         bool
	isDone           bool
	hasModelID       bool
	finished         bool
}

type conversationActive struct {
	id   string
	kind string // "text" | "reasoning"
}

// RequestBody implements provider.StreamRequestBody.
func (s *conversationStream) RequestBody() interface{} { return s.requestBody }

func (s *conversationStream) Close() error { return s.reader.Close() }

func (s *conversationStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

func (s *conversationStream) closeContent() {
	if s.active == nil {
		return
	}
	t := provider.ChunkTypeTextEnd
	if s.active.kind == "reasoning" {
		t = provider.ChunkTypeReasoningEnd
	}
	s.queue = append(s.queue, &provider.StreamChunk{Type: t, ID: s.active.id})
	s.active = nil
}

func (s *conversationStream) Next() (*provider.StreamChunk, error) {
	for {
		if len(s.queue) > 0 {
			c := s.queue[0]
			s.queue = s.queue[1:]
			return c, nil
		}
		if s.err != nil {
			return nil, s.err
		}
		if s.finished {
			s.err = io.EOF
			return nil, io.EOF
		}
		event, err := s.parser.Next()
		if err != nil {
			if err == io.EOF {
				s.flush()
				continue
			}
			s.err = err
			return nil, err
		}
		if streaming.IsStreamDone(event) {
			s.flush()
			continue
		}
		if s.includeRawChunks {
			var raw interface{}
			if json.Unmarshal([]byte(event.Data), &raw) != nil {
				raw = event.Data
			}
			s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: raw})
		}
		s.handle(event.Data)
	}
}

func (s *conversationStream) handle(data string) {
	var ev struct {
		Type           string                 `json:"type"`
		ConversationID string                 `json:"conversation_id"`
		CreatedAt      *string                `json:"created_at"`
		Usage          json.RawMessage        `json:"usage"`
		Message        string                 `json:"message"`
		Code           int                    `json:"code"`
		ID             string                 `json:"id"`
		ContentIndex   *int                   `json:"content_index"`
		Content        json.RawMessage        `json:"content"`
		Model          *string                `json:"model"`
		ToolCallID     string                 `json:"tool_call_id"`
		Name           string                 `json:"name"`
		Arguments      string                 `json:"arguments"`
		OutputIndex    *int                   `json:"output_index"`
		Function       *string                `json:"function"`
		Info           map[string]interface{} `json:"info"`
	}
	if err := json.Unmarshal([]byte(data), &ev); err != nil {
		s.hasError = true
		s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: fmt.Sprintf("mistral: failed to parse stream chunk: %v", err)})
		return
	}
	switch ev.Type {
	case "conversation.response.started":
		meta := &provider.ResponseMetadata{ID: ev.ConversationID, Headers: s.headers}
		if ev.CreatedAt != nil {
			if ts, err := time.Parse(time.RFC3339Nano, *ev.CreatedAt); err == nil {
				meta.Timestamp = ts
			}
		}
		s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: meta})
	case "conversation.response.done":
		s.usage = ev.Usage
		s.isDone = true
	case "conversation.response.error":
		s.hasError = true
		s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: ev.Message})
	case "message.output.delta":
		if !validConversationContent(ev.Content) {
			s.hasError = true
			s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: "mistral: invalid message.output.delta content in conversation stream"})
			return
		}
		if !s.hasModelID && ev.Model != nil {
			s.hasModelID = true
			s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ModelID: *ev.Model}})
		}
		idx := 0
		if ev.ContentIndex != nil {
			idx = *ev.ContentIndex
		}
		id := fmt.Sprintf("%s-%d", ev.ID, idx)
		for _, chunk := range parseConversationContent(ev.Content) {
			for _, part := range s.converter.convertChunk(chunk) {
				switch part.kind {
				case "text", "reasoning":
					if s.active == nil || s.active.id != id || s.active.kind != part.kind {
						s.closeContent()
						s.active = &conversationActive{id: id, kind: part.kind}
						start := provider.ChunkTypeTextStart
						if part.kind == "reasoning" {
							start = provider.ChunkTypeReasoningStart
						}
						s.queue = append(s.queue, &provider.StreamChunk{Type: start, ID: id})
					}
					if part.kind == "text" {
						s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: id, Text: part.text})
					} else {
						s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeReasoning, ID: id, Reasoning: part.text})
					}
				case "source":
					s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: part.source})
				}
			}
		}
	case "function.call.delta":
		s.closeContent()
		s.hasFunctionCalls = true
		for _, c := range s.tracker.Track(ev.OutputIndex, ev.ToolCallID, ev.Name, ev.Arguments) {
			cc := c
			s.queue = append(s.queue, &cc)
		}
	case "tool.execution.started", "tool.execution.delta":
		s.closeContent()
		if ex, ok := s.executions[ev.ID]; !ok {
			s.executions[ev.ID] = &conversationExecution{ID: ev.ID, Name: ev.Name, Arguments: ev.Arguments, Function: ev.Function}
			s.queue = append(s.queue, &provider.StreamChunk{
				Type:     provider.ChunkTypeToolInputStart,
				ToolCall: &types.ToolCall{ID: ev.ID, ToolName: s.converter.names.customName(ev.Name), ProviderExecuted: true},
			})
		} else {
			ex.Arguments += ev.Arguments
		}
	case "tool.execution.done":
		ex, ok := s.executions[ev.ID]
		if !ok {
			s.hasError = true
			s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: fmt.Sprintf("Missing tool execution start for %s", ev.ID)})
			return
		}
		delta, _ := json.Marshal(map[string]interface{}{"arguments": ex.Arguments})
		s.queue = append(s.queue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: ev.ID, Text: string(delta)},
			&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ID: ev.ID, ToolCall: &types.ToolCall{ID: ev.ID}},
		)
		fn := ev.Function
		if fn == nil {
			fn = ex.Function
		}
		call, res := s.converter.convertTool(conversationExecution{ID: ev.ID, Name: ex.Name, Arguments: ex.Arguments, Function: fn, Info: ev.Info})
		s.queue = append(s.queue,
			&provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{
				ID: call.ToolCallID, ToolName: call.ToolName, Arguments: call.Arguments, ProviderExecuted: true,
				ProviderMetadata: map[string]interface{}{"mistral": mistralExecutionMetadata(ex.Name, fn)},
			}},
			&provider.StreamChunk{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{
				ToolCallID: res.ToolCallID, ToolName: res.ToolName, Result: res.Result,
			}},
		)
		delete(s.executions, ev.ID)
	default:
		// TS validates every event against a closed discriminated union; an
		// unknown event type is a parse error that fails the stream.
		s.hasError = true
		s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeError, Text: fmt.Sprintf("mistral: unknown conversation stream event type %q", ev.Type)})
	}
}

func mistralExecutionMetadata(name string, function *string) map[string]interface{} {
	meta := map[string]interface{}{"type": "tool.execution", "name": name}
	if function != nil {
		meta["function"] = *function
	}
	return meta
}

func (s *conversationStream) flush() {
	if s.finished {
		return
	}
	s.finished = true
	s.closeContent()
	for _, c := range s.tracker.Flush() {
		cc := c
		s.queue = append(s.queue, &cc)
	}
	reason := types.FinishReasonStop
	switch {
	case s.hasError || !s.isDone || len(s.executions) > 0:
		reason = types.FinishReasonError
	case s.hasFunctionCalls:
		reason = types.FinishReasonToolCalls
	}
	usage := convertMistralUsage(s.usage)
	s.queue = append(s.queue, &provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: reason, Usage: &usage})
}

var _ provider.LanguageModel = (*ConversationLanguageModel)(nil)
