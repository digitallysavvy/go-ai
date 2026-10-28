package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// LanguageModel implements provider.LanguageModel using the Gemini wire format.
// It is shared by both the google and googlevertex packages; provider-specific
// details (auth, base URL, metadata keys) are injected via Config.
type LanguageModel struct {
	cfg     Config
	modelID string
}

// NewLanguageModel creates a LanguageModel with the given configuration.
func NewLanguageModel(cfg Config, modelID string) *LanguageModel {
	return &LanguageModel{cfg: cfg, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *LanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *LanguageModel) Provider() string { return m.cfg.ProviderName }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports whether the model supports tool calling.
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports whether the model supports structured output.
func (m *LanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports whether the model accepts image inputs.
func (m *LanguageModel) SupportsImageInput() bool {
	if m.cfg.SupportsImageInput == nil {
		return false
	}
	return m.cfg.SupportsImageInput(m.modelID)
}

// SupportedURLs returns the URL patterns (regular expressions keyed by media
// type, "*" for all) this model accepts directly without downloading first.
// Mirrors TS getSupportedUrls (google-provider.ts / google-vertex-provider-base.ts).
func (m *LanguageModel) SupportedURLs() map[string][]string {
	if m.cfg.SupportedURLs == nil {
		return nil
	}
	return m.cfg.SupportedURLs(m.modelID)
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	opts, jsonResponseToolName := m.withJSONResponseTool(opts)
	reqBody, headers, warnings, err := m.buildRequest(ctx, opts, false)
	if err != nil {
		return nil, err
	}

	generateContent := func(body map[string]interface{}) (Response, *internalhttp.Response, error) {
		var response GenerateResponse
		resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    m.cfg.GeneratePath(m.modelID),
			Body:    body,
			Headers: headers,
			// TS createJsonResponseHandler(responseSchema): a body that is not
			// JSON or does not match the schema is an APICallError
			// "Invalid JSON response".
			ValidateResponse: providerutils.NodeResponseValidator(m.cfg.ProviderName, zResponse),
		}, &response)
		return response.Response, resp, err
	}

	response, resp, err := generateContent(reqBody)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Gemini 4 long decode: a request that reaches the per-request cap while
	// maxOutputTokens budget remains finishes with CONTINUATION. Resend the
	// same request with the continuation token (the server tracks the
	// cumulative tokens) and combine the content of all requests. Usage is
	// summed across requests (matching billing); the first request's prompt
	// token count is kept separately for context window checks.
	tnm := newToolNameMapping(opts.Tools)
	var (
		prior         []*types.GenerateResult
		usageMetadata *UsageMetadata
		firstPrompt   *int
		// groundingPerRequest holds each request's grounding snapshot (null
		// when absent), in request order (TS groundingMetadataPerRequest).
		groundingPerRequest []json.RawMessage
	)
	firstPrompt = initialPromptTokens(response.UsageMetadata)
	for {
		var token string
		if len(response.Candidates) > 0 {
			groundingPerRequest = append(groundingPerRequest, rawOrNull(response.Candidates[0].GroundingMetadata))
			if response.Candidates[0].FinishReason == "CONTINUATION" {
				token = response.Candidates[0].ContinuationToken
			}
		} else {
			groundingPerRequest = append(groundingPerRequest, json.RawMessage("null"))
		}
		usageMetadata = addUsageMetadata(usageMetadata, response.UsageMetadata)
		if token == "" {
			break
		}
		// TS has no bound on the loop; honor cancellation between hops so a
		// server that keeps answering CONTINUATION cannot outlive the caller.
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		prior = append(prior, m.convertResponseFull(response, tnm, jsonResponseToolName, nil))

		next := make(map[string]interface{}, len(reqBody)+1)
		for k, v := range reqBody {
			next[k] = v
		}
		next["continuationToken"] = token
		response, resp, err = generateContent(next)
		if err != nil {
			return nil, m.handleError(err)
		}
	}

	last := response
	last.UsageMetadata = usageMetadata
	var result *types.GenerateResult
	if len(prior) == 0 {
		result = m.convertResponseFull(last, tnm, jsonResponseToolName, nil)
	} else {
		result = m.convertResponseWithGrounding(last, tnm, jsonResponseToolName, firstPrompt, groundingPerRequest)
		for i := len(prior) - 1; i >= 0; i-- {
			p := prior[i]
			result.Content = append(append([]types.ContentPart{}, p.Content...), result.Content...)
			result.ToolCalls = append(append([]types.ToolCall{}, p.ToolCalls...), result.ToolCalls...)
			if p.Text != "" {
				result.Text = p.Text + result.Text
			}
		}
	}
	// TS response.body is the last request's rawValue, not the summed usage.
	result.RawResponse = response
	result.Warnings = append(warnings, result.Warnings...)
	result.RawRequest = reqBody
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	if result.ResponseMetadata == nil {
		result.ResponseMetadata = &types.ResponseMetadata{ModelID: m.modelID}
	}
	result.ResponseMetadata.Headers = result.ResponseHeaders
	return result, nil
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	opts, jsonResponseToolName := m.withJSONResponseTool(opts)
	reqBody, headers, warnings, err := m.buildRequest(ctx, opts, true)
	if err != nil {
		return nil, err
	}

	httpResp, err := m.cfg.Client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   m.cfg.StreamPath(m.modelID),
		Body:   reqBody,
		Headers: internalhttp.MergeHeaders(headers, map[string]string{
			"Accept": "text/event-stream",
		}),
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	stream := newStream(httpResp.Body, m.cfg, newToolNameMapping(opts.Tools), httpResp.Header, m.ModelID())
	stream.includeRawChunks = opts.IncludeRawChunks
	stream.requestBody = reqBody
	stream.continueFn = func(token string) (io.ReadCloser, error) {
		next := make(map[string]interface{}, len(reqBody)+1)
		for k, v := range reqBody {
			next[k] = v
		}
		next["continuationToken"] = token
		resp, err := m.cfg.Client.DoStream(ctx, internalhttp.Request{
			Method: http.MethodPost,
			Path:   m.cfg.StreamPath(m.modelID),
			Body:   next,
			Headers: internalhttp.MergeHeaders(headers, map[string]string{
				"Accept": "text/event-stream",
			}),
		})
		if err != nil {
			return nil, m.handleError(err)
		}
		return resp.Body, nil
	}
	var textStream provider.TextStream = stream
	if jsonResponseToolName != "" {
		textStream = streaming.NewJSONResponseToolStream(textStream, jsonResponseToolName)
	}
	return streaming.NewWarningsStream(textStream, warnings), nil
}

