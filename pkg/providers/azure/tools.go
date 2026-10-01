package azure

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
func WebSearchPreview(config openaitool.WebSearchPreviewConfig) types.Tool {
	return openaitool.WebSearchPreview(config)
}
