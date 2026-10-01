package huggingface

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModel implements provider.LanguageModel using the Hugging Face
// Router Responses API (POST /responses), mirroring TS
// HuggingFaceResponsesLanguageModel.
type LanguageModel struct {
	provider *Provider
	modelID  string
}

// NewLanguageModel creates a new Hugging Face Responses language model.
func NewLanguageModel(p *Provider, modelID string) *LanguageModel {
	return &LanguageModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version ("v4", matching TS
// LanguageModelV4).
func (m *LanguageModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider identifier (TS config.provider =
// 'huggingface.responses').
func (m *LanguageModel) Provider() string { return "huggingface.responses" }

// ModelID returns the model ID.
func (m *LanguageModel) ModelID() string { return m.modelID }

// SupportsTools reports whether the model supports tool calling.
func (m *LanguageModel) SupportsTools() bool { return true }

// SupportsStructuredOutput reports whether the model supports JSON schema output.
func (m *LanguageModel) SupportsStructuredOutput() bool { return true }

// SupportsImageInput reports whether the model accepts image inputs.
func (m *LanguageModel) SupportsImageInput() bool { return true }

// SupportedURLs reports the URL patterns this model can receive directly
// (TS `supportedUrls: {'image/*': [/^https?:\/\/.*$/]}`).
func (m *LanguageModel) SupportedURLs() map[string][]string {
	return map[string][]string{"image/*": {`^https?://.*$`}}
}

// hfProviderOptions mirrors TS HuggingFaceLanguageModelResponsesOptions.
type hfProviderOptions struct {
	Metadata         map[string]string `json:"metadata,omitempty"`
	Instructions     string            `json:"instructions,omitempty"`
	StrictJSONSchema *bool             `json:"strictJsonSchema,omitempty"`
	ReasoningEffort  string            `json:"reasoningEffort,omitempty"`
}

func extractHFProviderOptions(raw map[string]interface{}) *hfProviderOptions {
	if raw == nil {
		return nil
	}
	value, ok := raw[hfProviderOptionsKey]
	if !ok {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var opts hfProviderOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil
	}
	return &opts
}

// getArgs mirrors TS getArgs: it builds the request body shared by
// doGenerate/doStream (before the "stream" flag is added) plus warnings.
func (m *LanguageModel) getArgs(opts *provider.GenerateOptions) (map[string]interface{}, []types.Warning, error) {
	var warnings []types.Warning

	if opts.TopK != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if len(opts.StopSequences) > 0 {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "stopSequences"})
	}

	input, messageWarnings, err := convertToHuggingFaceResponsesInput(opts.Prompt)
	if err != nil {
		return nil, warnings, err
	}
	warnings = append(warnings, messageWarnings...)

	hfOpts := extractHFProviderOptions(opts.ProviderOptions)

	hfTools, toolsPresent, mappedToolChoice, toolWarnings := prepareResponsesTools(opts.Tools, opts.ToolChoice)
	warnings = append(warnings, toolWarnings...)

	body := map[string]interface{}{
		"model": m.modelID,
		"input": input,
	}
	if opts.Temperature != nil {
		body["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		body["top_p"] = *opts.TopP
	}
	if opts.MaxTokens != nil {
		body["max_output_tokens"] = *opts.MaxTokens
	}

	// Hugging Face Responses API uses text.format for structured output.
	if opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" && opts.ResponseFormat.Schema != nil {
		strict := false
		if hfOpts != nil && hfOpts.StrictJSONSchema != nil {
			strict = *hfOpts.StrictJSONSchema
		}
		name := opts.ResponseFormat.Name
		if name == "" {
			name = "response"
		}
		format := map[string]interface{}{
			"type":   "json_schema",
			"strict": strict,
			"name":   name,
			"schema": opts.ResponseFormat.Schema,
		}
		if opts.ResponseFormat.Description != "" {
			format["description"] = opts.ResponseFormat.Description
		}
		body["text"] = map[string]interface{}{"format": format}
	}

	if hfOpts != nil && len(hfOpts.Metadata) > 0 {
		body["metadata"] = hfOpts.Metadata
	}
	if hfOpts != nil && hfOpts.Instructions != "" {
		body["instructions"] = hfOpts.Instructions
	}

	if toolsPresent {
		body["tools"] = hfTools
	}
	if mappedToolChoice != nil {
		body["tool_choice"] = mappedToolChoice
	}

	if hfOpts != nil && hfOpts.ReasoningEffort != "" {
		body["reasoning"] = map[string]interface{}{"effort": hfOpts.ReasoningEffort}
	}

	return body, warnings, nil
}

// DoGenerate performs non-streaming generation via POST /responses.
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	args, warnings, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	for k, v := range args {
		body[k] = v
	}
	body["stream"] = false

	var resp hfResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: opts.Headers,
	}, &resp)
	if err != nil {
		if converted := parseHuggingFaceError(err); converted != nil {
			return nil, converted
		}
		return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
	}

	responseHeaders := providerutils.ExtractHeaders(httpResp.Headers)

	if resp.Error != nil {
		retryable := false
		return nil, &providererrors.ProviderError{
			Provider:        "huggingface",
			StatusCode:      400,
			Message:         resp.Error.Message,
			ResponseHeaders: responseHeaders,
			ResponseBody:    string(httpResp.Body),
			Retryable:       &retryable,
		}
	}

	result := m.convertResponse(resp)
	result.Warnings = warnings
	result.RawRequest = body
	result.RawResponse = resp
	result.ResponseHeaders = responseHeaders
	result.ResponseMetadata = &types.ResponseMetadata{
		ID:        resp.ID,
		Timestamp: time.Unix(resp.CreatedAt, 0).UTC(),
		ModelID:   resp.Model,
		Headers:   responseHeaders,
		Body:      json.RawMessage(httpResp.Body),
	}
	result.ProviderMetadata = map[string]interface{}{
		hfProviderOptionsKey: map[string]interface{}{"responseId": resp.ID},
	}
	return result, nil
}