// jsonResponseToolModelPattern is TS getArgs' /(^|\/)gemini-/i: unlike the
// capability patterns it does not treat a "." as a prefix separator, so an id
// such as "eu.gemini-2.0-flash" is not rewritten to use a JSON response tool.
var jsonResponseToolModelPattern = regexp.MustCompile(`(?i)(^|/)gemini-`)

// withJSONResponseTool returns opts rewritten to request structured output
// through a synthetic JSON response tool when the model cannot combine a JSON
// response format with function tools, plus the tool's name ("" when no
// rewrite is needed). Gemini models before Gemini 3 reject JSON response
// formatting together with function tools; Gemini 3 supports the combination
// only with automatic tool choice, so a forced tool choice (required / a
// specific tool) also needs the response tool (TS #21989).
func (m *LanguageModel) withJSONResponseTool(opts *provider.GenerateOptions) (*provider.GenerateOptions, string) {
	if !providerutils.NeedsJSONResponseTool(opts.ResponseFormat, opts.Tools) || !jsonResponseToolModelPattern.MatchString(m.modelID) {
		return opts, ""
	}
	forced := opts.ToolChoice.Type == types.ToolChoiceRequired || opts.ToolChoice.Type == types.ToolChoiceTool
	if GetModelCapabilities(m.modelID).UsesGemini3Features && !forced {
		return opts, ""
	}
	responseTool := providerutils.NewJSONResponseTool(opts.ResponseFormat, opts.Tools)
	rewritten := *opts
	rewritten.ResponseFormat = nil
	rewritten.Tools = append(append([]types.Tool(nil), opts.Tools...), responseTool)
	rewritten.ToolChoice = providerutils.JSONResponseToolChoice(opts.ToolChoice, responseTool.Name)
	return &rewritten, responseTool.Name
}

