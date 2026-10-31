package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for
// ElevenLabs transcription: batch (non-streaming) transcription via
// DoTranscribe (POST /v1/speech-to-text), and scribe_v2_realtime's
// WebSocket-based streaming transcription via DoStream (see
// transcription_stream.go), mirroring TS ElevenLabsTranscriptionModel.
// DoTranscribe rejects scribe_v2_realtime and DoStream rejects every other
// model ID, matching TS's cross-rejection (UnsupportedFunctionalityError).
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new ElevenLabs batch transcription model.
func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "elevenlabs.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// DoTranscribe performs batch speech-to-text transcription.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if m.modelID == ModelScribeV2Realtime {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: fmt.Sprintf("non-streaming transcription with %s", m.modelID)}
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}

	if err := validateTranscriptionOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	body, contentType, warnings, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	currentDate := time.Now()
	var response elevenlabsTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaElevenLabsTr),
		Method:           http.MethodPost,
		Path:             "/v1/speech-to-text",
		Body:             body,
		Headers:          internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &response)
	if err != nil {
		return nil, handleTranscriptionError(err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(response.Words))
	var durationInSeconds *float64
	for _, word := range response.Words {
		start := valueOrZero(word.Start)
		end := valueOrZero(word.End)
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  word.Text,
			Start: start,
			End:   end,
		})
	}
	// TS: response.audio_duration_secs ?? response.words?.at(-1)?.end ??
	// undefined. The reported audio duration includes trailing silence and a
	// reported zero is preserved; only the last word's end is the fallback.
	if response.AudioDurationSecs != nil {
		d := *response.AudioDurationSecs
		durationInSeconds = &d
	} else if n := len(response.Words); n > 0 && response.Words[n-1].End != nil {
		e := *response.Words[n-1].End
		durationInSeconds = &e
	}

	var providerMetadata map[string]interface{}
	if response.Words != nil {
		words := make([]interface{}, 0, len(response.Words))
		for _, word := range response.Words {
			words = append(words, schemaWord(word.Raw))
		}
		providerMetadata = map[string]interface{}{"elevenlabs": map[string]interface{}{"words": words}}
	}

	return &types.TranscriptionResult{
		ProviderMetadata:  providerMetadata,
		Text:              response.Text,
		Segments:          segments,
		Timestamps:        segments,
		Language:          response.LanguageCode,
		DurationInSeconds: durationInSeconds,
		Warnings:          warnings,
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationValue(durationInSeconds),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      providerutils.RawJSONBody(httpResp.Body),
		},
	}, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, []types.Warning, error) {
	warnings := []types.Warning{}
	elOpts, present, hasStreaming := extractTranscriptionOptions(opts.ProviderOptions)

	if hasStreaming {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.elevenlabs.streaming",
			Details: "ElevenLabs batch transcription does not support streaming options.",
		})
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model_id", m.modelID); err != nil {
		return nil, "", nil, err
	}

	fileHeader := textproto.MIMEHeader{}
	fileHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="audio.%s"`, elevenLabsAudioExtension(opts.MimeType)))
	contentType := opts.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	fileHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(fileHeader)
	if err != nil {
		return nil, "", nil, err
	}
	if _, err := part.Write(opts.Audio); err != nil {
		return nil, "", nil, err
	}

	diarize := "true"
	if present && elOpts.Diarize != nil {
		diarize = fmt.Sprintf("%t", *elOpts.Diarize)
	}
	if err := writer.WriteField("diarize", diarize); err != nil {
		return nil, "", nil, err
	}

	if present {
		if elOpts.LanguageCode != nil {
			if err := writer.WriteField("language_code", *elOpts.LanguageCode); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.TagAudioEvents != nil {
			if err := writer.WriteField("tag_audio_events", fmt.Sprintf("%t", *elOpts.TagAudioEvents)); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.NumSpeakers != nil {
			if err := writer.WriteField("num_speakers", fmt.Sprintf("%d", *elOpts.NumSpeakers)); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.TimestampsGranularity != "" {
			if err := writer.WriteField("timestamps_granularity", elOpts.TimestampsGranularity); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.FileFormat != "" {
			if err := writer.WriteField("file_format", elOpts.FileFormat); err != nil {
				return nil, "", nil, err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", nil, err
	}
	return &buf, writer.FormDataContentType(), warnings, nil
}

// elevenLabsAudioExtension is TS provider-utils mediaTypeToExtension.
func elevenLabsAudioExtension(mimeType string) string {
	return providerutils.MediaTypeToExtension(mimeType)
}

func valueOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func durationValue(duration *float64) float64 {
	if duration == nil {
		return 0
	}
	return *duration
}

type elevenlabsTranscriptionResponse struct {
	AudioDurationSecs   *float64         `json:"audio_duration_secs"`
	LanguageCode        string           `json:"language_code"`
	LanguageProbability float64          `json:"language_probability"`
	Text                string           `json:"text"`
	Words               []elevenlabsWord `json:"words"`
}

// elevenlabsWord is one entry of the response "words" array. Raw keeps the
// complete wire object so type and speaker_id are preserved in provider
// metadata exactly as returned.
type elevenlabsWord struct {
	Text      string   `json:"text"`
	Type      string   `json:"type"`
	Start     *float64 `json:"start"`
	End       *float64 `json:"end"`
	SpeakerID string   `json:"speaker_id"`

	Raw map[string]interface{} `json:"-"`
}

func (w *elevenlabsWord) UnmarshalJSON(data []byte) error {
	type plain elevenlabsWord
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*w = elevenlabsWord(p)
	return json.Unmarshal(data, &w.Raw)
}

// handleTranscriptionError converts an HTTP error into a ProviderError,
// parsing ElevenLabs' `{"error":{"message","code"}}` error shape when present.
func handleTranscriptionError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := providerutils.NestedErrorMessage(statusErr.StatusCode, statusErr.Body)
		return providererrors.NewProviderErrorFromResponse("elevenlabs", 0, code, message, err)
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// schemaWord keeps only the keys of the TS response schema's word object
// (zod strips unknown keys such as logprob).
func schemaWord(raw map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, k := range []string{"text", "type", "start", "end", "speaker_id"} {
		if v, ok := raw[k]; ok {
			out[k] = v
		}
	}
	if chars, ok := raw["characters"].([]interface{}); ok {
		kept := make([]interface{}, 0, len(chars))
		for _, c := range chars {
			cm, _ := c.(map[string]interface{})
			item := map[string]interface{}{}
			for _, k := range []string{"text", "start", "end"} {
				if v, ok := cm[k]; ok {
					item[k] = v
				}
			}
			kept = append(kept, item)
		}
		out["characters"] = kept
	} else if v, ok := raw["characters"]; ok && v == nil {
		out["characters"] = nil
	}
	return out
}
