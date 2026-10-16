package groq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strconv"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for
// Groq's Whisper-based batch transcription (POST /audio/transcriptions).
// Mirrors TS GroqTranscriptionModel; streaming transcription is out of scope.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Groq transcription model.
func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "groq.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// groqTranscriptionOptions mirrors groqTranscriptionModelOptions in TS.
type groqTranscriptionOptions struct {
	Language               *string
	Prompt                 *string
	ResponseFormat         *string
	Temperature            *float64
	TimestampGranularities []string
}

func resolveGroqTranscriptionOptions(providerOptions map[string]interface{}) groqTranscriptionOptions {
	raw, _ := providerOptions["groq"].(map[string]interface{})
	var opts groqTranscriptionOptions
	if raw == nil {
		return opts
	}
	if v, ok := raw["language"].(string); ok {
		opts.Language = &v
	}
	if v, ok := raw["prompt"].(string); ok {
		opts.Prompt = &v
	}
	if v, ok := raw["responseFormat"].(string); ok {
		opts.ResponseFormat = &v
	}
	switch v := raw["temperature"].(type) {
	case float64:
		opts.Temperature = &v
	case int:
		f := float64(v)
		opts.Temperature = &f
	}
	if arr, ok := raw["timestampGranularities"].([]interface{}); ok {
		for _, item := range arr {
			if s, ok := item.(string); ok {
				opts.TimestampGranularities = append(opts.TimestampGranularities, s)
			}
		}
	} else if arr, ok := raw["timestampGranularities"].([]string); ok {
		opts.TimestampGranularities = arr
	}
	return opts
}

// DoTranscribe performs batch speech-to-text transcription.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	audio, err := groqAudioBytes(opts)
	if err != nil {
		return nil, err
	}
	if err := validateGroqTranscriptionOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	groqOpts := resolveGroqTranscriptionOptions(opts.ProviderOptions)

	body, contentType, err := m.buildMultipartBody(audio, opts.MimeType, groqOpts)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Path:    "/v1/audio/transcriptions",
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	})
	if err != nil {
		return nil, providererrors.NewProviderError("groq", 0, "", err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		// TS groqFailedResponseHandler: use error.message from the JSON envelope.
		statusErr := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}
		if perr := parseGroqProviderError(statusErr); perr != nil {
			return nil, perr
		}
		return nil, providererrors.NewProviderError("groq", resp.StatusCode, "", string(resp.Body), nil)
	}

	timestamp := time.Now()
	responseHeaders := providerutils.ExtractHeaders(resp.Headers)

	// A plain-text response (responseFormat: "text") is not JSON; only try
	// the JSON path when the response doesn't look like bare text.
	if groqOpts.ResponseFormat != nil && *groqOpts.ResponseFormat == "text" {
		text := string(resp.Body)
		return &types.TranscriptionResult{
			Text: text,
			Response: &types.ResponseMetadata{
				Timestamp: timestamp,
				ModelID:   m.modelID,
				Headers:   responseHeaders,
				Body:      text,
			},
		}, nil
	}

	if err := providerutils.MediaResponseValidator("groq", providerutils.SchemaGroqTranscr)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var response groqTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("groq: failed to decode transcription response: %w", err)
	}

	result := &types.TranscriptionResult{
		Text:     response.Text,
		Language: response.Language,
	}
	if response.Duration != nil {
		result.DurationInSeconds = response.Duration
	}

	switch {
	// TS `segments?.map(...) ?? words?.map(...)`: an empty segments array wins.
	case response.Segments != nil:
		for _, seg := range response.Segments {
			result.Segments = append(result.Segments, types.TranscriptionTimestamp{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			})
		}
	case response.Words != nil:
		// Fall back to word-level timestamps when segment timestamps aren't
		// available (TS: response.segments?.map(...) ?? response.words?.map(...)).
		for _, word := range response.Words {
			result.Segments = append(result.Segments, types.TranscriptionTimestamp{
				Text:  word.Word,
				Start: word.Start,
				End:   word.End,
			})
		}
	}

	result.Response = &types.ResponseMetadata{
		Timestamp: timestamp,
		ModelID:   m.modelID,
		Headers:   responseHeaders,
		Body:      providerutils.RawJSONBody(resp.Body),
	}
	return result, nil
}

