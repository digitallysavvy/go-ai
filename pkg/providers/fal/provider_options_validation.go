package fal

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var (
	nullBool = providerutils.OptNullable(providerutils.OptBool)
	nullStr  = providerutils.OptNullable(providerutils.OptString)
	nullNum  = providerutils.OptNullable(providerutils.OptNumber)
	opt      = providerutils.OptNullable
)

// optSafetyTolerance is z.enum(['1'..'6']).or(z.number().min(1).max(6)).
func optSafetyTolerance(path string, v interface{}) error {
	if providerutils.OptEnum("1", "2", "3", "4", "5", "6")(path, v) == nil {
		return nil
	}
	if providerutils.OptRange(1, 6)(path, v) == nil {
		return nil
	}
	return fmt.Errorf("invalid input: expected \"1\"..\"6\" or a number between 1 and 6 (at %s)", path)
}

var (
	guidanceScale = opt(providerutils.OptRange(1, 20))
	inferenceStep = opt(providerutils.OptRange(1, 50))
	outputFormat  = opt(providerutils.OptEnum("jpeg", "png"))
)

// falImageOptionsSchema is falImageModelOptionsSchema (a z.looseObject, so
// model-specific keys pass through).
var falImageOptionsSchema = providerutils.OptionSchema{
	"imageUrl":              nullStr,
	"maskUrl":               nullStr,
	"guidanceScale":         guidanceScale,
	"numInferenceSteps":     inferenceStep,
	"enableSafetyChecker":   nullBool,
	"outputFormat":          outputFormat,
	"syncMode":              nullBool,
	"strength":              nullNum,
	"acceleration":          opt(providerutils.OptEnum("none", "regular", "high")),
	"safetyTolerance":       opt(optSafetyTolerance),
	"useMultipleImages":     nullBool,
	"image_url":             nullStr,
	"mask_url":              nullStr,
	"guidance_scale":        guidanceScale,
	"num_inference_steps":   inferenceStep,
	"enable_safety_checker": nullBool,
	"output_format":         outputFormat,
	"sync_mode":             nullBool,
	"safety_tolerance":      opt(optSafetyTolerance),
}

var falEmotions = []string{"happy", "sad", "angry", "fearful", "disgusted", "surprised", "neutral"}

var falLanguageBoosts = []string{
	"Chinese", "Chinese,Yue", "English", "Arabic", "Russian", "Spanish", "French", "Portuguese", "German", "Turkish",
	"Dutch", "Ukrainian", "Vietnamese", "Indonesian", "Japanese", "Italian", "Korean", "Thai", "Polish", "Romanian",
	"Greek", "Czech", "Finnish", "Hindi", "auto",
}

// falSpeechOptionsSchema is falSpeechModelOptionsSchema (loose).
var falSpeechOptionsSchema = providerutils.OptionSchema{
	"voice_setting": opt(providerutils.OptObject(providerutils.OptionSchema{
		"speed": nullNum, "vol": nullNum, "voice_id": nullStr, "pitch": nullNum,
		"english_normalization": nullBool,
		"emotion":               opt(providerutils.OptEnum(falEmotions...)),
	})),
	"audio_setting":      opt(providerutils.OptRecord(nil, nil)),
	"language_boost":     opt(providerutils.OptEnum(falLanguageBoosts...)),
	"pronunciation_dict": opt(providerutils.OptRecord(nil, providerutils.OptString)),
}

// falTranscriptionOptionsSchema is falTranscriptionModelOptionsSchema.
var falTranscriptionOptionsSchema = providerutils.OptionSchema{
	"language":    nullStr,
	"diarize":     nullBool,
	"chunkLevel":  opt(providerutils.OptEnum("segment", "word")),
	"version":     opt(providerutils.OptEnum("3")),
	"batchSize":   nullNum,
	"numSpeakers": nullNum,
}

// falVideoOptionsSchema is falVideoModelOptionsSchema (loose).
var falVideoOptionsSchema = providerutils.OptionSchema{
	"loop":            nullBool,
	"motionStrength":  opt(providerutils.OptRange(0, 1)),
	"resolution":      nullStr,
	"negativePrompt":  nullStr,
	"promptOptimizer": nullBool,
}

func parseFalOptions(schema providerutils.OptionSchema, providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("fal", providerOptions, schema)
	return err
}

// The validate* helpers are parseProviderOptions with the TS option schemas:
// a mistyped option is an InvalidArgumentError.
func validateImageOptions(providerOptions map[string]interface{}) error {
	return parseFalOptions(falImageOptionsSchema, providerOptions)
}
func validateSpeechOptions(providerOptions map[string]interface{}) error {
	return parseFalOptions(falSpeechOptionsSchema, providerOptions)
}
func validateTranscriptionOptions(providerOptions map[string]interface{}) error {
	return parseFalOptions(falTranscriptionOptionsSchema, providerOptions)
}
func validateVideoOptions(providerOptions map[string]interface{}) error {
	return parseFalOptions(falVideoOptionsSchema, providerOptions)
}
