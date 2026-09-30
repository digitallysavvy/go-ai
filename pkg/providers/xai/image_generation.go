package xai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// ImageGenerationConfig contains configuration for the xAI server-side
// image_generation tool (row fa2c2bb).
type ImageGenerationConfig struct {
	// Action restricts what the tool can do: "auto" (default, generate and
	// edit), "generate" (text-to-image only), or "edit" (image editing only).
	Action string
}

// ImageGeneration creates a provider-executed tool that lets the model
// generate or edit images inline during a Responses API conversation. The
// generated image (base64) and the model-written prompt are returned as a
// tool-result; see the Responses API handling of "image_generation_call".
//
// Example:
//
//	tool := xai.ImageGeneration(xai.ImageGenerationConfig{Action: "generate"})
func ImageGeneration(config ImageGenerationConfig) types.Tool {
	return types.Tool{
		Name:             "xai.image_generation",
		Description:      "Generate or edit images inline using xAI's server-side image generation tool.",
		ProviderExecuted: true,
		ProviderOptions:  config,
		Execute:          providerExecutedNoop("xai.image_generation"),
	}
}
