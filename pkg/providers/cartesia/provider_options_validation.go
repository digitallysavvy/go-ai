package cartesia

import (
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var (
	nullStr = providerutils.OptNullable(providerutils.OptString)
	opt     = providerutils.OptNullable
)

// cartesiaSpeechOptionsSchema is cartesiaSpeechModelOptionsSchema.
var cartesiaSpeechOptionsSchema = providerutils.OptionSchema{
	"container":  opt(providerutils.OptEnum("raw", "wav", "mp3")),
	"encoding":   opt(providerutils.OptEnum("pcm_f32le", "pcm_s16le", "pcm_mulaw", "pcm_alaw")),
	"sampleRate": opt(providerutils.OptNumberLiterals(8000, 16000, 22050, 24000, 44100, 48000)),
	"bitRate":    opt(providerutils.OptNumberLiterals(32000, 64000, 96000, 128000, 192000)),
	"speed":      opt(providerutils.OptRange(0.6, 1.5)),
	"language":   nullStr,
}

// cartesiaTranscriptionOptionsSchema is cartesiaTranscriptionModelOptionsSchema
// (streaming members are optional, null rejected).
var cartesiaTranscriptionOptionsSchema = providerutils.OptionSchema{
	"language":               nullStr,
	"timestampGranularities": opt(providerutils.OptArray(providerutils.OptEnum("word"), 0, -1)),
	"streaming": providerutils.OptObject(providerutils.OptionSchema{
		"encoding":      providerutils.OptEnum("pcm_alaw", "pcm_f16le", "pcm_f32le", "pcm_mulaw", "pcm_s16le", "pcm_s32le"),
		"turnDetection": providerutils.OptBool,
	}),
}

func typedOptions(providerOptions map[string]interface{}) bool {
	switch providerOptions["cartesia"].(type) {
	case SpeechModelOptions, *SpeechModelOptions, TranscriptionModelOptions, *TranscriptionModelOptions:
		return true // typed options are well-formed by construction
	}
	return false
}

// validateSpeechOptions / validateTranscriptionOptions are parseProviderOptions
// with the TS option schemas: a mistyped option is an InvalidArgumentError.
func validateSpeechOptions(providerOptions map[string]interface{}) error {
	if typedOptions(providerOptions) {
		return nil
	}
	_, err := providerutils.ParseProviderOptions("cartesia", providerOptions, cartesiaSpeechOptionsSchema)
	return err
}

func validateTranscriptionOptions(providerOptions map[string]interface{}) error {
	if typedOptions(providerOptions) {
		return nil
	}
	_, err := providerutils.ParseProviderOptions("cartesia", providerOptions, cartesiaTranscriptionOptionsSchema)
	return err
}
