package assemblyai

// TranscriptionModelOptions contains AssemblyAI-specific transcription
// options, mirroring the TypeScript SDK's assemblyaiTranscriptionModelOptionsSchema.
// See https://www.assemblyai.com/docs/api-reference/transcripts/submit
type TranscriptionModelOptions struct {
	// AudioEndAt is the end time of the audio in milliseconds.
	AudioEndAt *int `json:"audioEndAt,omitempty"`

	// AudioStartFrom is the start time of the audio in milliseconds.
	AudioStartFrom *int `json:"audioStartFrom,omitempty"`

	// AutoChapters enables automatic chapter generation.
	AutoChapters *bool `json:"autoChapters,omitempty"`

	// AutoHighlights enables automatic key-phrase generation.
	AutoHighlights *bool `json:"autoHighlights,omitempty"`

	// BoostParam is the boost level for wordBoost. Deprecated.
	BoostParam string `json:"boostParam,omitempty"`

	// ContentSafety enables content safety filtering.
	ContentSafety *bool `json:"contentSafety,omitempty"`

	// ContentSafetyConfidence is the confidence threshold (25-100).
	ContentSafetyConfidence *int `json:"contentSafetyConfidence,omitempty"`

	// CustomSpelling customizes how words are spelled and formatted.
	CustomSpelling []CustomSpelling `json:"customSpelling,omitempty"`

	// Disfluencies transcribes filler words like "umm".
	Disfluencies *bool `json:"disfluencies,omitempty"`

	// Domain enables a domain-specific model, e.g. "medical-v1".
	Domain string `json:"domain,omitempty"`

	// EntityDetection enables entity detection.
	EntityDetection *bool `json:"entityDetection,omitempty"`

	// FilterProfanity filters profanity from the transcribed text.
	FilterProfanity *bool `json:"filterProfanity,omitempty"`

	// FormatText enables text formatting.
	FormatText *bool `json:"formatText,omitempty"`

	// IabCategories enables topic detection.
	IabCategories *bool `json:"iabCategories,omitempty"`

	// KeytermsPrompt boosts recognition for domain-specific keyterms.
	KeytermsPrompt []string `json:"keytermsPrompt,omitempty"`

	// LanguageCode is the language of the audio file.
	LanguageCode string `json:"languageCode,omitempty"`

	// LanguageConfidenceThreshold is the confidence threshold for detected language.
	LanguageConfidenceThreshold *float64 `json:"languageConfidenceThreshold,omitempty"`

	// LanguageDetection enables automatic language detection.
	LanguageDetection *bool `json:"languageDetection,omitempty"`

	// LanguageDetectionOptions configures automatic language detection.
	LanguageDetectionOptions *LanguageDetectionOptions `json:"languageDetectionOptions,omitempty"`

	// Multichannel processes audio as multichannel.
	Multichannel *bool `json:"multichannel,omitempty"`

	// Prompt provides natural-language context to steer the model.
	Prompt string `json:"prompt,omitempty"`

	// Punctuate adds punctuation to the transcription.
	Punctuate *bool `json:"punctuate,omitempty"`

	// RedactPii redacts personally identifiable information.
	RedactPii *bool `json:"redactPii,omitempty"`

	// RedactPiiAudio generates a PII-redacted copy of the audio.
	RedactPiiAudio *bool `json:"redactPiiAudio,omitempty"`

	// RedactPiiAudioOptions configures PII-redacted audio files.
	RedactPiiAudioOptions *RedactPiiAudioOptions `json:"redactPiiAudioOptions,omitempty"`

	// RedactPiiAudioQuality is the audio format for PII-redacted audio.
	RedactPiiAudioQuality string `json:"redactPiiAudioQuality,omitempty"`

	// RedactPiiPolicies lists PII types to redact.
	RedactPiiPolicies []string `json:"redactPiiPolicies,omitempty"`

	// RedactPiiReturnUnredacted returns the original transcript alongside the redacted one.
	RedactPiiReturnUnredacted *bool `json:"redactPiiReturnUnredacted,omitempty"`

	// RedactPiiSub is the substitution method for redacted PII.
	RedactPiiSub string `json:"redactPiiSub,omitempty"`

	// RedactStaticEntities maps user-defined labels to exact terms to redact.
	RedactStaticEntities map[string][]string `json:"redactStaticEntities,omitempty"`

	// RemoveAudioTags removes inline annotations from rich transcripts.
	RemoveAudioTags string `json:"removeAudioTags,omitempty"`

	// SentimentAnalysis enables sentiment analysis.
	SentimentAnalysis *bool `json:"sentimentAnalysis,omitempty"`

	// SpeakerLabels enables speaker diarization.
	SpeakerLabels *bool `json:"speakerLabels,omitempty"`

	// SpeakerOptions configures speaker diarization.
	SpeakerOptions *SpeakerOptions `json:"speakerOptions,omitempty"`

	// SpeakersExpected is the expected number of speakers.
	SpeakersExpected *int `json:"speakersExpected,omitempty"`

	// SpeechThreshold rejects audio with less than this fraction of speech.
	SpeechThreshold *float64 `json:"speechThreshold,omitempty"`

	// Summarization enables transcript summarization.
	Summarization *bool `json:"summarization,omitempty"`

	// SummaryModel is the model used for summarization.
	SummaryModel string `json:"summaryModel,omitempty"`

	// SummaryType is the type of summary to generate.
	SummaryType string `json:"summaryType,omitempty"`

	// Temperature controls sampling randomness (0-1). Universal-3 Pro models.
	Temperature *float64 `json:"temperature,omitempty"`

	// WebhookAuthHeaderName is the header name for webhook auth.
	WebhookAuthHeaderName string `json:"webhookAuthHeaderName,omitempty"`

	// WebhookAuthHeaderValue is the header value for webhook auth.
	WebhookAuthHeaderValue string `json:"webhookAuthHeaderValue,omitempty"`

	// WebhookUrl is the URL to send webhook notifications to.
	WebhookUrl string `json:"webhookUrl,omitempty"` //nolint:revive,stylecheck // matches TS field naming

	// WordBoost lists words to boost recognition for. Deprecated.
	WordBoost []string `json:"wordBoost,omitempty"`
}

// CustomSpelling customizes how a word or phrase is spelled/formatted.
type CustomSpelling struct {
	From []string `json:"from"`
	To   string   `json:"to"`
}

// LanguageDetectionOptions configures automatic language detection.
type LanguageDetectionOptions struct {
	ExpectedLanguages                []string `json:"expectedLanguages,omitempty"`
	FallbackLanguage                 string   `json:"fallbackLanguage,omitempty"`
	CodeSwitching                    *bool    `json:"codeSwitching,omitempty"`
	CodeSwitchingConfidenceThreshold *float64 `json:"codeSwitchingConfidenceThreshold,omitempty"`
}

// RedactPiiAudioOptions configures PII-redacted audio output.
type RedactPiiAudioOptions struct {
	ReturnRedactedNoSpeechAudio  *bool  `json:"returnRedactedNoSpeechAudio,omitempty"`
	OverrideAudioRedactionMethod string `json:"overrideAudioRedactionMethod,omitempty"`
}

// SpeakerOptions configures speaker diarization.
type SpeakerOptions struct {
	MinSpeakersExpected *int `json:"minSpeakersExpected,omitempty"`
	MaxSpeakersExpected *int `json:"maxSpeakersExpected,omitempty"`
}
