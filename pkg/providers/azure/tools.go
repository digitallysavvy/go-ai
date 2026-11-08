package azure

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// CodeInterpreter creates the OpenAI Responses code_interpreter provider tool for Azure OpenAI.
func CodeInterpreter(config openaitool.CodeInterpreterConfig) types.Tool {
	return openaitool.CodeInterpreter(config)
}

// FileSearch creates the OpenAI Responses file_search provider tool for Azure OpenAI.
func FileSearch(config openaitool.FileSearchConfig) types.Tool {
	return openaitool.FileSearch(config)
}

// ImageGeneration creates the OpenAI Responses image_generation provider tool for Azure OpenAI.
func ImageGeneration(config openaitool.ImageGenerationConfig) types.Tool {
	return openaitool.ImageGeneration(config)
}

// WebSearch creates the stable OpenAI Responses web_search provider tool for Azure OpenAI.
func WebSearch(config openaitool.WebSearchConfig) types.Tool {
	return openaitool.WebSearch(config)
}

// WebSearchPreview creates the OpenAI Responses web_search_preview provider tool for Azure OpenAI.
//
// Deprecated: Use WebSearch instead.
func WebSearchPreview(config openaitool.WebSearchPreviewConfig) types.Tool {
	return openaitool.WebSearchPreview(config)
}

// ToolSearch creates the OpenAI Responses tool_search provider tool for Azure OpenAI.
// Mark function tools to defer with provider options {"azure": {"deferLoading": true}}.
func ToolSearch(args openaitool.ToolSearchArgs) types.Tool {
	return openaitool.ToolSearch(args)
}

// NewCustomTool creates the OpenAI Responses custom tool for Azure OpenAI
// (TS azure.tools.customTool, 27528dbf24). Register it with
// CustomTool.ToTool(name).
func NewCustomTool(opts ...openaitool.CustomToolOption) openaitool.CustomTool {
	return openaitool.NewCustomTool(opts...)
}

// ApplyPatch creates the OpenAI Responses apply_patch provider tool for
// Azure OpenAI (TS azure.tools.applyPatch, bf10079bc2).
func ApplyPatch() types.Tool {
	return responses.NewApplyPatchTool()
}

// Shell creates the OpenAI Responses shell provider tool for Azure OpenAI
// (TS azure.tools.shell, 5cc6d284e8).
func Shell(opts ...responses.ShellToolOption) types.Tool {
	return responses.NewShellTool(opts...)
}
