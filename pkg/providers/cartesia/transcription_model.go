package cartesia

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements provider.TranscriptionModel for Cartesia's
// batch transcription flow (POST /stt).
//
// ink-2 (and any "ink-2-*" variant) is a STREAMING-only realtime
// transcription model (WebSocket-based, over /stt/websocket and
// /stt/turns/websocket); that path is out of scope here, and DoTranscribe
// rejects it, matching the TypeScript SDK's UnsupportedFunctionalityError
// for non-streaming use of that model.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "cartesia.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// isStreamingTranscriptionModelID mirrors the TypeScript SDK's
// isStreamingTranscriptionModelId.
func isStreamingTranscriptionModelID(modelID string) bool {
	return modelID == ModelInk2 || strings.HasPrefix(modelID, "ink-2-")
}

type cartesiaTranscriptionResponse struct {
	Text     string   `json:"text"`
	Language *string  `json:"language"`
	Duration *float64 `json:"duration"`
	Words    []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words"`
}

// DoTranscribe performs batch speech-to-text transcription via POST /stt.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if isStreamingTranscriptionModelID(m.modelID) {
		return nil, fmt.Errorf("%w: non-streaming transcription with %s (this model is realtime/WebSocket-only)", providererrors.ErrUnsupportedFeature, m.modelID)
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	currentDate := time.Now()

	body, contentType, warnings, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	var response cartesiaTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/stt",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &response)
	if err != nil {
		return nil, handleError(err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(response.Words))
	for _, word := range response.Words {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  word.Word,
			Start: word.Start,
			End:   word.End,
		})
	}

	result := &types.TranscriptionResult{
		Text:       response.Text,
		Segments:   segments,
		Timestamps: segments,
		Warnings:   warnings,
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      response,
		},
	}
	if response.Language != nil {
		result.Language = *response.Language
	}
	if response.Duration != nil {
		result.DurationInSeconds = response.Duration
		result.Usage = types.TranscriptionUsage{DurationSeconds: *response.Duration}
	}
	return result, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, []types.Warning, error) {
	warnings := []types.Warning{}
	cartesiaOpts := extractTranscriptionModelOptions(opts.ProviderOptions)

	if cartesiaOpts != nil && cartesiaOpts.Streaming != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.cartesia.streaming",
			Details: "Cartesia batch transcription does not support streaming options.",
		})
	}

	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.AudioBase64)
		if err != nil {
			return nil, "", nil, fmt.Errorf("cartesia: failed to decode base64 audio: %w", err)
		}
		audio = decoded
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", nil, err
	}

	part, err := writer.CreateFormFile("file", "audio."+cartesiaAudioExtension(opts.MimeType))
	if err != nil {
		return nil, "", nil, err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", nil, err
	}

	if cartesiaOpts != nil {
		if cartesiaOpts.Language != "" {
			if err := writer.WriteField("language", cartesiaOpts.Language); err != nil {
				return nil, "", nil, err
			}
		}
		for _, granularity := range cartesiaOpts.TimestampGranularities {
			if err := writer.WriteField("timestamp_granularities[]", granularity); err != nil {
				return nil, "", nil, err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", nil, err
	}
	return &buf, writer.FormDataContentType(), warnings, nil
}

func cartesiaAudioExtension(mimeType string) string {
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
