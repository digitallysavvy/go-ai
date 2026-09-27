package anthropic

import (
	"fmt"
	"math"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// Beta flags added by request preparation (anthropic-language-model.ts).
const (
	BetaHeaderDangerousToolUse          = "dangerous-tool-use-2026-09-03"
	BetaHeaderThinkingDisplayUpdates    = "thinking-display-updates-2026-08-18"
	BetaHeaderThinkingBindingControls   = "thinking-binding-controls-2026-08-01"
	BetaHeaderServerSideFallbackDefault = "server-side-fallback-2026-07-01"
)

// preparedRequest is the result of prepareRequest: the request body plus the
// warnings, betas and flags the response handlers need. Mirrors the return
// value of AnthropicLanguageModel.prepareRequest in the TypeScript SDK.
type preparedRequest struct {
	body                 map[string]interface{}
	warnings             []types.Warning
	betas                []string
	usesJSONResponseTool bool
	// markCodeExecutionDynamic is true when code_execution calls must be
	// marked dynamic (see HasDynamicFilteringWebToolWithoutCodeExecution).
	markCodeExecutionDynamic bool
	toolsetNames             map[string]string
}

type betaSet struct {
	list []string
	seen map[string]bool
}

func (b *betaSet) add(values ...string) {
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || b.seen[v] {
			continue
		}
		b.seen[v] = true
		b.list = append(b.list, v)
	}
}

// userSuppliedBetas returns the anthropic-beta values from the provider
// config headers and the call headers (TS getBetasFromHeaders).
func (m *LanguageModel) userSuppliedBetas(opts *provider.GenerateOptions) []string {
	var out []string
	collect := func(headers map[string]string) {
		for k, v := range headers {
			if strings.EqualFold(k, "anthropic-beta") {
				for _, b := range strings.Split(strings.ToLower(v), ",") {
					if b = strings.TrimSpace(b); b != "" {
						out = append(out, b)
					}
				}
			}
		}
	}
	collect(m.provider.config.Headers)
	if opts != nil {
		collect(opts.Headers)
	}
	return out
}

func warnUnsupported(warnings *[]types.Warning, feature, details string) {
	*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: feature, Details: details})
}

// effectiveThinking is the resolved thinking configuration of a request.
type effectiveThinking struct {
	typ          ThinkingType
	budgetTokens *int
	display      ThinkingDisplay
	blockBinding *ThinkingBlockBinding
	set          bool
}

