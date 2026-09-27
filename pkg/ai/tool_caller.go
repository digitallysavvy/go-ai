package ai

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DirectToolCall is the sentinel caller name permitting the model to call a
// tool directly. Mirrors the TypeScript SDK's
// DIRECT_TOOL_CALL / AI_SDK_DIRECT_TOOL_CALL constant.
const DirectToolCall = "AI_SDK_DIRECT_TOOL_CALL"

// ExperimentalToolCallers configures, per tool name, which callers may
// invoke it. Each entry is a list of caller tool names, or DirectToolCall to
// permit the model to call the tool directly. A tool name absent from this
// map is unaffected by tool-caller routing. Mirrors the TypeScript SDK's
// Experimental_ToolCallers<TOOLS>.
type ExperimentalToolCallers map[string][]string

// ResolvedToolCallers is the validated form of ExperimentalToolCallers,
// produced by ResolveToolCallerConfiguration.
type ResolvedToolCallers map[string][]string

// ResolveToolCallerConfiguration validates a raw ExperimentalToolCallers
// configuration against the available tools. It mirrors the TypeScript SDK's
// resolveToolCallerConfiguration.
func ResolveToolCallerConfiguration(tools []types.Tool, toolCallers ExperimentalToolCallers) (ResolvedToolCallers, error) {
	if len(tools) == 0 || toolCallers == nil {
		return nil, nil
	}

	byName := make(map[string]types.Tool, len(tools))
	for _, t := range tools {
		byName[t.Name] = t
	}

	resolved := make(ResolvedToolCallers, len(toolCallers))
	for toolName, callers := range toolCallers {
		if _, ok := byName[toolName]; !ok {
			return nil, fmt.Errorf("experimental_toolCallers: unknown tool %q.", toolName)
		}

		out := make([]string, len(callers))
		for i, caller := range callers {
			if caller == DirectToolCall {
				out[i] = caller
				continue
			}
			callerTool, ok := byName[caller]
			if !ok || callerTool.ExperimentalToolCaller == nil {
				return nil, fmt.Errorf("experimental_toolCallers: tool %q contains an invalid caller.", toolName)
			}
			out[i] = caller
		}
		resolved[toolName] = out
	}
	return resolved, nil
}

