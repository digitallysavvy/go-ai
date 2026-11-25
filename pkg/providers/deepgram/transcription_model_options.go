package deepgram

// TranscriptionModelOptions contains Deepgram-specific transcription options,
// mirroring the TypeScript SDK's deepgramTranscriptionModelOptionsSchema.
// See https://developers.deepgram.com/docs/pre-recorded-audio#results
type TranscriptionModelOptions struct {
	// Language to use for transcription. Deepgram defaults to English.
	Language string `json:"language,omitempty"`

	// DetectLanguage enables automatic language detection.
	DetectLanguage *bool `json:"detectLanguage,omitempty"`

	// SmartFormat formats written-out numbers, dates, times, etc.
	SmartFormat *bool `json:"smartFormat,omitempty"`

	// Punctuate adds punctuation to the transcript.
	Punctuate *bool `json:"punctuate,omitempty"`

	// Paragraphs formats the transcript into paragraphs.
	Paragraphs *bool `json:"paragraphs,omitempty"`

	// Summarize generates a summary of the transcript. "v2" or false.
	Summarize interface{} `json:"summarize,omitempty"`

	// Topics identifies topics in the transcript.
	Topics *bool `json:"topics,omitempty"`

	// Intents identifies intents in the transcript.
	Intents *bool `json:"intents,omitempty"`

	// Sentiment analyzes sentiment in the transcript.
	Sentiment *bool `json:"sentiment,omitempty"`

	// DetectEntities detects and tags named entities.
	DetectEntities *bool `json:"detectEntities,omitempty"`

	// Redact specifies terms or patterns to redact.
	Redact interface{} `json:"redact,omitempty"`

	// Replace is the string to replace redacted content with.
	Replace string `json:"replace,omitempty"`

	// Search is a term or phrase to search for in the transcript.
	Search string `json:"search,omitempty"`

	// Keyterm identifies a key term in the transcript.
	Keyterm string `json:"keyterm,omitempty"`

	// Diarize identifies different speakers in the audio. NOT defaulted to true.
	Diarize *bool `json:"diarize,omitempty"`

	// Utterances segments the transcript into utterances.
	Utterances *bool `json:"utterances,omitempty"`

	// UttSplit is the minimum silence duration (seconds) to trigger a new utterance.
	UttSplit *float64 `json:"uttSplit,omitempty"`

	// FillerWords includes filler words (um, uh, etc.) in the transcript.
	FillerWords *bool `json:"fillerWords,omitempty"`

	// Streaming holds options for live transcription over the /v1/listen
	// WebSocket (TranscriptionModel.DoStream). DoTranscribe ignores it with
	// an unsupported warning.
	Streaming *StreamingOptions `json:"streaming,omitempty"`
}

// StreamingOptions are Deepgram live transcription options (TS
// deepgramTranscriptionModelOptionsSchema.streaming).
type StreamingOptions struct {
	// InterimResults returns revisable transcripts before a segment is
	// finalized.
	InterimResults *bool `json:"interimResults,omitempty"`

	// Endpointing is the silence duration in milliseconds (a non-negative
	// integer), or false to disable endpointing.
	Endpointing interface{} `json:"endpointing,omitempty"`

	// Channels is the number of interleaved input audio channels (default
	// one).
	Channels *int `json:"channels,omitempty"`

	// Multichannel transcribes each input channel independently.
	Multichannel *bool `json:"multichannel,omitempty"`

	// VADEvents emits speech-start events in raw chunks.
	VADEvents *bool `json:"vadEvents,omitempty"`

	// UtteranceEndMs is the silence duration in milliseconds (at least
	// 1000) before an utterance-end event.
	UtteranceEndMs *int `json:"utteranceEndMs,omitempty"`
}
