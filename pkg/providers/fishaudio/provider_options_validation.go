package fishaudio

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// fishAudioSpeechOptionsSchema is fishAudioSpeechModelOptionsSchema
// (fish-audio-speech-model-options.ts).
var fishAudioSpeechOptionsSchema = func() providerutils.OptionSchema {
	hundred, threeHundred, zero := 100, 300, 0
	unit := providerutils.OptRange(0, 1)
	return providerutils.OptionSchema{
		// z.union([z.string(), z.array(z.string())])
		"referenceId": func(path string, v interface{}) error {
			if err := providerutils.OptString(path, v); err == nil {
				return nil
			}
			return providerutils.OptArray(providerutils.OptString, 0, -1)(path, v)
		},
		"sampleRate":                providerutils.OptPositive(true),
		"mp3Bitrate":                providerutils.OptNumberLiterals(64, 128, 192),
		"opusBitrate":               providerutils.OptNumberLiterals(-1000, 24000, 32000, 48000, 64000),
		"latency":                   providerutils.OptEnum("low", "normal", "balanced"),
		"volume":                    providerutils.OptNumber,
		"normalizeLoudness":         providerutils.OptBool,
		"temperature":               unit,
		"topP":                      unit,
		"chunkLength":               providerutils.OptInt(&hundred, &threeHundred),
		"minChunkLength":            providerutils.OptInt(&zero, &hundred),
		"normalize":                 providerutils.OptBool,
		"maxNewTokens":              providerutils.OptPositive(true),
		"repetitionPenalty":         providerutils.OptNumber,
		"conditionOnPreviousChunks": providerutils.OptBool,
		"earlyStopThreshold":        unit,
		"features":                  providerutils.OptArray(providerutils.OptString, 0, -1),
	}
}()

// fishAudioTranscriptionOptionsSchema is fishAudioTranscriptionModelOptionsSchema.
var fishAudioTranscriptionOptionsSchema = providerutils.OptionSchema{
	"language":         providerutils.OptString,
	"ignoreTimestamps": providerutils.OptBool,
}

func validateSpeechOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("fishAudio", providerOptions, fishAudioSpeechOptionsSchema)
}

func validateTranscriptionOptions(providerOptions map[string]interface{}) error {
	return providerutils.ValidateMapProviderOptions("fishAudio", providerOptions, fishAudioTranscriptionOptionsSchema)
}
