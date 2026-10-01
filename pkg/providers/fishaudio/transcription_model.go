package fishaudio

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements provider.TranscriptionModel for Fish Audio
// (POST /v1/asr).
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "fish-audio.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

type fishAudioTranscriptionResponse struct {
	Text         string   `json:"text"`
	Language     *string  `json:"language"`
	LanguageCode *string  `json:"language_code"`
	Duration     *float64 `json:"duration"`
	Segments     []struct {
		Text  string  `json:"text"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"segments"`
}

// DoTranscribe performs speech-to-text transcription via POST /v1/asr.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	currentDate := time.Now()

	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}

	var response fishAudioTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v1/asr",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &response)
	if err != nil {
		return nil, handleError(err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(response.Segments))
	for _, seg := range response.Segments {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  seg.Text,
			Start: seg.Start,
			End:   seg.End,
		})
	}

	result := &types.TranscriptionResult{
		Text:       response.Text,
		Segments:   segments,
		Timestamps: segments,
		Warnings:   []types.Warning{},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      response,
		},
	}
	// `language_code` is absent from the documented response schema but is
	// returned in practice, and reflects the detected language rather than
	// the requested one.
	if response.LanguageCode != nil {
		result.Language = *response.LanguageCode
	}
	if response.Duration != nil {
		result.DurationInSeconds = response.Duration
		result.Usage = types.TranscriptionUsage{DurationSeconds: *response.Duration}
	}
	if response.Language != nil {
		result.ProviderMetadata = map[string]interface{}{
			"fishAudio": map[string]interface{}{
				// Human-readable display name, e.g. "English". Its exact form
				// is not guaranteed, so Language above (the ISO-639-1 code)
				// is the value to branch on.
				"language": *response.Language,
			},
		}
	}
	return result, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, error) {
	fishOpts := extractTranscriptionModelOptions(opts.ProviderOptions)

	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.AudioBase64)
		if err != nil {
			return nil, "", fmt.Errorf("fish-audio: failed to decode base64 audio: %w", err)
		}
		audio = decoded
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// CreateFormFile always sets Content-Type: application/octet-stream, but
	// the TypeScript SDK constructs the multipart file with the actual audio
	// media type (`new File([blob], 'audio', { type: mediaType })`), so build
	// the part headers explicitly to preserve that.
	filename := "audio." + fishAudioExtension(opts.MimeType)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="audio"; filename=%q`, filename))
	if opts.MimeType != "" {
		header.Set("Content-Type", opts.MimeType)
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}

	if fishOpts != nil && fishOpts.Language != "" {
		if err := writer.WriteField("language", fishOpts.Language); err != nil {
			return nil, "", err
		}
	}

	// Fish Audio defaults ignore_timestamps to true, which leaves the
	// transcription result without segments. Request timestamps by default
	// and let callers opt back out.
	ignoreTimestamps := false
	if fishOpts != nil && fishOpts.IgnoreTimestamps != nil {
		ignoreTimestamps = *fishOpts.IgnoreTimestamps
	}
	if err := writer.WriteField("ignore_timestamps", fmt.Sprintf("%t", ignoreTimestamps)); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func fishAudioExtension(mimeType string) string {
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