// PrepareBatchRequestBody builds the GenerateContent request body and
// warnings for opts, without performing the request. Exposes the otherwise
// unexported buildRequest to batch-processing callers in other packages
// (google.Batch / googlevertex.Batch), mirroring TS's static
// GoogleLanguageModel.prepareRequest used by GoogleBatch.
func (m *LanguageModel) PrepareBatchRequestBody(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	body, _, warnings, err := m.buildRequest(context.Background(), opts, false)
	return body, warnings, err
}

// ConvertBatchResponse converts a raw GenerateContent response into a
// GenerateResult for batch-processing callers in other packages. Batch
// results are retrieved independently of the original request, so there is
// no original tool list to map provider tool names against.
func (m *LanguageModel) ConvertBatchResponse(response Response) *types.GenerateResult {
	return m.convertResponse(response, newToolNameMapping(nil))
}

// HandleError exposes the otherwise unexported handleError to callers in
// other packages (google.Batch / googlevertex.Batch).
func (m *LanguageModel) HandleError(err error) error {
	return m.handleError(err)
}

// googleErrorData mirrors TS googleErrorDataSchema (google-error.ts):
// {"error":{"code","message","status","details"}}.
type googleErrorData struct {
	Error struct {
		Code    *int          `json:"code"`
		Message string        `json:"message"`
		Status  string        `json:"status"`
		Details []interface{} `json:"details,omitempty"`
	} `json:"error"`
}

