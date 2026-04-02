package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
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
func (m *LanguageModel) SpecificationVersion() string { return "v3" }

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
	reqBody := m.buildRequestBody(opts)

	var response Response
	resp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   m.cfg.GeneratePath(m.modelID),
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	return result, nil
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	reqBody := m.buildRequestBody(opts)

	httpResp, err := m.cfg.Client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   m.cfg.StreamPath(m.modelID),
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	return providerutils.WithResponseMetadata(newStream(httpResp.Body, m.cfg), httpResp.Header), nil
}

// handleError wraps a low-level error into a provider error.
func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError(m.cfg.ProviderName, 0, "", err.Error(), err)
}

// getProviderOpts returns the first matching provider options map from
// GenerateOptions.ProviderOptions using the configured key precedence order.
func (m *LanguageModel) getProviderOpts(opts *provider.GenerateOptions) map[string]interface{} {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	for _, key := range m.cfg.ProviderOptionsKeys {
		if v, ok := opts.ProviderOptions[key].(map[string]interface{}); ok {
			return v
		}
	}
	return nil
}

// buildRequestBody builds the Gemini API request body from GenerateOptions.
func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) map[string]interface{} {
	body := map[string]interface{}{}

	// Messages / simple prompt → contents.
	if opts.Prompt.IsMessages() {
		body["contents"] = prompt.ToGoogleMessages(opts.Prompt.Messages, m.supportsFunctionResponseParts())
	} else if opts.Prompt.IsSimple() {
		body["contents"] = prompt.ToGoogleMessages(prompt.SimpleTextToMessages(opts.Prompt.Text), false)
	}

	// System instruction — skipped for Gemma models.
	if opts.Prompt.System != "" && !isGemmaModel(m.modelID) {
		body["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]interface{}{
				{"text": opts.Prompt.System},
			},
		}
	}

	// Generation config.
	genConfig := map[string]interface{}{}
	if opts.Temperature != nil {
		genConfig["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *opts.MaxTokens
	}
	if opts.TopP != nil {
		genConfig["topP"] = *opts.TopP
	}
	if opts.TopK != nil {
		genConfig["topK"] = *opts.TopK
	}
	if len(opts.StopSequences) > 0 {
		genConfig["stopSequences"] = opts.StopSequences
	}
	if opts.FrequencyPenalty != nil {
		genConfig["frequencyPenalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		genConfig["presencePenalty"] = *opts.PresencePenalty
	}
	if opts.Seed != nil {
		genConfig["seed"] = *opts.Seed
	}

	// Reasoning → thinkingConfig.
	// Gemini 3 (non-image) uses thinkingLevel strings; Gemini 2.x uses thinkingBudget.
	// A call-level Reasoning value takes precedence over provider options.
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		if isGemini3Model(m.modelID) && !isImageModel(m.modelID) {
			genConfig["thinkingConfig"] = map[string]interface{}{
				"thinkingLevel": mapReasoningToGemini3Level(*opts.Reasoning),
			}
		} else {
			switch *opts.Reasoning {
			case types.ReasoningNone:
				genConfig["thinkingConfig"] = map[string]interface{}{"thinkingBudget": 0}
			default:
				maxOut := 0
				if opts.MaxTokens != nil {
					maxOut = *opts.MaxTokens
				}
				genConfig["thinkingConfig"] = map[string]interface{}{
					"thinkingBudget": mapReasoningBudget(*opts.Reasoning, maxOut, m.modelID),
				}
			}
		}
	} else {
		// Fall back to thinkingConfig from provider options.
		if provOpts := m.getProviderOpts(opts); provOpts != nil {
			if tc, ok := provOpts["thinkingConfig"].(map[string]interface{}); ok {
				genConfig["thinkingConfig"] = tc
			}
		}
	}

	// JSON response format and optional schema.
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json_object" {
		genConfig["responseMimeType"] = "application/json"
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" {
		genConfig["responseMimeType"] = "application/json"
		if opts.ResponseFormat.Schema != nil {
			structuredOutputs := true
			if provOpts := m.getProviderOpts(opts); provOpts != nil {
				if so, ok := provOpts["structuredOutputs"].(bool); ok {
					structuredOutputs = so
				}
			}
			if structuredOutputs {
				genConfig["responseSchema"] = opts.ResponseFormat.Schema
			}
		}
	}

	// Forward additional provider options into generationConfig and the top-level body.
	provOpts := m.getProviderOpts(opts)
	if provOpts != nil {
		for _, key := range []string{"responseModalities", "mediaResolution", "audioTimestamp", "imageConfig"} {
			if v, ok := provOpts[key]; ok {
				genConfig[key] = v
			}
		}
		if v, ok := provOpts["safetySettings"]; ok {
			body["safetySettings"] = v
		}
		if v, ok := provOpts["cachedContent"]; ok {
			body["cachedContent"] = v
		}
		if v, ok := provOpts["labels"]; ok {
			body["labels"] = v
		}
		if rc, ok := provOpts["retrievalConfig"]; ok {
			// retrievalConfig is merged into toolConfig when present alongside native tools.
			// Store it temporarily; the tools section below will pick it up.
			body["_retrievalConfig"] = rc
		}
		// serviceTier: forward to top-level request body (not generationConfig).
		// Allowed values: SERVICE_TIER_STANDARD, SERVICE_TIER_FLEX, SERVICE_TIER_PRIORITY.
		if st, ok := provOpts["serviceTier"].(string); ok && st != "" {
			body["serviceTier"] = st
		}
	}

	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	// Tools.
	if len(opts.Tools) > 0 {
		var functionTools []types.Tool
		var nativeEntries []map[string]interface{}

		for _, t := range opts.Tools {
			if t.Type == "provider" {
				if entry := buildNativeToolEntry(t); entry != nil {
					nativeEntries = append(nativeEntries, entry)
				}
			} else {
				functionTools = append(functionTools, t)
			}
		}

		if len(nativeEntries) > 0 {
			body["tools"] = nativeEntries
			if rc, ok := body["_retrievalConfig"]; ok {
				body["toolConfig"] = map[string]interface{}{"retrievalConfig": rc}
			}
		} else if len(functionTools) > 0 {
			body["tools"] = []map[string]interface{}{
				{"functionDeclarations": tool.ToGoogleFormat(functionTools)},
			}
			if tc := m.buildFunctionCallingConfig(functionTools, opts); tc != nil {
				body["toolConfig"] = tc
			}
		}
	}
	delete(body, "_retrievalConfig")

	return body
}

