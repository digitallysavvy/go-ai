package bedrock

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// LanguageModel implements the provider.LanguageModel interface for AWS Bedrock
type LanguageModel struct {
	provider *Provider
	modelID  string
	options  *ModelOptions
}

// NewLanguageModel creates a new AWS Bedrock language model
func NewLanguageModel(provider *Provider, modelID string, options ...*ModelOptions) *LanguageModel {
	var opts *ModelOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &LanguageModel{
		provider: provider,
		modelID:  modelID,
		options:  opts,
	}
}

// SpecificationVersion returns the specification version
func (m *LanguageModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *LanguageModel) Provider() string {
	return "aws-bedrock"
}

// ModelID returns the model ID
func (m *LanguageModel) ModelID() string {
	return m.modelID
}

// SupportsTools returns whether the model supports tool calling
func (m *LanguageModel) SupportsTools() bool {
	// Claude models on Bedrock support tools
	return true
}

// SupportsStructuredOutput returns whether the model supports structured output
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}

// SupportsImageInput returns whether the model accepts image inputs
func (m *LanguageModel) SupportsImageInput() bool {
	// Claude models on Bedrock support vision
	return true
}

// DoGenerate performs non-streaming text generation
func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	reqBody, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine the model provider (Claude, Llama, etc.)
	endpoint := m.getInvokeEndpoint()

	// Create HTTP request
	url := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com%s", m.provider.config.Region, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	creds, err := m.provider.resolveCredentials(ctx)
	if err != nil {
		return nil, err
	}

	// Sign the request with AWS Signature V4
	signer := NewAWSSigner(
		creds.AccessKeyID,
		creds.SecretAccessKey,
		creds.SessionToken,
		m.provider.config.Region,
	)

	if err := signer.SignRequest(req, bodyBytes); err != nil {
		return nil, fmt.Errorf("failed to sign request: %w", err)
	}

	// Make the request using provider-scoped transport so telemetry/patching applies.
	httpClient := m.provider.Client().HTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError("aws-bedrock", 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LAWS Bedrock API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	result, err := m.convertResponse(respBody)
	if err != nil {
		return nil, err
	}
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Header)
	return result, nil
}

// DoStream performs streaming text generation
func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	// For simplicity, simulate streaming by chunking the non-streaming response
	result, err := m.DoGenerate(ctx, opts)
	if err != nil {
		return nil, err
	}

	stream := &bedrockStream{
		result:   result,
		position: 0,
		done:     false,
	}

	return stream, nil
}

func (m *LanguageModel) getInvokeEndpoint() string {
	// Bedrock model invocation endpoint
	return fmt.Sprintf("/model/%s/invoke", m.modelID)
}

func (m *LanguageModel) buildRequestBody(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	// Different Bedrock models have different request formats
	// We'll support Claude models (most common on Bedrock)
	if strings.Contains(m.modelID, "anthropic") || strings.Contains(m.modelID, "claude") {
		return m.buildClaudeRequest(opts)
	}

	// Amazon Nova models use an OpenAI-compatible format
	if strings.HasPrefix(m.modelID, "amazon.nova") {
		return m.buildNovaRequest(opts)
	}

	// Default format for other models
	return m.buildGenericRequest(opts)
}

// mapReasoningToBedrockAnthropicConfig converts a ReasoningLevel to the Bedrock
// Anthropic-style reasoningConfig map. Returns nil when level is ReasoningDefault
// (meaning "don't override").
func mapReasoningToBedrockAnthropicConfig(level types.ReasoningLevel) map[string]interface{} {
	switch level {
	case types.ReasoningNone:
		return map[string]interface{}{"type": "disabled"}
	case types.ReasoningMinimal:
		return map[string]interface{}{"type": "enabled", "budgetTokens": 1024}
	case types.ReasoningLow:
		return map[string]interface{}{"type": "enabled", "budgetTokens": 4000}
	case types.ReasoningMedium:
		return map[string]interface{}{"type": "enabled", "budgetTokens": 10000}
	case types.ReasoningHigh:
		return map[string]interface{}{"type": "enabled", "budgetTokens": 16000}
	case types.ReasoningXHigh:
		return map[string]interface{}{"type": "enabled", "budgetTokens": 32000}
	default:
		// ReasoningDefault: omit
		return nil
	}
}

// mapReasoningToOpenAIEffort converts a ReasoningLevel to the OpenAI-style
// reasoning_effort string. Returns "" when level is ReasoningDefault (omit).
func mapReasoningToOpenAIEffort(level types.ReasoningLevel) string {
	switch level {
	case types.ReasoningNone:
		return "disabled"
	case types.ReasoningMinimal, types.ReasoningLow:
		return "low"
	case types.ReasoningMedium:
		return "medium"
	case types.ReasoningHigh, types.ReasoningXHigh:
		return "high"
	default:
		// ReasoningDefault: omit
		return ""
	}
}

func isMistralModel(modelID string) bool {
	return strings.Contains(modelID, "mistral.")
}