// prepareRequest builds the Anthropic Messages request. It mirrors
// AnthropicLanguageModel.prepareRequest (anthropic-language-model.ts at
// ai@7.0.113).
func (m *LanguageModel) prepareRequest(opts *provider.GenerateOptions, stream bool) (*preparedRequest, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	var warnings []types.Warning
	o := m.options
	if o == nil {
		o = &ModelOptions{}
	}

	if o.ContextManagement != nil && o.Compaction != nil {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "Anthropic provider options `compaction` and `contextManagement` cannot be used together.",
		}
	}

	if opts.FrequencyPenalty != nil {
		warnUnsupported(&warnings, "frequencyPenalty", "")
	}
	if opts.PresencePenalty != nil {
		warnUnsupported(&warnings, "presencePenalty", "")
	}
	if opts.Seed != nil {
		warnUnsupported(&warnings, "seed", "")
	}

	temperature := opts.Temperature
	if temperature != nil && *temperature > 1 {
		warnUnsupported(&warnings, "temperature", fmt.Sprintf("%v exceeds anthropic maximum of 1.0. clamped to 1.0", *temperature))
		one := 1.0
		temperature = &one
	} else if temperature != nil && *temperature < 0 {
		warnUnsupported(&warnings, "temperature", fmt.Sprintf("%v is below anthropic minimum of 0. clamped to 0", *temperature))
		zero := 0.0
		temperature = &zero
	}
	topK := opts.TopK
	topP := opts.TopP

	jsonFormat := opts.ResponseFormat != nil && (opts.ResponseFormat.Type == "json" || opts.ResponseFormat.Type == "json_schema")
	if jsonFormat && opts.ResponseFormat.Schema == nil {
		warnUnsupported(&warnings, "responseFormat", "JSON response format requires a schema. The response format is ignored.")
	}
	jsonSchemaFormat := jsonFormat && opts.ResponseFormat.Schema != nil

	caps := GetModelCapabilities(m.modelID)

	if !caps.IsKnownModel && opts.MaxTokens == nil {
		warnings = append(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "maxOutputTokens",
			Details: fmt.Sprintf("The model %q is unknown. The max output tokens have been limited to %d. Set maxOutputTokens explicitly to override this limit.", m.modelID, caps.MaxOutputTokens),
		})
	}

	if caps.RejectsSamplingParameters {
		if temperature != nil {
			warnUnsupported(&warnings, "temperature", fmt.Sprintf("temperature is not supported by %s and will be ignored", m.modelID))
			temperature = nil
		}
		if topK != nil {
			warnUnsupported(&warnings, "topK", fmt.Sprintf("topK is not supported by %s and will be ignored", m.modelID))
			topK = nil
		}
		if topP != nil {
			warnUnsupported(&warnings, "topP", fmt.Sprintf("topP is not supported by %s and will be ignored", m.modelID))
			topP = nil
		}
	}

	isAnthropicModel := caps.IsKnownModel || strings.Contains(m.modelID, "claude-")
	supportsStructuredOutput := m.configBool(m.provider.config.SupportsNativeStructuredOutput) && caps.SupportsStructuredOutput
	supportsStrictTools := m.configBool(m.provider.config.SupportsStrictTools) && caps.SupportsStructuredOutput

	mode := StructuredOutputAuto
	if o.StructuredOutputMode != "" {
		mode = o.StructuredOutputMode
	}
	useStructuredOutput := mode == StructuredOutputFormat || (mode == StructuredOutputAuto && supportsStructuredOutput)
	if !useStructuredOutput && caps.RejectsForcedToolUse && supportsStructuredOutput && jsonSchemaFormat {
		warnUnsupported(&warnings, "providerOptions.anthropic.structuredOutputMode", fmt.Sprintf(
			"structuredOutputMode 'jsonTool' is not supported by %s because it rejects forced tool use. Using 'outputFormat' instead.", m.modelID))
		useStructuredOutput = true
	}
	usesJSONResponseTool := jsonSchemaFormat && !useStructuredOutput
	if usesJSONResponseTool && o.DisableParallelToolUse != nil && !*o.DisableParallelToolUse {
		warnUnsupported(&warnings, "providerOptions.anthropic.disableParallelToolUse",
			"`disableParallelToolUse: false` is ignored when using the JSON response tool. "+
				"Parallel tool use is disabled to ensure a single coherent JSON tool call.")
	}

	// Shared cache control validator across the prompt and tool definitions.
	validator := prompt.NewAnthropicCacheControlValidator()
	toolsetNames := prompt.AnthropicToolsetNames(opts.Tools)
	sendReasoning := o.SendReasoning
	if sendReasoning == nil {
		sendReasoning = opts.SendReasoning
	}
	promptInfo, err := convertCallPrompt(opts, prompt.AnthropicPromptOptions{
		SendReasoning:         sendReasoning,
		ToolsetNames:          toolsetNames,
		CacheControlValidator: validator,
	})
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, promptInfo.Warnings...)
	var betas betaSet
	betas.add(promptInfo.Betas...)

	// Resolve thinking / effort.
	th := effectiveThinking{}
	if o.Thinking != nil {
		th = effectiveThinking{
			typ:          o.Thinking.Type,
			budgetTokens: o.Thinking.BudgetTokens,
			display:      o.Thinking.Display,
			blockBinding: o.Thinking.BlockBinding,
			set:          true,
		}
	}
	effort := string(o.Effort)

	if opts.Reasoning != nil && *opts.Reasoning != types.ReasoningDefault && effort == "" {
		rTh, rEffort := resolveReasoningConfig(*opts.Reasoning, m.modelID, caps, &warnings)
		if rTh != nil && !th.set {
			th = *rTh
		}
		if rEffort != "" && th.typ != ThinkingTypeDisabled {
			effort = rEffort
		}
	}

	if caps.RejectsThinkingDisabled && th.set {
		switch th.typ {
		case ThinkingTypeDisabled:
			warnUnsupported(&warnings, "providerOptions.anthropic.thinking", fmt.Sprintf(
				"thinking cannot be disabled for %s; it always uses adaptive thinking. The thinking setting has been removed. Lower 'effort' to reduce thinking.", m.modelID))
			th = effectiveThinking{}
		case ThinkingTypeEnabled:
			warnUnsupported(&warnings, "providerOptions.anthropic.thinking", fmt.Sprintf(
				"budget-based thinking is not supported by %s; it always uses adaptive thinking. Using adaptive thinking instead. Use 'effort' to control how much the model thinks.", m.modelID))
			th = effectiveThinking{typ: ThinkingTypeAdaptive, set: true}
		}
	}

	if caps.RejectsThinkingDisabledAboveHighEffort && th.typ == ThinkingTypeDisabled && (effort == "xhigh" || effort == "max") {
		warnUnsupported(&warnings, "providerOptions.anthropic.effort", fmt.Sprintf(
			"effort '%s' is not supported by %s when thinking is disabled. The effort has been lowered to 'high'.", effort, m.modelID))
		effort = "high"
	}

	isThinking := th.typ == ThinkingTypeEnabled || th.typ == ThinkingTypeAdaptive
	sendThinking := isThinking || th.typ == ThinkingTypeDisabled || th.blockBinding != nil
	var thinkingBudget *int
	if th.typ == ThinkingTypeEnabled {
		thinkingBudget = th.budgetTokens
	}
	var thinkingDisplay ThinkingDisplay
	if th.typ == ThinkingTypeAdaptive {
		thinkingDisplay = th.display
	}

	maxTokens := caps.MaxOutputTokens
	if opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}

	body := map[string]interface{}{
		"model":      m.modelID,
		"max_tokens": maxTokens,
	}
	if temperature != nil {
		body["temperature"] = *temperature
	}
	if topK != nil {
		body["top_k"] = *topK
	}
	if topP != nil {
		body["top_p"] = *topP
	}
	if len(opts.StopSequences) > 0 {
		body["stop_sequences"] = opts.StopSequences
	}

	if sendThinking {
		thinking := map[string]interface{}{}
		if th.typ != "" {
			thinking["type"] = string(th.typ)
		}
		if thinkingBudget != nil {
			thinking["budget_tokens"] = *thinkingBudget
		}
		if thinkingDisplay != "" {
			thinking["display"] = string(thinkingDisplay)
		}
		if th.blockBinding != nil {
			thinking["block_binding"] = map[string]interface{}{
				"prefix_mismatch_behavior": th.blockBinding.PrefixMismatchBehavior,
			}
		}
		body["thinking"] = thinking
	}

	outputConfig := map[string]interface{}{}
	if effort != "" {
		outputConfig["effort"] = effort
	}
	if o.TaskBudget != nil {
		taskBudget := map[string]interface{}{
			"type":  o.TaskBudget.Type,
			"total": o.TaskBudget.Total,
		}
		if o.TaskBudget.Remaining != nil {
			taskBudget["remaining"] = *o.TaskBudget.Remaining
		}
		outputConfig["task_budget"] = taskBudget
	}
	if useStructuredOutput && jsonSchemaFormat {
		outputConfig["format"] = map[string]interface{}{
			"type":   "json_schema",
			"schema": tool.SanitizeAnthropicSchema(opts.ResponseFormat.Schema),
		}
	}
	if len(outputConfig) > 0 {
		body["output_config"] = outputConfig
	}
	if o.Speed != "" {
		body["speed"] = string(o.Speed)
	}
	if o.ServiceTier != "" {
		body["service_tier"] = o.ServiceTier
	}
	if o.InferenceGeo != "" {
		body["inference_geo"] = o.InferenceGeo
	}
	if o.FallbacksDefault {
		body["fallbacks"] = "default"
	} else if len(o.Fallbacks) > 0 {
		body["fallbacks"] = o.Fallbacks
	}
	// cache_control: explicit CacheControl takes precedence over AutomaticCaching.
	if o.CacheControl != nil {
		body["cache_control"] = o.CacheControl
	} else if o.AutomaticCaching {
		body["cache_control"] = map[string]string{"type": "auto"}
	}
	if userID := callMetadataUserID(opts); userID != "" {
		body["metadata"] = map[string]interface{}{"user_id": userID}
	}
	if len(o.MCPServers) > 0 {
		body["mcp_servers"] = mcpServersWire(o.MCPServers)
	}
	if c := containerWire(o); c != nil {
		body["container"] = c
	}

	body["messages"] = promptInfo.Messages
	if promptInfo.System != nil {
		body["system"] = promptInfo.System
	}

	if len(o.Safeguards) > 0 {
		sg := make([]map[string]interface{}, 0, len(o.Safeguards))
		for _, s := range o.Safeguards {
			item := map[string]interface{}{"type": s.Type}
			if s.ClassifierContext != nil {
				item["classifier_context"] = s.ClassifierContext
			}
			sg = append(sg, item)
		}
		body["safeguards"] = sg
	}
	if o.ContextManagement != nil {
		body["context_management"] = o.ContextManagement
	}
	if o.Compaction != nil {
		body["compaction"] = o.Compaction
	}

	if isThinking {
		if th.typ == ThinkingTypeEnabled && thinkingBudget == nil {
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "extended thinking",
				Details: "thinking budget is required when thinking is enabled. using default budget of 1024 tokens.",
			})
			budget := 1024
			thinkingBudget = &budget
			thinking := map[string]interface{}{"type": "enabled", "budget_tokens": budget}
			body["thinking"] = thinking
		}
		if _, ok := body["temperature"]; ok {
			delete(body, "temperature")
			warnUnsupported(&warnings, "temperature", "temperature is not supported when thinking is enabled")
		}
		if topK != nil {
			delete(body, "top_k")
			warnUnsupported(&warnings, "topK", "topK is not supported when thinking is enabled")
		}
		if topP != nil {
			delete(body, "top_p")
			warnUnsupported(&warnings, "topP", "topP is not supported when thinking is enabled")
		}
		budget := 0
		if thinkingBudget != nil {
			budget = *thinkingBudget
		}
		body["max_tokens"] = maxTokens + budget
	} else if isAnthropicModel && topP != nil && temperature != nil {
		// Only Claude models reject temperature and topP together;
		// Anthropic-compatible APIs may require both.
		warnUnsupported(&warnings, "topP", "topP is not supported when temperature is set. topP is ignored.")
		delete(body, "top_p")
	}

	if caps.IsKnownModel && body["max_tokens"].(int) > caps.MaxOutputTokens {
		if opts.MaxTokens != nil {
			warnUnsupported(&warnings, "maxOutputTokens", fmt.Sprintf(
				"%d (maxOutputTokens + thinkingBudget) is greater than %s %d max output tokens. The max output tokens have been limited to %d.",
				body["max_tokens"].(int), m.modelID, caps.MaxOutputTokens, caps.MaxOutputTokens))
		}
		body["max_tokens"] = caps.MaxOutputTokens
	}

	// Betas added by the request options (TS order).
	if len(o.MCPServers) > 0 {
		betas.add(BetaHeaderMCPClient)
	}
	if len(o.Safeguards) > 0 {
		betas.add(BetaHeaderDangerousToolUse)
	}
	if o.ContextManagement != nil && len(o.ContextManagement.Edits) > 0 {
		betas.add(BetaHeaderContextManagement)
		for _, edit := range o.ContextManagement.Edits {
			if _, ok := edit.(*CompactEdit); ok {
				betas.add(BetaHeaderCompact)
				break
			}
		}
	}
	if o.Compaction != nil {
		betas.add(BetaHeaderCompaction)
	}
	if o.Container != nil && len(o.Container.Skills) > 0 {
		betas.add(BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI)
		if w := m.detectSkillsWarning(opts); w != nil {
			warnings = append(warnings, *w)
		}
	}
	if o.TaskBudget != nil {
		betas.add(BetaHeaderTaskBudgets)
	}
	if o.Speed == SpeedFast {
		betas.add(BetaHeaderFastMode)
	}
	if o.AutomaticCaching && o.CacheControl == nil {
		betas.add(BetaHeaderPromptCaching)
	}
	if thinkingDisplay == ThinkingDisplayUpdates {
		betas.add(BetaHeaderThinkingDisplayUpdates)
	}
	if th.blockBinding != nil {
		betas.add(BetaHeaderThinkingBindingControls)
	}
	if o.FallbacksDefault {
		betas.add(BetaHeaderServerSideFallbackDefault)
	} else if len(o.Fallbacks) > 0 {
		betas.add(BetaHeaderServerSideFallback)
	}

	defaultEager := stream && (o.ToolStreaming == nil || *o.ToolStreaming)

	var prepared preparedTools
	if usesJSONResponseTool {
		tools := append(append([]types.Tool{}, opts.Tools...), types.Tool{
			Name:        "json",
			Description: "Respond with a JSON object.",
			Parameters:  opts.ResponseFormat.Schema,
		})
		disable := true
		prepared = prepareTools(prepareToolsOptions{
			tools:                      tools,
			toolChoice:                 &types.ToolChoice{Type: types.ToolChoiceRequired},
			disableParallelToolUse:     &disable,
			validator:                  validator,
			supportsStructuredOutput:   false,
			supportsStrictTools:        supportsStrictTools,
			defaultEagerInputStreaming: defaultEager,
			rejectsForcedToolUse:       caps.RejectsForcedToolUse,
		})
	} else {
		tc := opts.ToolChoice
		prepared = prepareTools(prepareToolsOptions{
			tools:                      opts.Tools,
			toolChoice:                 &tc,
			disableParallelToolUse:     o.DisableParallelToolUse,
			validator:                  validator,
			supportsStructuredOutput:   supportsStructuredOutput,
			supportsStrictTools:        supportsStrictTools,
			defaultEagerInputStreaming: defaultEager,
			rejectsForcedToolUse:       caps.RejectsForcedToolUse,
		})
	}
	if prepared.tools != nil {
		body["tools"] = prepared.tools
	}
	if prepared.toolChoice != nil {
		body["tool_choice"] = prepared.toolChoice
	}
	if stream {
		body["stream"] = true
	}
	warnings = append(warnings, prepared.warnings...)
	warnings = append(warnings, validator.Warnings()...)

	betas.add(prepared.betas...)
	betas.add(m.userSuppliedBetas(opts)...)
	betas.add(o.AnthropicBeta...)

	return &preparedRequest{
		body:                     body,
		warnings:                 warnings,
		betas:                    betas.list,
		usesJSONResponseTool:     usesJSONResponseTool,
		markCodeExecutionDynamic: HasDynamicFilteringWebToolWithoutCodeExecution(prepared.tools),
		toolsetNames:             toolsetNames,
	}, nil
}

