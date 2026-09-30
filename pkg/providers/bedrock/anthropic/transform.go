package anthropic

// This file mirrors amazon-bedrock-anthropic-provider.ts's transformRequestBody
// (ai@7.0.113): it is registered as anthropic.Config.TransformRequestBodyWithBetas
// and runs on the wire-shaped Anthropic request body already built by the
// shared anthropic.LanguageModel (prompt, tools, thinking, tool_choice, betas).

// bedrockToolVersionMap upgrades tool versions Bedrock requires over the
// Anthropic SDK defaults.
var bedrockToolVersionMap = map[string]string{
	"bash_20241022":        "bash_20250124",
	"text_editor_20241022": "text_editor_20250728",
	"computer_20241022":    "computer_20250124",
}

// bedrockToolNameMap renames tools when their version is upgraded and the new
// version uses a different wire name.
var bedrockToolNameMap = map[string]string{
	"text_editor_20250728": "str_replace_based_edit_tool",
}

// bedrockToolBetaMap maps a tool's wire type to the anthropic-beta value
// Bedrock requires for it.
var bedrockToolBetaMap = map[string]string{
	"bash_20250124":                   "computer-use-2025-01-24",
	"bash_20241022":                   "computer-use-2024-10-22",
	"text_editor_20250124":            "computer-use-2025-01-24",
	"text_editor_20241022":            "computer-use-2024-10-22",
	"text_editor_20250429":            "computer-use-2025-01-24",
	"text_editor_20250728":            "computer-use-2025-01-24",
	"computer_20250124":               "computer-use-2025-01-24",
	"computer_20241022":               "computer-use-2024-10-22",
	"tool_search_tool_regex_20251119": "tool-search-tool-2025-10-19",
	// BM25 is not currently supported on Bedrock, but including the beta flag
	// so that Bedrock returns a more useful error message if it's used.
	"tool_search_tool_bm25_20251119": "tool-search-tool-2025-10-19",
}

// betaOrderedSet is a minimal ordered-set of beta flags, mirroring the
// insertion-order semantics of a JS Set<string>.
type betaOrderedSet struct {
	list []string
	seen map[string]bool
}

func (s *betaOrderedSet) addAll(vals []string) {
	for _, v := range vals {
		s.add(v)
	}
}

func (s *betaOrderedSet) add(v string) {
	if v == "" {
		return
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[v] {
		return
	}
	s.seen[v] = true
	s.list = append(s.list, v)
}

// transformRequestBodyWithBetas mirrors
// amazon-bedrock-anthropic-provider.ts's transformRequestBody(args, betas):
//   - drops "model" and "stream" (the URL and Accept header carry those);
//   - collapses tool_choice to {type, name?}, dropping disable_parallel_tool_use
//     (Bedrock rejects it on tool_choice);
//   - renames thinking.block_binding.prefix_mismatch_behavior to
//     thinking.block_binding.mismatch_behavior;
//   - strips each tool's eager_input_streaming, adding the
//     fine-grained-tool-streaming-2025-05-14 beta when it was true;
//   - upgrades tool versions/names/betas via the maps above;
//   - sets anthropic_beta (only when non-empty) and anthropic_version.
func transformRequestBodyWithBetas(body map[string]interface{}, betas []string, stream bool) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+2)
	for k, v := range body {
		switch k {
		case "model", "stream", "tool_choice", "thinking", "tools":
			continue
		default:
			out[k] = v
		}
	}

	requiredBetas := betaOrderedSet{}
	requiredBetas.addAll(betas)

	if toolChoice, ok := body["tool_choice"].(map[string]interface{}); ok && toolChoice != nil {
		tc := map[string]interface{}{"type": toolChoice["type"]}
		if name, ok := toolChoice["name"]; ok {
			tc["name"] = name
		}
		out["tool_choice"] = tc
	}

	if thinking, ok := body["thinking"].(map[string]interface{}); ok && thinking != nil {
		out["thinking"] = transformThinkingForBedrock(thinking)
	}

	if tools, ok := body["tools"].([]map[string]interface{}); ok && tools != nil {
		out["tools"] = transformToolsForBedrock(tools, &requiredBetas)
	}

	if len(requiredBetas.list) > 0 {
		out["anthropic_beta"] = requiredBetas.list
	}
	out["anthropic_version"] = AnthropicVersion
	return out
}

// transformThinkingForBedrock renames block_binding.prefix_mismatch_behavior
// to block_binding.mismatch_behavior. Bedrock's Messages schema does not
// recognize the Anthropic API's field name; some regions (us-east-1) alias it
// automatically, but others (eu-central-1) reject the request outright.
func transformThinkingForBedrock(thinking map[string]interface{}) map[string]interface{} {
	blockBinding, ok := thinking["block_binding"].(map[string]interface{})
	if !ok || blockBinding == nil {
		return thinking
	}
	out := make(map[string]interface{}, len(thinking))
	for k, v := range thinking {
		out[k] = v
	}
	out["block_binding"] = map[string]interface{}{
		"mismatch_behavior": blockBinding["prefix_mismatch_behavior"],
	}
	return out
}

// transformToolsForBedrock strips eager_input_streaming (translating it into
// the fine-grained-tool-streaming beta) and applies the version/name/beta
// maps above.
func transformToolsForBedrock(tools []map[string]interface{}, betas *betaOrderedSet) []map[string]interface{} {
	out := make([]map[string]interface{}, len(tools))
	for i, raw := range tools {
		tool := make(map[string]interface{}, len(raw))
		for k, v := range raw {
			if k == "eager_input_streaming" {
				continue
			}
			tool[k] = v
		}
		if eager, ok := raw["eager_input_streaming"].(bool); ok && eager {
			betas.add("fine-grained-tool-streaming-2025-05-14")
		}

		toolType, _ := tool["type"].(string)
		if toolType != "" {
			if newType, ok := bedrockToolVersionMap[toolType]; ok {
				if beta, ok := bedrockToolBetaMap[newType]; ok {
					betas.add(beta)
				}
				tool["type"] = newType
				if newName, ok := bedrockToolNameMap[newType]; ok {
					tool["name"] = newName
				}
				out[i] = tool
				continue
			}
			if beta, ok := bedrockToolBetaMap[toolType]; ok {
				betas.add(beta)
			}
			if newName, ok := bedrockToolNameMap[toolType]; ok {
				tool["name"] = newName
			}
		}
		out[i] = tool
	}
	return out
}
