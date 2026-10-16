package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"io"
	"math"
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

// LanguageModel implements the provider.LanguageModel interface for Anthropic
type LanguageModel struct {
	provider *Provider
	modelID  string
	options  *ModelOptions
}

// NewLanguageModel creates a new Anthropic language model
func NewLanguageModel(provider *Provider, modelID string, options *ModelOptions) *LanguageModel {
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
		options:  options,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
//
// Like TS config.provider this is "anthropic.messages" for the direct
// provider (Config.Name, when set, is used as is); Provider.Name() keeps the
// bare "anthropic" registry name.
func (m *LanguageModel) Provider() string {
	if m.provider.config.Name != "" {
		return m.provider.config.Name
	}
	return "anthropic.messages"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Claude 3+ models support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
// via output_config.format (TS getModelCapabilities().supportsStructuredOutput,
// gated by the provider config).
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return m.configBool(m.provider.config.SupportsNativeStructuredOutput) &&
		GetModelCapabilities(m.modelID).SupportsStructuredOutput
}

// SupportedURLs returns the URL patterns (regular expressions keyed by media
// type) this model accepts directly without downloading first. Mirrors TS
// languageModelConfig.supportedUrls: the direct Anthropic API and
// anthropic-aws accept https image/PDF URLs directly; Vertex-Anthropic and
// Bedrock-Anthropic override Config.SupportedURLs to force base64 conversion.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	if m.provider.config.SupportedURLs != nil {
		return m.provider.config.SupportedURLs(m.modelID)
	}
	return DefaultSupportedURLs()
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	if m.provider.config.SupportsImageInput != nil {
		return *m.provider.config.SupportsImageInput
	}
	// Claude 3+ models support vision
	return m.modelID == "claude-3-opus-20240229" ||
		m.modelID == "claude-3-sonnet-20240229" ||
		m.modelID == "claude-3-haiku-20240307" ||
		m.modelID == "claude-3-5-sonnet-20241022"
}

func (m *LanguageModel) requestHeaders(opts *provider.GenerateOptions, betas []string, stream bool) (map[string]string, error) {
	headers := map[string]string{}
	if opts != nil {
		for k, v := range opts.Headers {
			if strings.EqualFold(k, "anthropic-beta") {
				continue
			}
			headers[k] = v
		}
	}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	if len(betas) > 0 {
		headers["anthropic-beta"] = strings.Join(betas, ",")
	}
	if m.provider.config.TransformRequestHeaders != nil {
		var providerOptions map[string]interface{}
		if opts != nil {
			providerOptions = opts.ProviderOptions
		}
		return m.provider.config.TransformRequestHeaders(headers, providerOptions)
	}
	return headers, nil
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	req, err := m.prepareRequest(opts, false)
	if err != nil {
		return nil, err
	}
	body := m.transformRequestBody(req.body, req.betas, false)

	headers, err := m.requestHeaders(opts, req.betas, false)
	if err != nil {
		return nil, err
	}
	internalReq := internalhttp.Request{
		Method:           http.MethodPost,
		Path:             m.messagesPath(false),
		Body:             body,
		Headers:          headers,
		ValidateResponse: anthropicResponseValidator(m.provider.Name()),
	}
	var response anthropicResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalReq, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	result := m.convertResponseWithOptions(response, convertOptions{
		usesJSONResponseTool:     req.usesJSONResponseTool,
		tools:                    opts.Tools,
		markCodeExecutionDynamic: req.markCodeExecutionDynamic,
		providerOptionsName:      req.providerOptionsName,
		usedCustomProviderKey:    req.usedCustomProviderKey,
		citationDocuments:        extractCitationDocuments(opts.Prompt.Messages),
	})
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	if result.ResponseMetadata == nil {
		result.ResponseMetadata = &types.ResponseMetadata{ModelID: m.ModelID()}
	}
	result.ResponseMetadata.Headers = result.ResponseHeaders
	result.RawRequest = req.body
	result.Warnings = append(result.Warnings, req.warnings...)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	req, err := m.prepareRequest(opts, true)
	if err != nil {
		return nil, err
	}
	body := m.transformRequestBody(req.body, req.betas, true)

	headers, err := m.requestHeaders(opts, req.betas, true)
	if err != nil {
		return nil, err
	}
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    m.messagesPath(true),
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		return nil, m.handleError(err)
	}

	respBody := httpResp.Body
	if m.provider.config.TransformStreamBody != nil {
		respBody = m.provider.config.TransformStreamBody(respBody, httpResp.Header)
	}
	stream := newAnthropicStreamWithWarnings(respBody, req.usesJSONResponseTool, opts.Tools, req.warnings)
	stream.markCodeExecutionDynamic = req.markCodeExecutionDynamic
	stream.providerOptionsName = req.providerOptionsName
	stream.usedCustomProviderKey = req.usedCustomProviderKey
	// TS doStream reports `request: {body}` with the args before
	// transformRequestBody, like doGenerate.
	stream.requestBody = req.body
	stream.includeRawChunks = opts.IncludeRawChunks
	stream.citationDocuments = extractCitationDocuments(opts.Prompt.Messages)
	stream.generateID = m.provider.config.GenerateID
	stream.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	stream.responseTimestamp = time.Now()
	// The API answers overloaded and similar failures with a 200 whose first
	// event is an error. TS reads the first part and throws an APICallError so
	// the request is retried; do the same.
	if err := m.failOnFirstStreamError(stream, httpResp.Header, body); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return providerutils.WithResponseMetadata(stream, httpResp.Header, m.ModelID()), nil
}

// failOnFirstStreamError peeks the stream's first part (past the stream-start and one raw chunk)
// and converts an error part into a *ProviderError, mirroring doStream's
// "first chunk is an error" check. Peeked parts are replayed to the consumer.
func (m *LanguageModel) failOnFirstStreamError(stream *anthropicStream, headers http.Header, body interface{}) error {
	var peeked []*provider.StreamChunk
	for len(peeked) < 3 {
		chunk, err := stream.Next()
		if err != nil {
			// EOF or a read failure: nothing to convert; the consumer sees the
			// same condition from its own Next call.
			break
		}
		peeked = append(peeked, chunk)
		if chunk.Type == provider.ChunkTypeRaw || chunk.Type == provider.ChunkTypeStreamStart {
			continue
		}
		if chunk.Type == provider.ChunkTypeError {
			return m.streamStartError(chunk, headers)
		}
		break
	}
	stream.pending = append(peeked, stream.pending...)
	// Next checks s.err before s.pending; drop a terminal error so the peeked
	// parts are replayed first (the parser reports the same condition again).
	stream.err = nil
	return nil
}

func (m *LanguageModel) streamStartError(chunk *provider.StreamChunk, headers http.Header) error {
	status := 500
	retryable := false
	var data interface{}
	var streamErr *providererrors.StreamProviderError
	if errors.As(chunk.Err, &streamErr) {
		if streamErr.StatusCode != nil {
			status = *streamErr.StatusCode
		}
		retryable = streamErr.IsRetryable
		data = streamErr.Data
	}
	pe := providererrors.NewProviderError(m.provider.Name(), status, "", chunk.Text, chunk.Err)
	pe.ResponseHeaders = providerutils.ExtractHeaders(headers)
	if b, err := json.Marshal(data); err == nil {
		pe.ResponseBody = string(b)
	}
	pe.Data = data
	pe.Retryable = &retryable
	return pe
}

func (m *LanguageModel) messagesPath(stream bool) string {
	if m.provider.config.MessagesPath != nil {
		return m.provider.config.MessagesPath(m.modelID, stream)
	}
	return "/messages"
}

func (m *LanguageModel) transformRequestBody(body map[string]interface{}, betas []string, stream bool) map[string]interface{} {
	if m.provider.config.TransformRequestBodyWithBetas != nil {
		body = m.provider.config.TransformRequestBodyWithBetas(body, betas, stream)
	}
	if m.provider.config.TransformRequestBody != nil {
		return m.provider.config.TransformRequestBody(body, stream)
	}
	return body
}

// ConvertPrompt converts a call prompt (system prompt, messages or text) with
// the Anthropic prompt converter (TS convertToAnthropicPrompt).
func ConvertPrompt(opts *provider.GenerateOptions, sendReasoning *bool) (*prompt.AnthropicPrompt, error) {
	return convertCallPrompt(opts, prompt.AnthropicPromptOptions{
		SendReasoning: sendReasoning,
		ToolsetNames:  prompt.AnthropicToolsetNames(opts.Tools),
	})
}

// convertCallPrompt converts the call prompt; opts.Prompt.System becomes a
// leading system message, matching how the TS core places the system prompt.
func convertCallPrompt(opts *provider.GenerateOptions, promptOpts prompt.AnthropicPromptOptions) (*prompt.AnthropicPrompt, error) {
	var msgs []types.Message
	if opts.Prompt.System != "" {
		msgs = append(msgs, types.Message{
			Role:    types.RoleSystem,
			Content: []types.ContentPart{types.TextContent{Text: opts.Prompt.System}},
		})
	}
	if opts.Prompt.IsMessages() {
		msgs = append(msgs, opts.Prompt.Messages...)
	} else if opts.Prompt.Text != "" {
		msgs = append(msgs, prompt.SimpleTextToMessages(opts.Prompt.Text)...)
	}
	return prompt.ConvertToAnthropicPrompt(msgs, promptOpts)
}

// buildRequestBody builds the Anthropic API request body. Preparation errors
// are dropped; DoGenerate/DoStream surface them.
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	req, err := m.prepareRequest(opts, stream)
	if err != nil {
		return map[string]interface{}{"model": m.modelID}
	}
	return req.body
}

// combineBetaHeaders returns the comma-separated anthropic-beta value for a
// request.
func (m *LanguageModel) combineBetaHeaders(opts *provider.GenerateOptions, stream bool) string {
	req, err := m.prepareRequest(opts, stream)
	if err != nil {
		return ""
	}
	return strings.Join(req.betas, ",")
}

// getBetaHeaders returns the betas required by the model options alone.
func (m *LanguageModel) getBetaHeaders() string {
	return m.combineBetaHeaders(&provider.GenerateOptions{}, false)
}

// convertResponse converts an Anthropic response to GenerateResult.
// usesJsonResponseTool must be true when the request was built with the jsonTool
// structured output strategy (a synthetic 'json' tool was injected). This gates
// the json-tool-as-text extraction so a real user tool named "json" is never
// misidentified as the structured output tool.
func (m *LanguageModel) convertResponse(response anthropicResponse, usesJsonResponseTool bool, toolsOpt ...[]types.Tool) *types.GenerateResult {
	var tools []types.Tool
	if len(toolsOpt) > 0 {
		tools = toolsOpt[0]
	}
	return m.convertResponseWithOptions(response, convertOptions{usesJSONResponseTool: usesJsonResponseTool, tools: tools})
}

// convertOptions carries the request-derived flags convertResponse needs.
type convertOptions struct {
	usesJSONResponseTool     bool
	tools                    []types.Tool
	markCodeExecutionDynamic bool
	// providerOptionsName / usedCustomProviderKey mirror preparedRequest's
	// fields of the same name (see request.go); when set, the response
	// providerMetadata is duplicated under providerOptionsName, matching TS
	// doGenerate's `if (usedCustomProviderKey && providerOptionsName !==
	// 'anthropic') providerMetadata[providerOptionsName] = anthropicMetadata`.
	providerOptionsName   string
	usedCustomProviderKey bool
	// citationDocuments maps citation document_index -> title/filename/mediaType,
	// extracted from citation-enabled file parts in the request prompt (TS
	// extractCitationDocuments). Used to resolve page_location/char_location
	// citations into document source parts.
	citationDocuments []citationDocument
	// rawBatchCitations is true for batch result conversion (TS
	// convertAnthropicBatchResponse), which preserves the *entire* raw
	// citations array on the text part's providerMetadata instead of just the
	// web-search subset doGenerate/doStream keep — batch results are
	// retrieved independently of the original request, so there is no
	// document ordering to normalize page_location/char_location citations
	// against (citationDocuments is left empty in that case).
	rawBatchCitations bool
}

