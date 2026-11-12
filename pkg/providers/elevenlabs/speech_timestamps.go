package elevenlabs

import (
	"encoding/json"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechAlignment is a character alignment of speech audio
// (providerMetadata.elevenlabs.alignment / normalizedAlignment, TS
// 48ab95d62a).
type SpeechAlignment struct {
	Characters                 []string  `json:"characters"`
	CharacterStartTimesSeconds []float64 `json:"characterStartTimesSeconds"`
	CharacterEndTimesSeconds   []float64 `json:"characterEndTimesSeconds"`
}

// SpeechVoiceSegment is one voice segment of a dialogue with timestamps
// (providerMetadata.elevenlabs.voiceSegments, TS 48ab95d62a).
type SpeechVoiceSegment struct {
	VoiceID             string  `json:"voiceId"`
	StartTimeSeconds    float64 `json:"startTimeSeconds"`
	EndTimeSeconds      float64 `json:"endTimeSeconds"`
	CharacterStartIndex float64 `json:"characterStartIndex"`
	CharacterEndIndex   float64 `json:"characterEndIndex"`
	DialogueInputIndex  float64 `json:"dialogueInputIndex"`
}

func alignmentSchema() zodlite.Node {
	return zodlite.Object(
		zodlite.F("characters", zodlite.Array(zodlite.String)),
		zodlite.F("character_start_times_seconds", zodlite.Array(zodlite.Number)),
		zodlite.F("character_end_times_seconds", zodlite.Array(zodlite.Number)),
	)
}

// speechTimestampsResponseSchema mirrors TS
// elevenLabsSpeechTimestampsResponseSchema.
var speechTimestampsResponseSchema = zodlite.Object(
	zodlite.F("audio_base64", zodlite.String),
	zodlite.F("alignment", zodlite.Nullish(alignmentSchema())),
	zodlite.F("normalized_alignment", zodlite.Nullish(alignmentSchema())),
	zodlite.F("voice_segments", zodlite.Nullish(zodlite.Array(zodlite.Object(
		zodlite.F("voice_id", zodlite.String),
		zodlite.F("start_time_seconds", zodlite.Number),
		zodlite.F("end_time_seconds", zodlite.Number),
		zodlite.F("character_start_index", zodlite.Number),
		zodlite.F("character_end_index", zodlite.Number),
		zodlite.F("dialogue_input_index", zodlite.Number),
	)))),
)

type speechTimestampsEnvelope struct {
	audio    []byte
	metadata map[string]interface{}
	// raw is the parsed JSON body (TS rawValue, reported as response.body).
	raw interface{}
}

type wireAlignment struct {
	Characters []string  `json:"characters"`
	Start      []float64 `json:"character_start_times_seconds"`
	End        []float64 `json:"character_end_times_seconds"`
}

func (a *wireAlignment) toMetadata() SpeechAlignment {
	return SpeechAlignment{Characters: a.Characters, CharacterStartTimesSeconds: a.Start, CharacterEndTimesSeconds: a.End}
}

// parseSpeechTimestampsResponse is the /with-timestamps JSON response (TS
// createJsonResponseHandler(elevenLabsSpeechTimestampsResponseSchema)): the
// base64 audio, and the alignments and voice segments as provider metadata.
//
// A body that is not JSON or fails the schema is the handler's APICallError
// "Invalid JSON response" (status, headers and body of the response) whose
// cause is the JSONParseError or the TypeValidationError.
func parseSpeechTimestampsResponse(statusCode int, headers http.Header, body []byte) (*speechTimestampsEnvelope, error) {
	text := textdecode.UTF8(body)
	var raw interface{}
	invalid := func(cause error) error {
		return &providererrors.ProviderError{
			Provider:        "elevenlabs.speech",
			StatusCode:      statusCode,
			Message:         providererrors.InvalidJSONResponseMessage,
			Cause:           cause,
			ResponseHeaders: providerutils.ExtractHeaders(headers),
			ResponseBody:    text,
		}
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, invalid(&providererrors.JSONParseError{Text: text, Cause: err})
	}
	if message := zodlite.ValidationMessage(speechTimestampsResponseSchema, text); message != "" {
		return nil, invalid(providererrors.NewValidationErrorWithContext(zodlite.CompactJSON(text), zodlite.IssueText(message), nil, &providererrors.ValidationContext{}))
	}
	var wire struct {
		AudioBase64         string         `json:"audio_base64"`
		Alignment           *wireAlignment `json:"alignment"`
		NormalizedAlignment *wireAlignment `json:"normalized_alignment"`
		VoiceSegments       *[]struct {
			VoiceID             string  `json:"voice_id"`
			StartTimeSeconds    float64 `json:"start_time_seconds"`
			EndTimeSeconds      float64 `json:"end_time_seconds"`
			CharacterStartIndex float64 `json:"character_start_index"`
			CharacterEndIndex   float64 `json:"character_end_index"`
			DialogueInputIndex  float64 `json:"dialogue_input_index"`
		} `json:"voice_segments"`
	}
	if err := jsonexact.Unmarshal([]byte(text), &wire); err != nil {
		return nil, invalid(&providererrors.JSONParseError{Text: text, Cause: err})
	}
	// TS convertBase64ToUint8Array (atob throws on invalid input).
	audio, err := fileutil.ConvertBase64ToBytes(wire.AudioBase64)
	if err != nil {
		return nil, err
	}
	metadata := map[string]interface{}{}
	if wire.Alignment != nil {
		metadata["alignment"] = wire.Alignment.toMetadata()
	}
	if wire.NormalizedAlignment != nil {
		metadata["normalizedAlignment"] = wire.NormalizedAlignment.toMetadata()
	}
	if wire.VoiceSegments != nil {
		segments := make([]SpeechVoiceSegment, 0, len(*wire.VoiceSegments))
		for _, s := range *wire.VoiceSegments {
			segments = append(segments, SpeechVoiceSegment(s))
		}
		metadata["voiceSegments"] = segments
	}
	return &speechTimestampsEnvelope{audio: audio, metadata: metadata, raw: raw}, nil
}
