package elevenlabs

import (
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// StreamingSpeechOptions are the streaming-only speech options
// (providerOptions.elevenlabs.streaming). They apply to
// ai.ExperimentalStreamSpeech and are ignored with a warning by
// ai.GenerateSpeech.
type StreamingSpeechOptions struct {
	// AutoMode lets the server choose when to generate (text-to-speech only).
	AutoMode *bool
	// ChunkLengthSchedule is the generation chunk length schedule, each entry
	// 50 to 500 (text-to-speech only).
	ChunkLengthSchedule []float64
	// EnableSSMLParsing enables SSML parsing (text-to-speech only).
	EnableSSMLParsing *bool
	// InactivityTimeout is the server inactivity timeout in seconds, 1 to 180
	// (text-to-speech only). Setting it turns off the SDK's keepalives.
	InactivityTimeout *int
	// OutputFormat is a native ElevenLabs output_format (for example
	// mp3_44100_192), overriding the call's output format.
	OutputFormat string
	// SyncAlignment requests character alignment with the audio.
	SyncAlignment *bool
}

// DialogueVoiceSettings are the voice settings Text-to-Dialogue streaming
// accepts.
type DialogueVoiceSettings struct {
	Stability *float64
}

// DialogueOptions turn streaming speech into Text-to-Dialogue
// (providerOptions.elevenlabs.dialogue). They require an eleven_v3 or
// eleven_v4 model. A streamed text part's Voice selects its speaker and must
// be one of Voices.
type DialogueOptions struct {
	// Voices lists the voice IDs of the dialogue (1 to 10).
	Voices        []string
	VoiceSettings *DialogueVoiceSettings
}

func dialogueOptionsCheck() providerutils.OptionCheck {
	return providerutils.OptObjectWith(providerutils.OptionSchema{
		"voices": providerutils.OptArray(providerutils.OptStringLen(1, -1), 1, 10),
		"voiceSettings": providerutils.OptObject(providerutils.OptionSchema{
			"stability": providerutils.OptRange(0, 1),
		}),
	}, "voices")
}

func parseDialogueOptions(raw map[string]interface{}) *DialogueOptions {
	out := &DialogueOptions{}
	if v, ok := stringSlice(raw["voices"]); ok {
		out.Voices = v
	}
	if vs, ok := raw["voiceSettings"].(map[string]interface{}); ok {
		settings := &DialogueVoiceSettings{}
		if n, ok := numberAsFloat(vs["stability"]); ok {
			settings.Stability = &n
		}
		out.VoiceSettings = settings
	}
	return out
}

func streamingSpeechOptionsCheck() providerutils.OptionCheck {
	one, max180 := 1, 180
	return providerutils.OptObject(providerutils.OptionSchema{
		"autoMode":            providerutils.OptBool,
		"chunkLengthSchedule": providerutils.OptArray(providerutils.OptRange(50, 500), 1, -1),
		"enableSsmlParsing":   providerutils.OptBool,
		"inactivityTimeout":   providerutils.OptInt(&one, &max180),
		"outputFormat":        providerutils.OptStringLen(1, -1),
		"syncAlignment":       providerutils.OptBool,
	})
}

func parseStreamingSpeechOptions(raw map[string]interface{}) *StreamingSpeechOptions {
	out := &StreamingSpeechOptions{}
	if v, ok := raw["autoMode"].(bool); ok {
		out.AutoMode = &v
	}
	if v, ok := floatSlice(raw["chunkLengthSchedule"]); ok {
		out.ChunkLengthSchedule = v
	}
	if v, ok := raw["enableSsmlParsing"].(bool); ok {
		out.EnableSSMLParsing = &v
	}
	if n, ok := numberAsFloat(raw["inactivityTimeout"]); ok {
		i := int(n)
		out.InactivityTimeout = &i
	}
	if v, ok := raw["outputFormat"].(string); ok {
		out.OutputFormat = v
	}
	if v, ok := raw["syncAlignment"].(bool); ok {
		out.SyncAlignment = &v
	}
	return out
}

func floatSlice(value interface{}) ([]float64, bool) {
	switch v := value.(type) {
	case []float64:
		return v, true
	case []int:
		out := make([]float64, len(v))
		for i, n := range v {
			out[i] = float64(n)
		}
		return out, true
	case []interface{}:
		out := make([]float64, 0, len(v))
		for _, item := range v {
			n, ok := numberAsFloat(item)
			if !ok {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	default:
		return nil, false
	}
}
