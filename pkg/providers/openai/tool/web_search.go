package tool

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// WebSearchLocation provides approximate user location for OpenAI web search.
type WebSearchLocation struct {
	Type     string `json:"type,omitempty"`
	Country  string `json:"country,omitempty"`
	City     string `json:"city,omitempty"`
	Region   string `json:"region,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// WebSearchFilters restricts OpenAI web search results.
type WebSearchFilters struct {
	AllowedDomains []string `json:"allowedDomains,omitempty"`
}

// WebSearchConfig configures the stable OpenAI Responses web_search tool.
type WebSearchConfig struct {
	ExternalWebAccess *bool              `json:"externalWebAccess,omitempty"`
	Filters           *WebSearchFilters  `json:"filters,omitempty"`
	SearchContextSize string             `json:"searchContextSize,omitempty"`
	UserLocation      *WebSearchLocation `json:"userLocation,omitempty"`
}

// WebSearchPreviewConfig configures the OpenAI Responses web_search_preview tool.
type WebSearchPreviewConfig struct {
	SearchContextSize string             `json:"searchContextSize,omitempty"`
	UserLocation      *WebSearchLocation `json:"userLocation,omitempty"`
}

// WebSearch creates an OpenAI provider-executed web_search tool.
func WebSearch(config WebSearchConfig) types.Tool {
	return types.Tool{
		Name:             "openai.web_search",
		Description:      "Search the web using OpenAI Responses.",
		ProviderExecuted: true,
		ProviderOptions:  config,
		Parameters:       map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}

// WebSearchPreview creates an OpenAI provider-executed web_search_preview tool.
func WebSearchPreview(config WebSearchPreviewConfig) types.Tool {
	return types.Tool{
		Name:             "openai.web_search_preview",
		Description:      "Search the web using OpenAI Responses preview.",
		ProviderExecuted: true,
		ProviderOptions:  config,
		Parameters:       map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}
}
