package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/eventstream"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	tool "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// LanguageModel implements provider.LanguageModel for AWS Bedrock's Converse
// API. It mirrors the TypeScript SDK's AmazonBedrockChatLanguageModel
// (amazon-bedrock-chat-language-model.ts), posting to
// /model/{id}/converse and /model/{id}/converse-stream.
type LanguageModel struct {
	provider *Provider
	modelID  string
	options  *ModelOptions
}

// NewLanguageModel creates a new AWS Bedrock language model.
func NewLanguageModel(provider *Provider, modelID string, options ...*ModelOptions) *LanguageModel {
	var opts *ModelOptions
	if len(options) > 0 {
		opts = options[0]
	}
	return &LanguageModel{provider: provider, modelID: modelID, options: opts}
}

func (m *LanguageModel) SpecificationVersion() string { return "v4" }
func (m *LanguageModel) Provider() string             { return "amazon-bedrock" }
func (m *LanguageModel) ModelID() string              { return m.modelID }
func (m *LanguageModel) SupportsTools() bool          { return true }
func (m *LanguageModel) SupportsStructuredOutput() bool {
	return true
}
func (m *LanguageModel) SupportsImageInput() bool { return true }

func (m *LanguageModel) modelFamily() string {
	if m.options != nil {
		return m.options.ModelFamily
	}
	return ""
}

// converseArgs is the result of building a Converse request body, mirroring
// TS getArgs()'s return shape.
type converseArgs struct {
	Body                 map[string]interface{}
	Warnings             []types.Warning
	UsesJSONInstruction  bool
	UsesJSONResponseTool bool
}

