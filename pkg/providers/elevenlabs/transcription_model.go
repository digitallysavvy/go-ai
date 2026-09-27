package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for
// ElevenLabs batch (non-streaming) transcription (POST /v1/speech-to-text).
//
// scribe_v2_realtime is a STREAMING-only realtime model (WebSocket-based) in
// the TypeScript SDK; that path is out of scope here, and DoTranscribe
// rejects it, matching TS's UnsupportedFunctionalityError for non-streaming
// use of that model.
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
func (m *TranscriptionModel) Provider() string { return "elevenlabs" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// DoTranscribe performs batch speech-to-text transcription.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if m.modelID == ModelScribeV2Realtime {
		return nil, fmt.Errorf("%w: non-streaming transcription with %s (this model is realtime/WebSocket-only)", providererrors.ErrUnsupportedFeature, m.modelID)
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}

	body, contentType, warnings, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	currentDate := time.Now()
	var response elevenlabsTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/speech-to-text",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
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
		if word.End != nil {
			e := *word.End
			durationInSeconds = &e
		}
	}

	return &types.TranscriptionResult{
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
			Body:      response,
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

	part, err := writer.CreateFormFile("file", "audio."+elevenLabsAudioExtension(opts.MimeType))
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
		if elOpts.LanguageCode != "" {
			if err := writer.WriteField("language_code", elOpts.LanguageCode); err != nil {
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

func elevenLabsAudioExtension(mimeType string) string {
	switch mimeType {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/flac":
		return "flac"
	case "audio/ogg":
		return "ogg"
	default:
		return "wav"
	}
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
	LanguageCode        string  `json:"language_code"`
	LanguageProbability float64 `json:"language_probability"`
	Text                string  `json:"text"`
	Words               []struct {
		Text      string   `json:"text"`
		Type      string   `json:"type"`
		Start     *float64 `json:"start"`
		End       *float64 `json:"end"`
		SpeakerID string   `json:"speaker_id"`
	} `json:"words"`
}

// handleTranscriptionError converts an HTTP error into a ProviderError,
// parsing ElevenLabs' `{"error":{"message","code"}}` error shape when present.
func handleTranscriptionError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Error.Message != "" {
			message = payload.Error.Message
			code = fmt.Sprintf("%d", payload.Error.Code)
		}
		providerErr := providererrors.NewProviderError("elevenlabs", statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("elevenlabs", 0, "", err.Error(), err)
}
