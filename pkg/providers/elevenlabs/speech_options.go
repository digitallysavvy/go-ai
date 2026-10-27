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

// DialogueInput is one line of a multi-voice dialogue for ai.GenerateSpeech
// (providerOptions.elevenlabs.inputs).
type DialogueInput struct {
	Text    string
	VoiceID string
}

func dialogueInputsCheck() providerutils.OptionCheck {
	return providerutils.OptArray(providerutils.OptObjectWith(providerutils.OptionSchema{
		"text":    providerutils.OptStringLen(1, -1),
		"voiceId": providerutils.OptStringLen(1, -1),
	}, "text", "voiceId"), 1, -1)
}

func parseDialogueInputs(value interface{}) []DialogueInput {
	switch v := value.(type) {
	case []DialogueInput:
		return v
	case []interface{}:
		out := make([]DialogueInput, 0, len(v))
		for _, item := range v {
			if im, ok := item.(map[string]interface{}); ok {
				text, _ := im["text"].(string)
				voiceID, _ := im["voiceId"].(string)
				out = append(out, DialogueInput{Text: text, VoiceID: voiceID})
			}
		}
		return out
	case []map[string]interface{}:
		out := make([]DialogueInput, 0, len(v))
		for _, im := range v {
			text, _ := im["text"].(string)
			voiceID, _ := im["voiceId"].(string)
			out = append(out, DialogueInput{Text: text, VoiceID: voiceID})
		}
		return out
	}
	return nil
}

// optUnion is a zod union: the value passes when any member passes, else the
// first member's failure is reported.
func optUnion(checks ...providerutils.OptionCheck) providerutils.OptionCheck {
	return func(path string, v interface{}) error {
		var first error
		for _, check := range checks {
			err := check(path, v)
			if err == nil {
				return nil
			}
			if first == nil {
				first = err
			}
		}
		return first
	}
}

// compositionPlanCheck validates the native (snake_case) composition plan
// sent as-is in place of the prompt: music_v1 sections or music_v2+ chunks.
// https://elevenlabs.io/docs/api-reference/music/compose
func compositionPlanCheck() providerutils.OptionCheck {
	zero, three, maxDuration := 0, 3000, 120000
	nonNegative := providerutils.OptInt(&zero, nil)
	timeRange := providerutils.OptObjectWith(providerutils.OptionSchema{
		"start_ms": nonNegative, "end_ms": nonNegative,
	}, "start_ms", "end_ms")
	audioRefChunk := providerutils.OptObjectWith(providerutils.OptionSchema{
		"song_id": providerutils.OptString, "range": timeRange,
	}, "song_id", "range")
	durationMs := providerutils.OptInt(&three, &maxDuration)
	styles := providerutils.OptArray(providerutils.OptString, 0, -1)

	// music_v1
	section := providerutils.OptObjectWith(providerutils.OptionSchema{
		"section_name":          providerutils.OptStringLen(1, 100),
		"positive_local_styles": styles,
		"negative_local_styles": styles,
		"duration_ms":           durationMs,
		"lines":                 providerutils.OptArray(providerutils.OptStringMax(200), 0, 30),
		"source_from": providerutils.OptNullable(providerutils.OptObjectWith(providerutils.OptionSchema{
			"song_id": providerutils.OptString, "range": timeRange,
			"negative_ranges": providerutils.OptArray(timeRange, 0, -1),
		}, "song_id", "range")),
	}, "section_name", "positive_local_styles", "negative_local_styles", "duration_ms", "lines")
	musicPrompt := providerutils.OptObjectWith(providerutils.OptionSchema{
		"positive_global_styles": styles,
		"negative_global_styles": styles,
		"sections":               providerutils.OptArray(section, 0, -1),
	}, "positive_global_styles", "negative_global_styles", "sections")

	// music_v2 and later
	generationChunk := providerutils.OptObjectWith(providerutils.OptionSchema{
		"text":               providerutils.OptString,
		"duration_ms":        durationMs,
		"positive_styles":    styles,
		"negative_styles":    styles,
		"context_adherence":  providerutils.OptEnum("low", "medium", "high"),
		"conditioning_ref":   providerutils.OptNullable(audioRefChunk),
		"condition_strength": providerutils.OptNullable(providerutils.OptEnum("low", "medium", "high", "xhigh")),
	}, "text", "duration_ms", "positive_styles")
	chunkedPlan := providerutils.OptObjectWith(providerutils.OptionSchema{
		"chunks": providerutils.OptArray(optUnion(generationChunk, audioRefChunk), 0, -1),
	}, "chunks")

	return optUnion(musicPrompt, chunkedPlan)
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

// compositionPlanDurationMs is the total length of a music composition plan:
// sections (music_v1) or chunks (music_v2 and later). It reports false when
// the plan has no parts or a part without a positive length.
func compositionPlanDurationMs(plan map[string]interface{}) (float64, bool) {
	var parts []float64
	if sections, ok := plan["sections"]; ok {
		for _, section := range mapList(sections) {
			d, _ := numberAsFloat(section["duration_ms"])
			parts = append(parts, d)
		}
	} else {
		for _, chunk := range mapList(plan["chunks"]) {
			// Audio reference chunks reuse a time range of an earlier song.
			if d, ok := numberAsFloat(chunk["duration_ms"]); ok {
				parts = append(parts, d)
				continue
			}
			rng, _ := chunk["range"].(map[string]interface{})
			end, _ := numberAsFloat(rng["end_ms"])
			start, _ := numberAsFloat(rng["start_ms"])
			parts = append(parts, end-start)
		}
	}
	if len(parts) == 0 {
		return 0, false
	}
	total := 0.0
	for _, d := range parts {
		if d <= 0 {
			return 0, false
		}
		total += d
	}
	return total, true
}

func mapList(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if im, ok := item.(map[string]interface{}); ok {
				out = append(out, im)
			}
		}
		return out
	}
	return nil
}