// PrepareToolsForToolCallers splits tools into the set used for execution
// (bound local callers, provider options merged onto routed tools) and the
// set exposed to the model (direct/provider-routed tools only; local
// caller-only callees are hidden), and collects the conversation messages
// that announce local caller catalogs. It mirrors the TypeScript SDK's
// prepareToolsForToolCallers.
//
// When toolCallers is nil, executionTools and modelTools are both tools
// unchanged and toolCallerMessages is nil.
func PrepareToolsForToolCallers(tools []types.Tool, toolCallers ResolvedToolCallers) (executionTools, modelTools []types.Tool, toolCallerMessages []types.Message) {
	if len(tools) == 0 || toolCallers == nil {
		return tools, tools, nil
	}

	order := make([]string, 0, len(tools))
	execByName := make(map[string]types.Tool, len(tools))
	for _, t := range tools {
		if _, exists := execByName[t.Name]; !exists {
			order = append(order, t.Name)
		}
		execByName[t.Name] = t
	}

	modelByName := make(map[string]types.Tool, len(execByName))
	inModel := make(map[string]bool, len(execByName))
	for name, t := range execByName {
		modelByName[name] = t
		inModel[name] = true
	}

	// Tools routed through each local caller, keyed by caller name.
	localToolsByCaller := make(map[string]map[string]types.Tool)

	for toolName, callerNames := range toolCallers {
		tool, ok := execByName[toolName]
		if !ok {
			continue
		}

		availableDirectly := false
		availableToProvider := false
		prepared := tool

		for _, callerName := range callerNames {
			if callerName == DirectToolCall {
				availableDirectly = true
				continue
			}
			callerTool, ok := execByName[callerName]
			if !ok || callerTool.ExperimentalToolCaller == nil {
				continue
			}
			caller := callerTool.ExperimentalToolCaller

			if caller.Type == types.ToolCallerTypeProvider {
				availableToProvider = true
				var providerOpts map[string]interface{}
				if m, ok := prepared.ProviderOptions.(map[string]interface{}); ok {
					providerOpts = m
				}
				if caller.PrepareProviderOptions != nil {
					prepared.ProviderOptions = caller.PrepareProviderOptions(providerOpts)
				}
			} else {
				localTools := localToolsByCaller[callerName]
				if localTools == nil {
					localTools = make(map[string]types.Tool)
					localToolsByCaller[callerName] = localTools
				}
				localTools[toolName] = prepared
			}
		}

		execByName[toolName] = prepared

		if availableDirectly || availableToProvider {
			modelByName[toolName] = prepared
			inModel[toolName] = true
		} else {
			delete(modelByName, toolName)
			inModel[toolName] = false
		}
	}

	for _, callerName := range order {
		callerTool := execByName[callerName]
		caller := callerTool.ExperimentalToolCaller
		if caller == nil || caller.Type != types.ToolCallerTypeLocal || caller.Bind == nil {
			continue
		}

		callerTools := localToolsByCaller[callerName]
		if callerTools == nil {
			callerTools = map[string]types.Tool{}
		}
		bound := caller.Bind(callerTools)
		bound.Name = callerName
		execByName[callerName] = bound

		if inModel[callerName] {
			if caller.PrepareModelMessage == nil {
				modelByName[callerName] = bound
			} else if content := caller.PrepareModelMessage(callerTools); content != nil {
				toolCallerMessages = append(toolCallerMessages, types.Message{
					Role:    types.RoleUser,
					Content: []types.ContentPart{types.TextContent{Text: *content}},
				})
			}
		}
	}

	executionTools = make([]types.Tool, 0, len(order))
	modelTools = make([]types.Tool, 0, len(order))
	for _, name := range order {
		executionTools = append(executionTools, execByName[name])
		if inModel[name] {
			modelTools = append(modelTools, modelByName[name])
		}
	}

	return executionTools, modelTools, toolCallerMessages
}

// AppendToolCallerMessages appends toolCallerMessages to messages, skipping
// any whose single-text content already matches the most recent user
// message with plain text content, and skipping duplicates among the
// additions themselves. It mirrors the TypeScript SDK's
// appendToolCallerMessages. The returned slice is always a fresh copy; the
// input messages slice is never mutated.
func AppendToolCallerMessages(messages []types.Message, toolCallerMessages []types.Message) []types.Message {
	if len(toolCallerMessages) == 0 {
		return messages
	}

	existingUserText := make(map[string]bool)
	if latest := latestUserText(messages); latest != nil {
		existingUserText[*latest] = true
	}

	var additions []types.Message
	for _, msg := range toolCallerMessages {
		text, ok := singleTextContent(msg)
		if !ok || existingUserText[text] {
			continue
		}
		existingUserText[text] = true
		additions = append(additions, msg)
	}

	if len(additions) == 0 {
		return messages
	}

	out := make([]types.Message, len(messages), len(messages)+len(additions))
	copy(out, messages)
	return append(out, additions...)
}

// latestUserText returns the text of the last user message whose content is
// a single plain-text part, or nil if there is none.
func latestUserText(messages []types.Message) *string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != types.RoleUser {
			continue
		}
		if text, ok := singleTextContent(messages[i]); ok {
			return &text
		}
	}
	return nil
}

// singleTextContent returns the text of a message whose content is exactly
// one TextContent part.
func singleTextContent(msg types.Message) (string, bool) {
	if len(msg.Content) != 1 {
		return "", false
	}
	if tc, ok := msg.Content[0].(types.TextContent); ok {
		return tc.Text, true
	}
	return "", false
}
