package hume

import "encoding/json"

// SpeechModelOptions contains Hume-specific speech provider options,
// mirroring the TypeScript SDK's humeSpeechModelOptionsSchema.
// See https://dev.hume.ai/reference/text-to-speech-tts/synthesize-file
type SpeechModelOptions struct {
	// Context is either a reference to a previous generation (GenerationID
	// set) or a list of utterances to synthesize (Utterances set). When both
	// are empty, Context is treated as absent.
	Context *Context `json:"context,omitempty"`
}

// Context selects the speech synthesis context: a previous generation to
// continue from, or an explicit list of utterances.
type Context struct {
	// GenerationID retrieves a previously generated speech synthesis.
	GenerationID string `json:"generationId,omitempty"`

	// Utterances lists utterances to synthesize into speech. Used only when
	// GenerationID is empty.
	Utterances []Utterance `json:"utterances,omitempty"`
}

// HasGenerationID reports whether this Context references a previous
// generation, mirroring the TypeScript SDK's `'generationId' in context`
// discriminant.
func (c *Context) HasGenerationID() bool {
	return c != nil && c.GenerationID != ""
}

// Utterance is one utterance to synthesize into speech.
type Utterance struct {
	// Text is the content to convert to speech.
	Text string `json:"text"`

	// Description gives optional instructions for how the text should be spoken.
	Description string `json:"description,omitempty"`

	// Speed is an optional speech rate multiplier.
	Speed *float64 `json:"speed,omitempty"`

	// TrailingSilence adds an optional duration of silence after the
	// utterance, in seconds.
	TrailingSilence *float64 `json:"trailingSilence,omitempty"`

	// Voice selects the voice for this utterance, by ID or by name.
	Voice *UtteranceVoice `json:"voice,omitempty"`
}

// UtteranceVoice identifies a voice by ID or by name.
type UtteranceVoice struct {
	// ID of the voice to use.
	ID string `json:"id,omitempty"`

	// Name of the voice to use (used when ID is empty).
	Name string `json:"name,omitempty"`

	// Provider of the voice: "HUME_AI" or "CUSTOM_VOICE".
	Provider string `json:"provider,omitempty"`
}

// extractSpeechModelOptions decodes providerOptions["hume"] into
// SpeechModelOptions, accepting either a native struct/pointer value or a
// generic map[string]interface{} by round-tripping through JSON.
func extractSpeechModelOptions(providerOptions map[string]interface{}) *SpeechModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["hume"]
	if !ok || raw == nil {
		return nil
	}
	if opts, ok := raw.(SpeechModelOptions); ok {
		return &opts
	}
	if opts, ok := raw.(*SpeechModelOptions); ok {
		return opts
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts SpeechModelOptions
	if err := json.Unmarshal(b, &opts); err != nil {
		return nil
	}
	return &opts
}
