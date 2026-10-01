package bedrock

import (
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// ImageModelOptions are typed, validated Amazon Bedrock image provider
// options (amazonBedrock / bedrock provider-options namespace). Ports TS
// amazon-bedrock-image-model-options.ts's zod schema.
type ImageModelOptions struct {
	// Quality: "standard" or "premium".
	Quality string
	// NegativeText describes what to avoid in the generated image.
	NegativeText string
	// CfgScale controls how closely generation follows the prompt.
	CfgScale    float64
	CfgScaleSet bool
	// Style is one of the Amazon Nova style presets.
	Style string
	// TaskType selects the Titan/Nova image task.
	TaskType string
	// MaskPrompt is a natural-language description of the mask region.
	MaskPrompt string
	// OutPaintingMode: "DEFAULT" or "PRECISE".
	OutPaintingMode string
	// SimilarityStrength controls how closely a variation matches the source.
	SimilarityStrength    float64
	SimilarityStrengthSet bool
}

var bedrockImageStyles = map[string]bool{
	"3D_ANIMATED_FAMILY_FILM":    true,
	"DESIGN_SKETCH":              true,
	"FLAT_VECTOR_ILLUSTRATION":   true,
	"GRAPHIC_NOVEL_ILLUSTRATION": true,
	"MAXIMALISM":                 true,
	"MIDCENTURY_RETRO":           true,
	"PHOTOREALISM":               true,
	"SOFT_DIGITAL_PAINTING":      true,
}

var bedrockImageTaskTypes = map[string]bool{
	"TEXT_IMAGE":         true,
	"IMAGE_VARIATION":    true,
	"INPAINTING":         true,
	"OUTPAINTING":        true,
	"BACKGROUND_REMOVAL": true,
}

// Validate checks enum fields and returns an *providererrors.InvalidArgumentError
// describing the first invalid value found.
func (o *ImageModelOptions) Validate() error {
	if o == nil {
		return nil
	}
	if o.Quality != "" && o.Quality != "standard" && o.Quality != "premium" {
		return &providererrors.InvalidArgumentError{Field: "quality", Message: fmt.Sprintf("must be 'standard' or 'premium', got %q", o.Quality)}
	}
	if o.Style != "" && !bedrockImageStyles[o.Style] {
		return &providererrors.InvalidArgumentError{Field: "style", Message: fmt.Sprintf("unsupported style %q", o.Style)}
	}
	if o.TaskType != "" && !bedrockImageTaskTypes[o.TaskType] {
		return &providererrors.InvalidArgumentError{Field: "taskType", Message: fmt.Sprintf("unsupported taskType %q", o.TaskType)}
	}
	if o.OutPaintingMode != "" && o.OutPaintingMode != "DEFAULT" && o.OutPaintingMode != "PRECISE" {
		return &providererrors.InvalidArgumentError{Field: "outPaintingMode", Message: fmt.Sprintf("must be 'DEFAULT' or 'PRECISE', got %q", o.OutPaintingMode)}
	}
	return nil
}

// parseBedrockImageOptions parses the amazonBedrock/bedrock provider-options
// namespace into a typed, validated ImageModelOptions.
func parseBedrockImageOptions(providerOptions map[string]interface{}) (*ImageModelOptions, error) {
	raw := bedrockImageOptions(providerOptions)
	if raw == nil {
		return &ImageModelOptions{}, nil
	}
	out := &ImageModelOptions{
		Quality:         stringOption(raw["quality"]),
		NegativeText:    stringOption(raw["negativeText"]),
		Style:           stringOption(raw["style"]),
		TaskType:        stringOption(raw["taskType"]),
		MaskPrompt:      stringOption(raw["maskPrompt"]),
		OutPaintingMode: stringOption(raw["outPaintingMode"]),
	}
	if v, ok := numberOption(raw["cfgScale"]); ok {
		if f, ok := toFloat64(v); ok {
			out.CfgScale = f
			out.CfgScaleSet = true
		}
	}
	if v, ok := numberOption(raw["similarityStrength"]); ok {
		if f, ok := toFloat64(v); ok {
			out.SimilarityStrength = f
			out.SimilarityStrengthSet = true
		}
	}
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

func toFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}
