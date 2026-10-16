package gladia

import (
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var (
	nullBool = providerutils.OptNullable(providerutils.OptBool)
	nullStr  = providerutils.OptNullable(providerutils.OptString)
	nullNum  = providerutils.OptNullable(providerutils.OptNumber)
	opt      = providerutils.OptNullable
	strArray = providerutils.OptArray(providerutils.OptString, 0, -1)
)

// optBoolOrArray is z.union([z.boolean(), z.array(z.any())]).
func optBoolOrArray(path string, v interface{}) error {
	if providerutils.OptBool(path, v) == nil {
		return nil
	}
	if err := providerutils.OptArray(nil, 0, -1)(path, v); err != nil {
		return fmt.Errorf("invalid input: expected boolean or array (at %s)", path)
	}
	return nil
}

// optVocabularyEntry is z.union([z.string(), z.object({value, intensity?,
// pronunciations?, language?})]).
func optVocabularyEntry(path string, v interface{}) error {
	if providerutils.OptString(path, v) == nil {
		return nil
	}
	return providerutils.OptObjectWith(providerutils.OptionSchema{
		"value":          providerutils.OptString,
		"intensity":      nullNum,
		"pronunciations": opt(strArray),
		"language":       nullStr,
	}, "value")(path, v)
}

// gladiaOptionsSchema is gladiaTranscriptionModelOptionsSchema.
var gladiaOptionsSchema = providerutils.OptionSchema{
	"contextPrompt":    nullStr,
	"customVocabulary": opt(optBoolOrArray),
	"customVocabularyConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"vocabulary":       providerutils.OptArray(optVocabularyEntry, 0, -1),
		"defaultIntensity": nullNum,
	}, "vocabulary")),
	"detectLanguage":      nullBool,
	"enableCodeSwitching": nullBool,
	"codeSwitchingConfig": opt(providerutils.OptObject(providerutils.OptionSchema{"languages": opt(strArray)})),
	"language":            nullStr,
	"callback":            nullBool,
	"callbackConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"url":    providerutils.OptString,
		"method": opt(providerutils.OptEnum("POST", "PUT")),
	}, "url")),
	"subtitles": nullBool,
	"subtitlesConfig": opt(providerutils.OptObject(providerutils.OptionSchema{
		"formats":                 opt(providerutils.OptArray(providerutils.OptEnum("srt", "vtt"), 0, -1)),
		"minimumDuration":         nullNum,
		"maximumDuration":         nullNum,
		"maximumCharactersPerRow": nullNum,
		"maximumRowsPerCaption":   nullNum,
		"style":                   opt(providerutils.OptEnum("default", "compliance")),
	})),
	"diarization": nullBool,
	"diarizationConfig": opt(providerutils.OptObject(providerutils.OptionSchema{
		"numberOfSpeakers": nullNum, "minSpeakers": nullNum, "maxSpeakers": nullNum, "enhanced": nullBool,
	})),
	"translation": nullBool,
	"translationConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"targetLanguages":         strArray,
		"model":                   opt(providerutils.OptEnum("base", "enhanced")),
		"matchOriginalUtterances": nullBool,
	}, "targetLanguages")),
	"summarization": nullBool,
	"summarizationConfig": opt(providerutils.OptObject(providerutils.OptionSchema{
		"type": opt(providerutils.OptEnum("general", "bullet_points", "concise")),
	})),
	"moderation":             nullBool,
	"namedEntityRecognition": nullBool,
	"chapterization":         nullBool,
	"nameConsistency":        nullBool,
	"customSpelling":         nullBool,
	"customSpellingConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"spellingDictionary": providerutils.OptRecord(nil, strArray),
	}, "spellingDictionary")),
	"structuredDataExtraction": nullBool,
	"structuredDataExtractionConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"classes": strArray,
	}, "classes")),
	"sentimentAnalysis": nullBool,
	"audioToLlm":        nullBool,
	"audioToLlmConfig": opt(providerutils.OptObjectWith(providerutils.OptionSchema{
		"prompts": strArray,
	}, "prompts")),
	"customMetadata":      opt(providerutils.OptRecord(nil, nil)),
	"sentences":           nullBool,
	"displayMode":         nullBool,
	"punctuationEnhanced": nullBool,
}

// validateOptions is parseProviderOptions with
// gladiaTranscriptionModelOptionsSchema: a mistyped option is an
// InvalidArgumentError.
func validateOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("gladia", providerOptions, gladiaOptionsSchema)
	return err
}
