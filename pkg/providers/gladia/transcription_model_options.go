package gladia

// TranscriptionModelOptions contains Gladia-specific transcription options,
// mirroring the TypeScript SDK's gladiaTranscriptionModelOptionsSchema.
// See https://docs.gladia.io/api-reference/v2/pre-recorded/init
type TranscriptionModelOptions struct {
	ContextPrompt                  string                          `json:"contextPrompt,omitempty"`
	CustomVocabulary               interface{}                     `json:"customVocabulary,omitempty"`
	CustomVocabularyConfig         *CustomVocabularyConfig         `json:"customVocabularyConfig,omitempty"`
	DetectLanguage                 *bool                           `json:"detectLanguage,omitempty"`
	EnableCodeSwitching            *bool                           `json:"enableCodeSwitching,omitempty"`
	CodeSwitchingConfig            *CodeSwitchingConfig            `json:"codeSwitchingConfig,omitempty"`
	Language                       string                          `json:"language,omitempty"`
	Callback                       *bool                           `json:"callback,omitempty"`
	CallbackConfig                 *CallbackConfig                 `json:"callbackConfig,omitempty"`
	Subtitles                      *bool                           `json:"subtitles,omitempty"`
	SubtitlesConfig                *SubtitlesConfig                `json:"subtitlesConfig,omitempty"`
	Diarization                    *bool                           `json:"diarization,omitempty"`
	DiarizationConfig              *DiarizationConfig              `json:"diarizationConfig,omitempty"`
	Translation                    *bool                           `json:"translation,omitempty"`
	TranslationConfig              *TranslationConfig              `json:"translationConfig,omitempty"`
	Summarization                  *bool                           `json:"summarization,omitempty"`
	SummarizationConfig            *SummarizationConfig            `json:"summarizationConfig,omitempty"`
	Moderation                     *bool                           `json:"moderation,omitempty"`
	NamedEntityRecognition         *bool                           `json:"namedEntityRecognition,omitempty"`
	Chapterization                 *bool                           `json:"chapterization,omitempty"`
	NameConsistency                *bool                           `json:"nameConsistency,omitempty"`
	CustomSpelling                 *bool                           `json:"customSpelling,omitempty"`
	CustomSpellingConfig           *CustomSpellingConfig           `json:"customSpellingConfig,omitempty"`
	StructuredDataExtraction       *bool                           `json:"structuredDataExtraction,omitempty"`
	StructuredDataExtractionConfig *StructuredDataExtractionConfig `json:"structuredDataExtractionConfig,omitempty"`
	SentimentAnalysis              *bool                           `json:"sentimentAnalysis,omitempty"`
	AudioToLlm                     *bool                           `json:"audioToLlm,omitempty"`
	AudioToLlmConfig               *AudioToLlmConfig               `json:"audioToLlmConfig,omitempty"`
	CustomMetadata                 map[string]interface{}          `json:"customMetadata,omitempty"`
	Sentences                      *bool                           `json:"sentences,omitempty"`
	DisplayMode                    *bool                           `json:"displayMode,omitempty"`
	PunctuationEnhanced            *bool                           `json:"punctuationEnhanced,omitempty"`
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
	Enhanced         *bool    `json:"enhanced,omitempty"`
}

// TranslationConfig configures translation.
type TranslationConfig struct {
	TargetLanguages         []string `json:"targetLanguages"`
	Model                   string   `json:"model,omitempty"`
	MatchOriginalUtterances *bool    `json:"matchOriginalUtterances,omitempty"`
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
}