func (m *LanguageModel) configBool(v *bool) bool {
	return v == nil || *v
}

// resolveReasoningConfig mirrors resolveAnthropicReasoningConfig: it maps the
// call-level reasoning level to thinking and effort.
func resolveReasoningConfig(level types.ReasoningLevel, modelID string, caps ModelCapabilities, warnings *[]types.Warning) (*effectiveThinking, string) {
	if level == types.ReasoningNone {
		if caps.RejectsThinkingDisabled {
			*warnings = append(*warnings, types.Warning{
				Type:    "compatibility",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning 'none' is not supported by %s; it always uses adaptive thinking. Using effort 'low' to minimize thinking instead.", modelID),
			})
			return nil, "low"
		}
		return &effectiveThinking{typ: ThinkingTypeDisabled, set: true}, ""
	}
	if caps.SupportsAdaptiveThinking {
		effortMap := map[types.ReasoningLevel]string{
			types.ReasoningMinimal: "low",
			types.ReasoningLow:     "low",
			types.ReasoningMedium:  "medium",
			types.ReasoningHigh:    "high",
			types.ReasoningXHigh:   "max",
		}
		if caps.SupportsXHighEffort {
			effortMap[types.ReasoningXHigh] = "xhigh"
		}
		effort, ok := effortMap[level]
		if !ok {
			*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "reasoning", Details: fmt.Sprintf("reasoning %q is not supported by this model.", level)})
		} else if effort != string(level) {
			*warnings = append(*warnings, types.Warning{Type: "compatibility", Feature: "reasoning", Details: fmt.Sprintf("reasoning %q is not directly supported by this model. mapped to effort %q.", level, effort)})
		}
		return &effectiveThinking{typ: ThinkingTypeAdaptive, display: ThinkingDisplaySummarized, set: true}, effort
	}
	pct, ok := map[types.ReasoningLevel]float64{
		types.ReasoningMinimal: 0.02,
		types.ReasoningLow:     0.10,
		types.ReasoningMedium:  0.30,
		types.ReasoningHigh:    0.60,
		types.ReasoningXHigh:   0.90,
	}[level]
	if !ok {
		*warnings = append(*warnings, types.Warning{Type: "unsupported", Feature: "reasoning", Details: fmt.Sprintf("reasoning %q is not supported by this model.", level)})
		return nil, ""
	}
	budget := int(math.Round(float64(caps.MaxOutputTokens) * pct))
	if budget < 1024 {
		budget = 1024
	}
	if budget > caps.MaxOutputTokens {
		budget = caps.MaxOutputTokens
	}
	return &effectiveThinking{typ: ThinkingTypeEnabled, budgetTokens: &budget, set: true}, ""
}