// getArgs ports TS amazon-bedrock-chat-language-model.ts#getArgs.
func (m *LanguageModel) getArgs(opts *provider.GenerateOptions) (*converseArgs, error) {
	warnings := []types.Warning{}

	amazonBedrockOptions := cloneMap(bedrockProviderOptions(opts))
	anthropicOptions := bedrockProviderOptionMap(opts.ProviderOptions, "anthropic")

	caps := anthropic.GetModelCapabilities(m.modelID)

	if opts.FrequencyPenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "frequencyPenalty"})
	}
	if opts.PresencePenalty != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "presencePenalty"})
	}
	if opts.Seed != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "seed"})
	}

	temperature := opts.Temperature
	topP := opts.TopP
	topK := opts.TopK

	if caps.RejectsSamplingParameters {
		if temperature != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "temperature", Details: fmt.Sprintf("temperature is not supported by %s and will be ignored", m.modelID)})
			temperature = nil
		}
		if topK != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK", Details: fmt.Sprintf("topK is not supported by %s and will be ignored", m.modelID)})
			topK = nil
		}
		if topP != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topP", Details: fmt.Sprintf("topP is not supported by %s and will be ignored", m.modelID)})
			topP = nil
		}
	}

	openAIModelID := openAIModelIDPattern.FindStringSubmatch(m.modelID)
	isOpenAIModel := openAIModelID != nil
	isOpenAIGptOssModel := isOpenAIModel && strings.HasPrefix(openAIModelID[1], "openai.gpt-oss-")
	shouldNormalizeTemperature := !isOpenAIModel || isOpenAIGptOssModel

	if shouldNormalizeTemperature && temperature != nil {
		if *temperature > 1 {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "temperature", Details: fmt.Sprintf("%v exceeds bedrock maximum of 1.0. clamped to 1.0", *temperature)})
			clamped := 1.0
			temperature = &clamped
		} else if *temperature < 0 {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "temperature", Details: fmt.Sprintf("%v is below bedrock minimum of 0. clamped to 0", *temperature)})
			clamped := 0.0
			temperature = &clamped
		}
	}

	if opts.ResponseFormat != nil && opts.ResponseFormat.Type != "" && opts.ResponseFormat.Type != "text" && opts.ResponseFormat.Type != "json" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "responseFormat", Details: "Only text and json response formats are supported."})
	}

	// Determine reasoning budget tokens from provider options (used by
	// isAnthropicModelID's application-inference-profile ARN detection).
	var reasoningBudgetTokens *int
	if rc, ok := amazonBedrockOptions["reasoningConfig"].(map[string]interface{}); ok {
		if bt, ok := intOption(rc["budgetTokens"]); ok {
			reasoningBudgetTokens = &bt
		}
	}

	isAnthropic := isAnthropicModelID(m.modelID, m.modelFamily(), reasoningBudgetTokens)

	existingReasoningConfig := mergeReasoningConfigSources(m.options, amazonBedrockOptions)
	resolvedReasoningConfig := resolveBedrockReasoningConfig(opts.Reasoning, existingReasoningConfig, isAnthropic, m.modelID, &warnings)
	if resolvedReasoningConfig != nil {
		amazonBedrockOptions["reasoningConfig"] = reasoningConfigToMap(resolvedReasoningConfig)
	}

	isThinkingEnabled := resolvedReasoningConfig != nil && (resolvedReasoningConfig.Type == "enabled" || resolvedReasoningConfig.Type == "adaptive")

	structuredOutputMode := stringOption(amazonBedrockOptions["structuredOutputMode"])
	if structuredOutputMode == "" {
		structuredOutputMode = stringOption(anthropicOptions["structuredOutputMode"])
	}
	if structuredOutputMode == "" && m.options != nil {
		structuredOutputMode = m.options.StructuredOutputMode
	}
	if structuredOutputMode == "" {
		structuredOutputMode = "auto"
	}

	additionalModelRequestFields := map[string]interface{}{}
	if m.options != nil {
		for k, v := range m.options.AdditionalModelRequestFields {
			additionalModelRequestFields[k] = v
		}
	}
	if raw, ok := amazonBedrockOptions["additionalModelRequestFields"].(map[string]interface{}); ok {
		for k, v := range raw {
			additionalModelRequestFields[k] = v
		}
	}

	if structuredOutputMode == "jsonTool" {
		if outputConfig, ok := additionalModelRequestFields["output_config"].(map[string]interface{}); ok {
			without := cloneMap(outputConfig)
			delete(without, "format")
			if len(without) > 0 {
				additionalModelRequestFields["output_config"] = without
			} else {
				delete(additionalModelRequestFields, "output_config")
			}
		}
	}

	modelSupportsNativeSO := bedrockSupportsNativeStructuredOutput(m.modelID) &&
		(caps.SupportsStructuredOutput || isThinkingEnabled || m.modelFamily() == "anthropic")

	responseFormatIsJSON := opts.ResponseFormat != nil && opts.ResponseFormat.Type == "json" && opts.ResponseFormat.Schema != nil

	useNativeStructuredOutput := isAnthropic && responseFormatIsJSON &&
		(structuredOutputMode == "outputFormat" || (structuredOutputMode == "auto" && modelSupportsNativeSO))

	useJSONInstructionForStructuredOutput := !useNativeStructuredOutput && isAnthropic && responseFormatIsJSON &&
		(caps.RejectsForcedToolUse ||
			(structuredOutputMode != "jsonTool" && !bedrockSupportsStrictTools(m.modelID) && len(opts.Tools) > 0))

	var jsonResponseTool *types.Tool
	if responseFormatIsJSON && !useNativeStructuredOutput && !useJSONInstructionForStructuredOutput {
		jsonResponseTool = &types.Tool{
			Type:        types.ToolTypeFunction,
			Name:        "json",
			Description: "Respond with a JSON object.",
			Parameters:  opts.ResponseFormat.Schema,
		}
	}

	toolsForPrep := opts.Tools
	toolChoiceForPrep := opts.ToolChoice
	hasToolChoice := opts.ToolChoice.Type != ""
	if jsonResponseTool != nil {
		toolsForPrep = append(append([]types.Tool{}, opts.Tools...), *jsonResponseTool)
		toolChoiceForPrep = types.RequiredToolChoice()
		hasToolChoice = true
	}

	var disableParallelToolUse *bool
	if v, ok := anthropicOptions["disableParallelToolUse"].(bool); ok {
		disableParallelToolUse = &v
	} else if m.options != nil {
		disableParallelToolUse = m.options.DisableParallelToolUse
	}

	prepared := prepareBedrockTools(toolsForPrep, toolChoiceForPrep, hasToolChoice, m.modelID, m.modelFamily(), reasoningBudgetTokens, disableParallelToolUse)
	warnings = append(warnings, prepared.Warnings...)
	for k, v := range prepared.AdditionalTools {
		additionalModelRequestFields[k] = v
	}

	if anthropicBetaRaw, ok := amazonBedrockOptions["anthropicBeta"].([]interface{}); ok && len(anthropicBetaRaw) > 0 {
		additionalModelRequestFields["anthropic_beta"] = anthropicBetaRaw
	}

	thinkingType := ""
	var thinkingBudget *int
	thinkingDisplay := ""
	if resolvedReasoningConfig != nil {
		thinkingType = resolvedReasoningConfig.Type
		if thinkingType == "enabled" {
			thinkingBudget = resolvedReasoningConfig.BudgetTokens
		}
		if thinkingType == "adaptive" {
			thinkingDisplay = resolvedReasoningConfig.Display
		}
	}
	isAnthropicThinkingEnabled := isAnthropic && isThinkingEnabled

	inferenceConfig := map[string]interface{}{}
	maxTokens := opts.MaxTokens
	if maxTokens != nil {
		inferenceConfig["maxTokens"] = *maxTokens
	}
	if temperature != nil {
		inferenceConfig["temperature"] = *temperature
	}
	if topP != nil {
		inferenceConfig["topP"] = *topP
	}
	if topK != nil {
		inferenceConfig["topK"] = *topK
	}
	if len(opts.StopSequences) > 0 {
		inferenceConfig["stopSequences"] = opts.StopSequences
	}

	if isAnthropicThinkingEnabled {
		if thinkingBudget != nil {
			if v, ok := inferenceConfig["maxTokens"].(int); ok {
				inferenceConfig["maxTokens"] = v + *thinkingBudget
			} else {
				inferenceConfig["maxTokens"] = *thinkingBudget + 4096
			}
			additionalModelRequestFields["thinking"] = map[string]interface{}{"type": "enabled", "budget_tokens": *thinkingBudget}
		} else if thinkingType == "adaptive" {
			thinking := map[string]interface{}{"type": "adaptive"}
			if thinkingDisplay != "" {
				thinking["display"] = thinkingDisplay
			}
			additionalModelRequestFields["thinking"] = thinking
		}
	} else if !isAnthropic {
		if thinkingBudget != nil {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "budgetTokens", Details: "budgetTokens applies only to Anthropic models on Bedrock and will be ignored for this model."})
		}
		if thinkingType == "adaptive" {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "adaptive thinking", Details: "adaptive thinking type applies only to Anthropic models on Bedrock."})
		}
	}

	maxReasoningEffort := ""
	if resolvedReasoningConfig != nil {
		maxReasoningEffort = resolvedReasoningConfig.MaxReasoningEffort
	}
	if maxReasoningEffort != "" {
		switch {
		case isAnthropic:
			outputConfig := map[string]interface{}{}
			if existing, ok := additionalModelRequestFields["output_config"].(map[string]interface{}); ok {
				outputConfig = cloneMap(existing)
			}
			outputConfig["effort"] = maxReasoningEffort
			additionalModelRequestFields["output_config"] = outputConfig
		case isOpenAIModel && isOpenAIGptOssModel:
			additionalModelRequestFields["reasoning_effort"] = maxReasoningEffort
		case isOpenAIModel:
			reasoning := map[string]interface{}{}
			if existing, ok := additionalModelRequestFields["reasoning"].(map[string]interface{}); ok {
				reasoning = cloneMap(existing)
			}
			reasoning["effort"] = maxReasoningEffort
			additionalModelRequestFields["reasoning"] = reasoning
		default:
			rc := map[string]interface{}{}
			if thinkingType != "" && thinkingType != "adaptive" {
				rc["type"] = thinkingType
			}
			if thinkingBudget != nil {
				rc["budgetTokens"] = *thinkingBudget
			}
			rc["maxReasoningEffort"] = maxReasoningEffort
			additionalModelRequestFields["reasoningConfig"] = rc
		}
	}

	// taskBudget (Anthropic's advisory task-level token budget, output_config.
	// task_budget) is not wired up in TS's amazon-bedrock-chat-language-
	// model.ts today (verified against ai@7.0.113: it has no `taskBudget`
	// handling at all, unlike anthropic-language-model.ts). Forwarding it here
	// via the same `anthropic` provider-options namespace Bedrock already
	// reads for disableParallelToolUse/structuredOutputMode gives Bedrock
	// Anthropic-model callers the same output_config.task_budget capability
	// the direct Anthropic provider has (pkg/providers/anthropic/request.go),
	// using the identical wire shape. It only activates when a caller
	// explicitly sets the option, so it cannot regress existing requests.
	if isAnthropic {
		if taskBudgetOpt, ok := anthropicOptions["taskBudget"].(map[string]interface{}); ok {
			taskBudgetType, _ := taskBudgetOpt["type"].(string)
			total, hasTotal := intOption(taskBudgetOpt["total"])
			if taskBudgetType != "" && hasTotal {
				taskBudget := map[string]interface{}{"type": taskBudgetType, "total": total}
				if remaining, ok := intOption(taskBudgetOpt["remaining"]); ok {
					taskBudget["remaining"] = remaining
				}
				outputConfig := map[string]interface{}{}
				if existing, ok := additionalModelRequestFields["output_config"].(map[string]interface{}); ok {
					outputConfig = cloneMap(existing)
				}
				outputConfig["task_budget"] = taskBudget
				additionalModelRequestFields["output_config"] = outputConfig

				// The underlying Anthropic model gates output_config.task_budget
				// on the task-budgets-2026-03-13 beta (pkg/providers/anthropic/
				// request.go, anthropic-language-model.ts:984-986); Bedrock
				// forwards it to that same backend via additionalModelRequestFields.
				// anthropic_beta, so it must be added here too or the request will
				// be rejected server-side.
				additionalModelRequestFields["anthropic_beta"] = addAnthropicBeta(
					additionalModelRequestFields["anthropic_beta"], anthropic.BetaHeaderTaskBudgets)
			}
		}
	}

	if useNativeStructuredOutput {
		outputConfig := map[string]interface{}{}
		if existing, ok := additionalModelRequestFields["output_config"].(map[string]interface{}); ok {
			outputConfig = cloneMap(existing)
		}
		outputConfig["format"] = map[string]interface{}{
			"type":   "json_schema",
			"schema": tool.SanitizeAnthropicSchema(opts.ResponseFormat.Schema),
		}
		additionalModelRequestFields["output_config"] = outputConfig
	}

	if isAnthropicThinkingEnabled {
		if _, ok := inferenceConfig["temperature"]; ok {
			delete(inferenceConfig, "temperature")
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "temperature", Details: "temperature is not supported when thinking is enabled"})
		}
		if _, ok := inferenceConfig["topP"]; ok {
			delete(inferenceConfig, "topP")
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topP", Details: "topP is not supported when thinking is enabled"})
		}
		if _, ok := inferenceConfig["topK"]; ok {
			delete(inferenceConfig, "topK")
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "topK", Details: "topK is not supported when thinking is enabled"})
		}
	}

	if isOpenAIModel {
		unsupported := []string{"temperature", "topP", "stopSequences"}
		if isOpenAIGptOssModel {
			unsupported = []string{"stopSequences"}
		}
		for _, feature := range unsupported {
			if _, ok := inferenceConfig[feature]; ok {
				delete(inferenceConfig, feature)
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature, Details: fmt.Sprintf("%s is not supported by this OpenAI model on the Converse API", feature)})
			}
		}
	}

	hasAnyTools := len(prepared.ToolConfig.Tools) > 0 || len(prepared.AdditionalTools) > 0
	filteredMessages := normalizePromptMessages(opts.Prompt)
	if !hasAnyTools {
		filtered, removed := filterToolContent(filteredMessages)
		filteredMessages = filtered
		if removed {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "toolContent",
				Details: "Tool calls and results removed from conversation because Bedrock does not support tool content without active tools.",
			})
		}
	}

	if useJSONInstructionForStructuredOutput {
		filteredMessages = injectBedrockJSONInstruction(filteredMessages, opts.ResponseFormat.Schema)
	}

	isMistral := isMistralModel(m.modelID)
	system, messages, err := convertToBedrockChatMessages(filteredMessages, isMistral)
	if err != nil {
		return nil, err
	}

	// TS always includes `system` (even as an empty array) in the Converse
	// command; match that here for wire-format parity.
	if system == nil {
		system = []map[string]interface{}{}
	}
	if messages == nil {
		messages = []map[string]interface{}{}
	}
	body := map[string]interface{}{
		"messages": messages,
		"system":   system,
	}
	if len(additionalModelRequestFields) > 0 {
		body["additionalModelRequestFields"] = additionalModelRequestFields
	}
	if isAnthropic {
		body["additionalModelResponseFieldPaths"] = []string{"/delta/stop_sequence"}
	}
	if len(inferenceConfig) > 0 {
		body["inferenceConfig"] = inferenceConfig
	}
	if serviceTier := stringOption(amazonBedrockOptions["serviceTier"]); serviceTier != "" {
		body["serviceTier"] = map[string]interface{}{"type": serviceTier}
	} else if m.options != nil && m.options.ServiceTier != "" {
		body["serviceTier"] = map[string]interface{}{"type": m.options.ServiceTier}
	}
	// Forward any remaining, unrecognized amazonBedrock/bedrock provider
	// options verbatim (e.g. guardrailConfig), excluding the fields already
	// handled above.
	for _, key := range []string{"reasoningConfig", "additionalModelRequestFields", "serviceTier", "structuredOutputMode", "anthropicBeta"} {
		delete(amazonBedrockOptions, key)
	}
	for k, v := range amazonBedrockOptions {
		body[k] = v
	}
	if len(prepared.ToolConfig.Tools) > 0 {
		toolConfig := map[string]interface{}{"tools": prepared.ToolConfig.Tools}
		if prepared.ToolConfig.ToolChoice != nil {
			toolConfig["toolChoice"] = prepared.ToolConfig.ToolChoice
		}
		body["toolConfig"] = toolConfig
	}

	return &converseArgs{
		Body:                 body,
		Warnings:             warnings,
		UsesJSONInstruction:  useJSONInstructionForStructuredOutput,
		UsesJSONResponseTool: jsonResponseTool != nil,
	}, nil
}

