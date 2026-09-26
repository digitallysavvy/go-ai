package gemini

import (
	"context"
	"encoding/json"
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
	result := m.convertResponse(response)
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
	stream := providerutils.WithResponseMetadata(newStream(httpResp.Body, m.cfg), httpResp.Header, m.ModelID())
	return streaming.NewWarningsStream(stream, warnings), nil
}

// handleError wraps a low-level error into a provider error.
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError(m.cfg.ProviderName, 0, "", err.Error(), err)
}

// convertResponse converts a Gemini API Response to a GenerateResult.
func (m *LanguageModel) convertResponse(response Response) *types.GenerateResult {
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
		// Code execution parts (Google only; safe to check because Part fields are nil on Vertex).
		if m.cfg.SupportsCodeExecution {
			if part.ExecutableCode != nil && part.ExecutableCode.Code != "" {
				toolCallID := fmt.Sprintf("code-exec-%d", len(result.ToolCalls)+1)
				lastCodeExecID = toolCallID
				result.ToolCalls = append(result.ToolCalls, types.ToolCall{
					ID:               toolCallID,
					ToolName:         "code_execution",
					Arguments:        map[string]interface{}{"code": part.ExecutableCode.Code, "language": part.ExecutableCode.Language},
					ProviderExecuted: true,
				})
				continue
			}
			if part.CodeExecutionResult != nil && lastCodeExecID != "" {
				result.Content = append(result.Content, types.ToolResultContent{
					ToolCallID: lastCodeExecID,
					ToolName:   "code_execution",
					Result: map[string]interface{}{
						"outcome": part.CodeExecutionResult.Outcome,
						"output":  part.CodeExecutionResult.Output,
					},
				})
				lastCodeExecID = ""
				continue
			}
		}
		// Regular text → TextContent (ThoughtSignature forwarded via ProviderMetadata).
		if part.Text != "" {
			textParts = append(textParts, part.Text)
			tc := types.TextContent{Text: part.Text}
			if part.ThoughtSignature != "" {
				meta, _ := json.Marshal(map[string]interface{}{
					m.cfg.MetadataKey: map[string]interface{}{
						"thoughtSignature": part.ThoughtSignature,
					},
				})
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
				providerMetadata = map[string]interface{}{
					m.cfg.MetadataKey: map[string]interface{}{
						"thoughtSignature": part.ThoughtSignature,
					},
				}
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
	}

	if len(textParts) > 0 {
		result.Text = textParts[0]
	}

	// Finish reason.
	hasToolCalls := len(result.ToolCalls) > 0
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

	// ProviderMetadata — assembled under the configured metadata key.
	meta := map[string]json.RawMessage{}
	if response.PromptFeedback != nil {
		meta["promptFeedback"] = response.PromptFeedback
	}
	if candidate.GroundingMetadata != nil {
		meta["groundingMetadata"] = candidate.GroundingMetadata
	}
	if candidate.UrlContextMetadata != nil {
		meta["urlContextMetadata"] = candidate.UrlContextMetadata
	}
	if candidate.SafetyRatings != nil {
		meta["safetyRatings"] = candidate.SafetyRatings
	}
	if candidate.FinishMessage != "" {
		if fm, err := json.Marshal(candidate.FinishMessage); err == nil {
			meta["finishMessage"] = fm
		}
	}
	if response.UsageMetadata != nil {
		if um, err := json.Marshal(response.UsageMetadata); err == nil {
			meta["usageMetadata"] = um
		}
		if mtc, err := json.Marshal(modalityTokenCounts(response.UsageMetadata)); err == nil {
			meta["modalityTokenCounts"] = mtc
		}
	}
	// serviceTier is always emitted (null when absent) to match TS SDK behavior:
	// `serviceTier: response.serviceTier ?? null`
	serviceTier := ""
	if response.UsageMetadata != nil {
		serviceTier = response.UsageMetadata.ServiceTier
	}
	if serviceTier == "" {
		serviceTier = response.ServiceTier
	}
	if serviceTier != "" {
		if st, err := json.Marshal(serviceTier); err == nil {
			meta["serviceTier"] = st
		}
	} else {
		meta["serviceTier"] = json.RawMessage("null")
	}
	if len(meta) > 0 {
		result.ProviderMetadata = map[string]interface{}{
			m.cfg.MetadataKey: meta,
		}
	}

	return result
}