func (m *LanguageModel) convertResponseWithOptions(response anthropicResponse, co convertOptions) *types.GenerateResult {
	usesJsonResponseTool := co.usesJSONResponseTool
	tools := co.tools
	result := &types.GenerateResult{
		Usage:       convertAnthropicUsage(response.Usage),
		RawResponse: response,
	}
	if response.ID != "" || response.Model != "" {
		result.ResponseMetadata = &types.ResponseMetadata{ID: response.ID, ModelID: response.Model}
	}
	toolNameMap := anthropicProviderToolNameMap(tools)

	// byBlock collects the content parts each response block produced, so the
	// final result.Content follows the order the model emitted them (TS builds
	// a single ordered `content` array). A thinking block must stay ahead of
	// the text and tool_use that follow it, and a server tool call ahead of
	// its result, or the next turn's replay is rejected by the API.
	byBlock := make([][]types.ContentPart, len(response.Content))

	// Extract text and reasoning content from content blocks.
	// When jsonTool mode is active the model responds via a synthetic tool, not
	// a text block — skip text blocks entirely in that case, matching the TS SDK.
	//
	// On-demand compaction responses ("compaction" content blocks) are text
	// content whose providerMetadata identifies them as a compaction summary
	// (TS: `content.push({type: 'text', text: part.content, providerMetadata})`).
	// An empty or null compaction content is omitted entirely (TS: `if
	// (!part.content) break;`).
	if !usesJsonResponseTool {
		var textParts []string
		var textBlocks []types.ContentPart
		hasExtraContent := false
		// citationDocs starts from the prompt-derived documents and grows in
		// response-content order as web_fetch_tool_result blocks are
		// encountered below, mirroring TS's single ordered `for (const part of
		// response.content)` loop where `citationDocuments.push(...)` runs
		// inline with citation resolution (anthropic-language-model.ts:1216).
		// A citation can only resolve against a web-fetched document that
		// appears earlier in the content array, same as TS.
		citationDocs := append([]citationDocument(nil), co.citationDocuments...)
		hasWebSearchResults := anthropicResponseHasWebSearchResults(response.Content)
		for blockIdx, content := range response.Content {
			switch content.Type {
			case "text":
				textParts = append(textParts, content.Text)
				// Every citation is normalized onto the text part and kept raw in
				// providerMetadata; retrieved sources are separate (TS #21797).
				textBlocks = append(textBlocks, types.TextContent{
					Text:             content.Text,
					Citations:        mapAnthropicCitations(content.Citations, citationDocs),
					ProviderMetadata: citationsProviderMetadata(content.Citations),
				})
				byBlock[blockIdx] = append(byBlock[blockIdx], textBlocks[len(textBlocks)-1])
				if len(content.Citations) > 0 {
					hasExtraContent = true
				}
				for _, citation := range content.Citations {
					if citationType(citation) == "web_search_result_location" && hasWebSearchResults {
						continue
					}
					if co.rawBatchCitations && citationType(citation) != "web_search_result_location" {
						// Batch results have no prompt to resolve indexed
						// document citations against (TS batch converter).
						continue
					}
					src, ok := createCitationSource(citation, citationDocs, m.provider.newID)
					if !ok {
						continue
					}
					textBlocks = append(textBlocks, src)
					byBlock[blockIdx] = append(byBlock[blockIdx], src)
					hasExtraContent = true
				}
			case "web_fetch_tool_result":
				// Batch result retrieval has no original prompt to derive
				// document ordering from at all, so indexed document citations
				// can never be normalized safely there -- not even against a
				// document fetched within the same batch response. TS's batch
				// converter always resolves citations against a hardcoded `[]`
				// (anthropic-batch.ts:762 `createCitationSource(citation, [],
				// generateId)`), never growing it for web_fetch_tool_result.
				if !co.rawBatchCitations {
					if doc, ok := extractWebFetchCitationDocument(content.Content); ok {
						citationDocs = append(citationDocs, doc)
					}
				}
			case "compaction":
				text, ok := anthropicCompactionText(content.Content)
				if !ok {
					continue
				}
				hasExtraContent = true
				textParts = append(textParts, text)
				textBlocks = append(textBlocks, types.TextContent{
					Text:             text,
					ProviderMetadata: anthropicCompactionMetadata(content.Signature),
				})
				byBlock[blockIdx] = append(byBlock[blockIdx], textBlocks[len(textBlocks)-1])
			}
		}
		if len(textParts) > 0 {
			result.Text = strings.Join(textParts, "")
		}
		// Only surface text blocks as explicit content parts when a compaction
		// block or citations are present. In the common single-text-block,
		// no-citations case, result.Text alone carries the content
		// (generateResultContentParts synthesizes the content part from it),
		// matching existing behavior.
		if hasExtraContent {
			result.Content = append(result.Content, textBlocks...)
		}
	}
	textInContent := len(result.Content) > 0
	result.Content = nil

	// Extract reasoning/thinking content blocks from the response.
	// These are surfaced as ReasoningContent parts in result.Content so callers
	// can include them in subsequent requests (round-tripping thinking blocks).
	for blockIdx, content := range response.Content {
		switch content.Type {
		case "thinking":
			// TS: providerMetadata.anthropic.signature; Signature stays for
			// existing Go callers.
			sigMeta, _ := json.Marshal(map[string]interface{}{ //nolint:errcheck // string map always marshals
				"anthropic": map[string]interface{}{"signature": content.Signature},
			})
			byBlock[blockIdx] = append(byBlock[blockIdx], types.ReasoningContent{
				Text:             content.Thinking,
				Signature:        content.Signature,
				ProviderMetadata: sigMeta,
			})
		case "redacted_thinking":
			// Redacted blocks have no visible text — only the opaque data blob.
			redactedMeta, _ := json.Marshal(map[string]interface{}{ //nolint:errcheck // string map always marshals
				"anthropic": map[string]interface{}{"redactedData": content.Data},
			})
			byBlock[blockIdx] = append(byBlock[blockIdx], types.ReasoningContent{
				RedactedData:     content.Data,
				ProviderMetadata: redactedMeta,
			})
		case "container_upload":
			// A file the model uploaded to the code execution container
			// (TS: content.push({type: 'custom', kind:
			// 'anthropic.container_upload', providerMetadata: {anthropic:
			// {fileId}}})). TS's doStream doesn't handle this block type, so
			// this is generate-only, matching TS parity. ProviderMetadata
			// must be namespaced under "anthropic" like every other
			// provider-metadata payload (see e.g. Google's CustomContent,
			// which nests under "google"), not a flat {fileId} object.
			metadata, _ := json.Marshal(map[string]interface{}{
				"anthropic": map[string]interface{}{"fileId": content.FileID},
			})
			byBlock[blockIdx] = append(byBlock[blockIdx], types.CustomContent{
				Kind:             "anthropic.container_upload",
				ProviderMetadata: metadata,
			})
		case "fallback":
			// Server-side model-hop marker (a mid-output decline fell back
			// to another model): preserved as anthropic.fallback custom
			// content with from/to metadata, round-tripped verbatim through
			// prompt conversion on the next turn so replayed signed
			// thinking blocks stay correctly bounded (TS commit a587f554f7,
			// #21736; previously dropped entirely).
			metadata, _ := json.Marshal(map[string]interface{}{
				"anthropic": map[string]interface{}{
					"type": "fallback",
					"from": content.From,
					"to":   content.To,
				},
			})
			byBlock[blockIdx] = append(byBlock[blockIdx], types.CustomContent{
				Kind:             "anthropic.fallback",
				ProviderMetadata: metadata,
			})
		}
	}

	// Track whether the response was actually delivered via the json tool so we
	// can map the stop reason correctly.
	isJsonResponseFromTool := false

	// Extract tool calls (regular and MCP).
	// The synthetic 'json' tool used in jsonTool structured output mode is handled
	// specially: its input is marshalled to JSON and set as the text result rather
	// than surfaced as a ToolCall. The usesJsonResponseTool gate prevents a real
	// user tool named "json" from being misidentified.
	//
	// mcpToolCalls tracks each mcp_tool_use call's toolName/providerMetadata by
	// id so the paired mcp_tool_result (TS mcpToolCalls[part.tool_use_id]) can
	// resolve the same toolName and reuse the same providerMetadata.
	mcpToolCalls := map[string]mcpToolCallInfo{}
	serverToolProviderNames := map[string]string{}
	addToolCall := func(blockIdx int, tc types.ToolCall) {
		result.ToolCalls = append(result.ToolCalls, tc)
		byBlock[blockIdx] = append(byBlock[blockIdx], toolCallContentOf(tc))
	}
	for blockIdx, content := range response.Content {
		switch content.Type {
		case "tool_use":
			if usesJsonResponseTool && content.Name == "json" {
				// jsonTool mode: the model responded via the synthetic json tool.
				// Extract the tool input as the structured text result.
				result.Text = inputText(content.RawInput, content.Input)
				isJsonResponseFromTool = true
			} else if content.ToolsetName != "" {
				// Toolset member calls (e.g. the computer toolset) map to the
				// toolset tool with the member name as the action.
				meta := map[string]interface{}{"toolsetName": content.ToolsetName}
				if caller := anthropicCallerInfo(content.Caller); caller != nil {
					meta["caller"] = caller
				}
				addToolCall(blockIdx, types.ToolCall{
					ID:               content.ID,
					ToolName:         mapAnthropicToolName(content.ToolsetName, toolNameMap),
					Arguments:        toToolsetMemberInput(content.Name, content.Input),
					RawArguments:     jsonorder.Input(content.RawInput, content.Input).WithLead("action", content.Name).String(),
					ProviderMetadata: map[string]interface{}{"anthropic": meta},
				})
			} else {
				addToolCall(blockIdx, types.ToolCall{
					ID:               content.ID,
					ToolName:         mapAnthropicToolName(content.Name, toolNameMap),
					Arguments:        content.Input,
					RawArguments:     inputText(content.RawInput, content.Input),
					ProviderMetadata: anthropicCallerMetadata(content.Caller),
				})
			}
		case "server_tool_use":
			serverToolProviderNames[content.ID] = content.Name
			if tc, ok := serverToolUseCall(content, toolNameMap, co.markCodeExecutionDynamic); ok {
				addToolCall(blockIdx, tc)
			}
		case "mcp_tool_use":
			// MCP tool calls are executed server-side; surface them as
			// provider-executed dynamic tool calls (TS mcp_tool_use handling).
			meta := anthropicMCPToolUseMetadata(content.ServerName)
			mcpToolCalls[content.ID] = mcpToolCallInfo{toolName: content.Name, providerMetadata: meta}
			addToolCall(blockIdx, types.ToolCall{
				ID:               content.ID,
				ToolName:         content.Name,
				Arguments:        content.Input,
				RawArguments:     inputText(content.RawInput, content.Input),
				ProviderExecuted: true,
				Dynamic:          true,
				ProviderMetadata: meta,
			})
		case "web_search_tool_result":
			// Remap snake_case wire fields to camelCase and emit source chunks.
			// Mirrors TS SDK anthropic-messages-language-model.ts:1055-1093.
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         mapAnthropicToolName(providerToolResultName(content.Type), toolNameMap),
				ProviderExecuted: true,
				ProviderMetadata: callerMetadataJSON(content.Caller),
			}
			if errResult, ok := parseAnthropicWebToolResultError(content.Content, "web_search_tool_result_error", isTrue(content.IsError)); ok {
				trc.Result = errResult
				trc.Error = fmt.Sprintf("%v", errResult["errorCode"])
				trc.IsError = trueFlag()
			} else if len(content.Content) > 0 {
				mapped, sources := convertWebSearchToolResult(content.Content, m.provider.newID)
				trc.Result = mapped
				// TS pushes the tool-result first, then one source per hit.
				byBlock[blockIdx] = append(byBlock[blockIdx], trc)
				for _, src := range sources {
					s := src
					byBlock[blockIdx] = append(byBlock[blockIdx], s)
				}
				break
			}
			byBlock[blockIdx] = append(byBlock[blockIdx], trc)
		case "web_fetch_tool_result":
			// Remap snake_case wire fields (retrieved_at, media_type) to camelCase.
			// Mirrors TS SDK anthropic-messages-language-model.ts:1015-1053.
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         mapAnthropicToolName(providerToolResultName(content.Type), toolNameMap),
				ProviderExecuted: true,
				ProviderMetadata: callerMetadataJSON(content.Caller),
			}
			if errResult, ok := parseAnthropicWebToolResultError(content.Content, "web_fetch_tool_result_error", isTrue(content.IsError)); ok {
				trc.Result = errResult
				trc.Error = fmt.Sprintf("%v", errResult["errorCode"])
				trc.IsError = trueFlag()
			} else if len(content.Content) > 0 {
				trc.Result = convertWebFetchToolResult(content.Content)
			}
			byBlock[blockIdx] = append(byBlock[blockIdx], trc)
		case "mcp_tool_result":
			// Resolve toolName and providerMetadata from the paired mcp_tool_use
			// call (TS: `mcpToolCalls[part.tool_use_id].toolName` /
			// `.providerMetadata`), and mark the result dynamic like TS.
			call := mcpToolCalls[content.ToolUseID]
			if call.toolName == "" && co.rawBatchCitations {
				// TS batch converter: toolCall?.toolName ?? 'mcp'.
				call.toolName = "mcp"
			}
			trc := types.ToolResultContent{
				ToolCallID: content.ToolUseID,
				ToolName:   call.toolName,
				Dynamic:    true,
			}
			if call.providerMetadata != nil {
				if meta, err := json.Marshal(call.providerMetadata); err == nil {
					trc.ProviderMetadata = meta
				}
			}
			var parsed interface{}
			if len(content.Content) > 0 {
				json.Unmarshal(content.Content, &parsed) //nolint:errcheck
			}
			// TS: isError: part.is_error, result: part.content (both kept).
			trc.IsError = content.IsError
			trc.Result = parsed
			if isTrue(content.IsError) {
				trc.Error = fmt.Sprintf("%v", parsed)
			}
			byBlock[blockIdx] = append(byBlock[blockIdx], trc)
		case "code_execution_tool_result", "bash_code_execution_tool_result",
			"text_editor_code_execution_tool_result", "tool_search_tool_result", "advisor_tool_result":
			// Provider-executed results (TS maps each to a tool-result with
			// camelCase error shapes and the caller's custom tool name).
			dr, ok := convertDeferredToolResult(content.Type, content.Content, toolNameMap, serverToolProviderNames[content.ToolUseID])
			if !ok {
				break
			}
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         dr.ToolName,
				Result:           dr.Result,
				ProviderExecuted: content.Type == "advisor_tool_result",
			}
			if dr.IsError {
				trc.Error = dr.ErrorCode
				trc.IsError = trueFlag()
			}
			byBlock[blockIdx] = append(byBlock[blockIdx], trc)
		}
	}

	// Surface the ordered parts. A plain run of text blocks stays on
	// result.Text alone (existing behaviour); anything else -- reasoning, tool
	// calls and results, citations, custom parts -- is emitted as one ordered
	// Content slice that includes the text, so text is no longer hoisted ahead
	// of the thinking or tool blocks that precede it.
	ordered := textInContent
	for i, c := range response.Content {
		if c.Type != "text" && len(byBlock[i]) > 0 {
			ordered = true
		}
	}
	if ordered {
		for _, parts := range byBlock {
			result.Content = append(result.Content, parts...)
		}
	}

	// Map finish reason.
	// When the json tool is used the API returns stop_reason="tool_use" but the
	// caller expects "stop" (the JSON content has been extracted as text, not a
	// tool call). Matches mapAnthropicStopReason() in the TypeScript SDK.
	switch response.StopReason {
	case "end_turn", "pause_turn", "stop_sequence":
		result.FinishReason = types.FinishReasonStop
	case "max_tokens", "model_context_window_exceeded":
		result.FinishReason = types.FinishReasonLength
	case "tool_use":
		if isJsonResponseFromTool {
			result.FinishReason = types.FinishReasonStop
		} else {
			result.FinishReason = types.FinishReasonToolCalls
		}
	case "refusal":
		result.FinishReason = types.FinishReasonContentFilter
	default:
		result.FinishReason = types.FinishReasonOther
	}
	result.RawFinishReason = response.StopReason

	// Extract context management (check root level first, then usage block)
	if response.ContextManagement != nil {
		result.ContextManagement = response.ContextManagement
	} else if response.Usage.ContextManagement != nil {
		// Fallback to legacy location in usage block
		result.ContextManagement = response.Usage.ContextManagement
	}
	result.ProviderMetadata = withCustomProviderKeyMetadata(anthropicProviderMetadata(response.Usage, response.StopSequence, response.StopDetails, response.Container, result.ContextManagement, metadataExtras{
		inputTransformations: rawJSONValue(response.InputTransformations),
		safeguardResults:     rawJSONValue(response.SafeguardResults),
	}), co.providerOptionsName, co.usedCustomProviderKey)

	return result
}