func normalizeToolCallID(toolCallID string, isMistral bool) string {
	if !isMistral {
		return toolCallID
	}
	var b strings.Builder
	b.Grow(9)
	for _, r := range toolCallID {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 9 {
				break
			}
		}
	}
	return b.String()
}

func (m *LanguageModel) buildClaudeRequest(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	messages := []map[string]interface{}{}
	systemBlocks := []map[string]interface{}{}
	documentCounter := 0
	if opts.Prompt.System != "" {
		systemBlocks = append(systemBlocks, map[string]interface{}{"text": opts.Prompt.System})
	}

	if opts.Prompt.IsMessages() {
		seenNonSystem := false
		for i := 0; i < len(opts.Prompt.Messages); {
			msg := opts.Prompt.Messages[i]
			switch msg.Role {
			case types.RoleSystem:
				if seenNonSystem {
					return nil, fmt.Errorf("multiple system messages that are separated by user/assistant messages are not supported")
				}
				systemBlocks = append(systemBlocks, m.toClaudeSystemContent(msg)...)
				i++
			case types.RoleUser, types.RoleTool:
				seenNonSystem = true
				content := []map[string]interface{}{}
				for i < len(opts.Prompt.Messages) && (opts.Prompt.Messages[i].Role == types.RoleUser || opts.Prompt.Messages[i].Role == types.RoleTool) {
					blocks, err := m.toClaudeMessageContent(opts.Prompt.Messages[i], false, &documentCounter)
					if err != nil {
						return nil, err
					}
					content = append(content, blocks...)
					i++
				}
				messages = append(messages, map[string]interface{}{
					"role":    types.RoleUser,
					"content": content,
				})
			case types.RoleAssistant:
				seenNonSystem = true
				groupEnd := i
				for groupEnd < len(opts.Prompt.Messages) && opts.Prompt.Messages[groupEnd].Role == types.RoleAssistant {
					groupEnd++
				}
				isLastBlock := groupEnd == len(opts.Prompt.Messages)
				content := []map[string]interface{}{}
				for i < groupEnd {
					blocks, err := m.toClaudeMessageContent(opts.Prompt.Messages[i], isLastBlock && i == groupEnd-1, &documentCounter)
					if err != nil {
						return nil, err
					}
					content = append(content, blocks...)
					i++
				}
				messages = append(messages, map[string]interface{}{
					"role":    types.RoleAssistant,
					"content": content,
				})
			default:
				return nil, fmt.Errorf("unsupported role: %s", msg.Role)
			}
		}
	} else if opts.Prompt.IsSimple() {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": []map[string]interface{}{{"text": opts.Prompt.Text}},
		})
	}

	reqBody := map[string]interface{}{
		"messages":          messages,
		"anthropic_version": "bedrock-2023-05-31",
	}
	if len(systemBlocks) > 0 {
		reqBody["system"] = systemBlocks
	}
	for k, v := range m.bedrockProviderOptions(opts) {
		if k == "serviceTier" || k == "reasoningConfig" || k == "additionalModelRequestFields" {
			continue
		}
		reqBody[k] = v
	}
	if m.options != nil {
		for k, v := range m.options.AdditionalModelRequestFields {
			reqBody[k] = v
		}
	}

	if opts.MaxTokens != nil {
		reqBody["max_tokens"] = *opts.MaxTokens
	} else {
		reqBody["max_tokens"] = 4096 // Claude requires max_tokens
	}

	if opts.Temperature != nil {
		reqBody["temperature"] = *opts.Temperature
	}

	if opts.TopP != nil {
		reqBody["top_p"] = *opts.TopP
	}

	// Map top-level Reasoning to Bedrock Anthropic-style reasoningConfig and
	// merge partial caller/model config over the derived defaults.
	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault {
		if rc := mapReasoningToBedrockAnthropicConfig(*opts.Reasoning); rc != nil {
			reqBody["reasoningConfig"] = rc
		}
	} else if m.options != nil && m.options.Thinking != nil {
		// Fall back to model-level Thinking option (existing behavior)
		thinkingConfig := map[string]interface{}{
			"type": string(m.options.Thinking.Type),
		}
		if m.options.Thinking.Type == ThinkingTypeEnabled && m.options.Thinking.BudgetTokens != nil {
			thinkingConfig["budget_tokens"] = *m.options.Thinking.BudgetTokens
		}
		reqBody["thinking"] = thinkingConfig
	}
	if rc := m.mergedReasoningConfig(opts, reqBody["reasoningConfig"]); rc != nil {
		reqBody["reasoningConfig"] = rc
	}
	if opts.ResponseFormat != nil && opts.ResponseFormat.Schema != nil &&
		(opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema") {
		outputConfig := map[string]interface{}{}
		if existing, ok := reqBody["output_config"].(map[string]interface{}); ok {
			outputConfig = cloneMap(existing)
		}
		outputConfig["format"] = map[string]interface{}{
			"type":   "json_schema",
			"schema": opts.ResponseFormat.Schema,
		}
		reqBody["output_config"] = outputConfig
	}

	if serviceTier := m.serviceTier(opts); serviceTier != "" {
		reqBody["serviceTier"] = map[string]interface{}{"type": serviceTier}
	}

	// Add tools and tool choice (#12893 strict mode, #12854 tool choice enforcement).
	// When toolChoice is "none" the tools array is cleared and no toolChoice is sent.
	if len(opts.Tools) > 0 && opts.ToolChoice.Type != types.ToolChoiceNone {
		// Filter to the single requested tool when a specific tool is named.
		toolList := opts.Tools
		if opts.ToolChoice.Type == types.ToolChoiceTool && opts.ToolChoice.ToolName != "" {
			filtered := make([]types.Tool, 0, 1)
			for _, t := range opts.Tools {
				if t.Name == opts.ToolChoice.ToolName {
					filtered = append(filtered, t)
				}
			}
			toolList = filtered
		}

		bedrockTools := make([]interface{}, 0, len(toolList))
		for _, t := range toolList {
			if providerTool := bedrockAnthropicProviderTool(t); providerTool != nil {
				bedrockTools = append(bedrockTools, providerTool)
				continue
			}
			toolSpec := map[string]interface{}{
				"name": t.Name,
			}
			if t.Description != "" {
				toolSpec["description"] = t.Description
			}
			if t.Strict {
				toolSpec["strict"] = true
			}
			var inputSchema interface{}
			if t.Parameters != nil {
				inputSchema = t.Parameters
			} else {
				inputSchema = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
			}
			toolSpec["inputSchema"] = map[string]interface{}{"json": inputSchema}
			bedrockTools = append(bedrockTools, map[string]interface{}{"toolSpec": toolSpec})
		}
		reqBody["tools"] = bedrockTools

		// Map SDK tool choice to Bedrock's format.
		switch opts.ToolChoice.Type {
		case types.ToolChoiceAuto:
			reqBody["toolChoice"] = map[string]interface{}{"auto": map[string]interface{}{}}
		case types.ToolChoiceRequired:
			reqBody["toolChoice"] = map[string]interface{}{"any": map[string]interface{}{}}
		case types.ToolChoiceTool:
			reqBody["toolChoice"] = map[string]interface{}{
				"tool": map[string]interface{}{"name": opts.ToolChoice.ToolName},
			}
		}
	}

	return reqBody, nil
}

