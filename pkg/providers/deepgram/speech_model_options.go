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

	// sampleRate carries the exact TS z.number() value, which may be
	// fractional (SampleRate cannot hold it). It is read from providerOptions
	// by extractSpeechModelOptions and takes precedence over SampleRate.
	sampleRate *float64

	// Callback is the URL to which Deepgram would make the callback request.
	//
	// Deprecated: not supported. Deepgram answers a callback request with a
	// JSON acknowledgement instead of audio, so a call that sets it fails
	// with an InvalidArgumentError before any request (TS 5cc6dcaa65).
	Callback string `json:"callback,omitempty"`

	// CallbackMethod is the HTTP method by which the callback request is made.
	//
	// Deprecated: only used with Callback, which is not supported; it is
	// ignored with an unsupported warning.
	CallbackMethod string `json:"callbackMethod,omitempty"`

	// MipOptOut opts out requests from the Deepgram Model Improvement Program.
	MipOptOut *bool `json:"mipOptOut,omitempty"`

	// Tag labels requests for identification during usage reporting.
	Tag interface{} `json:"tag,omitempty"`
}
