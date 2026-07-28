package xai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

func NewTranscriptionModel(provider *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: provider, modelID: modelID}
}

func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *TranscriptionModel) Provider() string             { return "xai.transcription" }
func (m *TranscriptionModel) ModelID() string              { return m.modelID }

type XAITranscriptionProviderOptions struct {
	AudioFormat  *string     `json:"audioFormat,omitempty"`
	SampleRate   *int        `json:"sampleRate,omitempty"`
	Language     *string     `json:"language,omitempty"`
	Format       *bool       `json:"format,omitempty"`
	Multichannel *bool       `json:"multichannel,omitempty"`
	Channels     *int        `json:"channels,omitempty"`
	Diarize      *bool       `json:"diarize,omitempty"`
	Keyterm      interface{} `json:"keyterm,omitempty"`
	FillerWords  *bool       `json:"fillerWords,omitempty"`
}

func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}
	timestamp := time.Now()

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/stt",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, newXAIProviderError(m.Provider(), resp.StatusCode, resp.Body)
	}

	var parsed xaiTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode xAI transcription response: %w", err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(parsed.Words))
	for _, word := range parsed.Words {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  word.Text,
			Start: word.Start,
			End:   word.End,
		})
	}

	return &types.TranscriptionResult{
		Text:              parsed.Text,
		Segments:          segments,
		Language:          parsed.Language,
		DurationInSeconds: parsed.Duration,
		Warnings:          []types.Warning{},
		Response: &types.ResponseMetadata{
			Timestamp: timestamp,
			ModelID:   m.modelID,
			Headers:   flattenHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}, nil
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (io.Reader, string, error) {
	xaiOpts, err := extractXAITranscriptionProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fields := []struct {
		key   string
		value string
		ok    bool
	}{
		{"audio_format", stringPtrValue(xaiOpts.AudioFormat), xaiOpts.AudioFormat != nil},
		{"sample_rate", intPtrValue(xaiOpts.SampleRate), xaiOpts.SampleRate != nil},
		{"language", stringPtrValue(xaiOpts.Language), xaiOpts.Language != nil},
		{"format", boolPtrValue(xaiOpts.Format), xaiOpts.Format != nil},
		{"multichannel", boolPtrValue(xaiOpts.Multichannel), xaiOpts.Multichannel != nil},
		{"channels", intPtrValue(xaiOpts.Channels), xaiOpts.Channels != nil},
		{"diarize", boolPtrValue(xaiOpts.Diarize), xaiOpts.Diarize != nil},
		{"filler_words", boolPtrValue(xaiOpts.FillerWords), xaiOpts.FillerWords != nil},
	}
	for _, field := range fields {
		if field.ok {
			if err := writer.WriteField(field.key, field.value); err != nil {
				return nil, "", err
			}
		}
	}
	for _, keyterm := range xaiKeyterms(xaiOpts.Keyterm) {
		if err := writer.WriteField("keyterm", keyterm); err != nil {
			return nil, "", err
		}
	}

	audio := opts.Audio
	if opts.AudioBase64 != "" {
		decoded, err := decodeXAITranscriptionAudioBase64(opts.AudioBase64)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode base64 audio: %w", err)
		}
		audio = decoded
	}
	mediaType := opts.MimeType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	filename := "audio." + xaiAudioExtension(mediaType)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filename))
	header.Set("Content-Type", mediaType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create audio file part: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", fmt.Errorf("failed to write audio file part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart body: %w", err)
	}
	return &buf, writer.FormDataContentType(), nil
}

func extractXAITranscriptionProviderOptions(providerOptions map[string]interface{}) (XAITranscriptionProviderOptions, error) {
	var opts XAITranscriptionProviderOptions
	raw, ok := providerOptions["xai"]
	if !ok || raw == nil {
		return opts, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if opts.AudioFormat != nil && !xaiStringIn(*opts.AudioFormat, "pcm", "mulaw", "alaw") {
		return opts, invalidXAIProviderOptions(fmt.Errorf("audioFormat must be \"pcm\", \"mulaw\", or \"alaw\""))
	}
	if opts.SampleRate != nil && !xaiIntIn(*opts.SampleRate, 8000, 16000, 22050, 24000, 44100, 48000) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("sampleRate must be one of 8000, 16000, 22050, 24000, 44100, or 48000"))
	}
	if opts.Channels != nil && (*opts.Channels < 2 || *opts.Channels > 8) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("channels must be between 2 and 8"))
	}
	if err := validateXAIKeyterm(opts.Keyterm); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	return opts, nil
}

func xaiKeyterms(raw interface{}) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return []string{v}
	case []string:
		return append([]string(nil), v...)
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func xaiAudioExtension(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/opus":
		return "ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return "m4a"
	case "audio/pcm":
		return "pcm"
	case "audio/mulaw":
		return "mulaw"
	case "audio/alaw":
		return "alaw"
	default:
		if slash := strings.LastIndex(mediaType, "/"); slash >= 0 && slash < len(mediaType)-1 {
			return mediaType[slash+1:]
		}
		return ""
	}
}

func decodeXAITranscriptionAudioBase64(content string) ([]byte, error) {
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(content)
	if decoded, err := base64.StdEncoding.DecodeString(normalized); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(normalized)
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func intPtrValue(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func boolPtrValue(v *bool) string {
	if v == nil {
		return ""
	}
	if *v {
		return "true"
	}
	return "false"
}

type xaiTranscriptionResponse struct {
	Text     string   `json:"text"`
	Language string   `json:"language,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Words    []struct {
		Text  string  `json:"text"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words,omitempty"`
}