var openAIModelIDPattern = regexp.MustCompile(`^(?:[^.]+\.)?(openai\..+)$`)

// bedrockProviderOptions extracts the amazonBedrock (falling back to legacy
// bedrock) provider-options map for a call.
func bedrockProviderOptions(opts *provider.GenerateOptions) map[string]interface{} {
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

func mergeReasoningConfigSources(modelOptions *ModelOptions, amazonBedrockOptions map[string]interface{}) *ReasoningConfig {
	var result *ReasoningConfig
	if modelOptions != nil && modelOptions.ReasoningConfig != nil {
		clone := *modelOptions.ReasoningConfig
		result = &clone
	}
	if rc, ok := amazonBedrockOptions["reasoningConfig"].(map[string]interface{}); ok {
		if result == nil {
			result = &ReasoningConfig{}
		}
		if t, ok := rc["type"].(string); ok {
			result.Type = t
		}
		if bt, ok := intOption(rc["budgetTokens"]); ok {
			result.BudgetTokens = &bt
		}
		if e, ok := rc["maxReasoningEffort"].(string); ok {
			result.MaxReasoningEffort = e
		}
		if d, ok := rc["display"].(string); ok {
			result.Display = d
		}
	}
	return result
}

func reasoningConfigToMap(rc *ReasoningConfig) map[string]interface{} {
	if rc == nil {
		return nil
	}
	out := map[string]interface{}{}
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
	return out
}

// normalizePromptMessages folds a types.Prompt's simple-text or system-string
// forms into a unified message list, mirroring how the TS SDK's core prompt
// standardization builds a LanguageModelV4Prompt before calling into the
// provider.
func normalizePromptMessages(prompt types.Prompt) []types.Message {
	var messages []types.Message
	if prompt.System != "" {
		messages = append(messages, types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: prompt.System}}})
	}
	if prompt.IsMessages() {
		messages = append(messages, prompt.Messages...)
	} else if prompt.IsSimple() {
		messages = append(messages, types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: prompt.Text}}})
	}
	return messages
}

