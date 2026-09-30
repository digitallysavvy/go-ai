package xai

import (
	"context"
	"encoding/json"
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
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements provider.LanguageModel for Grok models served
// through Google Vertex AI's MaaS OpenAI-compatible Chat Completions
// endpoint. Mirrors the language/chat model TS builds via
// `@ai-sdk/openai-compatible`'s createOpenAICompatible in
// google-vertex-xai-provider.ts (supportsStructuredOutputs: true,
// includeUsage: true, transformRequestBody stripping reasoning_effort,
// convertUsage splitting cache/reasoning tokens, supportedUrls for image/*).
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version.
func (m *LanguageModel) SpecificationVersion() string { return "v3" }

// Provider returns the provider name. Matches TS's `name: 'googleVertex.xai'`.
func (m *LanguageModel) Provider() string { return "googleVertex.xai" }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports tool-calling support (standard Chat Completions tools).
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports json_schema structured-output support.
// Matches TS's `supportsStructuredOutputs: true` passed to
// createOpenAICompatible (unconditional, unlike e.g. Together AI's
// per-model allowlist).
func (m *LanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports image input support. Matches TS's
// `supportedUrls: () => ({'image/*': [...]})`, which implies the endpoint
// accepts image content (message conversion always supports image_url parts;
// SupportedURLs below governs whether a raw URL can be sent directly instead
// of being downloaded).
func (m *LanguageModel) SupportsImageInput() bool { return true }

// SupportedURLs reports the URL patterns this model can receive directly
// without downloading, mirroring TS's `supportedUrls: () =>
// ({'image/*': [/^https?:\/\/.*$/]})` exactly.
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*": {`^https?://.*$`},
	}
}

// DoGenerate performs non-streaming text generation.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	warnings := m.checkUnsupportedOptions(opts)
	reqBody := m.buildRequestBody(opts, false)
	var response vertexXaiResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
		Body:   reqBody,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	result := m.convertResponse(response)
	result.Warnings = append(warnings, result.Warnings...)
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Headers)
	responseMetadata := providerutils.BuildResponseMetadata(response.ID, response.Model, response.Created)
	responseMetadata.Headers = result.ResponseHeaders
	result.ResponseMetadata = responseMetadata
	return result, nil
}

// DoStream performs streaming text generation.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	warnings := m.checkUnsupportedOptions(opts)
	reqBody := m.buildRequestBody(opts, true)
	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/chat/completions",
		Body:   reqBody,
		Headers: map[string]string{
			"Accept": "text/event-stream",
		},
	})
	if err != nil {
		return nil, m.handleError(err)
	}
	inner := newVertexXaiStream(httpResp.Body)
	inner.SetRequestBody(reqBody)
	inner.IncludeRawChunks = opts.IncludeRawChunks
	inner.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return streaming.NewWarningsStream(inner, warnings), nil
}