func callMetadataUserID(opts *provider.GenerateOptions) string {
	if opts == nil || opts.ProviderOptions == nil {
		return ""
	}
	anthropicOpts, ok := opts.ProviderOptions["anthropic"].(map[string]interface{})
	if !ok {
		return ""
	}
	metadata, ok := anthropicOpts["metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	userID, _ := metadata["userId"].(string)
	return userID
}

func mcpServersWire(servers []MCPServerConfig) []map[string]interface{} {
	out := make([]map[string]interface{}, len(servers))
	for i, s := range servers {
		srv := map[string]interface{}{
			"type": s.Type,
			"name": s.Name,
			"url":  s.URL,
		}
		if s.AuthorizationToken != "" {
			srv["authorization_token"] = s.AuthorizationToken
		}
		if s.ToolConfiguration != nil {
			tc := map[string]interface{}{}
			if len(s.ToolConfiguration.AllowedTools) > 0 {
				tc["allowed_tools"] = s.ToolConfiguration.AllowedTools
			}
			if s.ToolConfiguration.Enabled != nil {
				tc["enabled"] = *s.ToolConfiguration.Enabled
			}
			if len(tc) > 0 {
				srv["tool_configuration"] = tc
			}
		}
		out[i] = srv
	}
	return out
}

// containerWire returns the container request field: a plain ID string, or an
// object {id, skills} when skills are configured.
func containerWire(o *ModelOptions) interface{} {
	if o.ContainerID != "" {
		return o.ContainerID
	}
	if o.Container == nil {
		return nil
	}
	if len(o.Container.Skills) > 0 {
		containerBody := map[string]interface{}{}
		if o.Container.ID != "" {
			containerBody["id"] = o.Container.ID
		}
		skills := make([]map[string]interface{}, len(o.Container.Skills))
		for i, s := range o.Container.Skills {
			skill := map[string]interface{}{
				"type":     s.Type,
				"skill_id": s.SkillID,
			}
			if s.Version != "" {
				skill["version"] = s.Version
			}
			skills[i] = skill
		}
		containerBody["skills"] = skills
		return containerBody
	}
	if o.Container.ID != "" {
		return o.Container.ID
	}
	return nil
}
