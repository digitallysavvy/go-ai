package gladia

// TranscriptionModelOptions contains Gladia-specific transcription options,
// mirroring the TypeScript SDK's gladiaTranscriptionModelOptionsSchema.
// See https://docs.gladia.io/api-reference/v2/pre-recorded/init
type TranscriptionModelOptions struct {
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API; it
	// is not sent and gives an unsupported warning.
	ContextPrompt string `json:"contextPrompt,omitempty"`
	// CustomVocabulary is a boolean. Deprecated: the array form; use true
	// with CustomVocabularyConfig instead (an array is translated with a
	// deprecated warning).
	CustomVocabulary       interface{}             `json:"customVocabulary,omitempty"`
	CustomVocabularyConfig *CustomVocabularyConfig `json:"customVocabularyConfig,omitempty"`
	// Deprecated: use LanguageConfig (translated with a deprecated warning).
	DetectLanguage *bool `json:"detectLanguage,omitempty"`
	// Deprecated: use LanguageConfig.CodeSwitching (translated with a
	// deprecated warning).
	EnableCodeSwitching *bool `json:"enableCodeSwitching,omitempty"`
	// Deprecated: use LanguageConfig (translated with a deprecated warning).
	CodeSwitchingConfig *CodeSwitchingConfig `json:"codeSwitchingConfig,omitempty"`
	// Deprecated: use LanguageConfig.Languages (translated with a
	// deprecated warning).
	Language string `json:"language,omitempty"`
	// LanguageConfig is the v2 language configuration (language_config).
	LanguageConfig  *LanguageConfig  `json:"languageConfig,omitempty"`
	Callback        *bool            `json:"callback,omitempty"`
	CallbackConfig  *CallbackConfig  `json:"callbackConfig,omitempty"`
	Subtitles       *bool            `json:"subtitles,omitempty"`
	SubtitlesConfig *SubtitlesConfig `json:"subtitlesConfig,omitempty"`
	Diarization     *bool            `json:"diarization,omitempty"`
	// DiarizationConfig.Enhanced is deprecated (not sent, unsupported
	// warning).
	DiarizationConfig   *DiarizationConfig   `json:"diarizationConfig,omitempty"`
	Translation         *bool                `json:"translation,omitempty"`
	TranslationConfig   *TranslationConfig   `json:"translationConfig,omitempty"`
	Summarization       *bool                `json:"summarization,omitempty"`
	SummarizationConfig *SummarizationConfig `json:"summarizationConfig,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	Moderation             *bool `json:"moderation,omitempty"`
	NamedEntityRecognition *bool `json:"namedEntityRecognition,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	Chapterization *bool `json:"chapterization,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	NameConsistency      *bool                 `json:"nameConsistency,omitempty"`
	CustomSpelling       *bool                 `json:"customSpelling,omitempty"`
	CustomSpellingConfig *CustomSpellingConfig `json:"customSpellingConfig,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	StructuredDataExtraction *bool `json:"structuredDataExtraction,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	StructuredDataExtractionConfig *StructuredDataExtractionConfig `json:"structuredDataExtractionConfig,omitempty"`
	SentimentAnalysis              *bool                           `json:"sentimentAnalysis,omitempty"`
	AudioToLlm                     *bool                           `json:"audioToLlm,omitempty"`
	AudioToLlmConfig               *AudioToLlmConfig               `json:"audioToLlmConfig,omitempty"`
	// PiiRedaction enables PII redaction (pii_redaction).
	PiiRedaction *bool `json:"piiRedaction,omitempty"`
	// PiiRedactionConfig configures PII redaction; it has no effect unless
	// PiiRedaction is true (warned).
	PiiRedactionConfig *PiiRedactionConfig    `json:"piiRedactionConfig,omitempty"`
	CustomMetadata     map[string]interface{} `json:"customMetadata,omitempty"`
	Sentences          *bool                  `json:"sentences,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	DisplayMode         *bool `json:"displayMode,omitempty"`
	PunctuationEnhanced *bool `json:"punctuationEnhanced,omitempty"`

	// PollIntervalMs overrides Config.PollInterval for this call (a positive
	// integer number of milliseconds; not sent to Gladia).
	PollIntervalMs *int64 `json:"pollIntervalMs,omitempty"`
	// PollTimeoutMs overrides Config.PollTimeout for this call (a positive
	// integer number of milliseconds; not sent to Gladia).
	PollTimeoutMs *int64 `json:"pollTimeoutMs,omitempty"`
}

// LanguageConfig is the Gladia v2 language configuration. An empty
// Languages list means auto-detection.
type LanguageConfig struct {
	Languages     []string `json:"languages,omitempty"`
	CodeSwitching *bool    `json:"codeSwitching,omitempty"`
}

// PiiRedactionConfig configures PII redaction.
type PiiRedactionConfig struct {
	EntityTypes []string `json:"entityTypes,omitempty"`
	// ProcessedTextType is "MARKER" or "MASK".
	ProcessedTextType string `json:"processedTextType,omitempty"`
}

// CustomVocabularyConfig configures custom vocabulary.
type CustomVocabularyConfig struct {
	Vocabulary       []interface{} `json:"vocabulary"`
	DefaultIntensity *float64      `json:"defaultIntensity,omitempty"`
}

// CodeSwitchingConfig configures code switching.
type CodeSwitchingConfig struct {
	Languages []string `json:"languages,omitempty"`
}

// CallbackConfig configures the completion callback.
type CallbackConfig struct {
	URL    string `json:"url"`
	Method string `json:"method,omitempty"`
}

// SubtitlesConfig configures subtitles generation.
type SubtitlesConfig struct {
	Formats                 []string `json:"formats,omitempty"`
	MinimumDuration         *float64 `json:"minimumDuration,omitempty"`
	MaximumDuration         *float64 `json:"maximumDuration,omitempty"`
	MaximumCharactersPerRow *float64 `json:"maximumCharactersPerRow,omitempty"`
	MaximumRowsPerCaption   *float64 `json:"maximumRowsPerCaption,omitempty"`
	Style                   string   `json:"style,omitempty"`
}

// DiarizationConfig configures speaker diarization.
type DiarizationConfig struct {
	NumberOfSpeakers *float64 `json:"numberOfSpeakers,omitempty"`
	MinSpeakers      *float64 `json:"minSpeakers,omitempty"`
	MaxSpeakers      *float64 `json:"maxSpeakers,omitempty"`
	// Deprecated: no longer supported by the Gladia v2 pre-recorded API.
	Enhanced *bool `json:"enhanced,omitempty"`
}

// TranslationConfig configures translation.
type TranslationConfig struct {
	TargetLanguages []string `json:"targetLanguages"`
	// Model is "base", "batch" or "enhanced".
	Model                   string  `json:"model,omitempty"`
	MatchOriginalUtterances *bool   `json:"matchOriginalUtterances,omitempty"`
	Lipsync                 *bool   `json:"lipsync,omitempty"`
	ContextAdaptation       *bool   `json:"contextAdaptation,omitempty"`
	Context                 *string `json:"context,omitempty"`
	Informal                *bool   `json:"informal,omitempty"`
}

// SummarizationConfig configures summarization.
type SummarizationConfig struct {
	Type string `json:"type,omitempty"`
}

// CustomSpellingConfig configures custom spelling.
type CustomSpellingConfig struct {
	SpellingDictionary map[string][]string `json:"spellingDictionary"`
}

// StructuredDataExtractionConfig configures structured data extraction.
type StructuredDataExtractionConfig struct {
	Classes []string `json:"classes"`
}

// AudioToLlmConfig configures audio-to-LLM processing.
type AudioToLlmConfig struct {
	Prompts []string `json:"prompts"`
	// Model is the language model to use (audio_to_llm_config.model).
	Model *string `json:"model,omitempty"`
}

// Gladia transcription model IDs (TS GladiaTranscriptionModelId). An empty
// model ID (or "default") lets the Gladia API pick its default model
// (solaria-1).
const (
	// ModelSolaria1 is the generalist model with the broadest language
	// coverage; it supports code switching.
	ModelSolaria1 = "solaria-1"
	// ModelSolaria3 is tuned for European audio; pre-recorded only, a single
	// language (exactly one in LanguageConfig.Languages) and no code
	// switching.
	ModelSolaria3 = "solaria-3"
)
