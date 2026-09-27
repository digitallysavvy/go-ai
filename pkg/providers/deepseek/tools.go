package deepseek

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// prepareDeepSeekTools converts unified tools/tool choice to DeepSeek's wire
// format. It mirrors the TypeScript SDK's prepareTools, including strict
// tool-call validation: `strict: true` requires the beta base URL, and mixing
// strict and non-strict function tools in one request is rejected.
func (m *LanguageModel) prepareDeepSeekTools(tools []types.Tool, toolChoice types.ToolChoice) ([]map[string]interface{}, interface{}, []types.Warning, error) {
	if len(tools) == 0 {
		return nil, nil, nil, nil
	}

	var warnings []types.Warning
	var functionTools []types.Tool
	for _, t := range tools {
		if t.Type == "provider" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: fmt.Sprintf("provider-defined tool %s", firstNonEmptyDS(t.ProviderID, t.Name)),
			})
			continue
		}
		functionTools = append(functionTools, t)
	}

	hasStrict := false
	hasNonStrict := false
	for _, t := range functionTools {
		if t.Strict {
			hasStrict = true
		} else {
			hasNonStrict = true
		}
	}

	if hasStrict && !m.provider.supportsBeta() {
		return nil, nil, nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "DeepSeek strict tool calls",
			Message:       "DeepSeek strict tool calls require a beta base URL ending in `/beta`.",
		}
	}
	if hasStrict && hasNonStrict {
		return nil, nil, nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "mixed DeepSeek strict and non-strict tool calls",
			Message:       "DeepSeek strict mode requires every function tool in the request to set `strict: true`.",
		}
	}

	deepseekTools := tool.ToOpenAIFormat(functionTools)

	if toolChoice.Type == "" {
		return deepseekTools, nil, warnings, nil
	}

	return deepseekTools, tool.ConvertToolChoiceToOpenAI(toolChoice), warnings, nil
}