func (m *LanguageModel) toClaudeSystemContent(msg types.Message) []map[string]interface{} {
	blocks := []map[string]interface{}{}
	for _, part := range msg.Content {
		if text, ok := part.(types.TextContent); ok {
			blocks = append(blocks, map[string]interface{}{"text": text.Text})
			blocks = appendCachePointBlock(blocks, text.ProviderOptions)
		}
	}
	blocks = appendCachePointBlock(blocks, msg.ProviderOptions)
	return blocks
}

func (m *LanguageModel) toClaudeMessageContent(msg types.Message, trimFinalAssistantText bool, documentCounter *int) ([]map[string]interface{}, error) {
	blocks := make([]map[string]interface{}, 0, len(msg.Content))
	normalizeMistralToolIDs := isMistralModel(m.modelID)
	hasReasoningBlocks := false
	for _, part := range msg.Content {
		if _, ok := part.(types.ReasoningContent); ok {
			hasReasoningBlocks = true
			break
		}
	}

	for i, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			partText := p.Text
			if msg.Role == types.RoleAssistant && strings.TrimSpace(partText) == "" && !hasReasoningBlocks {
				blocks = appendCachePointBlock(blocks, p.ProviderOptions)
				break
			}
			if msg.Role == types.RoleAssistant && trimFinalAssistantText && i == len(msg.Content)-1 {
				partText = strings.TrimSpace(partText)
			}
			blocks = append(blocks, map[string]interface{}{"text": partText})
			blocks = appendCachePointBlock(blocks, p.ProviderOptions)
		case types.ImageContent:
			if p.Image == nil {
				if p.URL != "" {
					return nil, fmt.Errorf("image URL data is not supported")
				}
				return nil, fmt.Errorf("image content requires inline data")
			}
			format, err := bedrockImageFormat(p.MimeType)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, map[string]interface{}{
				"image": map[string]interface{}{
					"format": format,
					"source": map[string]interface{}{"bytes": base64.StdEncoding.EncodeToString(p.Image)},
				},
			})
			blocks = appendCachePointBlock(blocks, p.ProviderOptions)
		case types.FileContent:
			block, ok, err := bedrockFileBlock(p, documentCounter)
			if err != nil {
				return nil, err
			}
			if ok {
				blocks = append(blocks, block)
				blocks = appendCachePointBlock(blocks, p.ProviderOptions)
			}
		case types.ReasoningContent:
			signature, hasSignature, redactedData, hasRedactedData := bedrockReasoningMetadata(p)
			if hasSignature {
				blocks = append(blocks, map[string]interface{}{
					"reasoningContent": map[string]interface{}{
						"reasoningText": map[string]interface{}{
							"text":      p.Text,
							"signature": signature,
						},
					},
				})
			} else if hasRedactedData {
				blocks = append(blocks, map[string]interface{}{
					"reasoningContent": map[string]interface{}{
						"redactedReasoning": map[string]interface{}{"data": redactedData},
					},
				})
			}
			blocks = appendCachePointBlock(blocks, p.ProviderOptions)
		case types.ToolCallContent:
			input := map[string]interface{}{}
			if p.Input != "" {
				_ = json.Unmarshal([]byte(p.Input), &input)
			}
			if p.Arguments != nil {
				input = p.Arguments
			}
			blocks = append(blocks, map[string]interface{}{
				"toolUse": map[string]interface{}{
					"toolUseId": normalizeToolCallID(p.ToolCallID, normalizeMistralToolIDs),
					"name":      p.ToolName,
					"input":     input,
				},
			})
			blocks = appendCachePointBlock(blocks, p.ProviderOptions)
		case types.ToolResultContent:
			content, err := bedrockToolResultContent(p)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, map[string]interface{}{
				"toolResult": map[string]interface{}{
					"toolUseId": normalizeToolCallID(p.ToolCallID, normalizeMistralToolIDs),
					"content":   content,
				},
			})
			blocks = appendCachePointBlock(blocks, p.ProviderOptions)
		default:
			return nil, fmt.Errorf("unsupported content part type: %s", part.ContentType())
		}
	}
	if hasBedrockCachePoint(msg.ProviderOptions) {
		blocks = appendCachePointBlock(blocks, msg.ProviderOptions)
	}

	return blocks, nil
}

