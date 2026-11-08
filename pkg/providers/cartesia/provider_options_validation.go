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
	// TS 392d2fa3a4 / f10414f247 / 47b666b6c7: .optional(), null rejected.
	"volume":                    providerutils.OptRange(0.5, 2),
	"emotion":                   providerutils.OptEnum(cartesiaSpeechEmotions...),
	"locale":                    providerutils.OptString,
	"normalization":             providerutils.OptString,
	"pronunciationDictionaryId": providerutils.OptString,
}

// cartesiaSpeechEmotions is the TS cartesiaSpeechEmotions list.
var cartesiaSpeechEmotions = []string{
	"neutral", "happy", "excited", "enthusiastic", "elated", "euphoric", "triumphant", "amazed",
	"surprised", "flirtatious", "curious", "content", "peaceful", "serene", "calm", "grateful",
	"affectionate", "trust", "sympathetic", "anticipation", "mysterious", "angry", "mad", "outraged",
	"frustrated", "agitated", "threatened", "disgusted", "contempt", "envious", "sarcastic", "ironic",
	"sad", "dejected", "melancholic", "disappointed", "hurt", "guilty", "bored", "tired",
	"rejected", "nostalgic", "wistful", "apologetic", "hesitant", "insecure", "confused", "resigned",
	"anxious", "panicked", "alarmed", "scared", "proud", "confident", "distant", "skeptical",
	"contemplative", "determined",
}

// cartesiaTranscriptionOptionsSchema is cartesiaTranscriptionModelOptionsSchema
// (streaming members are optional, null rejected).
var cartesiaTranscriptionOptionsSchema = providerutils.OptionSchema{
	"language":               nullStr,
	"timestampGranularities": opt(providerutils.OptArray(providerutils.OptEnum("word"), 0, -1)),
	"streaming": providerutils.OptObject(providerutils.OptionSchema{
		"encoding":      providerutils.OptEnum("pcm_alaw", "pcm_f16le", "pcm_f32le", "pcm_mulaw", "pcm_s16le", "pcm_s32le"),
		"turnDetection": providerutils.OptBool,
		// TS 467b299146.
		"keyterms":              providerutils.OptArray(providerutils.OptString, 0, 100),
		"turnStartThreshold":    providerutils.OptNumber,
		"turnEagerEndThreshold": providerutils.OptNumber,
		"turnEndThreshold":      providerutils.OptNumber,
		"turnEndTimeoutMs":      providerutils.OptInt(nil, nil),
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
