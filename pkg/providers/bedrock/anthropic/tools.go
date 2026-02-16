package anthropic

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// UpgradeToolVersion upgrades Anthropic tool versions for Bedrock compatibility
func (p *BedrockAnthropicProvider) UpgradeToolVersion(tool types.Tool) types.Tool {
	if newVersion, ok := p.toolVersionMap[tool.Name]; ok {
		tool.Name = newVersion
	}
	return tool
}

// MapToolName maps Anthropic tool names to Bedrock-required names
func (p *BedrockAnthropicProvider) MapToolName(tool types.Tool) types.Tool {
	if newName, ok := p.toolNameMap[tool.Name]; ok {
		tool.Name = newName
	}
	return tool
}

// PrepareTools processes tools for Bedrock compatibility
// This includes:
// 1. Upgrading tool versions (e.g., bash_20241022 → bash_20250124)
// 2. Mapping tool names (e.g., text_editor_20250728 → str_replace_based_edit_tool)
func (p *BedrockAnthropicProvider) PrepareTools(tools []types.Tool) []types.Tool {
	if len(tools) == 0 {
		return tools
	}

	result := make([]types.Tool, len(tools))
	for i, tool := range tools {
		// First upgrade version
		tool = p.UpgradeToolVersion(tool)
		// Then map name if needed
		tool = p.MapToolName(tool)
		result[i] = tool
	}
	return result
}

// GetBetaHeaders returns the anthropic_beta headers needed for the given tools
// This is required for computer use tools
func (p *BedrockAnthropicProvider) GetBetaHeaders(tools []types.Tool) []string {
	if len(tools) == 0 {
		return nil
	}

	betaSet := make(map[string]bool)
	for _, tool := range tools {
		if beta, ok := p.toolBetaMap[tool.Name]; ok {
			betaSet[beta] = true
		}
	}

	if len(betaSet) == 0 {
		return nil
	}

	// Convert set to slice
	betas := make([]string, 0, len(betaSet))
	for beta := range betaSet {
		betas = append(betas, beta)
	}

	return betas
}

// IsComputerUseTool checks if a tool is a computer use tool
func (p *BedrockAnthropicProvider) IsComputerUseTool(toolName string) bool {
	_, ok := p.toolBetaMap[toolName]
	return ok
}
