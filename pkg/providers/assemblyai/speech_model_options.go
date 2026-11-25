package assemblyai

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModelOptions contains AssemblyAI text-to-speech options
// (providerOptions["assemblyai"]), mirroring TS
// assemblyaiSpeechModelOptionsSchema.
// See https://www.assemblyai.com/docs/tts/api-reference/http-api/create-speech
type SpeechModelOptions struct {
	// Encoding of the output audio ("pcm_s16le", "pcm_mulaw", "pcm_alaw" or
	// "mp3"). It overrides the encoding derived from the output format.
	Encoding string `json:"encoding,omitempty"`

	// Container is "raw" (the encoded samples alone) or "wav" (a WAV file).
	// It requires a pcm_* encoding.
	Container string `json:"container,omitempty"`

	// SampleRate of the output audio in Hz (8000, 16000, 22050, 24000, 44100
	// or 48000).
	SampleRate *int `json:"sampleRate,omitempty"`

	// BitRate is the MP3 bit rate in bits per second (32000, 48000, 64000,
	// 96000 or 128000). MP3 output only.
	BitRate *int `json:"bitRate,omitempty"`
}

// assemblyaiSpeechOptionsSchema is assemblyaiSpeechModelOptionsSchema: every
// key is optional but not nullable (z.enum / z.union of literals .optional()).
var assemblyaiSpeechOptionsSchema = providerutils.OptionSchema{
	"encoding":   providerutils.OptEnum("pcm_s16le", "pcm_mulaw", "pcm_alaw", "mp3"),
	"container":  providerutils.OptEnum("raw", "wav"),
	"sampleRate": providerutils.OptNumberLiterals(8000, 16000, 22050, 24000, 44100, 48000),
	"bitRate":    providerutils.OptNumberLiterals(32000, 48000, 64000, 96000, 128000),
}

// parseSpeechOptions is parseProviderOptions with the speech schema: a
// mistyped option is an InvalidArgumentError. It returns the decoded options
// (nil when none were given); a typed SpeechModelOptions value is valid by
// construction.
func parseSpeechOptions(providerOptions map[string]interface{}) (*SpeechModelOptions, error) {
	raw := providerOptions["assemblyai"]
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case SpeechModelOptions:
		return &v, nil
	case *SpeechModelOptions:
		return v, nil
	}
	if err := providerutils.ValidateMapProviderOptions("assemblyai", providerOptions, assemblyaiSpeechOptionsSchema); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, nil
	}
	var opts SpeechModelOptions
	if json.Unmarshal(encoded, &opts) != nil {
		return nil, nil
	}
	return &opts, nil
}
