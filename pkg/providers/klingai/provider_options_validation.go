package klingai

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// klingaiOptionsSchema is klingaiVideoModelOptionsSchema
// (klingai-video-model-options.ts, a loose object: unknown keys pass
// through).
var klingaiOptionsSchema = func() providerutils.OptionSchema {
	null := providerutils.OptNullable
	number := null(providerutils.OptNumber)
	return providerutils.OptionSchema{
		"mode":           null(providerutils.OptEnum("std", "pro")),
		"pollIntervalMs": null(providerutils.OptPositive(false)),
		"pollTimeoutMs":  null(providerutils.OptPositive(false)),

		"negativePrompt": null(providerutils.OptString),
		"sound":          null(providerutils.OptEnum("on", "off")),
		"cfgScale":       number,
		"cameraControl": null(providerutils.OptObjectWith(providerutils.OptionSchema{
			"type": providerutils.OptEnum("simple", "down_back", "forward_up", "right_turn_forward", "left_turn_forward"),
			"config": null(providerutils.OptObject(providerutils.OptionSchema{
				"horizontal": number, "vertical": number, "pan": number,
				"tilt": number, "roll": number, "zoom": number,
			})),
		}, "type")),

		"multiShot": null(providerutils.OptBool),
		"shotType":  null(providerutils.OptEnum("customize", "intelligence")),
		"multiPrompt": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"index": providerutils.OptNumber, "prompt": providerutils.OptString, "duration": providerutils.OptString,
		}, "index", "prompt", "duration"), 0, -1)),
		"elementList": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"element_id": providerutils.OptNumber,
		}, "element_id"), 0, -1)),
		"voiceList": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"voice_id": providerutils.OptString,
		}, "voice_id"), 0, -1)),

		"imageTail":  null(providerutils.OptString),
		"staticMask": null(providerutils.OptString),
		"dynamicMasks": null(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
			"mask": providerutils.OptString,
			"trajectories": providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
				"x": providerutils.OptNumber, "y": providerutils.OptNumber,
			}, "x", "y"), 0, -1),
		}, "mask", "trajectories"), 0, -1)),

		"videoUrl":             null(providerutils.OptString),
		"characterOrientation": null(providerutils.OptEnum("image", "video")),
		"keepOriginalSound":    null(providerutils.OptEnum("yes", "no")),
		"watermarkEnabled":     null(providerutils.OptBool),
	}
}()

func validateProviderOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("klingai", providerOptions, klingaiOptionsSchema)
}