// convertResponse mirrors TS doGenerate's output-array processing.
func (m *LanguageModel) convertResponse(resp hfResponse) *types.GenerateResult {
	var content []types.ContentPart
	var toolCalls []types.ToolCall
	var textBuilder strings.Builder

	for _, item := range resp.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				textBuilder.WriteString(part.Text)
				content = append(content, types.TextContent{
					Text:             part.Text,
					ProviderMetadata: hfItemMetadata(item.ID),
				})
				for _, ann := range part.Annotations {
					content = append(content, types.SourceContent{
						SourceType: "url",
						ID:         m.provider.generateID(),
						URL:        ann.URL,
						Title:      ann.Title,
					})
				}
			}

		case "reasoning":
			for _, part := range item.Content {
				content = append(content, types.ReasoningContent{
					Text:             part.Text,
					ProviderMetadata: hfItemMetadata(item.ID),
				})
			}

		case "mcp_call":
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			content = append(content, types.ToolCallContent{
				ToolCallID:       item.ID,
				ToolName:         item.Name,
				Input:            item.Arguments,
				ProviderExecuted: true,
			})
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         item.Name,
				Arguments:        args,
				RawArguments:     item.Arguments,
				ProviderExecuted: true,
			})
			if item.Output != "" {
				content = append(content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   item.Name,
					Result:     item.Output,
				})
			}

		case "mcp_list_tools":
			inputJSON, _ := json.Marshal(map[string]interface{}{"server_label": item.ServerLabel})
			content = append(content, types.ToolCallContent{
				ToolCallID:       item.ID,
				ToolName:         "list_tools",
				Input:            string(inputJSON),
				ProviderExecuted: true,
			})
			toolCalls = append(toolCalls, types.ToolCall{
				ID:               item.ID,
				ToolName:         "list_tools",
				Arguments:        map[string]interface{}{"server_label": item.ServerLabel},
				RawArguments:     string(inputJSON),
				ProviderExecuted: true,
			})
			if len(item.Tools) > 0 {
				content = append(content, types.ToolResultContent{
					ToolCallID: item.ID,
					ToolName:   "list_tools",
					Result:     map[string]interface{}{"tools": item.Tools},
				})
			}

		case "function_call":
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(item.Arguments), &args) //nolint:errcheck
			content = append(content, types.ToolCallContent{
				ToolCallID: item.CallID,
				ToolName:   item.Name,
				Input:      item.Arguments,
			})
			toolCalls = append(toolCalls, types.ToolCall{
				ID:           item.CallID,
				ToolName:     item.Name,
				Arguments:    args,
				RawArguments: item.Arguments,
			})
			if item.Output != "" {
				content = append(content, types.ToolResultContent{
					ToolCallID: item.CallID,
					ToolName:   item.Name,
					Result:     item.Output,
				})
			}
		}
	}

	reason := "stop"
	rawFinishReason := ""
	if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
		reason = resp.IncompleteDetails.Reason
		rawFinishReason = resp.IncompleteDetails.Reason
	}

	return &types.GenerateResult{
		Text:            textBuilder.String(),
		Content:         content,
		ToolCalls:       toolCalls,
		FinishReason:    mapHuggingFaceResponsesFinishReason(reason),
		RawFinishReason: rawFinishReason,
		Usage:           convertHuggingFaceResponsesUsage(resp.Usage),
	}
}

// hfItemMetadata builds the {huggingface: {itemId}} provider metadata carried
// on text/reasoning content parts.
func hfItemMetadata(itemID string) json.RawMessage {
	raw, err := json.Marshal(map[string]interface{}{
		hfProviderOptionsKey: map[string]interface{}{"itemId": itemID},
	})
	if err != nil {
		return nil
	}
	return raw
}

// DoStream performs streaming generation via POST /responses (stream: true),
// returning a real SSE-backed TextStream.
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	args, warnings, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{}
	for k, v := range args {
		body[k] = v
	}
	body["stream"] = true

	headers := internalhttp.MergeHeaders(map[string]string{"Accept": "text/event-stream"}, opts.Headers)

	httpResp, err := m.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/responses",
		Body:    body,
		Headers: headers,
	})
	if err != nil {
		if converted := parseHuggingFaceError(err); converted != nil {
			return nil, converted
		}
		return nil, providererrors.NewProviderError("huggingface", 0, "", err.Error(), err)
	}

	s := newHFStream(httpResp.Body, warnings, m.provider.responsesProviderName())
	s.requestBody = body
	s.responseHeaders = providerutils.ExtractHeaders(httpResp.Header)
	return s, nil
}

// responsesProviderName returns the provider name attached to
// StreamProviderError values built for this model's streams (TS's `this
// .provider`).
func (p *Provider) responsesProviderName() string {
	return "huggingface.responses"
}