// parseGoogleErrorData validates body against TS googleErrorDataSchema:
// error.code must be present (number or null), error.message and
// error.status must be strings, error.details an array when not null.
func parseGoogleErrorData(body []byte, out *googleErrorData) bool {
	var raw struct {
		Error *struct {
			Code    json.RawMessage `json:"code"`
			Message *string         `json:"message"`
			Status  *string         `json:"status"`
			Details json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &raw) != nil || raw.Error == nil || raw.Error.Message == nil || raw.Error.Status == nil || len(raw.Error.Code) == 0 {
		return false
	}
	var code *float64
	if json.Unmarshal(raw.Error.Code, &code) != nil {
		return false
	}
	var details []interface{}
	if len(raw.Error.Details) > 0 && json.Unmarshal(raw.Error.Details, &details) != nil {
		return false
	}
	if code != nil {
		c := int(*code)
		out.Error.Code = &c
	}
	out.Error.Message = *raw.Error.Message
	out.Error.Status = *raw.Error.Status
	out.Error.Details = details
	return true
}

// generateID returns a fresh ID for a server tool call/source, using the
// configured generator when set (TS: `config.generateId()`), falling back to
// the shared streaming ID generator otherwise.
func (m *LanguageModel) generateID() string {
	if m.cfg.GenerateID != nil {
		return m.cfg.GenerateID()
	}
	return streaming.GenerateID()
}

// handleError wraps a low-level error into a provider error, parsing the
// Google {error:{code,message,status,details}} JSON body when present
// (TS googleFailedResponseHandler / createJsonErrorResponseHandler).
func (m *LanguageModel) handleError(err error) error {
	var loadKeyErr *providererrors.LoadAPIKeyError
	if errors.As(err, &loadKeyErr) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var httpErr *internalhttp.HTTPStatusError
	var existing *providererrors.ProviderError
	if errors.As(err, &existing) && existing.Message == "Invalid JSON response" {
		return err
	}
	if !errors.As(err, &httpErr) {
		return providererrors.NewProviderError(m.cfg.ProviderName, 0, "", err.Error(), err)
	}

	return GoogleHTTPError(m.cfg.ProviderName, httpErr.StatusCode, httpErr.Headers, httpErr.Body, err)
}

// GoogleHTTPError builds the ProviderError for a failed Google API response
// (TS googleFailedResponseHandler): the message is error.message when the body
// matches googleErrorDataSchema, otherwise the HTTP status text.
func GoogleHTTPError(providerName string, status int, headers http.Header, body []byte, cause error) *providererrors.ProviderError {
	message := http.StatusText(status)
	if message == "" && cause != nil {
		message = cause.Error()
	}
	var data googleErrorData
	schemaOK := parseGoogleErrorData(body, &data)
	if schemaOK {
		message = data.Error.Message
	} else {
		data = googleErrorData{}
	}

	perr := providererrors.NewProviderError(providerName, status, data.Error.Status, message, cause)
	perr.ResponseBody = string(body)
	if len(headers) > 0 {
		perr.ResponseHeaders = providerutils.ExtractHeaders(headers)
	}
	if schemaOK {
		perr.Data = data
	}
	return perr
}

// convertResponse converts a Gemini API Response to a GenerateResult.
func (m *LanguageModel) convertResponse(response Response, tnm toolNameMapping) *types.GenerateResult {
	return m.convertResponseFull(response, tnm, "", nil)
}

// convertResponseFull converts a Gemini API Response. jsonResponseToolName is
// set when the request used a JSON response tool (see withJSONResponseTool):
// conversational text is dropped and the response tool's call becomes the
// text content. initialPromptTokenCount is set for a response assembled from
// several continued long-decode requests (the first request's prompt token
// count, exposed in provider metadata).
func (m *LanguageModel) convertResponseFull(response Response, tnm toolNameMapping, jsonResponseToolName string, initialPromptTokenCount *int) *types.GenerateResult {
	return m.convertResponseWithGrounding(response, tnm, jsonResponseToolName, initialPromptTokenCount, nil)
}

// convertResponseWithGrounding is convertResponseFull plus
// groundingMetadataPerRequest: for an automatically continued response, each
// request's grounding snapshot in order (null for requests that returned
// none). nil omits the provider metadata field.
func (m *LanguageModel) convertResponseWithGrounding(response Response, tnm toolNameMapping, jsonResponseToolName string, initialPromptTokenCount *int, groundingPerRequest []json.RawMessage) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertUsage(response.UsageMetadata),
		RawResponse: response,
	}

	// TS reads candidates?.[0]: a response without candidates (a blocked
	// prompt, for one) still maps usage, finish reason and metadata.
	var candidate Candidate
	if len(response.Candidates) > 0 {
		candidate = response.Candidates[0]
	}

	var textParts []string
	// addToolCall records a tool call both on result.ToolCalls and, in
	// position, on result.Content, so the content order matches the order of
	// the response parts (TS builds one ordered content array).
	addToolCall := func(tc types.ToolCall) {
		result.ToolCalls = append(result.ToolCalls, tc)
		result.Content = append(result.Content, toolCallContentOf(tc))
	}
	var lastCodeExecID string
	var lastServerToolCallID string

	for _, part := range candidate.Content.Parts {
		// inlineData parts carry their own thoughtSignature in
		// providerMetadata.google (TS: hasThoughtSignature ? wrap(...)).
		if part.InlineData != nil {
			var fileMeta json.RawMessage
			if part.ThoughtSignature != "" {
				fileMeta, _ = json.Marshal(m.cfg.wrapProviderMetadata(map[string]interface{}{
					"thoughtSignature": part.ThoughtSignature,
				}))
			}
			if part.Thought {
				// Thought inlineData → ReasoningFileContent.
				result.Content = append(result.Content, types.ReasoningFileContent{
					MediaType:        part.InlineData.MimeType,
					Data:             decodeInlineData(part.InlineData.Data),
					ProviderMetadata: fileMeta,
				})
			} else {
				// Non-thought inlineData → GeneratedFileContent.
				result.Content = append(result.Content, types.GeneratedFileContent{
					MediaType:        part.InlineData.MimeType,
					Data:             decodeInlineData(part.InlineData.Data),
					ProviderMetadata: fileMeta,
				})
			}
			continue
		}
		// Thought text → ReasoningContent.
		if part.Thought {
			var sigMeta json.RawMessage
			if part.ThoughtSignature != "" {
				// TS carries the signature in providerMetadata.google
				// .thoughtSignature; Signature stays for existing callers.
				sigMeta, _ = json.Marshal(m.cfg.wrapProviderMetadata(map[string]interface{}{
					"thoughtSignature": part.ThoughtSignature,
				}))
			}
			if part.Text != "" {
				result.Content = append(result.Content, types.ReasoningContent{
					Text:             part.Text,
					Signature:        part.ThoughtSignature,
					ProviderMetadata: sigMeta,
				})
			} else if sigMeta != nil && !part.textAbsent {
				attachSignatureToLastContent(result, sigMeta)
			}
			continue
		}
		// Code execution parts. TS parses these for both Google and Vertex.
		if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
			toolCallID := m.generateID()
			lastCodeExecID = toolCallID
			addToolCall(types.ToolCall{
				ID:               toolCallID,
				ToolName:         tnm.toCustomToolName("code_execution"),
				Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
				RawArguments:     executableCodeInput(part.ExecutableCode.Language, part.ExecutableCode.Code),
				ProviderExecuted: true,
			})
			continue
		}
		if part.CodeExecutionResult != nil && lastCodeExecID != "" {
			result.Content = append(result.Content, types.ToolResultContent{
				ToolCallID: lastCodeExecID,
				ToolName:   tnm.toCustomToolName("code_execution"),
				Result: map[string]interface{}{
					"outcome": part.CodeExecutionResult.Outcome,
					"output":  part.CodeExecutionResult.Output,
				},
			})
			// Do not clear lastCodeExecID: TS associates a result only with the
			// most recently seen executable code part, but does not reset the
			// pointer, matching google-language-model.ts convertGenerateContentResponse.
			continue
		}
		// Regular text → TextContent (ThoughtSignature forwarded via ProviderMetadata).
		// TS: a text part (the key present with a value) is skipped when the
		// request used a JSON response tool, whatever its text.
		if jsonResponseToolName != "" && part.FunctionCall == nil && part.ToolCall == nil && part.ToolResponse == nil &&
			(part.Text != "" || !part.textAbsent) {
			continue
		}
		if part.Text == "" && !part.textAbsent && part.ThoughtSignature != "" && part.FunctionCall == nil && part.ToolCall == nil && part.ToolResponse == nil {
			// TS: an empty text part carrying a thoughtSignature attaches it
			// to the previous content part.
			meta, _ := json.Marshal(m.cfg.wrapProviderMetadata(map[string]interface{}{
				"thoughtSignature": part.ThoughtSignature,
			}))
			attachSignatureToLastContent(result, meta)
		}
		if part.Text != "" {
			textParts = append(textParts, part.Text)
			tc := types.TextContent{Text: part.Text}
			if part.ThoughtSignature != "" {
				meta, _ := json.Marshal(m.cfg.wrapProviderMetadata(map[string]interface{}{
					"thoughtSignature": part.ThoughtSignature,
				}))
				tc.ProviderMetadata = meta
			}
			result.Content = append(result.Content, tc)
		}
		// Function calls.
		if part.FunctionCall != nil && jsonResponseToolName != "" && part.FunctionCall.Name == jsonResponseToolName {
			tc := types.TextContent{Text: completeCallArgsJSON(part.FunctionCall)}
			if part.ThoughtSignature != "" {
				tc.ProviderMetadata, _ = json.Marshal(m.cfg.wrapProviderMetadata(map[string]interface{}{ //nolint:errcheck
					"thoughtSignature": part.ThoughtSignature,
				}))
			}
			textParts = append(textParts, tc.Text)
			result.Content = append(result.Content, tc)
			continue
		}
		// TS: `'functionCall' in part && part.functionCall.name != null`; a
		// call without a name is dropped.
		if part.FunctionCall != nil && part.FunctionCall.Name != "" {
			args := part.FunctionCall.Args
			if args == nil {
				args = map[string]interface{}{}
			}
			var providerMetadata map[string]interface{}
			if part.ThoughtSignature != "" {
				providerMetadata = m.cfg.wrapProviderMetadata(map[string]interface{}{
					"thoughtSignature": part.ThoughtSignature,
				})
			}
			// TS: functionCall.id || generateId(). The tool name is not an id:
			// parallel calls to one function would collide on it.
			toolCallID := part.FunctionCall.ID
			if toolCallID == "" {
				toolCallID = m.generateID()
			}
			addToolCall(types.ToolCall{
				ID:               toolCallID,
				ToolName:         part.FunctionCall.Name,
				Arguments:        args,
				RawArguments:     completeCallArgsJSON(part.FunctionCall),
				ProviderMetadata: providerMetadata,
				ThoughtSignature: part.ThoughtSignature,
			})
		}
		// Server-executed built-in tool call/result (distinct from
		// FunctionCall, which is user-invoked). TS: `'toolCall' in part` /
		// `'toolResponse' in part`.
		if part.ToolCall != nil {
			toolCallID := part.ToolCall.ID
			if toolCallID == "" {
				toolCallID = m.generateID()
			}
			lastServerToolCallID = toolCallID
			args := part.ToolCall.Args
			if args == nil {
				args = map[string]interface{}{}
			}
			meta := map[string]interface{}{
				"serverToolCallId": toolCallID,
				"serverToolType":   part.ToolCall.ToolType,
			}
			if part.ThoughtSignature != "" {
				meta["thoughtSignature"] = part.ThoughtSignature
			}
			addToolCall(types.ToolCall{
				ID:               toolCallID,
				ToolName:         "server:" + part.ToolCall.ToolType,
				Arguments:        args,
				RawArguments:     jsonorder.Text(json.RawMessage(part.toolCallArgsRaw), args),
				ProviderExecuted: true,
				Dynamic:          true,
				ProviderMetadata: m.cfg.wrapProviderMetadata(meta),
			})
		}
		if part.ToolResponse != nil {
			toolCallID := lastServerToolCallID
			if toolCallID == "" {
				toolCallID = part.ToolResponse.ID
			}
			if toolCallID == "" {
				toolCallID = m.generateID()
			}
			resultValue := part.ToolResponse.Response
			if resultValue == nil {
				resultValue = map[string]interface{}{}
			}
			meta := map[string]interface{}{
				"serverToolCallId": toolCallID,
				"serverToolType":   part.ToolResponse.ToolType,
			}
			if part.ThoughtSignature != "" {
				meta["thoughtSignature"] = part.ThoughtSignature
			}
			metaJSON, _ := json.Marshal(m.cfg.wrapProviderMetadata(meta))
			result.Content = append(result.Content, types.ToolResultContent{
				ToolCallID:       toolCallID,
				ToolName:         "server:" + part.ToolResponse.ToolType,
				Result:           resultValue,
				ProviderMetadata: metaJSON,
			})
			lastServerToolCallID = ""
		}
	}

	if len(textParts) > 0 {
		// TS text = every text part joined (a candidate may carry several).
		result.Text = strings.Join(textParts, "")
	}

	// A confirmed prompt block (promptFeedback.blockReason, excluding the
	// "unspecified" sentinels) is terminal when no candidate finishReason was
	// given, and always maps to content-filter (TS isPromptBlocked).
	confirmedPromptBlockReason := promptFeedbackBlockReason(response.PromptFeedback)
	if !isConfirmedPromptBlockReason(confirmedPromptBlockReason) {
		confirmedPromptBlockReason = ""
	}
	isPromptBlocked := candidate.FinishReason == "" && confirmedPromptBlockReason != ""

	for _, src := range extractSources(candidate.GroundingMetadata, m.generateID) {
		result.Content = append(result.Content, src)
	}

	// Finish reason.
	// Only client-executed tool calls count (TS: tool-call && !providerExecuted);
	// a provider-executed code execution call still finishes with "stop".
	hasToolCalls := false
	for _, tc := range result.ToolCalls {
		if !tc.ProviderExecuted {
			hasToolCalls = true
			break
		}
	}
	switch {
	case isPromptBlocked:
		result.FinishReason = types.FinishReasonContentFilter
	default:
		switch candidate.FinishReason {
		case "STOP":
			if hasToolCalls {
				result.FinishReason = types.FinishReasonToolCalls
			} else {
				result.FinishReason = types.FinishReasonStop
			}
		case "MAX_TOKENS", "CONTINUATION":
			// CONTINUATION (Gemini 4 long decode): the per-request cap was
			// reached while budget remains. Continued automatically; only
			// surfaced if continuing fails.
			result.FinishReason = types.FinishReasonLength
		case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			result.FinishReason = types.FinishReasonContentFilter
		case "MALFORMED_FUNCTION_CALL":
			result.FinishReason = types.FinishReasonError
		default:
			result.FinishReason = types.FinishReasonOther
		}
	}
	// Mirrors TS's rawFinishReason = candidate?.finishReason ?? confirmedPromptBlockReason.
	if candidate.FinishReason != "" {
		result.RawFinishReason = candidate.FinishReason
	} else {
		result.RawFinishReason = confirmedPromptBlockReason
	}

	if response.ResponseID != "" {
		result.ResponseMetadata = &types.ResponseMetadata{ID: response.ResponseID}
	}

	// ProviderMetadata is always fully populated (null for absent fields), to
	// match TS GoogleProviderMetadata, and is written under every configured
	// metadata key (Vertex writes both "googleVertex" and "vertex").
	meta := map[string]json.RawMessage{}
	meta["promptFeedback"] = rawOrNull(response.PromptFeedback)
	meta["groundingMetadata"] = rawOrNull(candidate.GroundingMetadata)
	if groundingPerRequest != nil {
		if gp, err := json.Marshal(groundingPerRequest); err == nil {
			meta["groundingMetadataPerRequest"] = gp
		}
	}
	meta["urlContextMetadata"] = rawOrNull(candidate.UrlContextMetadata)
	meta["safetyRatings"] = rawOrNull(candidate.SafetyRatings)
	if candidate.FinishMessage != "" {
		fm, _ := json.Marshal(candidate.FinishMessage)
		meta["finishMessage"] = fm
	} else {
		meta["finishMessage"] = json.RawMessage("null")
	}
	serviceTier := ""
	if response.UsageMetadata != nil {
		if um, err := json.Marshal(response.UsageMetadata); err == nil {
			meta["usageMetadata"] = um
		}
		serviceTier = response.UsageMetadata.ServiceTier
	} else {
		meta["usageMetadata"] = json.RawMessage("null")
	}
	if serviceTier == "" {
		serviceTier = response.ServiceTier
	}
	if serviceTier != "" {
		st, _ := json.Marshal(serviceTier)
		meta["serviceTier"] = st
	} else {
		meta["serviceTier"] = json.RawMessage("null")
	}
	if candidate.ContinuationToken != "" {
		ct, _ := json.Marshal(candidate.ContinuationToken)
		meta["continuationToken"] = ct
	}
	if initialPromptTokenCount != nil {
		ip, _ := json.Marshal(*initialPromptTokenCount)
		meta["initialPromptTokenCount"] = ip
	}
	result.ProviderMetadata = m.cfg.wrapProviderMetadata(meta)

	return result
}