// filterToolContent removes tool-call and tool-result parts from non-system
// messages (dropping messages left empty), matching TS getArgs()'s
// hasAnyTools filtering. Returns the filtered messages and whether anything
// was removed.
func filterToolContent(messages []types.Message) ([]types.Message, bool) {
	removed := false
	out := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == types.RoleSystem {
			out = append(out, msg)
			continue
		}
		filteredContent := make([]types.ContentPart, 0, len(msg.Content))
		for _, part := range msg.Content {
			switch part.(type) {
			case types.ToolCallContent, types.ToolResultContent:
				removed = true
			default:
				filteredContent = append(filteredContent, part)
			}
		}
		if len(filteredContent) > 0 {
			clone := msg
			clone.Content = filteredContent
			out = append(out, clone)
		} else if len(msg.Content) > 0 {
			removed = true
		}
	}
	return out, removed
}

// injectBedrockJSONInstruction ports TS provider-utils
// injectJsonInstructionIntoMessages for the Opus 4.7-style "JSON instruction"
// structured-output fallback.
func injectBedrockJSONInstruction(messages []types.Message, schema interface{}) []types.Message {
	const suffix = "You MUST answer with only a JSON object that matches the JSON schema above. Do not wrap it in markdown fences or include any other text."

	var existingText string
	hasSystem := len(messages) > 0 && messages[0].Role == types.RoleSystem
	if hasSystem {
		for _, part := range messages[0].Content {
			if t, ok := part.(types.TextContent); ok {
				existingText += t.Text
			}
		}
	}

	schemaJSON, _ := json.Marshal(schema)

	lines := []string{}
	if existingText != "" {
		lines = append(lines, existingText, "")
	}
	lines = append(lines, "JSON schema:", string(schemaJSON), suffix)

	newSystem := types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: strings.Join(lines, "\n")}}}

	if hasSystem {
		out := make([]types.Message, 0, len(messages))
		out = append(out, newSystem)
		out = append(out, messages[1:]...)
		return out
	}
	out := make([]types.Message, 0, len(messages)+1)
	out = append(out, newSystem)
	out = append(out, messages...)
	return out
}