func bedrockToolResultContent(part types.ToolResultContent) ([]map[string]interface{}, error) {
	if part.Output == nil {
		if part.Error != "" {
			return []map[string]interface{}{{"text": part.Error}}, nil
		}
		if part.Result != nil {
			return []map[string]interface{}{{"text": fmt.Sprint(part.Result)}}, nil
		}
		return []map[string]interface{}{{"text": ""}}, nil
	}
	switch part.Output.Type {
	case types.ToolResultOutputContent:
		out := make([]map[string]interface{}, 0, len(part.Output.Content))
		for _, block := range part.Output.Content {
			switch b := block.(type) {
			case types.TextContentBlock:
				out = append(out, map[string]interface{}{"text": b.Text})
			case types.ImageContentBlock:
				format, err := bedrockImageFormat(b.MediaType)
				if err != nil {
					return nil, err
				}
				out = append(out, map[string]interface{}{
					"image": map[string]interface{}{
						"format": format,
						"source": map[string]interface{}{"bytes": base64.StdEncoding.EncodeToString(b.Data)},
					},
				})
			case types.FileContentBlock:
				mediaType, payload, err := toolResultFileContentPayload(b)
				if err != nil {
					return nil, err
				}
				mediaType, err = resolveBedrockDataMediaType(mediaType, payload)
				if err != nil {
					return nil, err
				}
				if !strings.HasPrefix(mediaType, "image/") {
					return nil, fmt.Errorf("unsupported tool result media type: %s", mediaType)
				}
				format, err := bedrockImageFormat(mediaType)
				if err != nil {
					return nil, err
				}
				out = append(out, map[string]interface{}{
					"image": map[string]interface{}{
						"format": format,
						"source": map[string]interface{}{"bytes": payload.bytes},
					},
				})
			default:
				return nil, fmt.Errorf("unsupported tool content part type: %s", block.ToolResultContentType())
			}
		}
		return out, nil
	case types.ToolResultOutputText, types.ToolResultOutputError:
		return []map[string]interface{}{{"text": fmt.Sprint(part.Output.Value)}}, nil
	case types.ToolResultOutputExecutionDenied:
		reason := part.Output.Reason
		if reason == "" {
			reason = "Tool call execution denied."
		}
		return []map[string]interface{}{{"text": reason}}, nil
	case types.ToolResultOutputJSON:
		fallthrough
	default:
		data, err := json.Marshal(part.Output.Value)
		if err != nil {
			return []map[string]interface{}{{"text": fmt.Sprint(part.Output.Value)}}, nil
		}
		return []map[string]interface{}{{"text": string(data)}}, nil
	}
}

func bedrockProviderOptionMaps(providerOptions map[string]interface{}) []map[string]interface{} {
	maps := make([]map[string]interface{}, 0, 2)
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		maps = append(maps, raw)
	}
	return maps
}

func bedrockProviderOptionMap(providerOptions map[string]interface{}, providerName string) map[string]interface{} {
	raw, ok := providerOptions[providerName].(map[string]interface{})
	if !ok {
		return nil
	}
	return raw
}

func bedrockCachePoint(providerOptions map[string]interface{}) (map[string]interface{}, bool) {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		cachePoint, ok := raw["cachePoint"]
		if !ok || cachePoint == nil {
			continue
		}
		if disabled, ok := cachePoint.(bool); ok && !disabled {
			return nil, false
		}
		if cfg, ok := cachePoint.(map[string]interface{}); ok {
			return cloneMap(cfg), true
		}
		return map[string]interface{}{"type": "default"}, true
	}
	return nil, false
}

