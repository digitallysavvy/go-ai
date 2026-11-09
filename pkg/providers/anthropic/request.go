package anthropic

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	// preparedTools are the prepared tool definitions (TS preparedTools:
	// anthropicTools), kept for the toolset member checks even when the wire
	// request omits them.
	preparedTools []map[string]interface{}
	// nameTools are the inline tools followed by the declared tools, for
	// the response tool name mapping (TS toolNameMapping).
	nameTools []types.Tool
	// includePings is TS `stream && includePings === true`.
	includePings bool
	// thread reports a message thread request (batches reject it).
	thread bool
	// providerOptionsName / usedCustomProviderKey mirror TS prepareRequest's
	// return fields of the same name: providerOptionsName is the wrapper's
	// own providerOptions key (e.g. "minimax", "bedrock"), and
	// usedCustomProviderKey is true when the caller supplied
	// providerOptions[providerOptionsName] for a non-"anthropic" name. The
	// response builders use these to duplicate providerMetadata under
	// providerOptionsName (TS: usedCustomProviderKey && providerOptionsName
	// !== 'anthropic').
	providerOptionsName   string
	usedCustomProviderKey bool
}

type betaSet struct {
	list []string
	seen map[string]bool
}

// add appends each value not yet in the set, as given: TS keeps the betas in
// a Set<string> and trims only the header values (getBetasFromHeaders), so
// providerOptions.anthropic.anthropicBeta entries are neither trimmed nor
// dropped when empty.
func (b *betaSet) add(values ...string) {
	if b.seen == nil {
		b.seen = map[string]bool{}
	}
	for _, v := range values {
		if b.seen[v] {
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
				for _, b := range strings.Split(providerutils.JSToLower(v), ",") {
					if b = providerutils.JSTrim(b); b != "" { // TS beta.trim()
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
	return m.prepareRequestWithBetas(opts, stream, m.userSuppliedBetas(opts))
}

// prepareRequestWithBetas is prepareRequest with an explicit userSuppliedBetas
// set: calls take them from the config and call headers, batches only from
// providerOptions.anthropic.anthropicBeta on startBatch (TS passes
// userSuppliedBetas: new Set(explicitBatchBetas)).
func (m *LanguageModel) prepareRequestWithBetas(opts *provider.GenerateOptions, stream bool, userBetas []string) (*preparedRequest, error) {
	if opts == nil {
		opts = &provider.GenerateOptions{}
	}
	var warnings []types.Warning
	o, usedCustomProviderKey, err := m.resolveCallOptions(opts)
	if err != nil {
		return nil, err
	}

	if o.ContextManagement != nil && o.Compaction != nil {
		return nil, providererrors.NewArgumentError("providerOptions", "Anthropic provider options `compaction` and `contextManagement` cannot be used together.", nil)
	}

	// TS 6b101ac076: message threads need platform support and allow no
	// compaction or context editing except keeping all thinking blocks.
	thread := o.Thread
	if thread != nil && !m.configBool(m.provider.config.SupportsMessageThreads) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "Anthropic message threads",
			Message:       fmt.Sprintf("%s does not support Anthropic message threads.", m.tsProviderString()),
		}
	}
	if thread != nil && (o.Compaction != nil || threadIncompatibleContextManagement(o.ContextManagement)) {
		return nil, providererrors.NewArgumentError("providerOptions.anthropic.thread",
			`Anthropic message threads do not support compaction or context editing, except clear_thinking_20251015 with keep: "all".`, nil)
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
	maxTemperature := 1.0
	temperatureProvider := "anthropic"
	if m.provider.config.MaxTemperature != nil {
		maxTemperature = *m.provider.config.MaxTemperature
		temperatureProvider = m.providerOptionsName()
	}
	if temperature != nil && *temperature > maxTemperature {
		warnUnsupported(&warnings, "temperature", fmt.Sprintf("%s exceeds %s maximum of %.1f. clamped to %.1f", providerutils.JSNumberString(*temperature), temperatureProvider, maxTemperature, maxTemperature))
		clamped := maxTemperature
		temperature = &clamped
	} else if temperature != nil && *temperature < 0 {
		warnUnsupported(&warnings, "temperature", fmt.Sprintf("%s is below %s minimum of 0. clamped to 0", providerutils.JSNumberString(*temperature), temperatureProvider))
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

	// TS 8291ccf237: Anthropic accepts auto / standard_only, MiniMax
	// standard / priority; another known provider's tier is omitted with a
	// warning, and other providers forward every value.
	if o.ServiceTier != "" {
		var supported []string
		switch name := m.providerOptionsName(); name {
		case "anthropic":
			supported = []string{"auto", "standard_only"}
		case "minimax":
			supported = []string{"standard", "priority"}
		}
		if supported != nil && o.ServiceTier != supported[0] && o.ServiceTier != supported[1] {
			warnUnsupported(&warnings, "serviceTier", fmt.Sprintf("serviceTier \"%s\" is not supported by %s. Use \"%s\" or \"%s\".", o.ServiceTier, m.providerOptionsName(), supported[0], supported[1]))
			o.ServiceTier = ""
		}
	}

	caps := GetModelCapabilities(m.modelID)

	if !caps.IsKnownModel && opts.MaxTokens == nil {
		warnings = append(warnings, types.Warning{
			Type:    "compatibility",
			Feature: "maxOutputTokens",
			Details: fmt.Sprintf("The model \"%s\" is unknown. The max output tokens have been limited to %d. Set maxOutputTokens explicitly to override this limit.", m.modelID, caps.MaxOutputTokens),
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
	// TS 2cd0a97424 / b0d51fecc0: auto mode uses native structured outputs
	// only for a schema they can represent (no cycles, no open objects, at
	// most 10 optional properties).
	supportsResponseSchema := !jsonSchemaFormat || SupportsNativeStructuredOutputSchema(opts.ResponseFormat.Schema)
	useStructuredOutput := mode == StructuredOutputFormat || (mode == StructuredOutputAuto && supportsStructuredOutput && supportsResponseSchema)
	// The JSON response tool relies on forced tool use, which some models
	// reject (e.g. Opus 5.5, Fable 5.1). Fall back to native structured
	// outputs even when the provider disables them by default (Bedrock,
	// Vertex), unless they would reject the schema or lose data from it
	// (TS f4c5f1e959, b0d51fecc0).
	if !useStructuredOutput && caps.RejectsForcedToolUse && caps.SupportsStructuredOutput && jsonSchemaFormat && supportsResponseSchema {
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
	// A thread with top-level cacheControl uses one of the four breakpoints.
	initialBreakpoints := 0
	if thread != nil && o.CacheControl != nil {
		initialBreakpoints = 1
	}
	validator := prompt.NewAnthropicCacheControlValidatorWithBreakpoints(m.provider.config.CacheControlProviderOptionsName, initialBreakpoints)
	defaultEager := stream && (o.ToolStreaming == nil || *o.ToolStreaming)

	// Tools that tool changes add by value are only known from the prompt
	// (TS getInlineToolsFromPrompt). They take part in tool name mapping;
	// the declared tools go last, so an inline tool cannot take over their
	// mapping.
	inlineTools, err := prompt.AnthropicInlineTools(opts.Prompt.Messages)
	if err != nil {
		return nil, err
	}
	nameTools := append(append([]types.Tool{}, inlineTools...), opts.Tools...)
	toolNames := newAnthropicToolNames(nameTools)
	toolsetNames := newAnthropicToolNames(opts.Tools).toolsets
	var inlineDefinitions []anthropicInlineDefinition
	sendReasoning := o.SendReasoning
	if sendReasoning == nil {
		sendReasoning = opts.SendReasoning
	}
	promptInfo, err := convertCallPrompt(opts, prompt.AnthropicPromptOptions{
		SendReasoning:         sendReasoning,
		ToolsetNames:          toolsetNames,
		ToProviderToolName:    toolNames.providerName,
		CacheControlValidator: validator,
		ConvertMediaPart:      m.provider.config.ConvertMediaPart,
		// TS convertInlineTool: prepareTools on the one tool with the shared
		// cache control validator and no stream-dependent defaults, so the
		// replayed definition stays identical between streaming and
		// non-streaming calls.
		ConvertInlineTool: func(tool types.Tool) (map[string]interface{}, []types.Warning, []string, error) {
			p := prepareTools(prepareToolsOptions{
				tools:                    []types.Tool{tool},
				validator:                validator,
				supportsStructuredOutput: supportsStructuredOutput,
				supportsStrictTools:      supportsStrictTools,
			})
			if p.err != nil {
				return nil, nil, nil, p.err
			}
			var definition map[string]interface{}
			if len(p.tools) > 0 {
				definition = p.tools[0]
				inlineDefinitions = append(inlineDefinitions, anthropicInlineDefinition{sdkName: tool.Name, definition: definition})
			}
			return definition, p.warnings, p.betas, nil
		},
	})
	if err != nil {
		return nil, err
	}
	warnings = append(warnings, promptInfo.Warnings...)
	if thread != nil {
		pinned := map[string]bool{}
		for _, b := range userBetas {
			pinned[b] = true
		}
		for _, b := range o.AnthropicBeta {
			pinned[b] = true
		}
		var unpinned []string
		for _, b := range promptInfo.Betas {
			if !pinned[b] {
				unpinned = append(unpinned, b)
			}
		}
		if len(unpinned) > 0 {
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "providerOptions.anthropic.thread",
				Details: fmt.Sprintf("Message content enabled beta headers: %s. ", strings.Join(unpinned, ", ")) +
					"Thread requests require the same beta set on every turn. Pin these values with anthropicBeta on every request, or start a new thread when the set changes.",
			})
		}
		if thread.Type == "continue" && (promptInfo.System != nil || len(opts.Tools) > 0) {
			warnings = append(warnings, types.Warning{
				Type:    "compatibility",
				Feature: "providerOptions.anthropic.thread",
				Details: "Continuation uses the system prompt and tool definitions stored by the create request; supplied definitions are omitted from the wire request. " +
					"Supply identical tools for local tool handling and fixed request settings. To change system instructions or tools, start a new thread.",
			})
		}
	}
	var betas betaSet
	if thread != nil {
		betas.add(BetaHeaderMessageThreads)
	}
	betas.add(promptInfo.Betas...)

	// Resolve thinking / effort.
	th := effectiveThinking{}
	if o.Thinking != nil {
		th = effectiveThinking{
			typ:          o.Thinking.Type,
			budgetTokens: o.Thinking.BudgetTokens,
			display:      o.Thinking.Display,
			set:          true,
		}
		// TS 'blockBinding' in thinking: only adaptive and the typeless form
		// keep it after the zod parse.
		if thinkingTakesBlockBinding(o.Thinking.Type) {
			th.blockBinding = o.Thinking.BlockBinding
		}
	}
	effort := string(o.Effort)

	if providerutils.IsCustomReasoning(opts.Reasoning) && effort == "" {
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
			if caps.SupportsBetweenToolsThinking {
				warnUnsupported(&warnings, "providerOptions.anthropic.thinking", fmt.Sprintf(
					"thinking cannot be disabled for %s. Using 'between_tools' thinking, the lowest thinking setting, instead.", m.modelID))
				th = effectiveThinking{typ: ThinkingTypeBetweenTools, set: true}
			} else {
				warnUnsupported(&warnings, "providerOptions.anthropic.thinking", fmt.Sprintf(
					"thinking cannot be disabled for %s; it always uses adaptive thinking. The thinking setting has been removed. Lower 'effort' to reduce thinking.", m.modelID))
				th = effectiveThinking{}
			}
		}
	}

	// Models that only support adaptive thinking reject budget-based thinking
	// with a 400. Use adaptive thinking instead and let effort control how
	// much the model thinks (TS #22239).
	if caps.RejectsBudgetThinking && th.typ == ThinkingTypeEnabled {
		details := fmt.Sprintf("budget-based thinking is not supported by %s. Using adaptive thinking instead. Use 'effort' to control how much the model thinks.", m.modelID)
		if caps.RejectsThinkingDisabled {
			details = fmt.Sprintf("budget-based thinking is not supported by %s; it always uses adaptive thinking. Using adaptive thinking instead. Use 'effort' to control how much the model thinks.", m.modelID)
		}
		warnUnsupported(&warnings, "providerOptions.anthropic.thinking", details)
		th = effectiveThinking{typ: ThinkingTypeAdaptive, set: true}
	}

	if caps.RejectsThinkingDisabledAboveHighEffort && th.typ == ThinkingTypeDisabled && (effort == "xhigh" || effort == "max") {
		warnUnsupported(&warnings, "providerOptions.anthropic.effort", fmt.Sprintf(
			"effort '%s' is not supported by %s when thinking is disabled. The effort has been lowered to 'high'.", effort, m.modelID))
		effort = "high"
	}

	// `between_tools` thinking is only accepted at "low", "medium", and
	// "high" effort. Lower the effort to "high" to keep the minimal
	// thinking setting instead of sending a request the API would reject.
	if th.typ == ThinkingTypeBetweenTools && (effort == "xhigh" || effort == "max") {
		warnUnsupported(&warnings, "providerOptions.anthropic.effort", fmt.Sprintf(
			"effort '%s' is not supported with 'between_tools' thinking. The effort has been lowered to 'high'.", effort))
		effort = "high"
	}

	isThinking := th.typ == ThinkingTypeEnabled || th.typ == ThinkingTypeAdaptive || th.typ == ThinkingTypeBetweenTools
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
	if opts.StopSequences != nil {
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
	if o.Metadata != nil && o.Metadata.UserID != "" {
		body["metadata"] = map[string]interface{}{"user_id": o.Metadata.UserID}
	}
	if len(o.MCPServers) > 0 {
		body["mcp_servers"] = mcpServersWire(o.MCPServers)
	}
	containerBody, err := containerWire(o)
	if err != nil {
		return nil, err
	}
	if containerBody != nil {
		body["container"] = containerBody
	}

	body["messages"] = promptInfo.Messages
	if promptInfo.System != nil && (thread == nil || thread.Type != "continue") {
		body["system"] = promptInfo.System
	}
	if thread != nil {
		if thread.Type == "create" {
			body["thread"] = map[string]interface{}{"type": "create"}
		} else {
			body["thread"] = map[string]interface{}{"type": "continue", "previous_message_id": thread.PreviousMessageID}
		}
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
		cm, err := wireContextManagement(o.ContextManagement)
		if err != nil {
			return nil, err
		}
		body["context_management"] = cm
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
		keepSampling := m.provider.config.SupportsTemperatureAndTopPWithThinking
		if _, ok := body["temperature"]; ok && !keepSampling {
			delete(body, "temperature")
			warnUnsupported(&warnings, "temperature", "temperature is not supported when thinking is enabled")
		}
		if topK != nil {
			delete(body, "top_k")
			warnUnsupported(&warnings, "topK", "topK is not supported when thinking is enabled")
		}
		if topP != nil && !keepSampling {
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
	if o.Compaction != nil {
		betas.add(BetaHeaderCompaction)
	}
	if o.ContextManagement != nil {
		betas.add(BetaHeaderContextManagement)
		for _, edit := range o.ContextManagement.Edits {
			if _, ok := edit.(*CompactEdit); ok {
				betas.add(BetaHeaderCompact)
				break
			}
		}
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

	var prepared preparedTools
	if usesJSONResponseTool {
		// TS 2cd0a97424: toolChoice none drops the user tools and a named
		// toolChoice keeps only that tool next to the JSON response tool.
		var tools []types.Tool
		for _, t := range opts.Tools {
			if opts.ToolChoice.Type == types.ToolChoiceNone || (opts.ToolChoice.Type == types.ToolChoiceTool && t.Name != opts.ToolChoice.ToolName) {
				continue
			}
			tools = append(tools, t)
		}
		tools = append(tools, types.Tool{
			Name:        "json",
			Description: "Respond with a JSON object.",
			Parameters:  providerutils.ResponseFormatJSONSchemaOrdered(opts.ResponseFormat.Schema),
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
			rejectsForcedToolUse:       caps.RejectsForcedToolUse || th.typ == ThinkingTypeEnabled,
		})
	} else {
		tc := opts.ToolChoice
		prepared = prepareTools(prepareToolsOptions{
			tools:                      opts.Tools,
			toolChoice:                 &tc,
			hasInlineTools:             len(inlineTools) > 0,
			disableParallelToolUse:     o.DisableParallelToolUse,
			validator:                  validator,
			supportsStructuredOutput:   supportsStructuredOutput,
			supportsStrictTools:        supportsStrictTools,
			defaultEagerInputStreaming: defaultEager,
			rejectsForcedToolUse:       caps.RejectsForcedToolUse || th.typ == ThinkingTypeEnabled,
		})
	}
	if prepared.err != nil {
		return nil, prepared.err
	}

	// Forced-choice and JSON-tool fallbacks restrict the declared tools; MCP
	// connectors must not restore other tools before the provider executes
	// the request (TS 5ab7ff14ad).
	restrictedChoice := (opts.ToolChoice.Type == types.ToolChoiceTool && (prepared.toolChoice["type"] == "auto" || usesJSONResponseTool)) ||
		(opts.ToolChoice.Type == types.ToolChoiceNone && usesJSONResponseTool)
	if restrictedChoice {
		if servers, ok := body["mcp_servers"].([]map[string]interface{}); ok {
			for _, srv := range servers {
				tc := map[string]interface{}{"enabled": false}
				if prev, ok := srv["tool_configuration"].(map[string]interface{}); ok {
					if allowed, ok := prev["allowed_tools"]; ok {
						tc["allowed_tools"] = allowed
					}
				}
				srv["tool_configuration"] = tc
			}
		}
		// Inline definitions, including those replayed from history, must
		// not restore other tools either (TS ba302a14fc, b3088a77b0).
		if err := checkInlineToolChoice(opts, usesJSONResponseTool, toolNames, promptInfo.Messages, inlineDefinitions); err != nil {
			return nil, err
		}
	}
	// A continuation uses the stored tool definitions (tool_choice stays).
	if prepared.tools != nil && (thread == nil || thread.Type != "continue") {
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
	betas.add(userBetas...)
	betas.add(o.AnthropicBeta...)

	return &preparedRequest{
		body:                     body,
		warnings:                 warnings,
		betas:                    betas.list,
		usesJSONResponseTool:     usesJSONResponseTool,
		markCodeExecutionDynamic: HasDynamicFilteringWebToolWithoutCodeExecution(prepared.tools),
		toolsetNames:             toolsetNames,
		preparedTools:            prepared.tools,
		nameTools:                nameTools,
		thread:                   thread != nil,
		includePings:             stream && o.IncludePings != nil && *o.IncludePings,
		providerOptionsName:      m.providerOptionsName(),
		usedCustomProviderKey:    usedCustomProviderKey,
	}, nil
}

func (m *LanguageModel) configBool(v *bool) bool {
	return v == nil || *v
}

// resolveReasoningConfig mirrors resolveAnthropicReasoningConfig: it maps the
// call-level reasoning level to thinking and effort.
func resolveReasoningConfig(level types.ReasoningLevel, modelID string, caps ModelCapabilities, warnings *[]types.Warning) (*effectiveThinking, string) {
	if level == types.ReasoningNone {
		// `between_tools` is the lowest thinking setting on models that
		// support it: no upfront thinking, only short progress notes
		// between tool calls (claude-sonnet-5-5+).
		if caps.SupportsBetweenToolsThinking {
			*warnings = append(*warnings, types.Warning{
				Type:    "compatibility",
				Feature: "reasoning",
				Details: fmt.Sprintf("reasoning 'none' is not supported by %s; thinking cannot be disabled. Using 'between_tools' thinking, the lowest thinking setting, instead.", modelID),
			})
			return &effectiveThinking{typ: ThinkingTypeBetweenTools, set: true}, ""
		}
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
		// TS 61a315e333: only the supported efforts; the shared helper rounds
		// an unsupported level down (minimal → low, xhigh → high).
		effortMap := map[types.ReasoningLevel]string{
			types.ReasoningLow:    "low",
			types.ReasoningMedium: "medium",
			types.ReasoningHigh:   "high",
			types.ReasoningMax:    "max",
		}
		if caps.SupportsXHighEffort {
			effortMap[types.ReasoningXHigh] = "xhigh"
		}
		effort, _ := providerutils.MapReasoningToProviderEffort(&level, effortMap, nil, warnings)
		return &effectiveThinking{typ: ThinkingTypeAdaptive, display: ThinkingDisplaySummarized, set: true}, effort
	}
	budget := providerutils.MapReasoningToProviderBudget(level, providerutils.ReasoningBudgetOptions{
		MaxOutputTokens:    caps.MaxOutputTokens,
		MaxReasoningBudget: caps.MaxOutputTokens,
	}, warnings)
	return &effectiveThinking{typ: ThinkingTypeEnabled, budgetTokens: &budget, set: true}, ""
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
			// TS maps an explicit empty allowedTools list ("no tools") as is and
			// sends an empty tool_configuration object.
			if s.ToolConfiguration.AllowedTools != nil {
				tc["allowed_tools"] = s.ToolConfiguration.AllowedTools
			}
			if s.ToolConfiguration.Enabled != nil {
				tc["enabled"] = *s.ToolConfiguration.Enabled
			}
			srv["tool_configuration"] = tc
		}
		out[i] = srv
	}
	return out
}

// containerWire returns the container request field: a plain ID string, or an
// object {id, skills} when skills are configured. For a Type "custom" skill,
// the wire skill_id is resolved from ProviderReference against this
// provider's name ("anthropic"), matching TS's `resolveProviderReference`
// call in anthropic-language-model.ts; a missing "anthropic" entry returns
// the same NoSuchProviderReferenceError TS throws.
func containerWire(o *ModelOptions) (interface{}, error) {
	if o.ContainerID != "" {
		return o.ContainerID, nil
	}
	if o.Container == nil {
		return nil, nil
	}
	if len(o.Container.Skills) > 0 {
		containerBody := map[string]interface{}{}
		if o.Container.ID != "" {
			containerBody["id"] = o.Container.ID
		}
		skills := make([]map[string]interface{}, len(o.Container.Skills))
		for i, s := range o.Container.Skills {
			skillID := s.SkillID
			if s.Type == "custom" {
				resolved, err := providerutils.ResolveProviderReference(s.ProviderReference, "anthropic")
				if err != nil {
					return nil, err
				}
				skillID = resolved
			}
			skill := map[string]interface{}{
				"type":     s.Type,
				"skill_id": skillID,
			}
			if s.Version != "" {
				skill["version"] = s.Version
			}
			skills[i] = skill
		}
		containerBody["skills"] = skills
		return containerBody, nil
	}
	if o.Container.ID != "" {
		return o.Container.ID, nil
	}
	return nil, nil
}

// anthropicInlineDefinition is a tool definition a tool change added by value.
type anthropicInlineDefinition struct {
	sdkName    string
	definition map[string]interface{}
}

// checkInlineToolChoice ports the TS inline tool choice restriction
// (anthropic-language-model.ts, ba302a14fc narrowed by b3088a77b0): under a
// forced-choice fallback or the JSON response tool, an inline definition
// emitted in the outgoing messages (ignored initial-system additions are
// not checked) conflicts with the tool choice unless it is the selected
// tool. Definitions are compared by identity, as TS Set.has does.
func checkInlineToolChoice(opts *provider.GenerateOptions, usesJSONResponseTool bool, names anthropicToolNames, messages []map[string]interface{}, defs []anthropicInlineDefinition) error {
	emitted := map[uintptr]bool{}
	for _, msg := range messages {
		if msg["role"] != "system" {
			continue
		}
		content, _ := msg["content"].([]map[string]interface{})
		for _, part := range content {
			tool, _ := part["tool"].(map[string]interface{})
			if part["type"] != "tool_addition" || tool["type"] != "tool_definition" {
				continue
			}
			if def, ok := tool["definition"].(map[string]interface{}); ok {
				emitted[reflect.ValueOf(def).Pointer()] = true
			}
		}
	}
	choice := opts.ToolChoice
	selectedSDKName := ""
	if choice.Type == types.ToolChoiceTool {
		selectedSDKName = choice.ToolName
	}
	selectedProviderName := selectedSDKName
	if usesJSONResponseTool && choice.Type == types.ToolChoiceTool {
		isFunction := false
		for _, t := range opts.Tools {
			if t.Name == selectedSDKName {
				isFunction = t.Type == "" || t.Type == types.ToolTypeFunction
				break
			}
		}
		if !isFunction {
			selectedProviderName = names.providerName(selectedSDKName)
		}
	}
	for _, d := range defs {
		if !emitted[reflect.ValueOf(d.definition).Pointer()] {
			continue
		}
		name, hasName := d.definition["name"]
		if choice.Type != types.ToolChoiceNone &&
			(!usesJSONResponseTool || d.sdkName == selectedSDKName) &&
			hasName && name == selectedProviderName {
			continue
		}
		toolName := d.definition["type"]
		if hasName {
			toolName = name
		}
		desc := "tool choice 'none'"
		if choice.Type == types.ToolChoiceTool {
			desc = fmt.Sprintf("named tool choice '%s'", choice.ToolName)
		}
		return providererrors.NewArgumentError("toolChoice", fmt.Sprintf(
			"Inline tool '%v' conflicts with %s. "+
				"Inline definitions must respect the tool choice when forced tool use falls back or the JSON response tool is used.",
			toolName, desc), nil)
	}
	return nil
}

// BetaHeaderMessageThreads is the beta of Anthropic message threads.
const BetaHeaderMessageThreads = "message-threads-2026-08-12"

// threadIncompatibleContextManagement reports a context edit other than
// clear_thinking_20251015 with keep "all" (TS 6b101ac076).
func threadIncompatibleContextManagement(cm *ContextManagement) bool {
	if cm == nil {
		return false
	}
	for _, edit := range cm.Edits {
		e, ok := edit.(*ClearThinkingEdit)
		if !ok || e == nil {
			return true
		}
		if _, keepAll := e.Keep.(*KeepAllThinking); !keepAll {
			return true
		}
	}
	return false
}

// tsProviderString is the TS config.provider of this provider, used in
// error text: Go names the direct and MiniMax providers without the
// ".messages" suffix TS uses.
func (m *LanguageModel) tsProviderString() string {
	switch name := m.provider.Name(); name {
	case "anthropic", "minimax":
		return name + ".messages"
	default:
		return name
	}
}