// jsonObjectTextExtractor ports TS
// amazon-bedrock-chat-language-model.ts#JsonObjectTextExtractor: it
// incrementally extracts the JSON object substring from a text stream that
// may contain a leading/trailing instruction wrapper.
type jsonObjectTextExtractor struct {
	started, completed, inString, escaped bool
	depth                                 int
}

func (e *jsonObjectTextExtractor) process(text string) string {
	var result strings.Builder
	for _, ch := range text {
		if e.completed {
			break
		}
		if !e.started {
			if ch != '{' {
				continue
			}
			e.started = true
			e.depth = 1
			result.WriteRune(ch)
			continue
		}
		result.WriteRune(ch)
		if e.escaped {
			e.escaped = false
			continue
		}
		if ch == '\\' && e.inString {
			e.escaped = true
			continue
		}
		if ch == '"' {
			e.inString = !e.inString
			continue
		}
		if e.inString {
			continue
		}
		if ch == '{' {
			e.depth++
		} else if ch == '}' {
			e.depth--
			if e.depth == 0 {
				e.completed = true
			}
		}
	}
	return result.String()
}

// ─── HTTP request construction ─────────────────────────────────────────────

// converseURL builds the Converse (or Converse-stream) endpoint URL and
// returns the raw wire path used both for the actual HTTP request-target and
// for AWS SigV4 canonicalization. Ports TS getUrl(): encodeURIComponent(id)
// so ARNs (which may contain '/' and ':') survive as a single path segment.
func (m *LanguageModel) converseURL(baseURL, suffix string) (*url.URL, string, error) {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil, "", err
	}
	rawPath := "/model/" + jsEncodeURIComponent(m.modelID) + suffix
	reqURL := &url.URL{Scheme: base.Scheme, Host: base.Host, Opaque: rawPath}
	return reqURL, rawPath, nil
}

