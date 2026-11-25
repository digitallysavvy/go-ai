package together

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// togetherImageOptionsSchema is togetheraiImageModelOptionsSchema
// (togetherai-image-model-options.ts, a loose object).
var togetherImageOptionsSchema = providerutils.OptionSchema{
	"steps":                  providerutils.OptNullable(providerutils.OptNumber),
	"guidance":               providerutils.OptNullable(providerutils.OptNumber),
	"negative_prompt":        providerutils.OptNullable(providerutils.OptString),
	"disable_safety_checker": providerutils.OptNullable(providerutils.OptBool),
}

// togetherRerankingOptionsSchema is togetheraiRerankingModelOptionsSchema.
var togetherRerankingOptionsSchema = providerutils.OptionSchema{
	"rankFields": providerutils.OptArray(providerutils.OptString, 0, -1),
}

func validateImageOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("togetherai", providerOptions, togetherImageOptionsSchema)
	return err
}

func validateRerankingOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("togetherai", providerOptions, togetherRerankingOptionsSchema)
	if err != nil {
		return err
	}
	// This package also accepts "together" as a fallback key.
	_, err = providerutils.ParseProviderOptions("together", providerOptions, togetherRerankingOptionsSchema)
	return err
}
