package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
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
	// AudioFormat is the input audio encoding for raw, headerless audio:
	// "pcm", "mulaw", or "alaw". Container formats such as Ogg/Opus are
	// auto-detected, so do not set this for them. "opus" is for streaming
	// only, with one raw Opus packet per chunk.
	AudioFormat  *string     `json:"audioFormat,omitempty"`
	SampleRate   *int        `json:"sampleRate,omitempty"`
	Language     *string     `json:"language,omitempty"`
	Format       *bool       `json:"format,omitempty"`
	Multichannel *bool       `json:"multichannel,omitempty"`
	Channels     *int        `json:"channels,omitempty"`
	Diarize      *bool       `json:"diarize,omitempty"`
	Keyterm      interface{} `json:"keyterm,omitempty"`
	FillerWords  *bool       `json:"fillerWords,omitempty"`

	// VadThreshold is the voice activity detection threshold between 0 and
	// 1. xAI defaults to 0.5 for batch transcription and 0.08 for streaming;
	// 0 disables the filter.
	VadThreshold *float64 `json:"vadThreshold,omitempty"`

	// Streaming holds options specific to streaming speech-to-text over
	// WebSocket (DoStream). Ignored by DoTranscribe.
	Streaming *XAIStreamingTranscriptionOptions `json:"streaming,omitempty"`
}

