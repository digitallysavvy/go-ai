package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

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
func (m *LanguageModel) Provider() string {
	return m.provider.Name()
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

func (m *LanguageModel) requestHeaders(opts *provider.GenerateOptions, betas []string, stream bool) map[string]string {
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
	return headers
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	req, err := m.prepareRequest(opts, false)
	if err != nil {
		return nil, err
	}
	body := m.transformRequestBody(req.body, req.betas, false)

	internalReq := internalhttp.Request{
		Method:  http.MethodPost,
		Path:    m.messagesPath(false),
		Body:    body,
		Headers: m.requestHeaders(opts, req.betas, false),
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

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    m.messagesPath(true),
		Body:    body,
		Headers: m.requestHeaders(opts, req.betas, true),
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
	stream.requestBody = body
	stream.citationDocuments = extractCitationDocuments(opts.Prompt.Messages)
	return providerutils.WithResponseMetadata(stream, httpResp.Header, m.ModelID()), nil
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
		for _, content := range response.Content {
			switch content.Type {
			case "text":
				textParts = append(textParts, content.Text)
				metadataCitations := filterWebSearchCitations(content.Citations)
				if co.rawBatchCitations {
					metadataCitations = content.Citations
				}
				textBlocks = append(textBlocks, types.TextContent{
					Text:             content.Text,
					ProviderMetadata: citationsProviderMetadata(metadataCitations),
				})
				if len(metadataCitations) > 0 {
					hasExtraContent = true
				}
				for _, citation := range content.Citations {
					src, ok := createCitationSource(citation, citationDocs, anthropicGenerateID)
					if !ok {
						continue
					}
					textBlocks = append(textBlocks, src)
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

	// Extract reasoning/thinking content blocks from the response.
	// These are surfaced as ReasoningContent parts in result.Content so callers
	// can include them in subsequent requests (round-tripping thinking blocks).
	for _, content := range response.Content {
		switch content.Type {
		case "thinking":
			result.Content = append(result.Content, types.ReasoningContent{
				Text:      content.Thinking,
				Signature: content.Signature,
			})
		case "redacted_thinking":
			// Redacted blocks have no visible text — only the opaque data blob.
			result.Content = append(result.Content, types.ReasoningContent{
				RedactedData: content.Data,
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
			result.Content = append(result.Content, types.CustomContent{
				Kind:             "anthropic.container_upload",
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
	for _, content := range response.Content {
		switch content.Type {
		case "tool_use":
			if usesJsonResponseTool && content.Name == "json" {
				// jsonTool mode: the model responded via the synthetic json tool.
				// Extract the tool input as the structured text result.
				inputJSON, _ := json.Marshal(content.Input)
				result.Text = string(inputJSON)
				isJsonResponseFromTool = true
			} else if content.ToolsetName != "" {
				// Toolset member calls (e.g. the computer toolset) map to the
				// toolset tool with the member name as the action.
				meta := map[string]interface{}{"toolsetName": content.ToolsetName}
				if caller := anthropicCallerInfo(content.Caller); caller != nil {
					meta["caller"] = caller
				}
				result.ToolCalls = append(result.ToolCalls, types.ToolCall{
					ID:               content.ID,
					ToolName:         mapAnthropicToolName(content.ToolsetName, toolNameMap),
					Arguments:        toToolsetMemberInput(content.Name, content.Input),
					ProviderMetadata: map[string]interface{}{"anthropic": meta},
				})
			} else {
				result.ToolCalls = append(result.ToolCalls, types.ToolCall{
					ID:               content.ID,
					ToolName:         mapAnthropicToolName(content.Name, toolNameMap),
					Arguments:        content.Input,
					ProviderMetadata: anthropicCallerMetadata(content.Caller),
				})
			}
		case "server_tool_use":
			if tc, ok := serverToolUseCall(content, toolNameMap, co.markCodeExecutionDynamic); ok {
				result.ToolCalls = append(result.ToolCalls, tc)
			}
		case "mcp_tool_use":
			// MCP tool calls are executed server-side; surface them as
			// provider-executed dynamic tool calls (TS mcp_tool_use handling).
			meta := anthropicMCPToolUseMetadata(content.ServerName)
			mcpToolCalls[content.ID] = mcpToolCallInfo{toolName: content.Name, providerMetadata: meta}
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:               content.ID,
				ToolName:         content.Name,
				Arguments:        content.Input,
				ProviderExecuted: true,
				Dynamic:          true,
				ProviderMetadata: meta,
			})
		case "advisor_tool_result":
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         mapAnthropicToolName("advisor", toolNameMap),
				ProviderExecuted: true,
			}
			res, isErr := convertAdvisorResult(content.Content)
			trc.Result = res
			if isErr {
				trc.Error = fmt.Sprintf("%v", res["errorCode"])
			}
			result.Content = append(result.Content, trc)
		case "web_search_tool_result":
			// Remap snake_case wire fields to camelCase and emit source chunks.
			// Mirrors TS SDK anthropic-messages-language-model.ts:1055-1093.
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         providerToolResultName(content.Type),
				ProviderExecuted: true,
			}
			if errResult, ok := parseAnthropicWebToolResultError(content.Content, "web_search_tool_result_error", content.IsError); ok {
				trc.Result = errResult
				trc.Error = fmt.Sprintf("%v", errResult["errorCode"])
			} else if len(content.Content) > 0 {
				mapped, sources := convertWebSearchToolResult(content.Content)
				trc.Result = mapped
				for _, src := range sources {
					s := src
					result.Content = append(result.Content, s)
				}
			}
			result.Content = append(result.Content, trc)
		case "web_fetch_tool_result":
			// Remap snake_case wire fields (retrieved_at, media_type) to camelCase.
			// Mirrors TS SDK anthropic-messages-language-model.ts:1015-1053.
			trc := types.ToolResultContent{
				ToolCallID:       content.ToolUseID,
				ToolName:         providerToolResultName(content.Type),
				ProviderExecuted: true,
			}
			if errResult, ok := parseAnthropicWebToolResultError(content.Content, "web_fetch_tool_result_error", content.IsError); ok {
				trc.Result = errResult
				trc.Error = fmt.Sprintf("%v", errResult["errorCode"])
			} else if len(content.Content) > 0 {
				trc.Result = convertWebFetchToolResult(content.Content)
			}
			result.Content = append(result.Content, trc)
		case "mcp_tool_result":
			// Resolve toolName and providerMetadata from the paired mcp_tool_use
			// call (TS: `mcpToolCalls[part.tool_use_id].toolName` /
			// `.providerMetadata`), and mark the result dynamic like TS.
			call := mcpToolCalls[content.ToolUseID]
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
			if content.IsError {
				trc.Error = fmt.Sprintf("%v", parsed)
			} else {
				trc.Result = parsed
			}
			result.Content = append(result.Content, trc)
		case "code_execution_tool_result", "bash_code_execution_tool_result",
			"text_editor_code_execution_tool_result", "tool_search_tool_result":
			// Deferred provider tool results: the provider executed the tool in a
			// previous step and delivers the result inline here. Surface as
			// ToolResultContent so the SDK's pendingDeferredToolCalls map is cleared.
			trc := types.ToolResultContent{
				ToolCallID: content.ToolUseID,
				ToolName:   providerToolResultName(content.Type),
			}
			var parsed interface{}
			if len(content.Content) > 0 {
				json.Unmarshal(content.Content, &parsed) //nolint:errcheck
			}
			if content.IsError {
				trc.Error = fmt.Sprintf("%v", parsed)
			} else {
				trc.Result = parsed
			}
			result.Content = append(result.Content, trc)
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
	result.OutputDetails = &types.OutputTokenDetails{TextTokens: &outputTokens}
	if usage.OutputTokensDetails != nil && usage.OutputTokensDetails.ThinkingTokens != nil {
		reasoning := int64(*usage.OutputTokensDetails.ThinkingTokens)
		text := outputTokens - reasoning
		result.OutputDetails = &types.OutputTokenDetails{TextTokens: &text, ReasoningTokens: &reasoning}
	}

	// Store raw usage for provider-specific details
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}

	if usage.CacheCreationInputTokens > 0 {
		result.Raw["cache_creation_input_tokens"] = usage.CacheCreationInputTokens
	}
	if usage.CacheReadInputTokens > 0 {
		result.Raw["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if len(usage.Iterations) > 0 {
		result.Raw["iterations"] = usage.Iterations
	}
	if usage.OutputTokensDetails != nil {
		result.Raw["output_tokens_details"] = usage.OutputTokensDetails
	}

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
	if len(skills) == 0 {
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
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		body := statusErr.Body
		if m.provider.config.TransformErrorBody != nil {
			body = m.provider.config.TransformErrorBody(body)
		}
		var parsed anthropicErrorBody
		if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil && (parsed.Error.Type != "" || parsed.Error.Message != "") {
			return providererrors.NewProviderError(m.provider.Name(), statusErr.StatusCode, parsed.Error.Type, parsed.Error.Message, err)
		}
		return providererrors.NewProviderError(m.provider.Name(), statusErr.StatusCode, "", string(body), err)
	}
	msg := err.Error()
	// HTTP errors from the internal client look like "HTTP NNN: <body>"
	// Try to find the JSON body portion.
	if idx := strings.Index(msg, ": {"); idx != -1 {
		jsonPart := msg[idx+2:]
		var body anthropicErrorBody
		if jsonErr := json.Unmarshal([]byte(jsonPart), &body); jsonErr == nil && body.Error.Type != "" {
			return providererrors.NewProviderError("anthropic", 0, body.Error.Type, body.Error.Message, err)
		}
	}
	return providererrors.NewProviderError("anthropic", 0, "", msg, err)
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
	Type      string                 `json:"type"` // "text", "tool_use", "thinking", "redacted_thinking", "*_tool_result"
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	From      map[string]interface{} `json:"from,omitempty"`
	To        map[string]interface{} `json:"to,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`  // For "thinking" type
	Signature string                 `json:"signature,omitempty"` // For "thinking" type
	Data      string                 `json:"data,omitempty"`      // For "redacted_thinking" type
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
	IsError   bool            `json:"is_error,omitempty"`
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
	// inputTransformations / safeguardResults are reported in the finish
	// provider metadata.
	inputTransformations interface{}
	safeguardResults     interface{}
	// finish holds the finish chunk assembled from message_delta; it is
	// emitted on message_stop (or at end of stream).
	finish       *provider.StreamChunk
	finishIssued bool

	// requestBody is the raw request body that opened this stream, exposed
	// via RequestBody() (provider.StreamRequestBody, hand-off: "stream
	// request body field").
	requestBody interface{}

	// citationDocuments maps citation document_index -> title/filename/mediaType,
	// extracted from citation-enabled file parts in the request prompt (TS
	// extractCitationDocuments). Set by DoStream before the first Next() call.
	citationDocuments []citationDocument
}

// newAnthropicStream creates a new Anthropic stream.
// usesJsonResponseTool must match the req.usesJSONResponseTool value DoStream computed via prepareRequest.
func newAnthropicStream(reader io.ReadCloser, usesJsonResponseTool bool, toolsOpt ...[]types.Tool) *anthropicStream {
	var tools []types.Tool
	if len(toolsOpt) > 0 {
		tools = toolsOpt[0]
	}
	return newAnthropicStreamWithWarnings(reader, usesJsonResponseTool, tools, nil)
}

func newAnthropicStreamWithWarnings(reader io.ReadCloser, usesJsonResponseTool bool, tools []types.Tool, warnings []types.Warning) *anthropicStream {
	var pending []*provider.StreamChunk
	if len(warnings) > 0 {
		pending = append(pending, &provider.StreamChunk{
			Type:     provider.ChunkTypeStreamStart,
			Warnings: warnings,
		})
	}
	return &anthropicStream{
		reader:               reader,
		parser:               streaming.NewSSEParser(reader),
		contentBlocks:        make(map[int]*streamContentBlock),
		pending:              pending,
		serverToolCallNames:  make(map[string]string),
		mcpToolCalls:         make(map[string]mcpToolCallInfo),
		usesJsonResponseTool: usesJsonResponseTool,
		toolNameMap:          anthropicProviderToolNameMap(tools),
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
		event, err := s.parser.Next()
		if err != nil {
			if s.spliced {
				// A spliced stream ends without a finish chunk: TS never
				// re-enqueues after hasInvalidMessageSequence is set, and the
				// ReadableStream simply closes once the underlying byte stream
				// ends (no synthesized finish part).
				s.err = io.EOF
				return nil, io.EOF
			}
			if err == io.EOF && s.finish != nil && !s.finishIssued {
				s.finishIssued = true
				s.err = io.EOF
				return s.finalizeFinish(), nil
			}
			s.err = err
			return nil, err
		}

		// Once a spliced-stream error chunk has been emitted, every remaining
		// event is silently discarded (TS hasInvalidMessageSequence guard).
		if s.spliced {
			continue
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
					Input       map[string]interface{} `json:"input"` // non-empty for programmatic deferred tool calls
					ToolsetName string                 `json:"toolset_name"`
					Caller      map[string]interface{} `json:"caller"`
					// mcp_tool_use fields
					ServerName string `json:"server_name"`
					// mcp_tool_result / web_search_tool_result / web_fetch_tool_result fields
					ToolUseID string          `json:"tool_use_id"`
					IsError   bool            `json:"is_error"`
					Content   json.RawMessage `json:"content"`
					// compaction fields
					Signature string `json:"signature"`
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
				if len(start.ContentBlock.Input) > 0 {
					if b, err := json.Marshal(start.ContentBlock.Input); err == nil {
						initialInput = string(b)
					}
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

			case "redacted_thinking":
				// Treat redacted thinking blocks the same as thinking: they carry
				// reasoning content whose text has been redacted for safety reasons.
				// We cannot surface the redacted data without a providerMetadata field,
				// but we track the block so content_block_stop is a clean no-op.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "reasoning",
				}

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
					if len(start.ContentBlock.Input) > 0 {
						if b, err := json.Marshal(start.ContentBlock.Input); err == nil {
							block.inputBuf.Write(b)
							block.firstDelta = false
						}
					}
				}
				s.contentBlocks[start.Index] = block

			case "mcp_tool_use":
				// MCP tool calls have their full input pre-populated in content_block_start.
				// Emit immediately as a tool call chunk (no input_json_delta accumulation needed).
				input := start.ContentBlock.Input
				if input == nil {
					input = map[string]interface{}{}
				}
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
				if start.ContentBlock.IsError {
					tr.Error = fmt.Errorf("mcp tool error: %v", mcpResultContent)
				} else {
					tr.Result = mcpResultContent
				}
				// Track so content_block_stop is a clean no-op.
				s.contentBlocks[start.Index] = &streamContentBlock{
					blockType: "mcp-tool-result",
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
					toolName = providerToolResultName(start.ContentBlock.Type)
				}
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         toolName,
					ProviderExecuted: true,
				}
				if errResult, ok := parseAnthropicWebToolResultError(start.ContentBlock.Content, "web_fetch_tool_result_error", start.ContentBlock.IsError); ok {
					tr.Result = errResult
					tr.Error = fmt.Errorf("%v", errResult["errorCode"])
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
					toolName = providerToolResultName(start.ContentBlock.Type)
				}
				tr := &types.ToolResult{
					ToolCallID:       start.ContentBlock.ToolUseID,
					ToolName:         toolName,
					ProviderExecuted: true,
				}
				var webSearchSources []types.SourceContent
				if errResult, ok := parseAnthropicWebToolResultError(start.ContentBlock.Content, "web_search_tool_result_error", start.ContentBlock.IsError); ok {
					tr.Result = errResult
					tr.Error = fmt.Errorf("%v", errResult["errorCode"])
				} else if len(start.ContentBlock.Content) > 0 {
					mapped, sources := convertWebSearchToolResult(start.ContentBlock.Content)
					tr.Result = mapped
					webSearchSources = sources
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
					InputTransformations json.RawMessage `json:"input_transformations"`
					Usage                struct {
						InputTokens              int              `json:"input_tokens"`
						OutputTokens             int              `json:"output_tokens,omitempty"`
						CacheReadInputTokens     int              `json:"cache_read_input_tokens"`
						CacheCreationInputTokens int              `json:"cache_creation_input_tokens"`
						Iterations               []UsageIteration `json:"iterations,omitempty"`
					} `json:"usage"`
					Container *anthropicContainerResponse `json:"container,omitempty"`
					Content   []struct {
						Type  string                 `json:"type"`
						ID    string                 `json:"id"`
						Name  string                 `json:"name"`
						Input map[string]interface{} `json:"input"`
						// mcp_tool_use fields
						ServerName string `json:"server_name,omitempty"`
						// Deferred provider tool result fields
						ToolUseID string          `json:"tool_use_id,omitempty"`
						Content   json.RawMessage `json:"content,omitempty"`
						IsError   bool            `json:"is_error,omitempty"`
					} `json:"content"`
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
					return &provider.StreamChunk{
						Type: provider.ChunkTypeError,
						Text: fmt.Sprintf(
							"Received message_start for message %s while message %s is still open.",
							jsonQuote(msg.Message.ID), jsonQuote(s.activeMessageID)),
					}, nil
				}
				s.isMessageOpen = true
				s.activeMessageID = msg.Message.ID
				if v := rawJSONValue(msg.Message.InputTransformations); v != nil {
					s.inputTransformations = v
				}
				s.inputTokens = int64(msg.Message.Usage.InputTokens)
				s.cacheReadTokens = int64(msg.Message.Usage.CacheReadInputTokens)
				s.cacheWriteTokens = int64(msg.Message.Usage.CacheCreationInputTokens)
				s.usage.InputTokens = msg.Message.Usage.InputTokens
				s.usage.OutputTokens = msg.Message.Usage.OutputTokens
				s.usage.CacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
				s.usage.CacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				if len(msg.Message.Usage.Iterations) > 0 {
					s.usage.Iterations = msg.Message.Usage.Iterations
				}

				// Capture container info (present when container was used/created).
				// In message_start it contains id and expires_at but no skills.
				if msg.Message.Container != nil {
					s.container = msg.Message.Container
				}

				// Buffer chunks for each pre-populated content block.
				// tool_use blocks become tool call chunks (or text in jsonTool mode).
				// Deferred provider tool result blocks (web_search_tool_result, etc.)
				// become ChunkTypeToolResult chunks so pendingDeferredToolCalls clears.
				for _, part := range msg.Message.Content {
					switch part.Type {
					case "tool_use":
						args := part.Input
						if args == nil {
							args = map[string]interface{}{}
						}
						if s.usesJsonResponseTool && part.Name == "json" {
							// jsonTool mode: emit the tool input as a text chunk.
							s.isJsonResponseFromTool = true
							inputJSON, _ := json.Marshal(args)
							s.pending = append(s.pending, &provider.StreamChunk{
								Type: provider.ChunkTypeText,
								Text: string(inputJSON),
							})
						} else {
							s.pending = append(s.pending, &provider.StreamChunk{
								Type: provider.ChunkTypeToolCall,
								ToolCall: &types.ToolCall{
									ID:        part.ID,
									ToolName:  mapAnthropicToolName(part.Name, s.toolNameMap),
									Arguments: args,
								},
							})
						}
					case "mcp_tool_use":
						// Pre-populated deferred MCP tool call (TS content_block_start
						// "mcp_tool_use" case, mirrored here for parity with the other
						// deferred block types already handled in this loop). Track the
						// call's toolName/providerMetadata in s.mcpToolCalls so a paired
						// mcp_tool_result elsewhere in this same content array (or a
						// later content_block_start) can resolve them.
						input := part.Input
						if input == nil {
							input = map[string]interface{}{}
						}
						s.serverToolCallNames[part.ID] = part.Name
						mcpMeta := anthropicMCPToolUseMetadata(part.ServerName)
						s.mcpToolCalls[part.ID] = mcpToolCallInfo{
							toolName:         part.Name,
							providerMetadata: mcpMeta,
						}
						s.pending = append(s.pending, &provider.StreamChunk{
							Type: provider.ChunkTypeToolCall,
							ToolCall: &types.ToolCall{
								ID:               part.ID,
								ToolName:         part.Name,
								Arguments:        input,
								ProviderExecuted: true,
								Dynamic:          true,
								ProviderMetadata: mcpMeta,
							},
						})
					case "web_search_tool_result":
						// Remap snake_case wire fields to camelCase and emit source chunks.
						toolName := s.serverToolCallNames[part.ToolUseID]
						if toolName == "" {
							toolName = providerToolResultName(part.Type)
						}
						tr := &types.ToolResult{
							ToolCallID:       part.ToolUseID,
							ToolName:         toolName,
							ProviderExecuted: true,
						}
						var webSearchSources []types.SourceContent
						if errResult, ok := parseAnthropicWebToolResultError(part.Content, "web_search_tool_result_error", part.IsError); ok {
							tr.Result = errResult
							tr.Error = fmt.Errorf("%v", errResult["errorCode"])
						} else if len(part.Content) > 0 {
							mapped, sources := convertWebSearchToolResult(part.Content)
							tr.Result = mapped
							webSearchSources = sources
						}
						s.pending = append(s.pending, &provider.StreamChunk{
							Type:       provider.ChunkTypeToolResult,
							ToolResult: tr,
						})
						for _, src := range webSearchSources {
							s2 := src
							s.pending = append(s.pending, &provider.StreamChunk{
								Type:          provider.ChunkTypeSource,
								SourceContent: &s2,
							})
						}
					case "web_fetch_tool_result":
						// Remap snake_case wire fields (retrieved_at, media_type) to camelCase.
						toolName := s.serverToolCallNames[part.ToolUseID]
						if toolName == "" {
							toolName = providerToolResultName(part.Type)
						}
						tr := &types.ToolResult{
							ToolCallID:       part.ToolUseID,
							ToolName:         toolName,
							ProviderExecuted: true,
						}
						if errResult, ok := parseAnthropicWebToolResultError(part.Content, "web_fetch_tool_result_error", part.IsError); ok {
							tr.Result = errResult
							tr.Error = fmt.Errorf("%v", errResult["errorCode"])
						} else if len(part.Content) > 0 {
							tr.Result = convertWebFetchToolResult(part.Content)
							// Grow the citation document list in stream order so a later
							// page_location/char_location citation (in a subsequent text
							// block) can resolve against this fetched document, mirroring
							// TS's inline `citationDocuments.push(...)` in the same
							// content_block_start switch (anthropic-language-model.ts:2228).
							if doc, ok := extractWebFetchCitationDocument(part.Content); ok {
								s.citationDocuments = append(s.citationDocuments, doc)
							}
						}
						s.pending = append(s.pending, &provider.StreamChunk{
							Type:       provider.ChunkTypeToolResult,
							ToolResult: tr,
						})
					case "mcp_tool_result":
						// Resolve toolName/providerMetadata from the paired mcp_tool_use
						// call (populated when it was streamed, potentially in an
						// earlier compaction iteration of this same connection), and
						// mark the result dynamic like TS.
						call := s.mcpToolCalls[part.ToolUseID]
						toolName := call.toolName
						if toolName == "" {
							toolName = s.serverToolCallNames[part.ToolUseID]
						}
						tr := &types.ToolResult{
							ToolCallID:       part.ToolUseID,
							ToolName:         toolName,
							Dynamic:          true,
							ProviderMetadata: call.providerMetadata,
						}
						if part.IsError {
							var errContent interface{}
							if len(part.Content) > 0 {
								json.Unmarshal(part.Content, &errContent) //nolint:errcheck
							}
							tr.Error = fmt.Errorf("%v", errContent)
						} else {
							if len(part.Content) > 0 {
								var result interface{}
								json.Unmarshal(part.Content, &result) //nolint:errcheck
								tr.Result = result
							}
						}
						s.pending = append(s.pending, &provider.StreamChunk{
							Type:       provider.ChunkTypeToolResult,
							ToolResult: tr,
						})
					case "code_execution_tool_result", "bash_code_execution_tool_result",
						"text_editor_code_execution_tool_result", "tool_search_tool_result",
						"advisor_tool_result":
						// Deferred provider tool results pre-populated in message_start.
						// Emit as ChunkTypeToolResult so the SDK can clear pendingDeferredToolCalls.
						toolName := s.serverToolCallNames[part.ToolUseID]
						if toolName == "" {
							toolName = providerToolResultName(part.Type)
						}
						tr := &types.ToolResult{
							ToolCallID: part.ToolUseID,
							ToolName:   toolName,
						}
						if part.IsError {
							var errContent interface{}
							if len(part.Content) > 0 {
								json.Unmarshal(part.Content, &errContent) //nolint:errcheck
							}
							tr.Error = fmt.Errorf("%v", errContent)
						} else {
							if len(part.Content) > 0 {
								var result interface{}
								json.Unmarshal(part.Content, &result) //nolint:errcheck
								tr.Result = result
							}
						}
						s.pending = append(s.pending, &provider.StreamChunk{
							Type:       provider.ChunkTypeToolResult,
							ToolResult: tr,
						})
					}
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
					Reasoning: delta.Delta.Thinking,
				}, nil

			case "signature_delta":
				// Thinking block signature: cryptographic attestation, not user-visible.
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
					if citationType(delta.Delta.Citation) == "web_search_result_location" {
						block.citations = append(block.citations, delta.Delta.Citation)
					}
				}
				if src, ok := createCitationSource(delta.Delta.Citation, s.citationDocuments, anthropicGenerateID); ok {
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
				if inputStr != "" {
					if err := json.Unmarshal([]byte(inputStr), &args); err != nil {
						// Malformed JSON from the API: surface as error.
						return nil, fmt.Errorf("failed to parse tool call arguments for %q: %w", block.toolName, err)
					}
				}
				if args == nil {
					args = map[string]interface{}{}
				}
				var toolsetDelta *provider.StreamChunk
				if block.toolsetName != "" {
					args = toToolsetMemberInput(block.memberName, args)
					b, _ := json.Marshal(args)
					toolsetDelta = &provider.StreamChunk{Type: provider.ChunkTypeToolInputDelta, ID: block.toolCallID, Text: string(b)}
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
				if len(block.citations) > 0 {
					meta = citationsProviderMetadata(block.citations)
				}
				return &provider.StreamChunk{
					Type:             provider.ChunkTypeTextEnd,
					ID:               strconv.Itoa(stop.Index),
					ProviderMetadata: meta,
				}, nil
			}
			// reasoning or unknown — no chunk to emit.
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

			// Update container state if the delta contains container info (includes skills).
			if delta.Delta.Container != nil {
				s.container = delta.Delta.Container
			} else if delta.Container != nil {
				s.container = delta.Container
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

			if delta.Delta.StopReason != "" {
				var finishReason types.FinishReason
				switch delta.Delta.StopReason {
				case "end_turn", "pause_turn", "stop_sequence":
					finishReason = types.FinishReasonStop
				case "max_tokens", "model_context_window_exceeded":
					finishReason = types.FinishReasonLength
				case "tool_use":
					// When the json tool is used, the API returns stop_reason="tool_use"
					// but the caller expects "stop" — the structured JSON has been
					// extracted as text, not a tool call. Matches mapAnthropicStopReason()
					// in the TypeScript SDK.
					if s.isJsonResponseFromTool {
						finishReason = types.FinishReasonStop
					} else {
						finishReason = types.FinishReasonToolCalls
					}
				case "refusal":
					finishReason = types.FinishReasonContentFilter
				default:
					finishReason = types.FinishReasonOther
				}

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
				continue
			}
			continue

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
					Data        interface{}     `json:"data"`
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
			data := errEvent.Error.Data
			if data == nil {
				data = errEvent.Error
			}
			chunkErr := providererrors.NewStreamProviderError(errEvent.Error.Message, "anthropic", errEvent.Error.Type, code, statusCode, isRetryable, data)
			return &provider.StreamChunk{Type: provider.ChunkTypeError, Text: errEvent.Error.Message, Err: chunkErr}, nil

		case "message_stop":
			s.isMessageOpen = false
			s.activeMessageID = ""
			if s.finish != nil && !s.finishIssued {
				s.finishIssued = true
				s.err = io.EOF
				return s.finalizeFinish(), nil
			}
			// Stream complete
			s.err = io.EOF
			return nil, io.EOF
		}

		// Unknown event, get next
		continue

	}
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
func convertWebSearchToolResult(raw json.RawMessage) (interface{}, []types.SourceContent) {
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
			"title":            r.Title,
			"pageAge":          nil,
			"encryptedContent": r.EncryptedContent,
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
	Type        string `json:"type"`
	URL         string `json:"url"`
	RetrievedAt string `json:"retrieved_at"`
	Content     struct {
		Type      string      `json:"type"`
		Title     string      `json:"title"`
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
	return map[string]interface{}{
		"type":        wire.Type,
		"url":         wire.URL,
		"retrievedAt": wire.RetrievedAt,
		"content": map[string]interface{}{
			"type":      wire.Content.Type,
			"title":     wire.Content.Title,
			"citations": wire.Content.Citations,
			"source": map[string]interface{}{
				"type":      wire.Content.Source.Type,
				"mediaType": wire.Content.Source.MediaType,
				"data":      wire.Content.Source.Data,
			},
		},
	}
}