// providerToolResultName returns the canonical tool name for an Anthropic deferred
// tool result block type. Used when the serverToolCallNames lookup misses (e.g.,
// the server_tool_use was in a previous step's response).
func providerToolResultName(resultType string) string {
	switch resultType {
	case "web_search_tool_result":
		return "web_search"
	case "web_fetch_tool_result":
		return "web_fetch"
	case "code_execution_tool_result", "bash_code_execution_tool_result",
		"text_editor_code_execution_tool_result":
		return "code_execution"
	case "tool_search_tool_result":
		return "tool_search"
	case "advisor_tool_result":
		return "advisor"
	default:
		return resultType
	}
}

func anthropicProviderToolNameMap(tools []types.Tool) map[string]string {
	out := map[string]string{}
	for _, t := range tools {
		if providerName := prompt.AnthropicProviderToolName(t.Name); providerName != t.Name {
			out[providerName] = t.Name
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mapAnthropicToolName(name string, providerToSDK map[string]string) string {
	if providerToSDK == nil {
		return name
	}
	if mapped, ok := providerToSDK[name]; ok {
		return mapped
	}
	return name
}

// convertAnthropicUsage converts Anthropic usage to detailed Usage struct
// Implements v6.0 detailed token tracking with prompt caching and compaction support
func convertAnthropicUsage(usage anthropicUsage) types.Usage {
	var inputTokens, outputTokens int64

	servedByFallback := false
	for _, iter := range usage.Iterations {
		if iter.Type == "fallback_message" {
			servedByFallback = true
			break
		}
	}

	// When executor iterations are present for compaction/advisor work, sum the
	// executor iterations because top-level input/output exclude that work.
	// Server-side fallback is the exception: top-level usage already reflects
	// the served fallback answer, while the primary message iteration is only
	// the blocked/failed attempt.
	if len(usage.Iterations) > 0 && !servedByFallback {
		hasExecutorIteration := false
		for _, iter := range usage.Iterations {
			if iter.Type == "compaction" || iter.Type == "message" {
				hasExecutorIteration = true
				inputTokens += int64(iter.InputTokens)
				outputTokens += int64(iter.OutputTokens)
			}
		}
		if !hasExecutorIteration {
			inputTokens = int64(usage.InputTokens)
			outputTokens = int64(usage.OutputTokens)
		}
	} else {
		inputTokens = int64(usage.InputTokens)
		outputTokens = int64(usage.OutputTokens)
	}

	cacheCreationTokens := int64(usage.CacheCreationInputTokens)
	cacheReadTokens := int64(usage.CacheReadInputTokens)

	// Calculate total input tokens (includes all cache-related tokens)
	totalInputTokens := inputTokens + cacheCreationTokens + cacheReadTokens
	totalTokens := totalInputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &totalInputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Set input token details
	// Anthropic provides: input_tokens (regular), cache_creation_input_tokens (write), cache_read_input_tokens (read)
	result.InputDetails = &types.InputTokenDetails{
		NoCacheTokens:    &inputTokens,
		CacheReadTokens:  &cacheReadTokens,
		CacheWriteTokens: &cacheCreationTokens,
	}

	// usage.output_tokens_details.thinking_tokens is the reasoning share of
	// the output tokens (TS convertAnthropicUsage); text is the remainder.
	// Without thinking_tokens TS leaves text and reasoning undefined.
	result.OutputDetails = &types.OutputTokenDetails{}
	if usage.OutputTokensDetails != nil && usage.OutputTokensDetails.ThinkingTokens != nil {
		reasoning := int64(*usage.OutputTokensDetails.ThinkingTokens)
		text := outputTokens - reasoning
		result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	}

	// Store raw usage for provider-specific details
	result.Raw = anthropicRawUsage(usage)

	return result
}

// anthropicCompactionText extracts the compaction block's text content,
// treating an absent, null, or empty string as "no content" (TS: `if
// (!part.content) break;`). The second return value is false when the block
// should be omitted entirely.
func anthropicCompactionText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var text *string
	if err := json.Unmarshal(raw, &text); err != nil || text == nil || *text == "" {
		return "", false
	}
	return *text, true
}

// anthropicCompactionMetadata builds the providerMetadata.anthropic payload
// for a compaction text block (TS: `{type: 'compaction', ...(signature && {signature})}`).
func anthropicCompactionMetadata(signature string) json.RawMessage {
	meta := map[string]interface{}{"type": "compaction"}
	if signature != "" {
		meta["signature"] = signature
	}
	raw, err := json.Marshal(map[string]interface{}{"anthropic": meta})
	if err != nil {
		return nil
	}
	return raw
}

// metadataExtras carries optional provider metadata fields.
type metadataExtras struct {
	inputTransformations interface{}
	safeguardResults     interface{}
}

func anthropicProviderMetadata(usage anthropicUsage, stopSequence string, stopDetails *anthropicStopDetails, container *anthropicContainerResponse, contextManagement interface{}, extrasOpt ...metadataExtras) map[string]interface{} {
	anthropic := map[string]interface{}{
		"usage":             anthropicRawUsage(usage),
		"stopSequence":      nil,
		"iterations":        anthropicUsageIterationsMetadata(usage.Iterations),
		"container":         nil,
		"contextManagement": mapAnthropicContextManagement(contextManagement),
	}
	if stopSequence != "" {
		anthropic["stopSequence"] = stopSequence
	}
	if mappedStopDetails := mapAnthropicStopDetails(stopDetails); mappedStopDetails != nil {
		anthropic["stopDetails"] = mappedStopDetails
	}
	if len(extrasOpt) > 0 {
		if extrasOpt[0].inputTransformations != nil {
			anthropic["inputTransformations"] = extrasOpt[0].inputTransformations
		}
		if extrasOpt[0].safeguardResults != nil {
			anthropic["safeguardResults"] = extrasOpt[0].safeguardResults
		}
	}
	if container != nil {
		anthropic["container"] = map[string]interface{}{
			"expiresAt": container.ExpiresAt,
			"id":        container.ID,
			"skills":    mapAnthropicContainerSkills(container.Skills),
		}
	}
	return map[string]interface{}{"anthropic": anthropic}
}

// withCustomProviderKeyMetadata duplicates meta["anthropic"] under
// providerOptionsName when usedCustomProviderKey is set and
// providerOptionsName isn't "anthropic" itself, matching TS's
// `if (usedCustomProviderKey && providerOptionsName !== 'anthropic')
// providerMetadata[providerOptionsName] = anthropicMetadata`.
func withCustomProviderKeyMetadata(meta map[string]interface{}, providerOptionsName string, usedCustomProviderKey bool) map[string]interface{} {
	if usedCustomProviderKey && providerOptionsName != "" && providerOptionsName != "anthropic" {
		if anthropicMeta, ok := meta["anthropic"]; ok {
			meta[providerOptionsName] = anthropicMeta
		}
	}
	return meta
}

func anthropicProviderMetadataRaw(usage anthropicUsage, stopSequence string, stopDetails *anthropicStopDetails, container *anthropicContainerResponse, contextManagement interface{}, providerOptionsName string, usedCustomProviderKey bool, extras ...metadataExtras) json.RawMessage {
	meta := withCustomProviderKeyMetadata(anthropicProviderMetadata(usage, stopSequence, stopDetails, container, contextManagement, extras...), providerOptionsName, usedCustomProviderKey)
	raw, err := json.Marshal(meta)
	if err != nil {
		return nil
	}
	return raw
}

func mapAnthropicStopDetails(stopDetails *anthropicStopDetails) map[string]interface{} {
	if stopDetails == nil {
		return nil
	}
	out := map[string]interface{}{"type": stopDetails.Type}
	if stopDetails.Category != "" {
		out["category"] = stopDetails.Category
	}
	if stopDetails.Explanation != "" {
		out["explanation"] = stopDetails.Explanation
	}
	if stopDetails.RecommendedModel != "" {
		out["recommendedModel"] = stopDetails.RecommendedModel
	}
	return out
}

func mapAnthropicContainerSkills(skills []anthropicContainerSkillResponse) interface{} {
	// TS `skills?.map(...) ?? null`: an absent or null list is null, but an
	// empty list stays an empty array.
	if skills == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(skills))
	for _, skill := range skills {
		item := map[string]interface{}{
			"type":    skill.Type,
			"skillId": skill.SkillID,
		}
		if skill.Version != "" {
			item["version"] = skill.Version
		}
		out = append(out, item)
	}
	return out
}

func mapAnthropicContextManagement(contextManagement interface{}) interface{} {
	if contextManagement == nil {
		return nil
	}
	switch cm := contextManagement.(type) {
	case *ContextManagementResponse:
		return mapAnthropicContextManagementResponse(cm)
	case ContextManagementResponse:
		return mapAnthropicContextManagementResponse(&cm)
	default:
		return contextManagement
	}
}

func mapAnthropicContextManagementResponse(contextManagement *ContextManagementResponse) interface{} {
	if contextManagement == nil {
		return nil
	}
	edits := make([]map[string]interface{}, 0, len(contextManagement.AppliedEdits))
	for _, edit := range contextManagement.AppliedEdits {
		switch e := edit.(type) {
		case *AppliedClearToolUsesEdit:
			edits = append(edits, map[string]interface{}{
				"type":               e.Type,
				"clearedToolUses":    e.ClearedToolUses,
				"clearedInputTokens": e.ClearedInputTokens,
			})
		case *AppliedClearThinkingEdit:
			edits = append(edits, map[string]interface{}{
				"type":                 e.Type,
				"clearedThinkingTurns": e.ClearedThinkingTurns,
				"clearedInputTokens":   e.ClearedInputTokens,
			})
		case *AppliedCompactEdit:
			edits = append(edits, map[string]interface{}{
				"type": e.Type,
			})
		}
	}
	return map[string]interface{}{"appliedEdits": edits}
}

func anthropicRawUsage(usage anthropicUsage) map[string]interface{} {
	if usage.Raw != nil {
		out := make(map[string]interface{}, len(usage.Raw))
		for k, v := range usage.Raw {
			out[k] = v
		}
		return out
	}
	raw := map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}
	if usage.CacheCreationInputTokens > 0 {
		raw["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		raw["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if len(usage.Iterations) > 0 {
		raw["iterations"] = usage.Iterations
	}
	if usage.OutputTokensDetails != nil {
		raw["output_tokens_details"] = usage.OutputTokensDetails
	}
	return raw
}

func anthropicUsageIterationsMetadata(iterations []UsageIteration) interface{} {
	if len(iterations) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(iterations))
	for _, iter := range iterations {
		item := map[string]interface{}{
			"type":         iter.Type,
			"inputTokens":  iter.InputTokens,
			"outputTokens": iter.OutputTokens,
		}
		if iter.Model != "" {
			item["model"] = iter.Model
		}
		if iter.CacheCreationInputTokens > 0 {
			item["cacheCreationInputTokens"] = iter.CacheCreationInputTokens
		}
		if iter.CacheReadInputTokens > 0 {
			item["cacheReadInputTokens"] = iter.CacheReadInputTokens
		}
		out = append(out, item)
	}
	return out
}

// Tool name constants used to detect code execution tools when building beta headers and warnings.
// Defined here to avoid importing the tools sub-package.
const (
	codeExecution20260120ToolName = "anthropic.code_execution_20260120"
	codeExecution20250825ToolName = "anthropic.code_execution_20250825"
)

// detectSkillsWarning returns a warning when container skills are configured but no code
// execution tool is present in opts. Matches TypeScript SDK behavior.
//
// Resolves the effective per-call ModelOptions (construction-time defaults
// merged with this call's providerOptions.anthropic/providerOptions.<custom>
// container) rather than reading m.options directly, so a container/skills
// configuration supplied only via providerOptions (not construction-time
// ModelOptions) is still detected.
func (m *LanguageModel) detectSkillsWarning(opts *provider.GenerateOptions) *types.Warning {
	o, _, err := m.resolveCallOptions(opts)
	if err != nil || o.Container == nil || len(o.Container.Skills) == 0 {
		return nil
	}
	if opts != nil {
		for _, t := range opts.Tools {
			if t.Name == codeExecution20250825ToolName || t.Name == codeExecution20260120ToolName {
				return nil
			}
		}
	}
	return &types.Warning{
		Type:    "other",
		Message: "code execution tool is required when using skills",
	}
}

// anthropicErrorBody is the top-level structure of an Anthropic API error response.
// Example: {"type":"error","error":{"type":"overloaded_error","message":"..."}}
type anthropicErrorBody struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicStreamErrorMetadata returns the inferred (statusCode, isRetryable)
// pair for a mid-stream Anthropic error's type field, mirroring TS
// anthropic-language-model.ts's getAnthropicStreamErrorMetadata. The zero
// value (0, false) means "no inference" (TS returns {}); the caller only
// falls back to it when the wire event didn't supply its own statusCode/
// isRetryable.
func anthropicStreamErrorMetadata(errType string) (statusCode int, isRetryable bool) {
	switch errType {
	case "api_error":
		return 500, true
	case "overloaded_error":
		return 529, true
	case "rate_limit_error":
		return 429, true
	case "request_too_large":
		return 413, false
	case "authentication_error":
		return 401, false
	case "permission_error":
		return 403, false
	case "not_found_error":
		return 404, false
	case "billing_error", "invalid_request_error":
		return 400, false
	default:
		return 0, false
	}
}

// handleError converts various errors to provider errors.
// It attempts to parse Anthropic API error responses (HTTP NNN: {...}) so that
// error.type is surfaced as ProviderError.ErrorCode rather than being lost.
func (m *LanguageModel) handleError(err error) error {
	if err == nil {
		return nil
	}
	var loadKeyErr *providererrors.LoadAPIKeyError
	if errors.As(err, &loadKeyErr) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	if providerutils.IsInvalidJSONResponse(err) {
		return err // TS createJsonResponseHandler failure, as is
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		body := statusErr.Body
		if m.provider.config.TransformErrorBody != nil {
			body = m.provider.config.TransformErrorBody(body)
		}
		return anthropicHTTPError(m.provider.Name(), statusErr.StatusCode, statusErr.Headers, body, err)
	}
	msg := err.Error()
	// HTTP errors from the internal client look like "HTTP NNN: <body>"
	// Try to find the JSON body portion.
	if idx := strings.Index(msg, ": {"); idx != -1 {
		jsonPart := msg[idx+2:]
		var body anthropicErrorBody
		if jsonErr := json.Unmarshal([]byte(jsonPart), &body); jsonErr == nil && body.Error.Type != "" {
			return providererrors.NewProviderError(m.errorProviderName(), 0, body.Error.Type, body.Error.Message, err)
		}
	}
	return providererrors.NewProviderError(m.errorProviderName(), 0, "", msg, err)
}

// errorProviderName is the provider name attributed to non-HTTP failures.
func (m *LanguageModel) errorProviderName() string {
	if m.provider == nil {
		return "anthropic"
	}
	return m.provider.Name()
}

// anthropicMaxOutputTokens returns the maximum output tokens for a model.
func anthropicMaxOutputTokens(modelID string) int {
	return GetModelCapabilities(modelID).MaxOutputTokens
}

func anthropicSupportsAdaptiveThinking(modelID string) bool {
	return GetModelCapabilities(modelID).SupportsAdaptiveThinking
}

// anthropicReasoningBudget computes the budget_tokens for Anthropic's extended thinking
// from a ReasoningLevel. Mirrors mapReasoningToProviderBudget in the TS SDK with
// percentages 2/10/30/60/90% of the model's max output tokens, floored at 1024.
func anthropicReasoningBudget(level types.ReasoningLevel, modelID string) int {
	max := anthropicMaxOutputTokens(modelID)
	var pct float64
	switch level {
	case types.ReasoningMinimal:
		pct = 0.02
	case types.ReasoningLow:
		pct = 0.10
	case types.ReasoningMedium:
		pct = 0.30
	case types.ReasoningHigh:
		pct = 0.60
	case types.ReasoningXHigh:
		pct = 0.90
	case types.ReasoningMax:
		pct = 0.95
	default:
		pct = 0.30
	}
	budget := int(math.Round(float64(max) * pct))
	if budget < 1024 {
		budget = 1024
	}
	if budget > max {
		budget = max
	}
	return budget
}

// anthropicContainerResponse represents container info returned in the Anthropic API response.
// The container field is present when a container was used or created during the request.
type anthropicContainerResponse struct {
	ID        string                            `json:"id"`
	ExpiresAt string                            `json:"expires_at,omitempty"`
	Skills    []anthropicContainerSkillResponse `json:"skills,omitempty"`
}

type anthropicContainerSkillResponse struct {
	Type    string `json:"type"`
	SkillID string `json:"skill_id"`
	Version string `json:"version,omitempty"`
}

type anthropicStopDetails struct {
	Type             string `json:"type"`
	Category         string `json:"category,omitempty"`
	Explanation      string `json:"explanation,omitempty"`
	RecommendedModel string `json:"recommended_model,omitempty"`
}

// anthropicResponse represents the Anthropic API response
// Updated in v6.0 to support prompt caching and context management
type anthropicResponse struct {
	ID           string                `json:"id"`
	Type         string                `json:"type"`
	Role         string                `json:"role"`
	Content      []anthropicContent    `json:"content"`
	Model        string                `json:"model"`
	StopReason   string                `json:"stop_reason"`
	StopSequence string                `json:"stop_sequence,omitempty"`
	StopDetails  *anthropicStopDetails `json:"stop_details,omitempty"`
	Usage        anthropicUsage        `json:"usage"`
	// Root-level context management (new location - takes precedence)
	ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
	// Container info returned when a container was used/created
	Container *anthropicContainerResponse `json:"container,omitempty"`
	// InputTransformations reports preserved-thinking input transformations.
	InputTransformations json.RawMessage `json:"input_transformations,omitempty"`
	// SafeguardResults holds safeguard classifier verdicts.
	SafeguardResults json.RawMessage `json:"safeguard_results,omitempty"`
}

// anthropicUsage represents Anthropic usage information with cache tracking and context management
type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"` // v6.0
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`     // v6.0
	// Legacy location for context management (fallback)
	ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
	// Iterations breakdown when compaction is used
	Iterations []UsageIteration `json:"iterations,omitempty"`
	// OutputTokensDetails carries thinking_tokens (reasoning share of output).
	OutputTokensDetails *anthropicOutputTokensDetails `json:"output_tokens_details,omitempty"`
	// Raw is the usage object exactly as the API sent it (TS passes
	// response.usage through as Usage.raw and providerMetadata.anthropic.usage,
	// including fields such as service_tier, server_tool_use, cache_creation
	// and inference_geo). Nil when the usage was built without a wire object.
	Raw map[string]interface{} `json:"-"`
}

// UnmarshalJSON decodes the typed fields and keeps the full wire object in Raw.
func (u *anthropicUsage) UnmarshalJSON(data []byte) error {
	type plain anthropicUsage
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*u = anthropicUsage(p)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err == nil {
		stripUsageRawToSchema(raw)
		u.Raw = raw
	}
	return nil
}

// stripUsageRawToSchema drops the keys TS's zod schema strips from the raw
// usage: the usage object itself is loose, but each iteration and
// output_tokens_details are plain z.object schemas.
func stripUsageRawToSchema(raw map[string]interface{}) {
	keep := func(m map[string]interface{}, keys ...string) map[string]interface{} {
		out := make(map[string]interface{}, len(keys))
		for _, k := range keys {
			if v, ok := m[k]; ok {
				out[k] = v
			}
		}
		return out
	}
	if iters, ok := raw["iterations"].([]interface{}); ok {
		for i, it := range iters {
			if m, ok := it.(map[string]interface{}); ok {
				iters[i] = keep(m, "type", "model", "input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")
			}
		}
	}
	if d, ok := raw["output_tokens_details"].(map[string]interface{}); ok {
		raw["output_tokens_details"] = keep(d, "thinking_tokens")
	}
}

type anthropicOutputTokensDetails struct {
	ThinkingTokens *int `json:"thinking_tokens,omitempty"`
}

// UsageIteration represents a single iteration in the usage breakdown
// When compaction occurs, the API returns an iterations array showing
// usage for each sampling iteration (compaction + message).
type UsageIteration struct {
	Type                     string `json:"type"`                                  // "compaction", "message", or fallback iteration type
	Model                    string `json:"model,omitempty"`                       // Model used for this iteration, when provided
	InputTokens              int    `json:"input_tokens"`                          // Input tokens for this iteration
	OutputTokens             int    `json:"output_tokens"`                         // Output tokens for this iteration
	CacheCreationInputTokens int    `json:"cache_creation_input_tokens,omitempty"` // Cache write tokens for this iteration
	CacheReadInputTokens     int    `json:"cache_read_input_tokens,omitempty"`     // Cache read tokens for this iteration
}

// mcpToolCallInfo records an mcp_tool_use call's toolName and providerMetadata
// so a later mcp_tool_result block for the same tool_use_id can resolve the
// same values (TS mcpToolCalls[part.id]).
type mcpToolCallInfo struct {
	toolName         string
	providerMetadata map[string]interface{}
}

// anthropicMCPToolUseMetadata builds the {"anthropic":{"type":"mcp-tool-use",
// "serverName":...}} providerMetadata shared by an mcp_tool_use call and its
// paired mcp_tool_result.
func anthropicMCPToolUseMetadata(serverName string) map[string]interface{} {
	return map[string]interface{}{
		"anthropic": map[string]interface{}{
			"type":       "mcp-tool-use",
			"serverName": serverName,
		},
	}
}

// anthropicContent represents content in an Anthropic response
type anthropicContent struct {
	Type  string                 `json:"type"` // "text", "tool_use", "thinking", "redacted_thinking", "*_tool_result"
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	From  map[string]interface{} `json:"from,omitempty"`
	To    map[string]interface{} `json:"to,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
	// RawInput is the wire form of Input (see anthropicContent.UnmarshalJSON).
	RawInput  json.RawMessage `json:"-"`
	Thinking  string          `json:"thinking,omitempty"`  // For "thinking" type
	Signature string          `json:"signature,omitempty"` // For "thinking" type
	Data      string          `json:"data,omitempty"`      // For "redacted_thinking" type
	// Citations holds the raw citation objects attached to a "text" content
	// block (page_location, char_location, content_block_location,
	// search_result_location, web_search_result_location, ...). Kept as raw
	// maps so unknown/newer citation shapes round-trip untouched into
	// providerMetadata.anthropic.citations (TS keeps the validated-but-typed
	// object as-is).
	Citations []map[string]interface{} `json:"citations,omitempty"`
	// ToolsetName is set on tool_use blocks of toolset members.
	ToolsetName string `json:"toolset_name,omitempty"`
	// Caller identifies the caller of a tool_use / server_tool_use block.
	Caller map[string]interface{} `json:"caller,omitempty"`
	// ServerName is set on mcp_tool_use blocks.
	ServerName string `json:"server_name,omitempty"`
	// Deferred provider tool result fields (web_search_tool_result, code_execution_tool_result, etc.)
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
	// FileID is set on "container_upload" blocks (the uploaded file made
	// available in the code execution container).
	FileID string `json:"file_id,omitempty"`
}