func (m *LanguageModel) newConverseRequest(ctx context.Context, suffix string, body []byte) (*http.Request, error) {
	baseURL, err := m.provider.runtimeBaseURL()
	if err != nil {
		return nil, err
	}
	// Build the request against the plain base URL first so http.NewRequest
	// sets up Host/ContentLength/GetBody correctly, then swap in the opaque,
	// doubly-escapable raw path so literal percent-encoding (e.g. an
	// application-inference-profile ARN's '/' and ':') survives on the wire.
	// Go's url.URL otherwise decodes %2F/%3A back into '/' and ':' when
	// populating .Path.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	reqURL, _, err := m.converseURL(baseURL, suffix)
	if err != nil {
		return nil, err
	}
	req.URL = reqURL
	req.Host = reqURL.Host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	m.provider.applyRequestHeaders(req, nil)
	if err := m.provider.authenticateRequest(ctx, req, body); err != nil {
		return nil, err
	}
	return req, nil
}

// ─── DoGenerate ─────────────────────────────────────────────────────────────

func (m *LanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	args, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(args.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := m.newConverseRequest(ctx, "/converse", bodyBytes)
	if err != nil {
		return nil, err
	}

	httpClient := m.provider.Client().HTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, providerErrorFromTransport(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, bedrockAPIError(resp.StatusCode, respBody, nil)
	}

	result, err := m.convertConverseResponse(respBody, args.UsesJSONInstruction, args.UsesJSONResponseTool)
	if err != nil {
		return nil, err
	}
	result.ResponseHeaders = providerutils.ExtractHeaders(resp.Header)
	result.RawRequest = args.Body
	if result.ResponseMetadata == nil {
		result.ResponseMetadata = &types.ResponseMetadata{}
	}
	result.ResponseMetadata.ID = resp.Header.Get("x-amzn-requestid")
	result.ResponseMetadata.ModelID = m.modelID
	result.ResponseMetadata.Headers = result.ResponseHeaders
	if dateHeader := resp.Header.Get("date"); dateHeader != "" {
		if ts, err := time.Parse(time.RFC1123, dateHeader); err == nil {
			result.ResponseMetadata.Timestamp = ts
		}
	}
	result.Warnings = args.Warnings
	return result, nil
}

func providerErrorFromTransport(err error) error {
	return fmt.Errorf("amazon-bedrock request failed: %w", err)
}

// ─── Response conversion ────────────────────────────────────────────────────

type bedrockConverseToolUse struct {
	ToolUseID string          `json:"toolUseId"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
}

type bedrockConverseReasoningContent struct {
	ReasoningText *struct {
		Text      string `json:"text"`
		Signature string `json:"signature"`
	} `json:"reasoningText"`
	RedactedReasoning *struct {
		Data string `json:"data"`
	} `json:"redactedReasoning"`
	RedactedContent string `json:"redactedContent"`
}

type bedrockConverseContentBlock struct {
	Text             *string                          `json:"text"`
	CitationsContent *bedrockCitationsContent         `json:"citationsContent"`
	ToolUse          *bedrockConverseToolUse          `json:"toolUse"`
	ReasoningContent *bedrockConverseReasoningContent `json:"reasoningContent"`
}

type bedrockCitationsContent struct {
	Content []struct {
		Text *string `json:"text"`
	} `json:"content"`
}

type bedrockConverseResponse struct {
	Output struct {
		Message struct {
			Content []bedrockConverseContentBlock `json:"content"`
			Role    string                        `json:"role"`
		} `json:"message"`
	} `json:"output"`
	StopReason                    string                       `json:"stopReason"`
	AdditionalModelResponseFields *bedrockAdditionalRespFields `json:"additionalModelResponseFields"`
	Trace                         json.RawMessage              `json:"trace"`
	PerformanceConfig             map[string]interface{}       `json:"performanceConfig"`
	ServiceTier                   map[string]interface{}       `json:"serviceTier"`
	Usage                         json.RawMessage              `json:"usage"`
}

type bedrockAdditionalRespFields struct {
	Delta *struct {
		StopSequence *string `json:"stop_sequence"`
	} `json:"delta"`
}

func (m *LanguageModel) convertConverseResponse(body []byte, usesJSONInstruction, usesJSONResponseTool bool) (*types.GenerateResult, error) {
	var resp bedrockConverseResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode Bedrock Converse response: %w", err)
	}

	result := &types.GenerateResult{RawResponse: json.RawMessage(body)}
	isMistral := isMistralModel(m.modelID)

	var extractor *jsonObjectTextExtractor
	if usesJSONInstruction {
		extractor = &jsonObjectTextExtractor{}
	}

	isJSONResponseFromTool := false

	for _, part := range resp.Output.Message.Content {
		var textParts []string
		if part.Text != nil {
			textParts = append(textParts, *part.Text)
		} else if part.CitationsContent != nil {
			for _, c := range part.CitationsContent.Content {
				if c.Text != nil {
					textParts = append(textParts, *c.Text)
				}
			}
		}
		for _, text := range textParts {
			if extractor != nil {
				text = extractor.process(text)
			}
			result.Text += text
		}

		if rc := part.ReasoningContent; rc != nil {
			switch {
			case rc.ReasoningText != nil:
				reasoning := types.ReasoningContent{Text: rc.ReasoningText.Text}
				if rc.ReasoningText.Signature != "" {
					reasoning.Signature = rc.ReasoningText.Signature
				}
				result.Content = append(result.Content, reasoning)
			case rc.RedactedReasoning != nil:
				result.Content = append(result.Content, types.ReasoningContent{RedactedData: rc.RedactedReasoning.Data})
			case rc.RedactedContent != "":
				result.Content = append(result.Content, types.ReasoningContent{
					ProviderMetadata: mustMarshalProviderMetadata(map[string]interface{}{
						"amazonBedrock": map[string]interface{}{"redactedContent": rc.RedactedContent},
						"bedrock":       map[string]interface{}{"redactedContent": rc.RedactedContent},
					}),
				})
			}
		}

		if part.ToolUse != nil {
			if usesJSONResponseTool && part.ToolUse.Name == "json" {
				isJSONResponseFromTool = true
				result.Text = string(part.ToolUse.Input)
				continue
			}
			var args map[string]interface{}
			_ = json.Unmarshal(part.ToolUse.Input, &args)
			if args == nil {
				args = map[string]interface{}{}
			}
			result.ToolCalls = append(result.ToolCalls, types.ToolCall{
				ID:        normalizeToolCallID(part.ToolUse.ToolUseID, isMistral),
				ToolName:  part.ToolUse.Name,
				Arguments: args,
			})
		}
	}

	var stopSequence *string
	if resp.AdditionalModelResponseFields != nil && resp.AdditionalModelResponseFields.Delta != nil {
		stopSequence = resp.AdditionalModelResponseFields.Delta.StopSequence
	}

	var usageMap map[string]interface{}
	_ = json.Unmarshal(resp.Usage, &usageMap)
	result.Usage = convertBedrockConverseUsage(resp.Usage)

	result.FinishReason = mapBedrockFinishReason(resp.StopReason, isJSONResponseFromTool)

	metadataPayload := map[string]interface{}{}
	if len(resp.Trace) > 0 && string(resp.Trace) != "null" {
		var trace interface{}
		if json.Unmarshal(resp.Trace, &trace) == nil {
			metadataPayload["trace"] = trace
		}
	}
	if len(resp.PerformanceConfig) > 0 {
		metadataPayload["performanceConfig"] = resp.PerformanceConfig
	}
	if len(resp.ServiceTier) > 0 {
		metadataPayload["serviceTier"] = resp.ServiceTier
	}
	if usageMap != nil {
		usageMeta := map[string]interface{}{}
		if v, ok := usageMap["cacheWriteInputTokens"]; ok {
			usageMeta["cacheWriteInputTokens"] = v
		}
		if v, ok := usageMap["cacheDetails"]; ok {
			usageMeta["cacheDetails"] = v
		}
		if len(usageMeta) > 0 {
			metadataPayload["usage"] = usageMeta
		}
	}
	if isJSONResponseFromTool {
		metadataPayload["isJsonResponseFromTool"] = true
	}
	if stopSequence != nil {
		metadataPayload["stopSequence"] = *stopSequence
	} else {
		metadataPayload["stopSequence"] = nil
	}
	if len(metadataPayload) > 1 || metadataPayload["stopSequence"] != nil {
		result.ProviderMetadata = map[string]interface{}{
			"amazonBedrock": metadataPayload,
			"bedrock":       metadataPayload,
		}
	}

	return result, nil
}

func mustMarshalProviderMetadata(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

func mapBedrockFinishReason(stopReason string, isJSONResponseFromTool bool) types.FinishReason {
	switch stopReason {
	case "stop_sequence", "end_turn", "stop":
		return types.FinishReasonStop
	case "max_tokens", "length":
		return types.FinishReasonLength
	case "content_filtered", "guardrail_intervened", "content-filter":
		return types.FinishReasonContentFilter
	case "tool_use", "tool-calls":
		if isJSONResponseFromTool {
			return types.FinishReasonStop
		}
		return types.FinishReasonToolCalls
	default:
		return types.FinishReasonOther
	}
}

// convertBedrockConverseUsage ports TS convert-amazon-bedrock-usage.ts.
func convertBedrockConverseUsage(raw json.RawMessage) types.Usage {
	var usage struct {
		InputTokens           int `json:"inputTokens"`
		OutputTokens          int `json:"outputTokens"`
		CacheReadInputTokens  int `json:"cacheReadInputTokens"`
		CacheWriteInputTokens int `json:"cacheWriteInputTokens"`
	}
	_ = json.Unmarshal(raw, &usage)

	inputTokens := int64(usage.InputTokens)
	outputTokens := int64(usage.OutputTokens)
	cacheReadTokens := int64(usage.CacheReadInputTokens)
	cacheWriteTokens := int64(usage.CacheWriteInputTokens)
	totalInput := inputTokens + cacheReadTokens + cacheWriteTokens
	totalTokens := totalInput + outputTokens

	result := types.Usage{
		InputTokens:  &totalInput,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:    &inputTokens,
			CacheReadTokens:  &cacheReadTokens,
			CacheWriteTokens: &cacheWriteTokens,
		},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &outputTokens},
	}

	var rawMap map[string]interface{}
	if json.Unmarshal(raw, &rawMap) == nil {
		result.Raw = rawMap
	}

	return result
}

// ─── DoStream ───────────────────────────────────────────────────────────────

func (m *LanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	args, err := m.getArgs(opts)
	if err != nil {
		return nil, err
	}
	bodyBytes, err := json.Marshal(args.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := m.newConverseRequest(ctx, "/converse-stream", bodyBytes)
	if err != nil {
		return nil, err
	}

	httpClient := m.provider.Client().HTTPClient()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, providerErrorFromTransport(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		return nil, bedrockAPIError(resp.StatusCode, respBody, nil)
	}

	stream := &bedrockConverseStream{
		decoder:              eventstream.NewDecoder(resp.Body),
		body:                 resp.Body,
		isMistral:            isMistralModel(m.modelID),
		usesJSONResponseTool: args.UsesJSONResponseTool,
		warnings:             args.Warnings,
		modelID:              m.modelID,
		responseHeaders:      providerutils.ExtractHeaders(resp.Header),
		requestID:            resp.Header.Get("x-amzn-requestid"),
		contentBlocks:        map[int]*bedrockStreamContentBlock{},
		finishReason:         types.FinishReasonOther,
	}
	if dateHeader := resp.Header.Get("date"); dateHeader != "" {
		if ts, err := time.Parse(time.RFC1123, dateHeader); err == nil {
			stream.responseTimestamp = &ts
		}
	}
	if extract := args.UsesJSONInstruction; extract {
		stream.extractor = &jsonObjectTextExtractor{}
	}
	return stream, nil
}
