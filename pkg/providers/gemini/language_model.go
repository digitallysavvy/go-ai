package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
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
	reqBody, headers, warnings, err := m.buildRequest(ctx, opts, false)
	if err != nil {
		return nil, err
	}

	var response Response
	resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    m.cfg.GeneratePath(m.modelID),
		Body:    reqBody,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response, newToolNameMapping(opts.Tools))
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	return result, nil
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
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
	return streaming.NewWarningsStream(stream, warnings), nil
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
	var httpErr *internalhttp.HTTPStatusError
	if !errors.As(err, &httpErr) {
		return providererrors.NewProviderError(m.cfg.ProviderName, 0, "", err.Error(), err)
	}

	message := err.Error()
	var data googleErrorData
	if jsonErr := json.Unmarshal(httpErr.Body, &data); jsonErr == nil && data.Error.Message != "" {
		message = data.Error.Message
	}

	perr := providererrors.NewProviderError(m.cfg.ProviderName, httpErr.StatusCode, data.Error.Status, message, err)
	perr.ResponseBody = string(httpErr.Body)
	if len(httpErr.Headers) > 0 {
		perr.ResponseHeaders = providerutils.ExtractHeaders(httpErr.Headers)
	}
	if data.Error.Message != "" {
		perr.Data = data
	}
	return perr
}

// convertResponse converts a Gemini API Response to a GenerateResult.
func (m *LanguageModel) convertResponse(response Response, tnm toolNameMapping) *types.GenerateResult {
	result := &types.GenerateResult{
		Usage:       convertUsage(response.UsageMetadata),
		RawResponse: response,
	}

	if len(response.Candidates) == 0 {
		return result
	}
	candidate := response.Candidates[0]

	var textParts []string
	var lastCodeExecID string
	var lastServerToolCallID string

	for _, part := range candidate.Content.Parts {
		// Thought inlineData → ReasoningFileContent.
		if part.Thought && part.InlineData != nil {
			result.Content = append(result.Content, types.ReasoningFileContent{
				MediaType: part.InlineData.MimeType,
				Data:      decodeInlineData(part.InlineData.Data),
			})
			continue
		}
		// Non-thought inlineData → GeneratedFileContent.
		if part.InlineData != nil {
			result.Content = append(result.Content, types.GeneratedFileContent{
				MediaType: part.InlineData.MimeType,
				Data:      decodeInlineData(part.InlineData.Data),
			})
			continue
		}
		// Thought text → ReasoningContent.
		if part.Thought {
			if part.Text != "" {
				result.Content = append(result.Content, types.ReasoningContent{
					Text:      part.Text,
					Signature: part.ThoughtSignature,
				})
			}
			continue
		}
		// Code execution parts. TS parses these for both Google and Vertex.
		if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
			toolCallID := fmt.Sprintf("code-exec-%d", len(result.ToolCalls)+1)
			lastCodeExecID = toolCallID
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:               toolCallID,
				ToolName:         tnm.toCustomToolName("code_execution"),
				Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
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
		if part.FunctionCall != nil {
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
			toolCallID := part.FunctionCall.ID
			if toolCallID == "" {
				toolCallID = part.FunctionCall.Name
			}
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:               toolCallID,
				ToolName:         part.FunctionCall.Name,
				Arguments:        args,
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
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:               toolCallID,
				ToolName:         "server:" + part.ToolCall.ToolType,
				Arguments:        args,
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
		result.Text = textParts[0]
	}

	// A confirmed prompt block (promptFeedback.blockReason, excluding the
	// "unspecified" sentinels) is terminal when no candidate finishReason was
	// given, and always maps to content-filter (TS isPromptBlocked).
	confirmedPromptBlockReason := promptFeedbackBlockReason(response.PromptFeedback)
	if !isConfirmedPromptBlockReason(confirmedPromptBlockReason) {
		confirmedPromptBlockReason = ""
	}
	isPromptBlocked := candidate.FinishReason == "" && confirmedPromptBlockReason != ""

	// Finish reason.
	hasToolCalls := len(result.ToolCalls) > 0
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
		case "MAX_TOKENS":
			result.FinishReason = types.FinishReasonLength
		case "IMAGE_SAFETY", "RECITATION", "SAFETY", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
			result.FinishReason = types.FinishReasonContentFilter
		case "MALFORMED_FUNCTION_CALL":
			result.FinishReason = types.FinishReasonError
		default:
			result.FinishReason = types.FinishReasonOther
		}
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
