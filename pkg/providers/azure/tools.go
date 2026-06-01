package azure

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// WebSearch creates the stable OpenAI Responses web_search provider tool for Azure OpenAI.
func WebSearch(config openaitool.WebSearchConfig) types.Tool {
	return openaitool.WebSearch(config)
}

// WebSearchPreview creates the OpenAI Responses web_search_preview provider tool for Azure OpenAI.
func WebSearchPreview(config openaitool.WebSearchPreviewConfig) types.Tool {
	return openaitool.WebSearchPreview(config)
}