func hasBedrockCachePoint(providerOptions map[string]interface{}) bool {
	_, ok := bedrockCachePoint(providerOptions)
	return ok
}

func bedrockReasoningMetadata(part types.ReasoningContent) (signature string, hasSignature bool, redactedData string, hasRedactedData bool) {
	signature = part.Signature
	hasSignature = part.Signature != ""
	redactedData = part.RedactedData
	hasRedactedData = part.RedactedData != ""
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(part.ProviderOptions, providerName)
		if raw == nil {
			continue
		}
		if value, ok := raw["signature"].(string); ok {
			signature = value
			hasSignature = true
		}
		if value, ok := raw["redactedData"].(string); ok {
			redactedData = value
			hasRedactedData = true
		}
		break
	}
	return signature, hasSignature, redactedData, hasRedactedData
}

func appendCachePointBlock(blocks []map[string]interface{}, providerOptions map[string]interface{}) []map[string]interface{} {
	cachePoint, ok := bedrockCachePoint(providerOptions)
	if !ok {
		return blocks
	}
	return append(blocks, map[string]interface{}{
		"cachePoint": cachePoint,
	})
}

func bedrockCitationsEnabled(providerOptions map[string]interface{}) bool {
	for _, providerName := range []string{"amazonBedrock", "bedrock"} {
		raw := bedrockProviderOptionMap(providerOptions, providerName)
		if raw == nil {
			continue
		}
		citations, ok := raw["citations"].(map[string]interface{})
		if !ok {
			return false
		}
		enabled, _ := citations["enabled"].(bool)
		return enabled
	}
	return false
}

func fileContentMediaType(file types.FileContent) string {
	if file.MediaType != "" {
		return file.MediaType
	}
	if file.MimeType != "" {
		return file.MimeType
	}
	return file.FileData.MediaType
}

type bedrockFilePayload struct {
	bytes       string
	raw         []byte
	hasRawBytes bool
	hasData     bool
}

func fileContentPayload(file types.FileContent) bedrockFilePayload {
	if file.Data != nil {
		return bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(file.Data), raw: file.Data, hasRawBytes: true, hasData: true}
	}
	switch file.FileData.Type {
	case types.FileDataTypeData:
		if file.FileData.DataString != "" {
			payload := bedrockFilePayload{bytes: file.FileData.DataString, hasData: true}
			if decoded, err := types.DecodeFileDataString(file.FileData.DataString); err == nil {
				payload.raw = decoded
				payload.hasRawBytes = true
			}
			return payload
		}
		if file.FileData.Data != nil {
			return bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(file.FileData.Data), raw: file.FileData.Data, hasRawBytes: true, hasData: true}
		}
		return bedrockFilePayload{hasRawBytes: true, hasData: true}
	case types.FileDataTypeText:
		raw := []byte(file.FileData.Text)
		return bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(raw), raw: raw, hasRawBytes: true, hasData: true}
	}
	if file.FileData.Data != nil {
		return bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(file.FileData.Data), raw: file.FileData.Data, hasRawBytes: true, hasData: true}
	}
	if file.Text != "" {
		raw := []byte(file.Text)
		return bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(raw), raw: raw, hasRawBytes: true, hasData: true}
	}
	return bedrockFilePayload{}
}

func toolResultFileContentPayload(block types.FileContentBlock) (string, bedrockFilePayload, error) {
	if block.Reference != "" || block.FileData.Type == types.FileDataTypeReference {
		return "", bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data of type: reference")
	}
	if block.URL != "" || block.FileData.Type == types.FileDataTypeURL {
		return "", bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data of type: url")
	}

	mediaType := block.MediaType
	if mediaType == "" {
		mediaType = block.FileData.MediaType
	}

	if block.Data != nil {
		return mediaType, bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(block.Data), raw: block.Data, hasRawBytes: true, hasData: true}, nil
	}
	if block.FileData.Type == types.FileDataTypeData {
		if block.FileData.DataString != "" {
			payload := bedrockFilePayload{bytes: block.FileData.DataString, hasData: true}
			if decoded, err := types.DecodeFileDataString(block.FileData.DataString); err == nil {
				payload.raw = decoded
				payload.hasRawBytes = true
			}
			return mediaType, payload, nil
		}
		if block.FileData.Data != nil {
			return mediaType, bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(block.FileData.Data), raw: block.FileData.Data, hasRawBytes: true, hasData: true}, nil
		}
		return mediaType, bedrockFilePayload{hasRawBytes: true, hasData: true}, nil
	}
	if block.FileData.Data != nil {
		return mediaType, bedrockFilePayload{bytes: base64.StdEncoding.EncodeToString(block.FileData.Data), raw: block.FileData.Data, hasRawBytes: true, hasData: true}, nil
	}
	return mediaType, bedrockFilePayload{}, fmt.Errorf("unsupported tool result file data for media type: %s", mediaType)
}

func isFullMediaType(mediaType string) bool {
	slash := strings.Index(mediaType, "/")
	return slash >= 0 && slash < len(mediaType)-1 && mediaType[slash+1:] != "*"
}