// checkUnsupportedOptions returns warnings for options the Chat Completions
// API does not support. Mirrors the generic openai-compatible chat model's
// unconditional topK warning (e.g. pkg/providers/mistral's
// checkReasoningWarnings).
func (m *LanguageModel) checkUnsupportedOptions(opts *provider.GenerateOptions) []types.Warning {
	var warnings []types.Warning
	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	return warnings
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions, stream bool) map[string]interface{} {
	body := map[string]interface{}{
		"model": m.modelID,
	}
	if stream {
		body["stream"] = true
		// includeUsage: true (TS createOpenAICompatible option) -- request
		// usage in the final streaming chunk.
		body["stream_options"] = map[string]interface{}{"include_usage": true}
	}
	if opts.Prompt.IsMessages() {
		body["messages"] = prompt.ToOpenAIMessages(opts.Prompt.Messages)
	} else if opts.Prompt.IsSimple() {
		body["messages"] = prompt.ToOpenAIMessages(prompt.SimpleTextToMessages(opts.Prompt.Text))
	}
	if opts.Prompt.System != "" {
		messages, _ := body["messages"].([]map[string]interface{})
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
	if opts.FrequencyPenalty != nil {
		body["frequency_penalty"] = *opts.FrequencyPenalty
	}
	if opts.PresencePenalty != nil {
		body["presence_penalty"] = *opts.PresencePenalty
	}
	if len(opts.StopSequences) > 0 {
		body["stop"] = opts.StopSequences
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}
	if len(opts.Tools) > 0 {
		body["tools"] = tool.ToOpenAIFormat(opts.Tools)
		if opts.ToolChoice.Type != "" {
			body["tool_choice"] = tool.ConvertToolChoiceToOpenAI(opts.ToolChoice)
		}
	}
	responseFormat, _ := providerutils.ChatResponseFormat(opts.ResponseFormat, providerutils.ChatResponseFormatOptions{
		StructuredOutputs:         true,
		WarnWhenSchemaUnsupported: true,
	})
	if responseFormat != nil {
		body["response_format"] = responseFormat
	}

	// Generic openai-compatible provider options
	// (providerOptions.googleVertex / .xai), including reasoning_effort --
	// which is then unconditionally stripped below, mirroring TS
	// transformGoogleVertexXaiRequestBody exactly: Vertex's Grok MaaS
	// endpoint rejects the field even though xAI's own API and the generic
	// openai-compatible mapping both support it.
	compatibleOptions, _ := providerutils.ResolveOpenAICompatibleProviderOptions("googleVertex", opts.ProviderOptions)
	xaiOptions, _ := providerutils.ResolveOpenAICompatibleProviderOptions("xai", opts.ProviderOptions)
	for k, v := range xaiOptions {
		compatibleOptions[k] = v
	}
	providerutils.ApplyOpenAICompatibleCommonRequestOptions(body, compatibleOptions)
	if effort := reasoningEffortFromTopLevel(opts.Reasoning); effort != "" {
		if _, alreadySet := body["reasoning_effort"]; !alreadySet {
			body["reasoning_effort"] = effort
		}
	}

	return stripReasoningEffort(body)
}

// reasoningEffortFromTopLevel maps the shared opts.Reasoning level to the
// Grok Chat API's "low"/"high" values (there is no "medium"; minimal/low/
// medium collapse to "low", high/xhigh collapse to "high", none omits the
// field), matching the reasoning-effort mapping the pre-removal
// pkg/providers/xai Chat Completions model used. The result is stripped by
// stripReasoningEffort regardless -- see buildRequestBody -- since Vertex
// rejects it either way; this mapping only matters if that Vertex-side
// restriction is ever lifted.
func reasoningEffortFromTopLevel(level *types.ReasoningLevel) string {
	if level == nil {
		return ""
	}
	switch *level {
	case types.ReasoningMinimal, types.ReasoningLow, types.ReasoningMedium:
		return "low"
	case types.ReasoningHigh, types.ReasoningXHigh:
		return "high"
	}
	return ""
}

// stripReasoningEffort removes the "reasoning_effort" field from the
// request body. Mirrors TS transformGoogleVertexXaiRequestBody.
func stripReasoningEffort(body map[string]interface{}) map[string]interface{} {
	delete(body, "reasoning_effort")
	return body
}

func (m *LanguageModel) convertResponse(response vertexXaiResponse) *types.GenerateResult {
	if len(response.Choices) == 0 {
		return &types.GenerateResult{
			Text:         "",
			FinishReason: types.FinishReasonOther,
			Usage:        convertVertexXaiUsage(response.Usage),
			RawResponse:  response,
		}
	}
	choice := response.Choices[0]
	result := &types.GenerateResult{
		Text:         choice.Message.Content,
		FinishReason: providerutils.MapOpenAIFinishReason(choice.FinishReason),
		Usage:        convertVertexXaiUsage(response.Usage),
		RawResponse:  response,
	}
	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]types.ToolCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args) //nolint:errcheck
			}
			result.ToolCalls[i] = types.ToolCall{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: args,
			}
		}
	}
	return result
}