// streamContentBlock tracks an in-flight content block across SSE events.
// A block is opened by content_block_start and closed by content_block_stop.
type streamContentBlock struct {
	blockType        string          // "text", "tool-call", "reasoning"
	toolCallID       string          // for tool-call blocks (content_block.id)
	toolName         string          // for tool-call blocks (display name emitted in chunk)
	providerToolName string          // original Anthropic provider name (e.g. "bash_code_execution")
	inputBuf         strings.Builder // accumulates input_json_delta fragments
	firstDelta       bool            // true until the first input_json_delta is consumed
	// isCustomTool is true for user-defined function tools (type: tool_use).
	// false for provider-executed tools (type: server_tool_use).
	// tool-input-start/delta/end stream events are only emitted for custom tools.
	isCustomTool bool
	// providerToolInputType is injected as {"type": ...} into the first input
	// delta of code execution server tools (TS providerToolInputType).
	providerToolInputType string
	providerExecuted      bool
	dynamic               bool
	// toolsetName/memberName are set for toolset member calls; their input is
	// emitted once, with the member injected as the action, at block stop.
	toolsetName string
	memberName  string
	caller      map[string]interface{}
	// citations accumulates web_search_result_location citations observed via
	// citations_delta events for "text" (and json-response-tool/compaction,
	// which are also surfaced as text) blocks, emitted on text-end
	// providerMetadata.anthropic.citations (TS contentBlock.citations).
	citations []map[string]interface{}
}

// isTextLikeBlock reports whether a streamContentBlock's blockType is one of
// the three block kinds that stream as ChunkTypeText content and therefore
// get text-start/text-end boundary chunks: plain text, on-demand compaction,
// and the synthetic json-response-tool block (jsonTool structured output
// mode). Matches TS's `contentBlocks[value.index] = {type: 'text', ...}`
// assignment for all three content_block_start cases.
func isTextLikeBlock(blockType string) bool {
	return blockType == "text" || blockType == "compaction" || blockType == "json-response-tool"
}

// anthropicStream implements provider.TextStream for Anthropic streaming
type anthropicStream struct {
	reader io.ReadCloser
	parser *streaming.SSEParser
	err    error
	// Input token counts captured from the message_start event.
	// These are combined with output tokens when emitting the finish chunk.
	inputTokens      int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	usage            anthropicUsage
	// includeRawChunks emits a ChunkTypeRaw chunk (the parsed SSE payload)
	// ahead of everything an event produces, like TS options.includeRawChunks.
	includeRawChunks bool
	rawEmitted       bool
	// heldEvent is an event whose raw chunk was just returned; it is
	// processed on the next Next call.
	heldEvent *streaming.SSEEvent
	// In-flight content blocks, keyed by SSE index.
	// Populated by content_block_start; removed on content_block_stop.
	contentBlocks map[int]*streamContentBlock
	// pending holds chunks assembled outside the normal one-event-per-call
	// flow (e.g. pre-populated tool calls from message_start.message.content).
	// They are drained before the next SSE event is read.
	pending []*provider.StreamChunk
	// container info parsed from message_start/message_delta events.
	// Present when the model used or created a container during the request.
	container *anthropicContainerResponse
	// stop metadata parsed from message_delta for finish provider metadata.
	stopSequence string
	stopDetails  *anthropicStopDetails
	// usesJsonResponseTool is true when the request was built with the synthetic
	// 'json' tool (jsonTool structured output mode). Text delta events are
	// suppressed and json tool input_json_delta events are emitted as text chunks.
	usesJsonResponseTool bool
	// isJsonResponseFromTool is set when a tool_use{name:"json"} content block
	// is opened in jsonTool mode. Used to map stop_reason="tool_use" to "stop".
	isJsonResponseFromTool bool
	// serverToolCallNames maps tool_use_id → tool name for provider-executed tools
	// (server_tool_use and mcp_tool_use blocks). Used to look up the tool name when
	// the corresponding *_tool_result block arrives (potentially in a later step).
	serverToolCallNames map[string]string
	toolNameMap         map[string]string
	// mcpToolCalls maps mcp_tool_use tool_use_id -> its toolName/providerMetadata
	// (TS mcpToolCalls[part.id]), so the paired mcp_tool_result can resolve the
	// same toolName, set dynamic:true, and reuse the same providerMetadata.
	mcpToolCalls map[string]mcpToolCallInfo

	// serverToolProviderNames maps a server_tool_use id to its provider tool
	// name (TS serverToolCalls), so a tool_search_tool_result resolves which
	// search variant produced it.
	serverToolProviderNames map[string]string

	// markCodeExecutionDynamic marks code_execution calls dynamic (see
	// HasDynamicFilteringWebToolWithoutCodeExecution).
	markCodeExecutionDynamic bool
	// providerOptionsName / usedCustomProviderKey mirror preparedRequest's
	// fields of the same name; finalizeFinish uses them to duplicate the
	// finish chunk's providerMetadata under providerOptionsName (TS
	// doStream's message_stop handler).
	providerOptionsName   string
	usedCustomProviderKey bool
	// isMessageOpen / activeMessageID detect spliced streams (a second
	// message_start while a message is still open).
	isMessageOpen   bool
	activeMessageID string
	// spliced is set once a spliced-stream error chunk has been emitted (TS
	// hasInvalidMessageSequence). Once true, every remaining SSE event is
	// discarded (never surfaced as a chunk, and no finish chunk is
	// synthesized) until the underlying stream ends.
	spliced bool

	// responseHeaders / responseTimestamp are repeated on the response-metadata
	// chunk emitted at message_start (see the message_start case).
	responseHeaders   map[string]string
	responseTimestamp time.Time
	// inputTransformations / safeguardResults are reported in the finish
	// provider metadata.
	inputTransformations interface{}
	safeguardResults     interface{}
	// finish holds the finish chunk assembled from message_delta; it is
	// emitted on message_stop (or at end of stream).
	finish       *provider.StreamChunk
	finishIssued bool
	// curFinishReason / curRawFinishReason mirror TS's stream-wide
	// finishReason variable: set by message_start (stop_reason) and
	// message_delta, kept across messages, and used by a message_stop that had
	// no message_delta of its own.
	curFinishReason    types.FinishReason
	curRawFinishReason string

	// requestBody is the raw request body that opened this stream, exposed
	// via RequestBody() (provider.StreamRequestBody, hand-off: "stream
	// request body field").
	requestBody interface{}

	// citationDocuments maps citation document_index -> title/filename/mediaType,
	// extracted from citation-enabled file parts in the request prompt (TS
	// extractCitationDocuments). Set by DoStream before the first Next() call.
	citationDocuments []citationDocument
	// hasWebSearchResults is set once a web_search_tool_result with retrieved
	// results is seen; fallbackWebCitations holds web_search_result_location
	// citations that only become sources at the end of the stream when no
	// search results were retrieved (TS #21797).
	hasWebSearchResults  bool
	fallbackWebCitations []map[string]interface{}

	// generateID creates source ids (Config.GenerateID); nil uses a random id.
	generateID func() string
}