func topLevelMediaType(mediaType string) string {
	if slash := strings.Index(mediaType, "/"); slash >= 0 {
		return mediaType[:slash]
	}
	return mediaType
}

func resolveBedrockDataMediaType(mediaType string, payload bedrockFilePayload) (string, error) {
	if isFullMediaType(mediaType) {
		return mediaType, nil
	}
	if !payload.hasRawBytes {
		return "", fmt.Errorf("file of media type %q must specify subtype since it could not be auto-detected", mediaType)
	}
	switch topLevelMediaType(mediaType) {
	case "image":
		switch {
		case len(payload.raw) >= 4 && payload.raw[0] == 0x89 && payload.raw[1] == 0x50 && payload.raw[2] == 0x4e && payload.raw[3] == 0x47:
			return "image/png", nil
		case len(payload.raw) >= 3 && payload.raw[0] == 0x47 && payload.raw[1] == 0x49 && payload.raw[2] == 0x46:
			return "image/gif", nil
		case len(payload.raw) >= 2 && payload.raw[0] == 0xff && payload.raw[1] == 0xd8:
			return "image/jpeg", nil
		case len(payload.raw) >= 12 && payload.raw[0] == 0x52 && payload.raw[1] == 0x49 && payload.raw[2] == 0x46 && payload.raw[3] == 0x46 && payload.raw[8] == 0x57 && payload.raw[9] == 0x45 && payload.raw[10] == 0x42 && payload.raw[11] == 0x50:
			return "image/webp", nil
		}
	case "application":
		if len(payload.raw) >= 4 && payload.raw[0] == 0x25 && payload.raw[1] == 0x50 && payload.raw[2] == 0x44 && payload.raw[3] == 0x46 {
			return "application/pdf", nil
		}
	}
	return "", fmt.Errorf("file of media type %q must specify subtype since it could not be auto-detected", mediaType)
}

func bedrockImageFormat(mediaType string) (string, error) {
	switch mediaType {
	case "image/jpeg":
		return "jpeg", nil
	case "image/png":
		return "png", nil
	case "image/gif":
		return "gif", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image mime type: %s, expected one of: image/jpeg, image/png, image/gif, image/webp", mediaType)
	}
}

func bedrockDocumentFormat(mediaType string) (string, error) {
	switch mediaType {
	case "application/pdf":
		return "pdf", nil
	case "text/csv":
		return "csv", nil
	case "application/msword":
		return "doc", nil
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx", nil
	case "application/vnd.ms-excel":
		return "xls", nil
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx", nil
	case "text/html":
		return "html", nil
	case "text/plain":
		return "txt", nil
	case "text/markdown":
		return "md", nil
	default:
		return "", fmt.Errorf("unsupported file mime type: %s, expected one of: application/pdf, text/csv, application/msword, application/vnd.openxmlformats-officedocument.wordprocessingml.document, application/vnd.ms-excel, application/vnd.openxmlformats-officedocument.spreadsheetml.sheet, text/html, text/plain, text/markdown", mediaType)
	}
}

func bedrockFileBlock(file types.FileContent, documentCounter *int) (map[string]interface{}, bool, error) {
	if file.Reference != "" || file.FileData.Type == types.FileDataTypeReference {
		return nil, false, fmt.Errorf("file parts with provider references are not supported")
	}
	if file.URL != "" || file.FileData.Type == types.FileDataTypeURL {
		return nil, false, fmt.Errorf("file URL data is not supported")
	}
	mediaType := fileContentMediaType(file)
	payload := fileContentPayload(file)
	if file.Text != "" || file.FileData.Type == types.FileDataTypeText {
		if !isFullMediaType(mediaType) {
			mediaType = "text/plain"
		}
	} else if payload.hasData {
		resolved, err := resolveBedrockDataMediaType(mediaType, payload)
		if err != nil {
			return nil, false, err
		}
		mediaType = resolved
	}
	if !payload.hasData {
		return nil, false, nil
	}
	if strings.HasPrefix(mediaType, "image/") {
		format, err := bedrockImageFormat(mediaType)
		if err != nil {
			return nil, false, err
		}
		return map[string]interface{}{
			"image": map[string]interface{}{
				"format": format,
				"source": map[string]interface{}{"bytes": payload.bytes},
			},
		}, true, nil
	}
	format, err := bedrockDocumentFormat(mediaType)
	if err != nil {
		return nil, false, err
	}
	name := stripBedrockFileExtension(file.Filename)
	if name == "" {
		if documentCounter != nil {
			(*documentCounter)++
			name = fmt.Sprintf("document-%d", *documentCounter)
		} else {
			name = "document-1"
		}
	}
	document := map[string]interface{}{
		"format": format,
		"name":   name,
		"source": map[string]interface{}{"bytes": payload.bytes},
	}
	if bedrockCitationsEnabled(file.ProviderOptions) {
		document["citations"] = map[string]interface{}{"enabled": true}
	}
	return map[string]interface{}{"document": document}, true, nil
}

