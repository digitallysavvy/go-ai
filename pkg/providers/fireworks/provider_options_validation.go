package fireworks

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// fireworksImageOptionsSchema is fireworksImageModelOptionsSchema
// (fireworks-image-model-options.ts, a loose object).
var fireworksImageOptionsSchema = func() providerutils.OptionSchema {
	zero, six := 0, 6
	return providerutils.OptionSchema{
		"guidance_scale":      providerutils.OptNumber,
		"num_inference_steps": providerutils.OptInt(nil, nil),
		"output_format":       providerutils.OptEnum("jpeg", "png"),
		"webhook_url":         providerutils.OptString,
		"webhook_secret":      providerutils.OptString,
		"prompt_upsampling":   providerutils.OptBool,
		"safety_tolerance":    providerutils.OptInt(&zero, &six),
		"cfg_scale":           providerutils.OptNumber,
		"steps":               providerutils.OptInt(nil, nil),
	}
}()

func validateImageOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("fireworks", providerOptions, fireworksImageOptionsSchema)
	return err
}