func (s *anthropicStream) newID() string {
	if s.generateID != nil {
		return s.generateID()
	}
	return anthropicGenerateID()
}

// newAnthropicStream creates a new Anthropic stream without the leading
// stream-start chunk (used by tests).
// usesJsonResponseTool must match the req.usesJSONResponseTool value DoStream computed via prepareRequest.
func newAnthropicStream(reader io.ReadCloser, usesJsonResponseTool bool, toolsOpt ...[]types.Tool) *anthropicStream {
	var tools []types.Tool
	if len(toolsOpt) > 0 {
		tools = toolsOpt[0]
	}
	// Test-facing constructor: DoStream always goes through
	// newAnthropicStreamWithWarnings and so always emits stream-start; this one
	// starts directly at the first SSE event.
	s := newAnthropicStreamWithWarnings(reader, usesJsonResponseTool, tools, nil)
	s.pending = nil
	return s
}

func newAnthropicStreamWithWarnings(reader io.ReadCloser, usesJsonResponseTool bool, tools []types.Tool, warnings []types.Warning) *anthropicStream {
	var pending []*provider.StreamChunk
	// TS always enqueues stream-start (warnings may be empty).
	pending = append(pending, &provider.StreamChunk{
		Type:     provider.ChunkTypeStreamStart,
		Warnings: warnings,
	})
	return &anthropicStream{
		reader:                  reader,
		parser:                  streaming.NewSSEParser(reader),
		contentBlocks:           make(map[int]*streamContentBlock),
		pending:                 pending,
		serverToolCallNames:     make(map[string]string),
		serverToolProviderNames: make(map[string]string),
		mcpToolCalls:            make(map[string]mcpToolCallInfo),
		usesJsonResponseTool:    usesJsonResponseTool,
		toolNameMap:             anthropicProviderToolNameMap(tools),
	}
}

// Close implements io.Closer
func (s *anthropicStream) Close() error {
	return s.reader.Close()
}

// RequestBody implements provider.StreamRequestBody, exposing the raw
// request body that opened this stream in types.StepRequest.Body for
// streaming calls, matching TS doStream()'s {request: {body}} (hand-off:
// "stream request body field").
func (s *anthropicStream) RequestBody() interface{} {
	return s.requestBody
}

