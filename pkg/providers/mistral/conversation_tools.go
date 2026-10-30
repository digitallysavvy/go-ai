package mistral

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// Provider tool IDs for the Mistral Conversations API built-in tools,
// mirroring TS mistralProviderToolNames.
const (
	ToolIDWebSearch        = "mistral.web_search"
	ToolIDWebSearchPremium = "mistral.web_search_premium"
)

var mistralProviderToolNames = map[string]string{
	ToolIDWebSearch:        "web_search",
	ToolIDWebSearchPremium: "web_search_premium",
}

// WebSearch creates the Mistral provider-executed web search tool. It is only
// supported by the Conversations API model (Provider.ConversationModel).
func WebSearch() types.Tool {
	return mistralProviderTool(ToolIDWebSearch, "Search the web using Mistral's Conversations API.")
}

// WebSearchPremium creates the Mistral provider-executed web search tool that
// also searches verified news. It is only supported by the Conversations API
// model (Provider.ConversationModel).
func WebSearchPremium() types.Tool {
	return mistralProviderTool(ToolIDWebSearchPremium, "Search the web and verified news using Mistral's Conversations API.")
}

func mistralProviderTool(id, description string) types.Tool {
	return types.Tool{
		Name:             id,
		ProviderID:       id,
		Description:      description,
		ProviderExecuted: true,
		// TS inputSchema z.object({ arguments: z.string() }): not strict.
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"arguments": map[string]interface{}{"type": "string"}},
			"required":   []string{"arguments"},
		},
	}
}

// mistralToolID returns the provider tool ID of t ("mistral.web_search" or
// "mistral.web_search_premium"), or "" for any other tool.
func mistralToolID(t types.Tool) string {
	for _, id := range []string{t.ProviderID, t.Name} {
		if _, ok := mistralProviderToolNames[id]; ok {
			return id
		}
	}
	return ""
}

// conversationToolNames maps between Mistral's wire tool names ("web_search")
// and the caller's tool names, mirroring TS createToolNameMapping.
type conversationToolNames struct {
	toCustom   map[string]string
	toProvider map[string]string
}

func newConversationToolNames(tools []types.Tool) conversationToolNames {
	m := conversationToolNames{toCustom: map[string]string{}, toProvider: map[string]string{}}
	for _, t := range tools {
		id := mistralToolID(t)
		if id == "" {
			continue
		}
		wire := mistralProviderToolNames[id]
		custom := t.Name
		if custom == "" || custom == id {
			custom = wire
		}
		m.toCustom[wire] = custom
		m.toProvider[custom] = wire
	}
	return m
}

func (m conversationToolNames) customName(wire string) string {
	if v, ok := m.toCustom[wire]; ok {
		return v
	}
	return wire
}

func (m conversationToolNames) providerName(custom string) string {
	if v, ok := m.toProvider[custom]; ok {
		return v
	}
	return custom
}

// prepareConversationTools converts tools and tool choice to the Conversations
// API shape, mirroring TS prepareConversationTools.
func prepareConversationTools(tools []types.Tool, choice types.ToolChoice) (wireTools []map[string]interface{}, toolChoice interface{}, warnings []types.Warning) {
	if len(tools) == 0 {
		return nil, nil, nil
	}
	wireTools = []map[string]interface{}{}
	for _, t := range tools {
		if id := mistralToolID(t); id != "" {
			wireTools = append(wireTools, map[string]interface{}{"type": mistralProviderToolNames[id]})
			continue
		}
		if t.ProviderExecuted || t.ProviderID != "" {
			feature := t.ProviderID
			if feature == "" {
				feature = t.Name
			}
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "provider-defined tool " + feature})
			continue
		}
		wireTools = append(wireTools, tool.ToJSONSchema(t))
	}
	switch choice.Type {
	case "":
		return wireTools, nil, warnings
	case types.ToolChoiceAuto:
		return wireTools, "auto", warnings
	case types.ToolChoiceNone:
		return wireTools, "none", warnings
	case types.ToolChoiceRequired:
		return wireTools, "any", warnings
	case types.ToolChoiceTool:
		// Mistral has no direct tool mode: filter the tools and force the
		// choice through "any".
		name := newConversationToolNames(tools).providerName(choice.ToolName)
		filtered := []map[string]interface{}{}
		for _, w := range wireTools {
			switch w["type"] {
			case "function":
				if fn, ok := w["function"].(map[string]interface{}); ok && fn["name"] == name {
					filtered = append(filtered, w)
				}
			case name:
				filtered = append(filtered, w)
			}
		}
		return filtered, "any", warnings
	}
	return wireTools, nil, warnings
}