// buildFunctionCallingConfig builds the functionCallingConfig map for the toolConfig
// field. Mirrors TS prepareTools logic exactly.
func (m *LanguageModel) buildFunctionCallingConfig(functionTools []types.Tool, opts *provider.GenerateOptions) map[string]interface{} {
	hasStrictTools := false
	for _, t := range functionTools {
		if t.Strict {
			hasStrictTools = true
			break
		}
	}

	var mode string
	var allowedFunctionNames []string
	if opts.ToolChoice != (types.ToolChoice{}) {
		switch opts.ToolChoice.Type {
		case types.ToolChoiceNone:
			mode = "NONE"
		case types.ToolChoiceRequired:
			if hasStrictTools {
				mode = "VALIDATED"
			} else {
				mode = "ANY"
			}
		case types.ToolChoiceTool:
			if hasStrictTools {
				mode = "VALIDATED"
			} else {
				mode = "ANY"
			}
			allowedFunctionNames = []string{opts.ToolChoice.ToolName}
		default: // auto
			if hasStrictTools {
				mode = "VALIDATED"
			} else {
				mode = "AUTO"
			}
		}
	} else if hasStrictTools {
		mode = "VALIDATED"
	}

	if mode == "" {
		return nil
	}
	fcConfig := map[string]interface{}{"mode": mode}
	if len(allowedFunctionNames) > 0 {
		fcConfig["allowedFunctionNames"] = allowedFunctionNames
	}
	return map[string]interface{}{"functionCallingConfig": fcConfig}
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
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:               part.FunctionCall.Name,
				ToolName:         part.FunctionCall.Name,
				Arguments:        part.FunctionCall.Args,
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
	}
	// serviceTier is always emitted (null when absent) to match TS SDK behavior:
	// `serviceTier: response.serviceTier ?? null`
	if response.ServiceTier != "" {
		if st, err := json.Marshal(response.ServiceTier); err == nil {
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

// supportsFunctionResponseParts reports whether the model supports multimodal
// content in tool result function responses. Only Gemini 3+ models support this.
func (m *LanguageModel) supportsFunctionResponseParts() bool {
	return isGemini3Model(m.modelID)
}

// isImageModel reports whether the model ID indicates an image-specialized model
// that does not support extended thinking (e.g. gemini-3-pro-image-*).
func isImageModel(modelID string) bool {
	return strings.Contains(modelID, "image")
}