// Next returns the next chunk in the stream
func (s *anthropicStream) Next() (*provider.StreamChunk, error) {
	for {
		if s.err != nil {
			return nil, s.err
		}

		// Drain any chunks buffered by a previous event (e.g. pre-populated tool
		// calls from message_start) before reading the next SSE event.
		if len(s.pending) > 0 {
			chunk := s.pending[0]
			s.pending = s.pending[1:]
			return chunk, nil
		}

		// Get next SSE event
		var event *streaming.SSEEvent
		var err error
		if s.heldEvent != nil {
			event, s.heldEvent = s.heldEvent, nil
		} else {
			event, err = s.parser.Next()
		}
		if err != nil {
			if s.spliced {
				// A spliced stream ends without a finish chunk: TS never
				// re-enqueues after hasInvalidMessageSequence is set, and the
				// ReadableStream simply closes once the underlying byte stream
				// ends (no synthesized finish part).
				s.err = io.EOF
				return nil, io.EOF
			}
			// TS only finishes on message_stop (its stream has no flush), so a
			// body that ends after message_delta ends without a finish part.
			s.err = err
			return nil, err
		}

		// TS's JSON event-stream parser drops the "[DONE]" sentinel.
		if s.heldEvent == nil && event.Data == "[DONE]" {
			continue
		}

		// TS ignores SSE event names and dispatches on the payload's `type`.
		// Transports that rebuild the stream without `event:` lines (Bedrock's
		// event-stream to SSE adapter) rely on this.
		if event.Event == "" {
			event.Event = sseEventTypeFromPayload(event.Data)
		}

		// Once a spliced-stream error chunk has been emitted, every remaining
		// event is silently discarded (TS hasInvalidMessageSequence guard).
		if s.spliced {
			continue
		}

		// TS emits the raw chunk (even for pings) before handling the event.
		if s.includeRawChunks && s.heldEvent == nil && !s.rawEmitted {
			s.heldEvent = event
			s.rawEmitted = true
			return &provider.StreamChunk{Type: provider.ChunkTypeRaw, Raw: rawSSEPayload(event.Data)}, nil
		}
		s.rawEmitted = false

		// TS createEventSourceResponseHandler yields {success:false} for a
		// frame that is not valid JSON, and doStream turns it into an `error`
		// part and keeps reading (it never ends the stream).
		if strings.TrimSpace(event.Data) != "" && !json.Valid([]byte(event.Data)) {
			var probe interface{}
			parseErr := providererrors.NewJSONParseError(event.Data, json.Unmarshal([]byte(event.Data), &probe))
			return &provider.StreamChunk{
				Type: provider.ChunkTypeError,
				Text: parseErr.Error(),
				Err:  parseErr,
			}, nil
		}

		// TS then validates the parsed value against anthropicChunkSchema; a
		// mismatch is a TypeValidationError `error` part and the stream goes
		// on. Empty data frames carry no payload and are left to the switch.
		if strings.TrimSpace(event.Data) != "" {
			if message := anthropicChunkValidationMessage(event.Data); message != "" {
				return &provider.StreamChunk{
					Type: provider.ChunkTypeError,
					Text: message,
					Err:  providererrors.NewValidationErrorWithContext(compactJSON(event.Data), validationIssueText(message), nil, &providererrors.ValidationContext{}),
				}, nil
			}
			// TS dispatches on the payload `type`, not the SSE event name.
			event.Event = anthropicChunkType(event.Data)
		}

		// Anthropic uses different event types
		switch event.Event {
		case "ping":
			// No-op: keep alive signal, get next chunk
			continue

		case "content_block_start":
			// Parse the opening of a content block. Store tool_use, server_tool_use,
			// and thinking blocks in s.contentBlocks for later accumulation/emission.
			var start struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type        string                 `json:"type"`
					ID          string                 `json:"id"`
					Name        string                 `json:"name"`
					Input       json.RawMessage        `json:"input"` // non-empty for programmatic deferred tool calls
					ToolsetName string                 `json:"toolset_name"`
					Caller      map[string]interface{} `json:"caller"`
					// mcp_tool_use fields
					ServerName string `json:"server_name"`
					// mcp_tool_result / web_search_tool_result / web_fetch_tool_result fields
					ToolUseID string          `json:"tool_use_id"`
					IsError   *bool           `json:"is_error"`
					Content   json.RawMessage `json:"content"`
					// compaction fields
					Signature string `json:"signature"`
					// redacted_thinking field
					Data string `json:"data"`
					// fallback fields
					From map[string]interface{} `json:"from"`
					To   map[string]interface{} `json:"to"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(event.Data), &start); err != nil {
				// Malformed start event: skip gracefully
				continue
			}
			switch start.ContentBlock.Type {
			case "tool_use":
				// When jsonTool mode is active and the block is the synthetic json
				// tool, treat it as a text block: input_json_delta events will be
				// emitted as ChunkTypeText rather than accumulated for a tool call.
				if s.usesJsonResponseTool && start.ContentBlock.Name == "json" {
					s.isJsonResponseFromTool = true
					s.contentBlocks[start.Index] = &streamContentBlock{
						blockType: "json-response-tool",
					}
					return &provider.StreamChunk{
						Type: provider.ChunkTypeTextStart,
						ID:   strconv.Itoa(start.Index),
					}, nil
				}

				// Some deferred (programmatic) tool calls carry their full input
				// directly in content_block_start rather than via input_json_delta.
				// Serialize it as the initial buffer content so content_block_stop
				// emits the correct arguments even with no following deltas.
				var initialInput string
				if startInput := jsonorder.Input(start.ContentBlock.Input, nil); startInput.Len() > 0 {
					initialInput = startInput.String()
				}
				block := &streamContentBlock{
					blockType:    "tool-call",
					toolCallID:   start.ContentBlock.ID,
					toolName:     mapAnthropicToolName(start.ContentBlock.Name, s.toolNameMap),
					firstDelta:   initialInput == "", // expect deltas only when no initial input
					isCustomTool: true,               // user-defined function tool
					caller:       anthropicCallerInfo(start.ContentBlock.Caller),
				}
				if start.ContentBlock.ToolsetName != "" {
					block.toolName = mapAnthropicToolName(start.ContentBlock.ToolsetName, s.toolNameMap)
					block.toolsetName = start.ContentBlock.ToolsetName
					block.memberName = start.ContentBlock.Name
					block.firstDelta = true
				}
				if initialInput != "" {
					block.inputBuf.WriteString(initialInput)
				}
				s.contentBlocks[start.Index] = block

				// Emit tool-input-start so consumers can observe when tool input
				// streaming begins. This enables fine-grained tool streaming UI.
				return &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:       block.toolCallID,
						ToolName: block.toolName,
					},
				}, nil

			case "thinking":
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "reasoning",
				}
				// TS: reasoning-start {id: String(index)}.
				return &provider.StreamChunk{
					Type: provider.ChunkTypeReasoningStart,
					ID:   strconv.Itoa(start.Index),
				}, nil

			case "redacted_thinking":
				// TS: reasoning-start carrying providerMetadata.anthropic.redactedData,
				// which must be replayed verbatim in the next turn's history.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "reasoning",
				}
				meta, _ := json.Marshal(map[string]interface{}{
					"anthropic": map[string]interface{}{"redactedData": start.ContentBlock.Data},
				})
				return &provider.StreamChunk{
					Type:             provider.ChunkTypeReasoningStart,
					ID:               strconv.Itoa(start.Index),
					ProviderMetadata: meta,
				}, nil

			case "server_tool_use":
				// Provider-executed tools: web_fetch, web_search, code_execution,
				// bash_code_execution, text_editor_code_execution.
				// bash/text_editor variants are normalized to "code_execution" for
				// the emitted tool name, but the original name is stored so the
				// first-delta type prefix can be injected (see input_json_delta).
				name := start.ContentBlock.Name
				providerToolName := name
				inputType := ""
				switch name {
				case "bash_code_execution", "text_editor_code_execution":
					providerToolName = "code_execution"
					inputType = name
				case "code_execution":
					inputType = "programmatic-tool-call"
				case "web_fetch", "web_search", "tool_search_tool_regex", "tool_search_tool_bm25", "advisor":
				default:
					// Unknown server tools are ignored (TS emits nothing for them).
					s.contentBlocks[start.Index] = &streamContentBlock{blockType: "unknown-server-tool"}
					continue
				}
				toolName := mapAnthropicToolName(providerToolName, s.toolNameMap)
				// Track tool call ID → tool name so the corresponding *_tool_result
				// block (deferred result) can resolve the tool name.
				s.serverToolCallNames[start.ContentBlock.ID] = toolName
				s.serverToolProviderNames[start.ContentBlock.ID] = name
				block := &streamContentBlock{
					blockType:             "tool-call",
					toolCallID:            start.ContentBlock.ID,
					toolName:              toolName,
					providerToolName:      providerToolName,
					providerToolInputType: inputType,
					providerExecuted:      true,
					dynamic:               s.markCodeExecutionDynamic && providerToolName == "code_execution",
					firstDelta:            true,
					caller:                anthropicCallerInfo(start.ContentBlock.Caller),
				}
				switch name {
				case "advisor":
					block.inputBuf.WriteString("{}")
				case "tool_search_tool_regex", "tool_search_tool_bm25":
				default:
					// Dynamic web tools provide their input here; other tools
					// stream it via deltas, so only non-empty input is used.
					if startInput := jsonorder.Input(start.ContentBlock.Input, nil); startInput.Len() > 0 {
						block.inputBuf.WriteString(startInput.String())
						block.firstDelta = false
					}
				}
				// TS streams provider-executed tool input like any other tool
				// call: tool-input-start (providerExecuted), tool-input-delta
				// per input_json_delta, tool-input-end, then tool-call.
				block.isCustomTool = true
				s.contentBlocks[start.Index] = block
				return &provider.StreamChunk{
					Type: provider.ChunkTypeToolInputStart,
					ToolCall: &types.ToolCall{
						ID:               block.toolCallID,
						ToolName:         block.toolName,
						ProviderExecuted: true,
						Dynamic:          block.dynamic,
					},
				}, nil

			case "mcp_tool_use":
				// MCP tool calls have their full input pre-populated in content_block_start.
				// Emit immediately as a tool call chunk (no input_json_delta accumulation needed).
				input := map[string]interface{}{}
				if len(start.ContentBlock.Input) > 0 {
					_ = json.Unmarshal(start.ContentBlock.Input, &input)
					if input == nil {
						input = map[string]interface{}{}
					}
				}
				mcpInput := jsonorder.Input(start.ContentBlock.Input, nil).String()
				// Track tool call ID → tool name for mcp_tool_result lookup, and the
				// full toolName/providerMetadata pair so the paired mcp_tool_result
				// can resolve the same values (TS mcpToolCalls[part.id]).
				s.serverToolCallNames[start.ContentBlock.ID] = start.ContentBlock.Name
				mcpMeta := anthropicMCPToolUseMetadata(start.ContentBlock.ServerName)
				s.mcpToolCalls[start.ContentBlock.ID] = mcpToolCallInfo{
					toolName:         start.ContentBlock.Name,
					providerMetadata: mcpMeta,
				}
				// Track as a non-buffering block so content_block_stop is a clean no-op.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "mcp-tool-use",
				}
				return &provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               start.ContentBlock.ID,
						ToolName:         start.ContentBlock.Name,
						Arguments:        input,
						RawArguments:     mcpInput,
						ProviderExecuted: true,
						Dynamic:          true,
						ProviderMetadata: mcpMeta,
					},
				}, nil

			case "mcp_tool_result":
				// MCP tool results arrive in content_block_start. Emit as ChunkTypeToolResult
				// so the SDK's pendingDeferredToolCalls map is cleared. Resolve
				// toolName/providerMetadata from the paired mcp_tool_use call (TS:
				// `mcpToolCalls[part.tool_use_id].toolName` / `.providerMetadata`) and
				// mark the result dynamic like TS.
				call := s.mcpToolCalls[start.ContentBlock.ToolUseID]
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         call.toolName,
					Dynamic:          true,
					ProviderMetadata: call.providerMetadata,
				}
				var mcpResultContent interface{}
				if len(start.ContentBlock.Content) > 0 {
					json.Unmarshal(start.ContentBlock.Content, &mcpResultContent) //nolint:errcheck
				}
				// TS: isError: part.is_error, result: part.content (both kept).
				tr.IsError = start.ContentBlock.IsError
				tr.Result = mcpResultContent
				if isTrue(start.ContentBlock.IsError) {
					tr.Error = fmt.Errorf("mcp tool error: %v", mcpResultContent)
				}
				// Track so content_block_stop is a clean no-op.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "mcp-tool-result",
				}
				return &provider.StreamChunk{
					Type:       provider.ChunkTypeToolResult,
					ToolResult: tr,
				}, nil

			case "code_execution_tool_result", "bash_code_execution_tool_result",
				"text_editor_code_execution_tool_result", "tool_search_tool_result", "advisor_tool_result":
				// Provider-executed results arrive fully formed in
				// content_block_start (TS emits a tool-result for each).
				s.contentBlocks[start.Index] = &streamContentBlock{blockType: "deferred-tool-result"}
				dr, ok := convertDeferredToolResult(start.ContentBlock.Type, start.ContentBlock.Content, s.toolNameMap, s.serverToolProviderNames[start.ContentBlock.ToolUseID])
				if !ok {
					continue
				}
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         dr.ToolName,
					Result:           dr.Result,
					ProviderExecuted: start.ContentBlock.Type == "advisor_tool_result",
				}
				if dr.IsError {
					tr.Error = fmt.Errorf("%s", dr.ErrorCode)
					tr.IsError = trueFlag()
				}
				return &provider.StreamChunk{
					Type:       provider.ChunkTypeToolResult,
					ToolResult: tr,
				}, nil

			case "text":
				// When a json response tool is used, the tool call is returned as
				// text, so real "text" content blocks are ignored entirely (TS: `if
				// (usesJsonResponseTool) { return; }`).
				if s.usesJsonResponseTool {
					continue
				}
				s.contentBlocks[start.Index] = &streamContentBlock{blockType: "text"}
				return &provider.StreamChunk{
					Type: provider.ChunkTypeTextStart,
					ID:   strconv.Itoa(start.Index),
				}, nil

			case "compaction":
				// Compaction blocks are surfaced as text chunks whose text-start
				// carries providerMetadata.anthropic = {type: 'compaction', signature?}
				// (TS content_block_start "compaction" case).
				s.contentBlocks[start.Index] = &streamContentBlock{blockType: "compaction"}
				meta := map[string]interface{}{"type": "compaction"}
				if start.ContentBlock.Signature != "" {
					meta["signature"] = start.ContentBlock.Signature
				}
				metaJSON, _ := json.Marshal(map[string]interface{}{"anthropic": meta})
				textStart := &provider.StreamChunk{
					Type:             provider.ChunkTypeTextStart,
					ID:               strconv.Itoa(start.Index),
					ProviderMetadata: metaJSON,
				}
				// On-demand compaction blocks may arrive fully formed in
				// content_block_start — without any following compaction_delta
				// events — when both signature and content are present.
				if start.ContentBlock.Signature != "" {
					var text string
					if err := json.Unmarshal(start.ContentBlock.Content, &text); err == nil && text != "" {
						s.pending = append(s.pending, &provider.StreamChunk{
							Type: provider.ChunkTypeText,
							ID:   strconv.Itoa(start.Index),
							Text: text,
						})
					}
				}
				return textStart, nil

			case "web_fetch_tool_result":
				// Live-streamed deferred tool result (TS anthropic-language-model.ts
				// content_block_start "web_fetch_tool_result" case, ~line 2226).
				// Resolve toolName from the paired server_tool_use block tracked in
				// s.serverToolCallNames, remap snake_case wire fields to camelCase,
				// and grow citationDocuments so a later page_location/char_location
				// citation can resolve against this fetched document.
				toolName := s.serverToolCallNames[start.ContentBlock.ToolUseID]
				if toolName == "" {
					toolName = mapAnthropicToolName(providerToolResultName(start.ContentBlock.Type), s.toolNameMap)
				}
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         toolName,
					ProviderExecuted: true,
					ProviderMetadata: anthropicCallerMetadata(start.ContentBlock.Caller),
				}
				if errResult, ok := parseAnthropicWebToolResultError(start.ContentBlock.Content, "web_fetch_tool_result_error", isTrue(start.ContentBlock.IsError)); ok {
					tr.Result = errResult
					tr.Error = fmt.Errorf("%v", errResult["errorCode"])
					tr.IsError = trueFlag()
				} else if len(start.ContentBlock.Content) > 0 {
					tr.Result = convertWebFetchToolResult(start.ContentBlock.Content)
					if doc, ok := extractWebFetchCitationDocument(start.ContentBlock.Content); ok {
						s.citationDocuments = append(s.citationDocuments, doc)
					}
				}
				s.contentBlocks[start.Index] = &streamContentBlock{blockType: "web-fetch-tool-result"}
				return &provider.StreamChunk{
					Type:       provider.ChunkTypeToolResult,
					ToolResult: tr,
				}, nil

			case "web_search_tool_result":
				// Live-streamed deferred tool result (TS anthropic-language-model.ts
				// content_block_start "web_search_tool_result" case, ~line 2272).
				// Emits the tool-result chunk followed by a source chunk for each
				// search result (TS enqueues 'source' parts after the 'tool-result').
				toolName := s.serverToolCallNames[start.ContentBlock.ToolUseID]
				if toolName == "" {
					toolName = mapAnthropicToolName(providerToolResultName(start.ContentBlock.Type), s.toolNameMap)
				}
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         toolName,
					ProviderExecuted: true,
					ProviderMetadata: anthropicCallerMetadata(start.ContentBlock.Caller),
				}
				var webSearchSources []types.SourceContent
				if errResult, ok := parseAnthropicWebToolResultError(start.ContentBlock.Content, "web_search_tool_result_error", isTrue(start.ContentBlock.IsError)); ok {
					tr.Result = errResult
					tr.Error = fmt.Errorf("%v", errResult["errorCode"])
					tr.IsError = trueFlag()
				} else if len(start.ContentBlock.Content) > 0 {
					mapped, sources := convertWebSearchToolResult(start.ContentBlock.Content, s.newID)
					tr.Result = mapped
					webSearchSources = sources
					if len(sources) > 0 {
						s.hasWebSearchResults = true
					}
				}
				s.contentBlocks[start.Index] = &streamContentBlock{blockType: "web-search-tool-result"}
				for _, src := range webSearchSources {
					s2 := src
					s.pending = append(s.pending, &provider.StreamChunk{
						Type:          provider.ChunkTypeSource,
						SourceContent: &s2,
					})
				}
				return &provider.StreamChunk{
					Type:       provider.ChunkTypeToolResult,
					ToolResult: tr,
				}, nil

			case "fallback":
				// Server-side model-hop marker, streamed as a single
				// content_block_start with no following deltas before its
				// content_block_stop. Preserved as an anthropic.fallback
				// custom chunk with from/to metadata (TS commit a587f554f7,
				// #21736; previously dropped entirely) so the next request's
				// prompt conversion can round-trip the fallback boundary
				// between signed thinking blocks. No contentBlocks entry is
				// needed: content_block_stop is already a no-op for an
				// unregistered index.
				metadata, _ := json.Marshal(map[string]interface{}{
					"anthropic": map[string]interface{}{
						"type": "fallback",
						"from": start.ContentBlock.From,
						"to":   start.ContentBlock.To,
					},
				})
				return &provider.StreamChunk{
					Type: provider.ChunkTypeCustom,
					CustomContent: &types.CustomContent{
						Kind:             "anthropic.fallback",
						ProviderMetadata: metadata,
					},
				}, nil

			default:
				// Any unknown types: record so content_block_stop is always a
				// clean no-op.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: start.ContentBlock.Type,
				}
			}
			continue

		case "message_start":
			// Capture input/cache tokens for inclusion in the final finish chunk.
			// These are only available here — the message_delta only has output_tokens.
			//
			// Also handle pre-populated tool_use content blocks (programmatic /
			// deferred tool calling). In this pattern the tool call input arrives
			// in message_start.message.content rather than via content_block_delta
			// events, so we emit ChunkTypeToolCall for each such block immediately.
			var msg struct {
				Message struct {
					ID                   string          `json:"id"`
					Model                string          `json:"model"`
					StopReason           string          `json:"stop_reason"`
					InputTransformations json.RawMessage `json:"input_transformations"`
					Usage                struct {
						InputTokens              int              `json:"input_tokens"`
						OutputTokens             int              `json:"output_tokens,omitempty"`
						CacheReadInputTokens     int              `json:"cache_read_input_tokens"`
						CacheCreationInputTokens int              `json:"cache_creation_input_tokens"`
						Iterations               []UsageIteration `json:"iterations,omitempty"`
					} `json:"usage"`
					Container *anthropicContainerResponse `json:"container,omitempty"`
					Content   []anthropicContent          `json:"content"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(event.Data), &msg); err == nil {
				if s.isMessageOpen {
					if s.activeMessageID == msg.Message.ID {
						continue
					}
					// A spliced stream: emit an error chunk in the stream itself
					// (matching TS, which enqueues an 'error' stream part rather
					// than throwing) and discard everything after it. This is
					// not a fatal Go error: the generation already produced
					// output, so the failure must surface the same way other
					// mid-stream provider errors do (see openai/language_model.go
					// for the analogous outputStarted convention).
					s.spliced = true
					text := fmt.Sprintf(
						"Received message_start for message %s while message %s is still open.",
						jsonQuote(msg.Message.ID), jsonQuote(s.activeMessageID))
					// TS carries an InvalidResponseDataError whose data is the
					// offending event; callers match it with errors.As.
					var eventData interface{}
					if json.Unmarshal([]byte(event.Data), &eventData) != nil {
						eventData = event.Data
					}
					return &provider.StreamChunk{
						Type: provider.ChunkTypeError,
						Text: text,
						Err:  providererrors.NewInvalidResponseDataError(eventData, text),
					}, nil
				}
				s.isMessageOpen = true
				s.activeMessageID = msg.Message.ID
				if msg.Message.StopReason != "" {
					// TS: a message_start that already carries a stop_reason
					// sets the stream-wide finish reason.
					s.curFinishReason = mapStopReason(msg.Message.StopReason, s.isJsonResponseFromTool)
					s.curRawFinishReason = msg.Message.StopReason
				}
				// A new message gets its own finish chunk.
				s.finishIssued = false
				s.finish = nil
				// Expose the initial message accounting as an
				// anthropic.message_start custom chunk (TS #21979). The raw
				// usage object is passed through unchanged.
				//
				// TS first enqueues response-metadata {id, modelId} from the
				// message itself, so streamText's response.id / response.modelId
				// reflect the message rather than the requested model. The
				// headers and timestamp ride along because consumers that keep
				// only the last response-metadata chunk would otherwise lose them.
				s.pending = append(s.pending, &provider.StreamChunk{
					Type: provider.ChunkTypeResponseMetadata,
					ResponseMetadata: &provider.ResponseMetadata{
						ID:        msg.Message.ID,
						ModelID:   msg.Message.Model,
						Headers:   s.responseHeaders,
						Timestamp: s.responseTimestamp,
					},
				})
				s.pending = append(s.pending, messageStartChunk(event.Data))
				if v := rawJSONValue(msg.Message.InputTransformations); v != nil {
					s.inputTransformations = v
				}
				s.inputTokens = int64(msg.Message.Usage.InputTokens)
				s.cacheReadTokens = int64(msg.Message.Usage.CacheReadInputTokens)
				s.cacheWriteTokens = int64(msg.Message.Usage.CacheCreationInputTokens)
				// TS: rawUsage = {...message_start.message.usage}.
				var startRaw struct {
					Message struct {
						Usage map[string]interface{} `json:"usage"`
					} `json:"message"`
				}
				s.usage.Raw = nil
				if json.Unmarshal([]byte(event.Data), &startRaw) == nil && startRaw.Message.Usage != nil {
					s.usage.Raw = startRaw.Message.Usage
				}
				s.usage.InputTokens = msg.Message.Usage.InputTokens
				// TS leaves output_tokens alone here: it carries over from the
				// previous message_delta (and is 0 before the first one).
				s.usage.CacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
				s.usage.CacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				// TS message_start does not read usage.iterations; only
				// message_delta sets them.

				// Capture container info (present when container was used/created).
				// In message_start it contains id and expires_at but no skills.
				if msg.Message.Container != nil {
					// TS: {expiresAt, id, skills: null} -- message_start never
					// reports skills.
					c := *msg.Message.Container
					c.Skills = nil
					s.container = &c
				}

				// Programmatic tool calling: pre-populated tool_use blocks. TS
				// handles tool_use only here; other pre-populated blocks (mcp,
				// web search, results) are ignored.
				for _, part := range msg.Message.Content {
					if part.Type != "tool_use" {
						continue
					}
					args := part.Input
					if args == nil {
						args = map[string]interface{}{}
					}
					inputJSON := inputText(part.RawInput, args)
					s.pending = append(s.pending,
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputStart, ToolCall: &types.ToolCall{ID: part.ID, ToolName: part.Name}},
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: part.ID, Text: inputJSON},
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: part.ID}},
						&provider.StreamChunk{
							Type: provider.ChunkTypeToolCall,
							ToolCall: &types.ToolCall{
								ID:               part.ID,
								ToolName:         part.Name,
								Arguments:        args,
								RawArguments:     inputJSON,
								ProviderMetadata: anthropicCallerMetadata(part.Caller),
							},
						})
				}
			}
			continue

		case "content_block_delta":
			// Parse delta — content is *string to allow null in compaction_delta events.
			// PartialJSON accumulates tool call argument fragments.
			// Thinking carries reasoning text deltas.
			var delta struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Delta struct {
					Type        string                 `json:"type"`
					Text        string                 `json:"text"`
					Content     *string                `json:"content"`      // nullable in compaction_delta
					PartialJSON string                 `json:"partial_json"` // in input_json_delta
					Thinking    string                 `json:"thinking"`     // in thinking_delta
					Signature   string                 `json:"signature"`    // in signature_delta
					Citation    map[string]interface{} `json:"citation"`     // in citations_delta
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
				return nil, fmt.Errorf("failed to parse content delta: %w", err)
			}

			switch delta.Delta.Type {
			case "text_delta":
				// When jsonTool mode is active the model should not emit plain text —
				// the response comes via the synthetic json tool. Suppress any text
				// deltas to match the TypeScript SDK's usesJsonResponseTool guard.
				if s.usesJsonResponseTool {
					continue
				}
				return &provider.StreamChunk{
					Type: provider.ChunkTypeText,
					ID:   strconv.Itoa(delta.Index),
					Text: delta.Delta.Text,
				}, nil

			case "input_json_delta":
				// Skip empty deltas — the TS SDK does the same to allow replacing
				// the first character in code-execution tools without double-writing.
				if delta.Delta.PartialJSON == "" {
					continue
				}
				block := s.contentBlocks[delta.Index]
				if block == nil {
					continue
				}
				// When this delta belongs to the synthetic json tool block, emit it
				// directly as a text chunk rather than accumulating for a tool call.
				if block.blockType == "json-response-tool" {
					return &provider.StreamChunk{
						Type: provider.ChunkTypeText,
						ID:   strconv.Itoa(delta.Index),
						Text: delta.Delta.PartialJSON,
					}, nil
				}
				partialJSON := delta.Delta.PartialJSON
				// Toolset member input is emitted once the block is complete.
				if block.toolsetName != "" {
					block.inputBuf.WriteString(partialJSON)
					continue
				}
				// Code execution server tools stream raw arguments without a type
				// discriminator. On the first delta, inject the providerToolInputType
				// (TS: `{"type": "<type>",` + delta.substring(1)).
				if block.firstDelta && block.providerToolInputType != "" {
					partialJSON = `{"type": "` + block.providerToolInputType + `",` + partialJSON[1:]
				}
				block.firstDelta = false
				block.inputBuf.WriteString(partialJSON)

				// For custom function tools, emit a tool-input-delta with the raw
				// delta so consumers can display streaming tool input in real time.
				if block.isCustomTool {
					return &provider.StreamChunk{
						Type: provider.ChunkTypeToolInputDelta,
						ID:   block.toolCallID,
						Text: partialJSON,
					}, nil
				}
				continue

			case "thinking_delta":
				// Emit each thinking fragment immediately as a reasoning chunk.
				return &provider.StreamChunk{
					Type:      provider.ChunkTypeReasoning,
					ID:        strconv.Itoa(delta.Index),
					Reasoning: delta.Delta.Thinking,
				}, nil

			case "signature_delta":
				// Thinking block signature: cryptographic attestation required to
				// replay this thinking block in a later turn's history. Only
				// meaningful on "thinking" blocks (TS: `if (blockType ===
				// 'thinking')`); emitted as a reasoning chunk with empty text and
				// the signature in ProviderMetadata, mirroring TS's
				// reasoning-delta with delta: '' (previously dropped entirely,
				// causing unsigned thinking blocks to be replayed and rejected by
				// Anthropic).
				if block := s.contentBlocks[delta.Index]; block != nil && block.blockType == "reasoning" {
					metadata, _ := json.Marshal(map[string]interface{}{
						"anthropic": map[string]interface{}{"signature": delta.Delta.Signature},
					})
					return &provider.StreamChunk{
						Type:             provider.ChunkTypeReasoning,
						ID:               strconv.Itoa(delta.Index),
						ProviderMetadata: metadata,
					}, nil
				}
				continue

			case "compaction_delta":
				// Emit non-null content as text; skip null content.
				if delta.Delta.Content != nil {
					return &provider.StreamChunk{
						Type: provider.ChunkTypeText,
						ID:   strconv.Itoa(delta.Index),
						Text: *delta.Delta.Content,
					}, nil
				}
				continue

			case "citations_delta":
				// Accumulate web_search_result_location citations onto the owning
				// text-like block (surfaced on its text-end providerMetadata), and
				// emit a source chunk for every citation that resolves to one (TS
				// createCitationSource): web_search_result_location always resolves;
				// page_location/char_location resolve against citationDocuments
				// extracted from the request prompt.
				if block := s.contentBlocks[delta.Index]; block != nil && isTextLikeBlock(block.blockType) {
					block.citations = append(block.citations, delta.Delta.Citation)
				}
				if citationType(delta.Delta.Citation) == "web_search_result_location" {
					// Decide at the end of the stream, once it is known whether
					// the search results were retrieved.
					s.fallbackWebCitations = append(s.fallbackWebCitations, delta.Delta.Citation)
					continue
				}
				if src, ok := createCitationSource(delta.Delta.Citation, s.citationDocuments, s.newID); ok {
					return &provider.StreamChunk{
						Type:          provider.ChunkTypeSource,
						SourceContent: &src,
					}, nil
				}
				continue
			}

		case "content_block_stop":
			// A content block has been fully delivered. For tool-call blocks, emit the
			// assembled ChunkTypeToolCall with the complete JSON-parsed arguments.
			// For json-response-tool blocks (jsonTool mode), no extra chunk is emitted
			// because the input was already streamed as individual text chunks via
			// input_json_delta. For all other block types this is a clean no-op.
			var stop struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(event.Data), &stop); err != nil {
				continue
			}
			block := s.contentBlocks[stop.Index]
			delete(s.contentBlocks, stop.Index)

			if block != nil && block.blockType == "tool-call" {
				// Parse the accumulated JSON into ToolCall.Arguments.
				var args map[string]interface{}
				inputStr := block.inputBuf.String()
				invalidInput := ""
				if inputStr != "" {
					if err := json.Unmarshal([]byte(inputStr), &args); err != nil {
						// Malformed or truncated input (e.g. max_tokens hit
						// mid-call): TS emits the tool-call with the raw input
						// text and lets the core mark it invalid / repair it,
						// instead of failing the whole stream.
						args = nil
						invalidInput = inputStr
					}
				}
				if args == nil && invalidInput == "" {
					args = map[string]interface{}{}
				}
				// finalInput is the tool-call input text TS reports: the
				// accumulated wire text, '{}' when empty, with the toolset
				// member / programmatic type injected in wire key order.
				finalInput := inputStr
				if finalInput == "" {
					finalInput = "{}"
				}
				var toolsetDelta *provider.StreamChunk
				if block.toolsetName != "" {
					args = toToolsetMemberInput(block.memberName, args)
					if invalidInput == "" {
						finalInput = jsonorder.Input(json.RawMessage(finalInput), nil).WithLead("action", block.memberName).String()
					}
					toolsetDelta = &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: block.toolCallID, Text: finalInput}
				}
				if block.providerToolName == "code_execution" {
					_, hasCode := args["code"]
					_, hasType := args["type"]
					if hasCode && !hasType {
						withType := map[string]interface{}{"type": "programmatic-tool-call"}
						for k, v := range args {
							withType[k] = v
						}
						args = withType
						if invalidInput == "" {
							finalInput = jsonorder.Input(json.RawMessage(finalInput), nil).WithLead("type", "programmatic-tool-call").String()
						}
					}
				}
				var meta map[string]interface{}
				if block.caller != nil || block.toolsetName != "" {
					am := map[string]interface{}{}
					if block.toolsetName != "" {
						am["toolsetName"] = block.toolsetName
					}
					if block.caller != nil {
						am["caller"] = block.caller
					}
					meta = map[string]interface{}{"anthropic": am}
				}
				toolCallChunk := &provider.StreamChunk{
					Type: provider.ChunkTypeToolCall,
					ToolCall: &types.ToolCall{
						ID:               block.toolCallID,
						ToolName:         block.toolName,
						Arguments:        args,
						RawArguments:     finalInput,
						ProviderExecuted: block.providerExecuted,
						Dynamic:          block.dynamic,
						ProviderMetadata: meta,
					},
				}
				if toolsetDelta != nil {
					s.pending = append(s.pending,
						&provider.StreamChunk{Type: provider.ChunkTypeToolInputEnd, ToolCall: &types.ToolCall{ID: block.toolCallID}},
						toolCallChunk)
					return toolsetDelta, nil
				}
				// For custom function tools, emit tool-input-end first, then the
				// assembled tool-call via the pending queue. This completes the
				// tool-input-start → tool-input-delta(×N) → tool-input-end sequence.
				if block.isCustomTool {
					s.pending = append(s.pending, toolCallChunk)
					return &provider.StreamChunk{
						Type: provider.ChunkTypeToolInputEnd,
						ToolCall: &types.ToolCall{
							ID: block.toolCallID,
						},
					}, nil
				}
				return toolCallChunk, nil
			}
			if block != nil && isTextLikeBlock(block.blockType) {
				// text, compaction, and json-response-tool blocks all close with a
				// text-end carrying the accumulated web-search citations, if any
				// (TS content_block_stop "text" case).
				var meta json.RawMessage
				var citations []types.Citation
				if len(block.citations) > 0 {
					meta = citationsProviderMetadata(block.citations)
					citations = mapAnthropicCitations(block.citations, s.citationDocuments)
				}
				return &provider.StreamChunk{
					Type:             provider.ChunkTypeTextEnd,
					ID:               strconv.Itoa(stop.Index),
					Citations:        citations,
					ProviderMetadata: meta,
				}, nil
			}
			if block != nil && block.blockType == "reasoning" {
				return &provider.StreamChunk{
					Type: provider.ChunkTypeReasoningEnd,
					ID:   strconv.Itoa(stop.Index),
				}, nil
			}
			// unknown — no chunk to emit.
			continue

		case "message_delta":
			// Parse message delta for finish reason, context management, and container.
			var delta struct {
				Delta struct {
					StopReason       string                      `json:"stop_reason"`
					StopSequence     string                      `json:"stop_sequence"`
					StopDetails      *anthropicStopDetails       `json:"stop_details,omitempty"`
					Container        *anthropicContainerResponse `json:"container,omitempty"`
					SafeguardResults json.RawMessage             `json:"safeguard_results,omitempty"`
				} `json:"delta"`
				InputTransformations json.RawMessage `json:"input_transformations,omitempty"`
				Usage                struct {
					InputTokens              *int `json:"input_tokens,omitempty"`
					OutputTokens             *int `json:"output_tokens,omitempty"`
					CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
					CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
					// Legacy location for context management
					ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
					// Iterations breakdown for compaction
					Iterations          []UsageIteration              `json:"iterations,omitempty"`
					OutputTokensDetails *anthropicOutputTokensDetails `json:"output_tokens_details,omitempty"`
				} `json:"usage"`
				// Root-level context management (new location - takes precedence)
				ContextManagement *ContextManagementResponse `json:"context_management,omitempty"`
				// Container info (with skills, if any) from message_delta
				Container *anthropicContainerResponse `json:"container,omitempty"`
			}
			if err := json.Unmarshal([]byte(event.Data), &delta); err != nil {
				return nil, fmt.Errorf("failed to parse message delta: %w", err)
			}

			// TS: container = delta.container != null ? {...} : null -- a
			// message_delta without a container clears the one message_start
			// reported.
			switch {
			case delta.Delta.Container != nil:
				s.container = delta.Delta.Container
			case delta.Container != nil:
				s.container = delta.Container
			default:
				s.container = nil
			}
			s.stopSequence = delta.Delta.StopSequence
			s.stopDetails = delta.Delta.StopDetails
			if v := rawJSONValue(delta.InputTransformations); v != nil {
				s.inputTransformations = v
			}
			// Earlier deltas may carry null while the classifier is still running;
			// the last non-null value is the final verdict.
			if v := rawJSONValue(delta.Delta.SafeguardResults); v != nil {
				s.safeguardResults = v
			}
			if delta.Usage.OutputTokensDetails != nil {
				s.usage.OutputTokensDetails = delta.Usage.OutputTokensDetails
			}
			// TS: rawUsage = {...rawUsage, ...message_delta.usage}.
			var deltaRaw struct {
				Usage map[string]interface{} `json:"usage"`
			}
			if json.Unmarshal([]byte(event.Data), &deltaRaw) == nil && len(deltaRaw.Usage) > 0 {
				if s.usage.Raw == nil {
					s.usage.Raw = map[string]interface{}{}
				}
				for k, v := range deltaRaw.Usage {
					s.usage.Raw[k] = v
				}
			}

			// TS maps the stop reason unconditionally: a null stop_reason
			// finishes as `other`, and usage is applied either way.
			{
				finishReason := mapStopReason(delta.Delta.StopReason, s.isJsonResponseFromTool)

				if delta.Usage.InputTokens != nil {
					s.inputTokens = int64(*delta.Usage.InputTokens)
					s.usage.InputTokens = *delta.Usage.InputTokens
				}
				if delta.Usage.OutputTokens != nil {
					s.usage.OutputTokens = *delta.Usage.OutputTokens
				}
				if delta.Usage.CacheReadInputTokens != nil {
					s.cacheReadTokens = int64(*delta.Usage.CacheReadInputTokens)
					s.usage.CacheReadInputTokens = *delta.Usage.CacheReadInputTokens
				}
				if delta.Usage.CacheCreationInputTokens != nil {
					s.cacheWriteTokens = int64(*delta.Usage.CacheCreationInputTokens)
					s.usage.CacheCreationInputTokens = *delta.Usage.CacheCreationInputTokens
				}
				if len(delta.Usage.Iterations) > 0 {
					s.usage.Iterations = delta.Usage.Iterations
				}
				usage := convertAnthropicUsage(s.usage)

				chunk := &provider.StreamChunk{
					Type:            provider.ChunkTypeFinish,
					FinishReason:    finishReason,
					RawFinishReason: delta.Delta.StopReason,
					Usage:           &usage,
				}

				// Extract context management (check root level first, then usage block)
				if delta.ContextManagement != nil {
					chunk.ContextManagement = delta.ContextManagement
				} else if delta.Usage.ContextManagement != nil {
					chunk.ContextManagement = delta.Usage.ContextManagement
				}
				// The finish chunk is emitted on message_stop so later deltas
				// (e.g. final safeguard results) are included.
				s.finish = chunk
				s.curFinishReason, s.curRawFinishReason = finishReason, delta.Delta.StopReason
				continue
			}

		case "error":
			// A mid-stream provider error (TS `case 'error'`, e.g. an
			// overloaded_error sent on an otherwise-200 response). Previously
			// this event type fell through to "Unknown event, get next" below
			// and was silently discarded; port TS's createAnthropicStreamError:
			// build a fully-normalized *providererrors.StreamProviderError so
			// pkg/ai's streamRetries / IsRetryable sees the correct type/
			// statusCode/isRetryable without falling back to generic inference.
			var errEvent struct {
				Error struct {
					Type        string          `json:"type"`
					Message     string          `json:"message"`
					Code        json.RawMessage `json:"code"`
					StatusCode  *int            `json:"statusCode"`
					IsRetryable *bool           `json:"isRetryable"`
					Data        json.RawMessage `json:"data"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(event.Data), &errEvent); err != nil {
				continue
			}
			// anthropicStreamErrorMetadata returns (0, false) for an
			// unrecognized type (TS's getAnthropicStreamErrorMetadata returns
			// {}, i.e. both fields undefined). Only apply the inferred
			// isRetryable when the type was actually recognized — otherwise
			// leave it nil so NewStreamProviderError falls back to its own
			// message/status-code inference instead of forcing false.
			inferredStatus, inferredRetryable := anthropicStreamErrorMetadata(errEvent.Error.Type)
			statusCode := errEvent.Error.StatusCode
			if statusCode == nil && inferredStatus != 0 {
				sc := inferredStatus
				statusCode = &sc
			}
			isRetryable := errEvent.Error.IsRetryable
			if isRetryable == nil && inferredStatus != 0 {
				ir := inferredRetryable
				isRetryable = &ir
			}
			var code interface{}
			if len(errEvent.Error.Code) > 0 {
				json.Unmarshal(errEvent.Error.Code, &code) //nolint:errcheck
			}
			// TS `'data' in error ? error.data : error`: an explicit data key
			// (even null) is the data; otherwise it is the parsed error object,
			// which keeps only the keys the event carried.
			var data interface{}
			if len(errEvent.Error.Data) > 0 {
				_ = json.Unmarshal(errEvent.Error.Data, &data)
			} else {
				var present map[string]json.RawMessage
				_ = json.Unmarshal([]byte(event.Data), &struct {
					Error *map[string]json.RawMessage `json:"error"`
				}{Error: &present})
				data = anthropicStreamErrorData(present)
			}
			chunkErr := providererrors.NewStreamProviderError(errEvent.Error.Message, "anthropic", errEvent.Error.Type, code, statusCode, isRetryable, data)
			return &provider.StreamChunk{Type: provider.ChunkTypeError, Text: errEvent.Error.Message, Err: chunkErr}, nil

		case "message_stop":
			s.isMessageOpen = false
			s.activeMessageID = ""
			// TS emits one finish part per message_stop and keeps reading, so a
			// stream of several concatenated messages yields one finish each.
			// The stream ends when the underlying body does.
			if s.finish == nil && !s.finishIssued {
				// message_stop without a preceding message_delta: TS still
				// finishes, with its initial finish reason {unified: other}.
				fr := s.curFinishReason
				if fr == "" {
					fr = types.FinishReasonOther
				}
				s.finish = &provider.StreamChunk{
					Type:            provider.ChunkTypeFinish,
					FinishReason:    fr,
					RawFinishReason: s.curRawFinishReason,
				}
			}
			if s.finish != nil && !s.finishIssued {
				s.finishIssued = true
				return s.issueFinish(false), nil
			}
			continue
		}

		// Unknown event, get next
		continue

	}
}

// anthropicStreamErrorData rebuilds the zod-parsed `error` object of an error
// event: only the keys the event carried, in schema order.
func anthropicStreamErrorData(present map[string]json.RawMessage) *orderedStreamErrorData {
	return &orderedStreamErrorData{
		Type: present["type"], Message: present["message"], Code: present["code"],
		StatusCode: present["statusCode"], IsRetryable: present["isRetryable"],
	}
}

type orderedStreamErrorData struct {
	Type        json.RawMessage `json:"type,omitempty"`
	Message     json.RawMessage `json:"message,omitempty"`
	Code        json.RawMessage `json:"code,omitempty"`
	StatusCode  json.RawMessage `json:"statusCode,omitempty"`
	IsRetryable json.RawMessage `json:"isRetryable,omitempty"`
}

// issueFinish returns the next chunk of the end-of-stream sequence: any
// citation-derived web-search sources that were deferred because no
// web_search_tool_result supplied retrieved sources (TS: fallbackWebCitations
// are enqueued just before the finish part), followed by the finish chunk.
//
// final marks the end of the underlying stream; otherwise more messages may
// follow and the stream stays open.
func (s *anthropicStream) issueFinish(final bool) *provider.StreamChunk {
	finish := s.finalizeFinish()
	var sources []*provider.StreamChunk
	if !s.hasWebSearchResults {
		for _, citation := range s.fallbackWebCitations {
			if src, ok := createCitationSource(citation, s.citationDocuments, s.newID); ok {
				src := src
				sources = append(sources, &provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src})
			}
		}
	}
	s.fallbackWebCitations = nil
	if len(sources) == 0 {
		if final {
			s.err = io.EOF
		}
		return finish
	}
	s.pending = append(s.pending, sources[1:]...)
	s.pending = append(s.pending, finish)
	return sources[0]
}

// finalizeFinish recomputes usage and provider metadata from the final
// stream state and returns the finish chunk.
func (s *anthropicStream) finalizeFinish() *provider.StreamChunk {
	chunk := s.finish
	usage := convertAnthropicUsage(s.usage)
	chunk.Usage = &usage
	chunk.ProviderMetadata = anthropicProviderMetadataRaw(s.usage, s.stopSequence, s.stopDetails, s.container, chunk.ContextManagement, s.providerOptionsName, s.usedCustomProviderKey, metadataExtras{
		inputTransformations: s.inputTransformations,
		safeguardResults:     s.safeguardResults,
	})
	return chunk
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// Err returns any error that occurred during streaming
func (s *anthropicStream) Err() error {
	if s.err == io.EOF {
		return nil
	}
	return s.err
}

// ---------------------------------------------------------------------------
// web_search_tool_result / web_fetch_tool_result wire-format converters
// ---------------------------------------------------------------------------

// webSearchResultWire is the snake_case wire format of a single web search result
// as returned by the Anthropic API.
type webSearchResultWire struct {
	Type             string  `json:"type"`
	URL              string  `json:"url"`
	Title            *string `json:"title"`
	PageAge          *string `json:"page_age"`
	EncryptedContent string  `json:"encrypted_content"`
}

func parseAnthropicWebToolResultError(raw json.RawMessage, expectedType string, force bool) (map[string]interface{}, bool) {
	if len(raw) == 0 && !force {
		return nil, false
	}
	var wire struct {
		Type      string `json:"type"`
		ErrorCode string `json:"error_code"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &wire); err != nil && !force {
			return nil, false
		}
	}
	if wire.Type != expectedType && !force {
		return nil, false
	}
	code := wire.ErrorCode
	if code == "" {
		code = "unavailable"
	}
	return map[string]interface{}{
		"type":      expectedType,
		"errorCode": code,
	}, true
}

// convertWebSearchToolResult parses a web_search_tool_result content payload,
// remaps snake_case wire fields to camelCase (page_age→pageAge,
// encrypted_content→encryptedContent), and generates SourceContent parts for
// each result entry.
//
// Mirrors TS SDK anthropic-messages-language-model.ts lines 1055-1093.
func convertWebSearchToolResult(raw json.RawMessage, generateID func() string) (interface{}, []types.SourceContent) {
	var wireResults []webSearchResultWire
	if err := json.Unmarshal(raw, &wireResults); err != nil {
		// Fallback: return raw data as-is.
		var fallback interface{}
		json.Unmarshal(raw, &fallback) //nolint:errcheck
		return fallback, nil
	}
	mapped := make([]map[string]interface{}, 0, len(wireResults))
	sources := make([]types.SourceContent, 0, len(wireResults))
	for _, r := range wireResults {
		m := map[string]interface{}{
			"type":             r.Type,
			"url":              r.URL,
			"pageAge":          nil,
			"encryptedContent": r.EncryptedContent,
		}
		// TS: ...(result.title != null ? { title: result.title } : {}).
		if r.Title != nil {
			m["title"] = *r.Title
		}
		if r.PageAge != nil {
			m["pageAge"] = *r.PageAge
		}
		mapped = append(mapped, m)

		// Build providerMetadata with anthropic.pageAge.
		var pageAge interface{}
		if r.PageAge != nil {
			pageAge = *r.PageAge
		}
		meta, _ := json.Marshal(map[string]interface{}{
			"anthropic": map[string]interface{}{
				"pageAge": pageAge,
			},
		})
		src := types.SourceContent{
			SourceType:       "url",
			ID:               generateID(),
			URL:              r.URL,
			ProviderMetadata: meta,
		}
		if r.Title != nil {
			src.Title = *r.Title
		}
		sources = append(sources, src)
	}
	return mapped, sources
}

// webFetchResultWire is the snake_case wire format of a web_fetch_result object.
type webFetchResultWire struct {
	Type        string  `json:"type"`
	URL         string  `json:"url"`
	RetrievedAt *string `json:"retrieved_at"`
	Content     struct {
		Type      string      `json:"type"`
		Title     *string     `json:"title"`
		Citations interface{} `json:"citations"`
		Source    struct {
			Type      string `json:"type"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	} `json:"content"`
}

// convertWebFetchToolResult parses a web_fetch_tool_result content payload and
// remaps snake_case wire fields to camelCase (retrieved_at→retrievedAt,
// media_type→mediaType).
//
// Mirrors TS SDK anthropic-messages-language-model.ts lines 1015-1053.
func convertWebFetchToolResult(raw json.RawMessage) interface{} {
	var wire webFetchResultWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		var fallback interface{}
		json.Unmarshal(raw, &fallback) //nolint:errcheck
		return fallback
	}
	if wire.Type != "web_fetch_result" {
		// Error or unknown type — pass through unchanged.
		var fallback interface{}
		json.Unmarshal(raw, &fallback) //nolint:errcheck
		return fallback
	}
	var title interface{}
	if wire.Content.Title != nil {
		title = *wire.Content.Title
	}
	// TS: retrievedAt: part.content.retrieved_at (null stays null).
	var retrievedAt interface{}
	if wire.RetrievedAt != nil {
		retrievedAt = *wire.RetrievedAt
	}
	content := map[string]interface{}{
		"type":  wire.Content.Type,
		"title": title,
		"source": map[string]interface{}{
			"type":      wire.Content.Source.Type,
			"mediaType": wire.Content.Source.MediaType,
			"data":      wire.Content.Source.Data,
		},
	}
	// TS passes `citations` through as-is, so an absent value is left out.
	if wire.Content.Citations != nil {
		content["citations"] = wire.Content.Citations
	}
	return map[string]interface{}{
		"type":        wire.Type,
		"url":         wire.URL,
		"retrievedAt": retrievedAt,
		"content":     content,
	}
}

// messageStartChunk builds the anthropic.message_start custom chunk from a
// raw message_start event payload: {id, model, usage} with usage passed
// through verbatim. Missing id/model are null, matching TS.
func messageStartChunk(data string) *provider.StreamChunk {
	var raw struct {
		Message struct {
			ID    *string                `json:"id"`
			Model *string                `json:"model"`
			Usage map[string]interface{} `json:"usage"`
		} `json:"message"`
	}
	_ = json.Unmarshal([]byte(data), &raw)
	var usage interface{}
	if raw.Message.Usage != nil {
		usage = raw.Message.Usage
	}
	metadata, _ := json.Marshal(map[string]interface{}{
		"anthropic": map[string]interface{}{
			"id":    raw.Message.ID,
			"model": raw.Message.Model,
			"usage": usage,
		},
	})
	return &provider.StreamChunk{
		Type: provider.ChunkTypeCustom,
		CustomContent: &types.CustomContent{
			Kind:             "anthropic.message_start",
			ProviderMetadata: metadata,
		},
	}
}

// rawSSEPayload decodes an SSE data payload to the value a raw chunk carries
// (the parsed JSON, or the text itself when it is not valid JSON).
func rawSSEPayload(data string) interface{} {
	var raw interface{}
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		return data
	}
	return raw
}

// anthropicHTTPError builds the ProviderError for a failed Anthropic HTTP
// response: the message is error.message of the API error body (TS
// anthropicFailedResponseHandler), falling back to the raw body, and the
// response headers and body are kept like TS APICallError.
func anthropicHTTPError(providerName string, status int, headers http.Header, body []byte, cause error) *providererrors.ProviderError {
	var perr *providererrors.ProviderError
	// TS anthropicErrorDataSchema: {type: 'error', error: {type: string, message: string}}.
	var parsed struct {
		Type  *string `json:"type"`
		Error *struct {
			Type    *string `json:"type"`
			Message *string `json:"message"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil && parsed.Type != nil && *parsed.Type == "error" &&
		parsed.Error != nil && parsed.Error.Type != nil && parsed.Error.Message != nil {
		perr = providererrors.NewProviderError(providerName, status, *parsed.Error.Type, *parsed.Error.Message, cause)
	} else {
		// TS createJsonErrorResponseHandler: an empty or unparseable body gives
		// the response status text (the body stays on ResponseBody).
		message := http.StatusText(status)
		if message == "" {
			message = string(body)
		}
		perr = providererrors.NewProviderError(providerName, status, "", message, cause)
	}
	perr.ResponseBody = string(body)
	if len(headers) > 0 {
		perr.ResponseHeaders = providerutils.ExtractHeaders(headers)
	}
	return perr
}

// sseEventTypeFromPayload returns the payload's top-level "type" string, or
// "" when the data is not a JSON object with one.
func sseEventTypeFromPayload(data string) string {
	var probe struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(data), &probe) != nil {
		return ""
	}
	return probe.Type
}

// mapStopReason is TS mapAnthropicStopReason.
func mapStopReason(stopReason string, isJSONResponseFromTool bool) types.FinishReason {
	switch stopReason {
	case "end_turn", "pause_turn", "stop_sequence":
		return types.FinishReasonStop
	case "max_tokens", "model_context_window_exceeded":
		return types.FinishReasonLength
	case "tool_use":
		// When the json tool is used the API returns stop_reason="tool_use"
		// but the structured JSON was extracted as text, not a tool call.
		if isJSONResponseFromTool {
			return types.FinishReasonStop
		}
		return types.FinishReasonToolCalls
	case "refusal":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonOther
	}
}

func isTrue(b *bool) bool { return b != nil && *b }

func trueFlag() *bool { b := true; return &b }