func (m *LanguageModel) handleError(err error) error {
	return providererrors.NewProviderError("googleVertex.xai", 0, "", err.Error(), err)
}

// convertVertexXaiUsage mirrors TS convertGoogleVertexXaiUsage exactly:
// cache-read tokens are additional detail *within* prompt_tokens (noCache =
// promptTokens - cacheReadTokens, unconditionally -- no inclusive/exclusive
// branching), and reasoning tokens are *additional to* completion_tokens
// (outputTokens.total = completionTokens + reasoningTokens).
func convertVertexXaiUsage(usage vertexXaiUsage) types.Usage {
	promptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	var cacheReadTokens int64
	if usage.PromptTokensDetails != nil && usage.PromptTokensDetails.CachedTokens != nil {
		cacheReadTokens = *usage.PromptTokensDetails.CachedTokens
	}
	var reasoningTokens int64
	if usage.CompletionTokensDetails != nil && usage.CompletionTokensDetails.ReasoningTokens != nil {
		reasoningTokens = *usage.CompletionTokensDetails.ReasoningTokens
	}

	noCache := promptTokens - cacheReadTokens
	outputTotal := completionTokens + reasoningTokens
	totalTokens := promptTokens + outputTotal

	raw := map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
	}
	if usage.PromptTokensDetails != nil {
		raw["prompt_tokens_details"] = usage.PromptTokensDetails
	}
	if usage.CompletionTokensDetails != nil {
		raw["completion_tokens_details"] = usage.CompletionTokensDetails
	}

	return types.Usage{
		InputTokens: &promptTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:   &noCache,
			CacheReadTokens: &cacheReadTokens,
		},
		OutputTokens: &outputTotal,
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &completionTokens,
			ReasoningTokens: &reasoningTokens,
		},
		TotalTokens: &totalTokens,
		Raw:         raw,
	}
}

// vertexXaiResponse is the Vertex MaaS Chat Completions response shape
// (standard OpenAI-compatible Chat Completions).
type vertexXaiResponse struct {
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
	Usage vertexXaiUsage `json:"usage"`
}

// vertexXaiUsage mirrors TS GoogleVertexXaiUsage.
type vertexXaiUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens *int64 `json:"cached_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens,omitempty"`
	} `json:"completion_tokens_details,omitempty"`
}

// vertexXaiStream embeds the shared OpenAI-compatible SSE stream parser,
// adding a response-metadata peek at the first chunk (id/model/created),
// matching other embedders of streaming.OpenAICompatStream (e.g.
// pkg/providers/together).
type vertexXaiStream struct {
	*streaming.OpenAICompatStream
	responseHeaders         map[string]string
	responseMetadataEmitted bool
}

func newVertexXaiStream(reader io.ReadCloser) *vertexXaiStream {
	s := &vertexXaiStream{
		OpenAICompatStream: streaming.NewOpenAICompatStream(reader, providerutils.MapOpenAIFinishReason),
	}
	s.OnBeforeDelta = func(data []byte) []*provider.StreamChunk {
		var peek struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Created int64  `json:"created"`
		}
		if json.Unmarshal(data, &peek) != nil {
			return nil
		}
		if s.responseMetadataEmitted || (peek.ID == "" && peek.Model == "" && peek.Created == 0 && len(s.responseHeaders) == 0) {
			return nil
		}
		s.responseMetadataEmitted = true
		meta := &provider.ResponseMetadata{
			ID:      peek.ID,
			ModelID: peek.Model,
			Headers: s.responseHeaders,
		}
		if peek.Created != 0 {
			meta.Timestamp = time.Unix(peek.Created, 0)
		}
		return []*provider.StreamChunk{{
			Type:             provider.ChunkTypeResponseMetadata,
			ResponseMetadata: meta,
		}}
	}
	return s
}
