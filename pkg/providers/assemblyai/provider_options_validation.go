package assemblyai

import (
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

var (
	nullBool = providerutils.OptNullable(providerutils.OptBool)
	nullStr  = providerutils.OptNullable(providerutils.OptString)
	nullNum  = providerutils.OptNullable(providerutils.OptNumber)
	nullInt  = providerutils.OptNullable(providerutils.OptInt(nil, nil))
	opt      = providerutils.OptNullable
	strArray = providerutils.OptArray(providerutils.OptString, 0, -1)
	unit     = providerutils.OptRange(0, 1)
)

func intRange(min, max int) providerutils.OptionCheck {
	return providerutils.OptNullable(providerutils.OptInt(&min, &max))
}

// assemblyaiOptionsSchema is assemblyaiTranscriptionModelOptionsSchema.
var assemblyaiOptionsSchema = providerutils.OptionSchema{
	"audioEndAt":              nullInt,
	"audioStartFrom":          nullInt,
	"autoChapters":            nullBool,
	"autoHighlights":          nullBool,
	"boostParam":              nullStr,
	"contentSafety":           nullBool,
	"contentSafetyConfidence": intRange(25, 100),
	"customSpelling": opt(providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"from": strArray,
		"to":   providerutils.OptString,
	}, "from", "to"), 0, -1)),
	"disfluencies":                nullBool,
	"domain":                      nullStr,
	"entityDetection":             nullBool,
	"filterProfanity":             nullBool,
	"formatText":                  nullBool,
	"iabCategories":               nullBool,
	"keytermsPrompt":              opt(strArray),
	"languageCode":                nullStr,
	"languageConfidenceThreshold": nullNum,
	"languageDetection":           nullBool,
	"languageDetectionOptions": opt(providerutils.OptObject(providerutils.OptionSchema{
		"expectedLanguages":                opt(strArray),
		"fallbackLanguage":                 nullStr,
		"codeSwitching":                    nullBool,
		"codeSwitchingConfidenceThreshold": opt(unit),
	})),
	"multichannel":   nullBool,
	"prompt":         nullStr,
	"punctuate":      nullBool,
	"redactPii":      nullBool,
	"redactPiiAudio": nullBool,
	"redactPiiAudioOptions": opt(providerutils.OptObject(providerutils.OptionSchema{
		"returnRedactedNoSpeechAudio":  nullBool,
		"overrideAudioRedactionMethod": opt(providerutils.OptEnum("silence")),
	})),
	"redactPiiAudioQuality":     nullStr,
	"redactPiiPolicies":         opt(strArray),
	"redactPiiReturnUnredacted": nullBool,
	"redactPiiSub":              nullStr,
	"redactStaticEntities":      opt(providerutils.OptRecord(nil, strArray)),
	"removeAudioTags":           opt(providerutils.OptEnum("all", "speaker")),
	"sentimentAnalysis":         nullBool,
	"speakerLabels":             nullBool,
	"speakerOptions": opt(providerutils.OptObject(providerutils.OptionSchema{
		"minSpeakersExpected": nullInt,
		"maxSpeakersExpected": nullInt,
	})),
	"speakersExpected":       nullInt,
	"speechThreshold":        opt(unit),
	"summarization":          nullBool,
	"summaryModel":           nullStr,
	"summaryType":            nullStr,
	"temperature":            opt(unit),
	"webhookAuthHeaderName":  nullStr,
	"webhookAuthHeaderValue": nullStr,
	"webhookUrl":             nullStr,
	"wordBoost":              opt(strArray),
}

// validateOptions is parseProviderOptions with
// assemblyaiTranscriptionModelOptionsSchema: a mistyped option is an
// InvalidArgumentError.
func validateOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("assemblyai", providerOptions, assemblyaiOptionsSchema)
	return err
}