func stripBedrockFileExtension(filename string) string {
	if dot := strings.Index(filename, "."); dot >= 0 {
		return filename[:dot]
	}
	return filename
}

func (m *LanguageModel) buildGenericRequest(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	var prompt string
	if opts.Prompt.IsMessages() {
		for _, msg := range opts.Prompt.Messages {
			content := ""
			for _, c := range msg.Content {
				if tc, ok := c.(types.TextContent); ok {
					content += tc.Text
				}
			}
			prompt += fmt.Sprintf("%s: %s\n", msg.Role, content)
		}
	} else if opts.Prompt.IsSimple() {
		prompt = opts.Prompt.Text
	}

	reqBody := map[string]interface{}{
		"prompt": prompt,
	}

	if opts.MaxTokens != nil {
		reqBody["max_tokens"] = *opts.MaxTokens
	}

	if opts.Temperature != nil {
		reqBody["temperature"] = *opts.Temperature
	}

	return reqBody, nil
}

// buildNovaRequest builds a request for Amazon Nova models using OpenAI-compatible format.
// Nova models support reasoning_effort instead of reasoningConfig.
func (m *LanguageModel) buildNovaRequest(opts *provider.GenerateOptions) (map[string]interface{}, error) {
	messages := []map[string]interface{}{}

	if opts.Prompt.IsMessages() {
		for _, msg := range opts.Prompt.Messages {
			content := ""
			for _, c := range msg.Content {
				if tc, ok := c.(types.TextContent); ok {
					content += tc.Text
				}
			}
			messages = append(messages, map[string]interface{}{
				"role":    msg.Role,
				"content": content,
			})
		}
	} else if opts.Prompt.IsSimple() {
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": opts.Prompt.Text,
		})
	}

	reqBody := map[string]interface{}{
		"messages": messages,
	}

	if opts.MaxTokens != nil {
		reqBody["max_tokens"] = *opts.MaxTokens
	}
	if opts.Temperature != nil {
		reqBody["temperature"] = *opts.Temperature
	}

	// Map top-level Reasoning to OpenAI-compatible reasoning_effort for Nova models.
	if opts.Reasoning != nil {
		if effort := mapReasoningToOpenAIEffort(*opts.Reasoning); effort != "" {
			reqBody["reasoning_effort"] = effort
		}
	}
	if serviceTier := m.serviceTier(opts); serviceTier != "" {
		reqBody["serviceTier"] = map[string]interface{}{"type": serviceTier}
	}

	return reqBody, nil
}

func (m *LanguageModel) bedrockProviderOptions(opts *provider.GenerateOptions) map[string]interface{} {
	if opts == nil || opts.ProviderOptions == nil {
		return nil
	}
	if raw, ok := opts.ProviderOptions["amazonBedrock"].(map[string]interface{}); ok {
		return raw
	}
	if raw, ok := opts.ProviderOptions["bedrock"].(map[string]interface{}); ok {
		return raw
	}
	return nil
}

func (m *LanguageModel) serviceTier(opts *provider.GenerateOptions) string {
	if provOpts := m.bedrockProviderOptions(opts); provOpts != nil {
		if v, ok := provOpts["serviceTier"].(string); ok && v != "" {
			return v
		}
	}
	if m.options != nil {
		return m.options.ServiceTier
	}
	return ""
}

