package deepgram

// SpeechModelOptions contains Deepgram-specific speech synthesis options,
// mirroring the TypeScript SDK's deepgramSpeechModelOptionsSchema.
// See https://developers.deepgram.com/reference/text-to-speech/speak-request
type SpeechModelOptions struct {
	// BitRate is the bitrate of the audio in bits per second.
	BitRate interface{} `json:"bitRate,omitempty"`

	// Container is the container format for the output audio (mp3, wav, etc.).
	Container string `json:"container,omitempty"`

	// Encoding is the encoding type for the audio output (linear16, mulaw, alaw, etc.).
	Encoding string `json:"encoding,omitempty"`

	// SampleRate is the sample rate for the output audio in Hz.
	SampleRate *int `json:"sampleRate,omitempty"`

	// Callback is the URL to which we'll make the callback request.
	Callback string `json:"callback,omitempty"`

	// CallbackMethod is the HTTP method by which the callback request is made.
	CallbackMethod string `json:"callbackMethod,omitempty"`

	// MipOptOut opts out requests from the Deepgram Model Improvement Program.
	MipOptOut *bool `json:"mipOptOut,omitempty"`

	// Tag labels requests for identification during usage reporting.
	Tag interface{} `json:"tag,omitempty"`
}
