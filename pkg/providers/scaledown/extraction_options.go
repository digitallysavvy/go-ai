package scaledown

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// ExtractionModelOptions are the ScaleDown extraction provider options
// (providerOptions["scaledown"]). Mirrors TS ScaleDownExtractionModelOptions.
type ExtractionModelOptions struct {
	// Threshold is the minimum confidence for returned matches, from 0 to 1.
	// Nil means unset (the API default is 0).
	Threshold *float64 `json:"threshold,omitempty"`
}

// extractionOptionsSchema is TS scaledownExtractionModelOptionsSchema:
// z.object({ threshold: z.number().min(0).max(1).optional() }).
var extractionOptionsSchema = providerutils.OptionSchema{
	"threshold": providerutils.OptRange(0, 1),
}