// rawOrNull returns raw, or the JSON null literal when raw is empty. Used to
// keep provider metadata objects fully populated (TS `field ?? null`).
func rawOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

// toolCallContentOf is the ordered-content form of a ToolCall.
// executableCodeInput is JSON.stringify(part.executableCode): the parsed
// object has its schema key order, language then code.
func executableCodeInput(language, code string) string {
	return `{"language":` + marshalJSONCompact(language) + `,"code":` + marshalJSONCompact(code) + `}`
}

func toolCallContentOf(tc types.ToolCall) types.ToolCallContent {
	var meta json.RawMessage
	if len(tc.ProviderMetadata) > 0 {
		meta, _ = json.Marshal(tc.ProviderMetadata)
	}
	return types.ToolCallContent{
		ToolCallID:       tc.ID,
		ToolName:         tc.ToolName,
		Input:            tc.RawArguments,
		Arguments:        tc.Arguments,
		ProviderExecuted: tc.ProviderExecuted,
		ProviderMetadata: meta,
		Dynamic:          tc.Dynamic,
		ThoughtSignature: tc.ThoughtSignature,
	}
}

// attachSignatureToLastContent sets providerMetadata on whatever content part
// came last (TS: lastContent.providerMetadata = thoughtSignatureMetadata): text,
// reasoning, files, sources, tool calls and tool results. For a tool call the
// matching entry of result.ToolCalls is updated too so replays keep the
// signature.
func attachSignatureToLastContent(result *types.GenerateResult, meta json.RawMessage) {
	content := result.Content
	if len(content) == 0 {
		return
	}
	last := len(content) - 1
	switch c := content[last].(type) {
	case types.TextContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.ReasoningContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.GeneratedFileContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.ReasoningFileContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.SourceContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.ToolResultContent:
		c.ProviderMetadata = meta
		content[last] = c
	case types.ToolCallContent:
		c.ProviderMetadata = meta
		var md map[string]interface{}
		if json.Unmarshal(meta, &md) == nil {
			for i := range result.ToolCalls {
				if result.ToolCalls[i].ID == c.ToolCallID {
					result.ToolCalls[i].ProviderMetadata = md
					if sig := signatureFromMetadata(md); sig != "" {
						result.ToolCalls[i].ThoughtSignature = sig
						c.ThoughtSignature = sig
					}
				}
			}
		}
		content[last] = c
	}
}

// signatureFromMetadata reads providerMetadata.<key>.thoughtSignature.
func signatureFromMetadata(md map[string]interface{}) string {
	for _, v := range md {
		if m, ok := v.(map[string]interface{}); ok {
			if s, ok := m["thoughtSignature"].(string); ok {
				return s
			}
		}
	}
	return ""
}