func (m *LanguageModel) mergedReasoningConfig(opts *provider.GenerateOptions, derived interface{}) map[string]interface{} {
	var out map[string]interface{}
	if existing, ok := derived.(map[string]interface{}); ok {
		out = cloneMap(existing)
	}
	if out == nil && m.optionsReasoningConfig() != nil {
		out = map[string]interface{}{}
	}
	overlayReasoningConfig(out, m.optionsReasoningConfig())
	if provOpts := m.bedrockProviderOptions(opts); provOpts != nil {
		if rc, ok := provOpts["reasoningConfig"].(map[string]interface{}); ok {
			if out == nil {
				out = map[string]interface{}{}
			}
			for k, v := range rc {
				out[k] = v
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *LanguageModel) optionsReasoningConfig() *ReasoningConfig {
	if m.options == nil {
		return nil
	}
	return m.options.ReasoningConfig
}

func overlayReasoningConfig(out map[string]interface{}, rc *ReasoningConfig) {
	if rc == nil {
		return
	}
	if out == nil {
		return
	}
	if rc.Type != "" {
		out["type"] = rc.Type
	}
	if rc.BudgetTokens != nil {
		out["budgetTokens"] = *rc.BudgetTokens
	}
	if rc.MaxReasoningEffort != "" {
		out["maxReasoningEffort"] = rc.MaxReasoningEffort
	}
	if rc.Display != "" {
		out["display"] = rc.Display
	}
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func bedrockAnthropicProviderTool(t types.Tool) map[string]interface{} {
	id := t.ProviderID
	if id == "" && t.Type == types.ToolTypeProviderDefined {
		id = t.Name
	}
	switch id {
	case "anthropic.tool_search_bm25_20251119", "anthropic_bm25_tool_search":
		return map[string]interface{}{
			"name": "tool_search_tool_bm25",
			"type": "tool_search_tool_bm25_20251119",
		}
	case "anthropic.tool_search_regex_20251119", "anthropic_regex_tool_search":
		return map[string]interface{}{
			"name": "tool_search_tool_regex",
			"type": "tool_search_tool_regex_20251119",
		}
	default:
		return nil
	}
}

// bedrockUsage represents Bedrock usage information with detailed token tracking
type bedrockUsage struct {
	InputTokens           int `json:"input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	TotalTokens           int `json:"total_tokens,omitempty"`
	CacheReadInputTokens  int `json:"cache_read_input_tokens,omitempty"`     // v6.0
	CacheWriteInputTokens int `json:"cache_creation_input_tokens,omitempty"` // v6.0
}

// convertBedrockUsage converts Bedrock usage to detailed Usage struct
// Implements v6.0 detailed token tracking with cache support
// Bedrock supports BOTH cache read and cache write tokens
func convertBedrockUsage(usage bedrockUsage) types.Usage {
	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	cacheReadTokens := int64(usage.CacheReadInputTokens)
	cacheWriteTokens := int64(usage.CacheWriteInputTokens)

	// Calculate totals
	totalInputTokens := inputTokens
	totalTokens := totalInputTokens + outputTokens

	result := types.Usage{
		InputTokens:  &totalInputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	// Set input token details (Bedrock provides BOTH cache read and write)
	if cacheReadTokens > 0 || cacheWriteTokens > 0 {
		noCacheTokens := inputTokens - cacheReadTokens
		result.InputDetails = &types.InputTokenDetails{
			NoCacheTokens:    &noCacheTokens,
			CacheReadTokens:  &cacheReadTokens,
			CacheWriteTokens: &cacheWriteTokens,
		}
	}

	// Bedrock doesn't provide reasoning tokens breakdown yet
	result.OutputDetails = &types.OutputTokenDetails{
		TextTokens:      &outputTokens,
		ReasoningTokens: nil,
	}

	// Store raw usage
	result.Raw = map[string]interface{}{
		"input_tokens":  usage.InputTokens,
		"output_tokens": usage.OutputTokens,
	}

	if usage.TotalTokens > 0 {
		result.Raw["total_tokens"] = usage.TotalTokens
	}
	if usage.CacheReadInputTokens > 0 {
		result.Raw["cache_read_input_tokens"] = usage.CacheReadInputTokens
	}
	if usage.CacheWriteInputTokens > 0 {
		result.Raw["cache_creation_input_tokens"] = usage.CacheWriteInputTokens
	}

	return result
}

func (m *LanguageModel) convertResponse(body []byte) (*types.GenerateResult, error) {
	// Try Claude response format
	// Updated in v6.0 to support detailed usage tracking
	var claudeResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string       `json:"stop_reason"`
		Usage      bedrockUsage `json:"usage"`
	}

	if err := json.Unmarshal(body, &claudeResp); err == nil && len(claudeResp.Content) > 0 {
		text := ""
		for _, content := range claudeResp.Content {
			if content.Type == "text" {
				text += content.Text
			}
		}

		finishReason := types.FinishReasonStop
		if claudeResp.StopReason == "max_tokens" {
			finishReason = types.FinishReasonLength
		}

		return &types.GenerateResult{
			Text:         text,
			FinishReason: finishReason,
			Usage:        convertBedrockUsage(claudeResp.Usage),
			RawResponse:  claudeResp,
		}, nil
	}

	// Try generic response format
	var genericResp struct {
		Completion string `json:"completion"`
		Generation string `json:"generation"`
	}

	if err := json.Unmarshal(body, &genericResp); err == nil {
		text := genericResp.Completion
		if text == "" {
			text = genericResp.Generation
		}

		// Generic format doesn't provide usage information
		return &types.GenerateResult{
			Text:         text,
			FinishReason: types.FinishReasonStop,
			Usage:        types.Usage{}, // Empty usage
			RawResponse:  genericResp,
		}, nil
	}

	return nil, fmt.Errorf("unexpected response format from Bedrock: %s", string(body))
}

type bedrockStream struct {
	result   *types.GenerateResult
	position int
	done     bool
}

func (s *bedrockStream) Next() (*provider.StreamChunk, error) {
	if s.done {
		return nil, fmt.Errorf("stream exhausted")
	}

	chunkSize := 10
	text := s.result.Text

	if s.position >= len(text) {
		s.done = true
		return &provider.StreamChunk{
			Type:         provider.ChunkTypeFinish,
			Text:         "",
			FinishReason: s.result.FinishReason,
			Usage:        &s.result.Usage,
		}, nil
	}

	end := s.position + chunkSize
	if end > len(text) {
		end = len(text)
	}

	chunk := text[s.position:end]
	s.position = end

	return &provider.StreamChunk{
		Type: provider.ChunkTypeText,
		Text: chunk,
	}, nil
}

func (s *bedrockStream) Err() error {
	return nil
}

func (s *bedrockStream) Close() error {
	s.done = true
	return nil
}