func groqAudioBytes(opts *provider.TranscriptionOptions) ([]byte, error) {
	if len(opts.Audio) > 0 {
		return opts.Audio, nil
	}
	if opts.AudioBase64 != "" {
		decoded, err := types.DecodeFileDataString(opts.AudioBase64)
		if err != nil {
			return nil, fmt.Errorf("groq: failed to decode base64 audio: %w", err)
		}
		return decoded, nil
	}
	return nil, fmt.Errorf("groq: no audio data provided")
}

func (m *TranscriptionModel) buildMultipartBody(audio []byte, mediaType string, opts groqTranscriptionOptions) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", err
	}

	part, err := providerutils.CreateTypedFormFile(writer, "file", "audio."+groqAudioExtension(mediaType), mediaType)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}

	// TS appends every defined option, including an empty string.
	if opts.Language != nil {
		if err := writer.WriteField("language", *opts.Language); err != nil {
			return nil, "", err
		}
	}
	// TS appends every defined option, including an empty string.
	if opts.Prompt != nil {
		if err := writer.WriteField("prompt", *opts.Prompt); err != nil {
			return nil, "", err
		}
	}
	if opts.ResponseFormat != nil {
		if err := writer.WriteField("response_format", *opts.ResponseFormat); err != nil {
			return nil, "", err
		}
	}
	if opts.Temperature != nil {
		if err := writer.WriteField("temperature", strconv.FormatFloat(*opts.Temperature, 'f', -1, 64)); err != nil {
			return nil, "", err
		}
	}
	for _, granularity := range opts.TimestampGranularities {
		if err := writer.WriteField("timestamp_granularities[]", granularity); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func groqAudioExtension(mediaType string) string {
	return providerutils.MediaTypeToExtension(mediaType)
}

// groqTranscriptionResponse mirrors groqTranscriptionResponseSchema in TS.
// Additional fields (task, language, duration, segments, words) are only
// present when response_format is verbose_json.
type groqTranscriptionResponse struct {
	Text     string   `json:"text"`
	Task     string   `json:"task,omitempty"`
	Language string   `json:"language,omitempty"`
	Duration *float64 `json:"duration,omitempty"`
	Segments []struct {
		Text  string  `json:"text"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"segments,omitempty"`
	Words []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words,omitempty"`
	XGroq struct {
		ID string `json:"id"`
	} `json:"x_groq,omitempty"`
}

// validateGroqTranscriptionOptions mirrors TS parseProviderOptions with
// groqTranscriptionModelOptions: language/prompt/responseFormat are nullish
// strings, temperature a nullish number in [0, 1], timestampGranularities a
// nullish array of strings. A violation is an InvalidArgumentError.
func validateGroqTranscriptionOptions(providerOptions map[string]interface{}) error {
	raw, ok := providerOptions["groq"]
	if !ok || raw == nil {
		return nil
	}
	fail := func(msg string) error {
		return &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "invalid groq provider options",
			Cause:   errors.New(msg),
		}
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return fail("Invalid input: expected object")
	}
	for _, key := range []string{"language", "prompt", "responseFormat"} {
		if v, ok := m[key]; ok && v != nil {
			if _, isString := v.(string); !isString {
				return fail("Invalid input: expected string at " + key)
			}
		}
	}
	if v, ok := m["temperature"]; ok && v != nil {
		var f float64
		switch n := v.(type) {
		case float64:
			f = n
		case float32:
			f = float64(n)
		case int:
			f = float64(n)
		case int64:
			f = float64(n)
		default:
			return fail("Invalid input: expected number at temperature")
		}
		if f < 0 || f > 1 {
			return fail("Invalid input: expected number between 0 and 1 at temperature")
		}
	}
	if v, ok := m["timestampGranularities"]; ok && v != nil {
		switch list := v.(type) {
		case []string:
		case []interface{}:
			for _, item := range list {
				if _, isString := item.(string); !isString {
					return fail("Invalid input: expected string at timestampGranularities")
				}
			}
		default:
			return fail("Invalid input: expected array at timestampGranularities")
		}
	}
	return nil
}
