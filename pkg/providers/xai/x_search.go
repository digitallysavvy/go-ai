package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// XSearchConfig contains configuration for the xAI XSearch tool.
type XSearchConfig struct {
	// AllowedXHandles limits search results to posts from specific X handles.
	AllowedXHandles []string

	// ExcludedXHandles omits results from specific X handles.
	ExcludedXHandles []string

	// FromDate limits results to posts after this date (YYYY-MM-DD).
	FromDate *string

	// ToDate limits results to posts before this date (YYYY-MM-DD).
	ToDate *string

	// EnableImageUnderstanding enables image understanding in search results.
	EnableImageUnderstanding *bool

	// EnableVideoUnderstanding enables video understanding in search results.
	EnableVideoUnderstanding *bool
}

// XSearch creates a provider-executed tool that searches X (Twitter) posts.
// Search is handled by xAI's servers and returns current information from X.
//
// Example:
//
//	tool := xai.XSearch(xai.XSearchConfig{
//	    AllowedXHandles: []string{"elonmusk"},
//	})
func XSearch(config XSearchConfig) types.Tool {
	args := map[string]interface{}{}
	if config.AllowedXHandles != nil {
		args["allowedXHandles"] = config.AllowedXHandles
	}
	if config.ExcludedXHandles != nil {
		args["excludedXHandles"] = config.ExcludedXHandles
	}
	if config.FromDate != nil {
		args["fromDate"] = *config.FromDate
	}
	if config.ToDate != nil {
		args["toDate"] = *config.ToDate
	}
	if config.EnableImageUnderstanding != nil {
		args["enableImageUnderstanding"] = *config.EnableImageUnderstanding
	}
	if config.EnableVideoUnderstanding != nil {
		args["enableVideoUnderstanding"] = *config.EnableVideoUnderstanding
	}
	return xaiProviderTool(types.Tool{
		Name:             "xai.x_search",
		Description:      "Search X (Twitter) posts for current information.",
		Parameters:       xaiSchema(xaiEmptyInputSchemaJSON),
		OutputSchema:     xaiSchema(xaiXSearchOutputSchemaJSON),
		ProviderExecuted: true,
		ProviderOptions:  config,
		Execute:          providerExecutedNoop("xai.x_search"),
	}, args)
}
