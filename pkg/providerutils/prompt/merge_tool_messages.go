package prompt

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// MergeConsecutiveToolMessages combines consecutive tool-role messages into a
// single tool message, mirroring TS convertToLanguageModelPrompt's "combine
// consecutive tool messages into a single tool message" step
// (packages/ai/src/prompt/convert-to-language-model-prompt.ts, hash
// 33647d7). Provider message converters call this once, up front, on the raw
// standardized message list -- matching TS, where combining happens in the
// shared core before any provider-specific conversion runs.
//
// Before a tool message is folded into the previous combined tool message,
// the previous message's own message-level ProviderOptions are deep-merged
// onto its LAST content part's ProviderOptions (part-level values win
// conflicts), so that information is not silently dropped once the combined
// message's top-level ProviderOptions are overwritten by the next message's.
// The combined message ends up keeping the latest message's ProviderOptions,
// exactly as TS does.
//
// Only ToolResultContent and ToolErrorContent parts carry a ProviderOptions
// field in this SDK (mirroring TS ToolResultPart); ToolApprovalResponseContent
// has none in either SDK, so the push-down is a no-op when the last part is
// an approval response -- consistent with TS, where nothing downstream reads
// a ToolApprovalResponse's providerOptions.
func MergeConsecutiveToolMessages(messages []types.Message) []types.Message {
	if len(messages) == 0 {
		return messages
	}

	combined := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != types.RoleTool {
			combined = append(combined, msg)
			continue
		}

		last := len(combined) - 1
		if last >= 0 && combined[last].Role == types.RoleTool {
			lastMsg := &combined[last]
			if n := len(lastMsg.Content); n > 0 && lastMsg.ProviderOptions != nil {
				lastMsg.Content[n-1] = pushDownProviderOptions(lastMsg.Content[n-1], lastMsg.ProviderOptions)
			}
			lastMsg.Content = append(append([]types.ContentPart{}, lastMsg.Content...), msg.Content...)
			lastMsg.ProviderOptions = msg.ProviderOptions
			continue
		}

		combined = append(combined, msg)
	}
	return combined
}

// pushDownProviderOptions deep-merges base into part's own ProviderOptions
// (part-level values win conflicts), returning an updated copy of part. Parts
// with no ProviderOptions field (ToolApprovalResponseContent) are returned
// unchanged.
func pushDownProviderOptions(part types.ContentPart, base map[string]interface{}) types.ContentPart {
	switch p := part.(type) {
	case types.ToolResultContent:
		p.ProviderOptions = mergeProviderOptionsDeep(base, p.ProviderOptions)
		return p
	case types.ToolErrorContent:
		p.ProviderOptions = mergeProviderOptionsDeep(base, p.ProviderOptions)
		return p
	default:
		return part
	}
}

// mergeProviderOptionsDeep mirrors TS util/merge-objects.ts mergeObjects:
// overrides win over base on conflicting keys, nested maps are merged
// recursively, and any other value (including slices) is replaced wholesale
// by the overrides value.
func mergeProviderOptionsDeep(base, overrides map[string]interface{}) map[string]interface{} {
	if base == nil && overrides == nil {
		return nil
	}
	if base == nil {
		return overrides
	}
	if overrides == nil {
		return base
	}

	result := make(map[string]interface{}, len(base)+len(overrides))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range overrides {
		if v == nil {
			continue
		}
		if baseVal, ok := result[k]; ok {
			if baseMap, ok1 := baseVal.(map[string]interface{}); ok1 {
				if overrideMap, ok2 := v.(map[string]interface{}); ok2 {
					result[k] = mergeProviderOptionsDeep(baseMap, overrideMap)
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}