// XAIStreamingTranscriptionOptions mirrors TS
// xaiTranscriptionModelOptionsSchema's nested `streaming` object.
type XAIStreamingTranscriptionOptions struct {
	InterimResults   *bool    `json:"interimResults,omitempty"`
	Endpointing      *int     `json:"endpointing,omitempty"`
	SmartTurn        *float64 `json:"smartTurn,omitempty"`
	SmartTurnTimeout *int     `json:"smartTurnTimeout,omitempty"`
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
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		if noResp, ok := internalhttp.NoResponseError(err); ok {
			return nil, noResp // TS handleFetchError: an abort or timeout is returned unchanged
		}
		return nil, err // TS handleFetchError: abort as is, else "Cannot connect to API"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 { // TS !response.ok
		return nil, newXAIProviderErrorFrom(m.Provider(), resp.StatusError(resp.Body))
	}

	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text(): BOM dropped, invalid UTF-8 replaced
	if err := providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaXAITranscr)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var parsed xaiTranscriptionResponse
	if err := jsonexact.Unmarshal(resp.Body, &parsed); err != nil {
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

	// Preserve diarized words (those carrying a speaker) in provider
	// metadata (TS #22048).
	var providerMetadata map[string]interface{}
	for _, word := range parsed.Words {
		if word.Speaker == nil {
			continue
		}
		// TS reports the zod-parsed words: a present `speaker: null` stays.
		var rawWords struct {
			Words []map[string]json.RawMessage `json:"words"`
		}
		_ = jsonexact.Unmarshal(resp.Body, &rawWords)
		words := make([]map[string]interface{}, 0, len(parsed.Words))
		for i, w := range parsed.Words {
			entry := map[string]interface{}{"text": w.Text, "start": w.Start, "end": w.End}
			if w.Speaker != nil {
				entry["speaker"] = *w.Speaker
			} else if i < len(rawWords.Words) {
				if _, present := rawWords.Words[i]["speaker"]; present {
					entry["speaker"] = nil
				}
			}
			words = append(words, entry)
		}
		providerMetadata = map[string]interface{}{"xai": map[string]interface{}{"words": words}}
		break
	}

	// TS 41465fd1b6: xAI returns word timings only; a segment request warns,
	// a word request answered without words (for non-blank text) warns, and
	// the words are reported separately when present.
	warnings := []types.Warning{}
	if containsGranularity(opts.TimestampGranularities, provider.TranscriptionTimestampGranularitySegment) {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "timestampGranularities",
			Details: "xAI transcription returns word timings and does not provide separate caption segments."})
	}
	if containsGranularity(opts.TimestampGranularities, provider.TranscriptionTimestampGranularityWord) &&
		len(parsed.Words) == 0 && providerutils.JSTrim(parsed.Text) != "" {
		warnings = append(warnings, types.Warning{Type: "other", Message: "The xAI transcription response omitted the requested word timings."})
	}
	var words []types.TranscriptionWord
	if len(parsed.Words) > 0 {
		words = make([]types.TranscriptionWord, len(parsed.Words))
		for i, w := range parsed.Words {
			words[i] = types.TranscriptionWord{Text: w.Text, StartSecond: w.Start, EndSecond: w.End}
		}
	}

	return &types.TranscriptionResult{
		Text:              parsed.Text,
		Segments:          segments,
		Words:             words,
		Language:          parsed.Language,
		DurationInSeconds: parsed.Duration,
		Warnings:          warnings,
		ProviderMetadata:  providerMetadata,
		Response: &types.ResponseMetadata{
			Timestamp: timestamp,
			ModelID:   m.modelID,
			Headers:   flattenHeaders(resp.Headers),
			Body:      providerutils.RawJSONBody(resp.Body),
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

	// Field order mirrors TS getXaiSttParams + audio_format/format; xAI
	// requires `file` to be the final multipart field.
	fields := []struct {
		key   string
		value string
		ok    bool
	}{
		{"model", m.modelID, m.modelID != ""},
		{"sample_rate", intPtrValue(xaiOpts.SampleRate), xaiOpts.SampleRate != nil},
		{"language", stringPtrValue(xaiOpts.Language), xaiOpts.Language != nil},
		{"multichannel", boolPtrValue(xaiOpts.Multichannel), xaiOpts.Multichannel != nil},
		{"channels", intPtrValue(xaiOpts.Channels), xaiOpts.Channels != nil},
		{"diarize", boolPtrValue(xaiOpts.Diarize), xaiOpts.Diarize != nil},
		{"filler_words", boolPtrValue(xaiOpts.FillerWords), xaiOpts.FillerWords != nil},
		{"vad_threshold", floatPtrValue(xaiOpts.VadThreshold), xaiOpts.VadThreshold != nil},
	}
	for _, field := range fields {
		if field.ok {
			if err := providerutils.WriteFormDataField(writer, field.key, field.value); err != nil {
				return nil, "", err
			}
		}
	}
	for _, keyterm := range xaiKeyterms(xaiOpts.Keyterm) {
		if err := providerutils.WriteFormDataField(writer, "keyterm", keyterm); err != nil {
			return nil, "", err
		}
	}
	if xaiOpts.AudioFormat != nil {
		if err := providerutils.WriteFormDataField(writer, "audio_format", *xaiOpts.AudioFormat); err != nil {
			return nil, "", err
		}
	}
	if xaiOpts.Format != nil {
		if err := providerutils.WriteFormDataField(writer, "format", boolPtrValue(xaiOpts.Format)); err != nil {
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
	// TS File type is the media type (an empty one is sent as
	// application/octet-stream by FormData) and the name is
	// audio.<mediaTypeToExtension(mediaType)>.
	mediaType := opts.MimeType
	filename := "audio." + xaiAudioExtension(mediaType)
	part, err := providerutils.CreateFormDataFile(writer, "file", filename, mediaType)
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
	b, err := json.Marshal(raw) // caller options: a non-finite number is invalid (zod), not null
	if err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	// TS vadThreshold, streaming and the streaming members are .optional()
	// (not nullish): a present null is invalid.
	var probe map[string]json.RawMessage
	if json.Unmarshal(b, &probe) == nil {
		for _, key := range []string{"vadThreshold", "streaming"} {
			if string(probe[key]) == "null" {
				return opts, invalidXAIProviderOptions(fmt.Errorf("%s must not be null", key))
			}
		}
		var streaming map[string]json.RawMessage
		if json.Unmarshal(probe["streaming"], &streaming) == nil {
			for _, key := range []string{"interimResults", "endpointing", "smartTurn", "smartTurnTimeout"} {
				if string(streaming[key]) == "null" {
					return opts, invalidXAIProviderOptions(fmt.Errorf("streaming.%s must not be null", key))
				}
			}
		}
	}
	if opts.AudioFormat != nil && !xaiStringIn(*opts.AudioFormat, "pcm", "mulaw", "alaw", "opus") {
		return opts, invalidXAIProviderOptions(fmt.Errorf("audioFormat must be \"pcm\", \"mulaw\", \"alaw\", or \"opus\""))
	}
	if opts.VadThreshold != nil && (*opts.VadThreshold < 0 || *opts.VadThreshold > 1) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("vadThreshold must be between 0 and 1"))
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
	if opts.Streaming != nil {
		s := opts.Streaming
		if s.Endpointing != nil && (*s.Endpointing < 0 || *s.Endpointing > 5000) {
			return opts, invalidXAIProviderOptions(fmt.Errorf("streaming.endpointing must be between 0 and 5000"))
		}
		if s.SmartTurn != nil && (*s.SmartTurn < 0 || *s.SmartTurn > 1) {
			return opts, invalidXAIProviderOptions(fmt.Errorf("streaming.smartTurn must be between 0 and 1"))
		}
		if s.SmartTurnTimeout != nil && (*s.SmartTurnTimeout < 1 || *s.SmartTurnTimeout > 5000) {
			return opts, invalidXAIProviderOptions(fmt.Errorf("streaming.smartTurnTimeout must be between 1 and 5000"))
		}
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
	return providerutils.MediaTypeToExtension(mediaType) // TS mediaTypeToExtension
}

func decodeXAITranscriptionAudioBase64(content string) ([]byte, error) {
	// TS convertBase64ToUint8Array: base64url mapped, atob's forgiving rules.
	return fileutil.ConvertBase64ToBytes(content)
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

func floatPtrValue(v *float64) string {
	if v == nil {
		return ""
	}
	return providerutils.JSNumberString(*v) // TS String(number), as the stream query does
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
		Text    string   `json:"text"`
		Start   float64  `json:"start"`
		End     float64  `json:"end"`
		Speaker *float64 `json:"speaker"`
	} `json:"words,omitempty"`
}

func containsGranularity(list []string, g string) bool {
	for _, v := range list {
		if v == g {
			return true
		}
	}
	return false
}
