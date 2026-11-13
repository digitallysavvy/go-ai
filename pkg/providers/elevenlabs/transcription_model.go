package elevenlabs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
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
	if opts.ProviderOptions != nil {
		// Go-typed option literals are read through their JSON form.
		normalized := *opts
		normalized.ProviderOptions = normalizeProviderOptions(opts.ProviderOptions)
		opts = &normalized
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

	if err := providerutils.WriteFormDataField(writer, "model_id", m.modelID); err != nil {
		return nil, "", nil, err
	}

	// ElevenLabs accepts exactly one of file or source_url (TS 8b2fa5ba00).
	if present && elOpts.SourceURL != nil {
		if err := providerutils.WriteFormDataField(writer, "source_url", *elOpts.SourceURL); err != nil {
			return nil, "", nil, err
		}
		if len(opts.Audio) > 0 {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "ElevenLabs transcribes providerOptions.elevenlabs.sourceUrl; the audio input was ignored.",
			})
		}
	} else {
		part, err := providerutils.CreateFormDataFile(writer, "file", "audio."+elevenLabsAudioExtension(opts.MimeType), opts.MimeType)
		if err != nil {
			return nil, "", nil, err
		}
		if _, err := part.Write(opts.Audio); err != nil {
			return nil, "", nil, err
		}
	}

	diarize := "true"
	if present && elOpts.Diarize != nil {
		diarize = fmt.Sprintf("%t", *elOpts.Diarize)
	}
	if err := providerutils.WriteFormDataField(writer, "diarize", diarize); err != nil {
		return nil, "", nil, err
	}

	if present {
		if elOpts.LanguageCode != nil {
			if err := providerutils.WriteFormDataField(writer, "language_code", *elOpts.LanguageCode); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.TagAudioEvents != nil {
			if err := providerutils.WriteFormDataField(writer, "tag_audio_events", fmt.Sprintf("%t", *elOpts.TagAudioEvents)); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.NumSpeakers != nil {
			if err := providerutils.WriteFormDataField(writer, "num_speakers", fmt.Sprintf("%d", *elOpts.NumSpeakers)); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.TimestampsGranularity != "" {
			if err := providerutils.WriteFormDataField(writer, "timestamps_granularity", elOpts.TimestampsGranularity); err != nil {
				return nil, "", nil, err
			}
		}
		if elOpts.FileFormat != "" {
			if err := providerutils.WriteFormDataField(writer, "file_format", elOpts.FileFormat); err != nil {
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
	if err := jsonexact.Unmarshal(data, &p); err != nil {
		return err
	}
	*w = elevenlabsWord(p)
	return jsonexact.Unmarshal(data, &w.Raw)
}

// elevenlabsErrorDataSchema mirrors TS elevenlabsErrorDataSchema
// (elevenlabs-error.ts, TS 7062ab6818): errors under `detail` (an API error
// object, FastAPI's validation issue list, or a string), or the older
// {"error":{"message","code"}} shape.
var elevenlabsErrorDataSchema = zodlite.Union(elevenlabsErrorDetailSchema, elevenlabsErrorObjectSchema)

// elevenlabsErrorDetailSchema is the first union member ({detail}).
var elevenlabsErrorDetailSchema = zodlite.Object(zodlite.F("detail", zodlite.Union(
	zodlite.Object(
		zodlite.F("message", zodlite.String),
		zodlite.F("code", zodlite.Nullish(zodlite.String)),
		zodlite.F("status", zodlite.Nullish(zodlite.String)),
		zodlite.F("type", zodlite.Nullish(zodlite.String)),
		zodlite.F("request_id", zodlite.Nullish(zodlite.String)),
		zodlite.F("param", zodlite.Nullish(zodlite.String)),
	),
	zodlite.Array(zodlite.Object(
		zodlite.F("msg", zodlite.String),
		zodlite.F("loc", zodlite.Nullish(zodlite.Array(zodlite.Union(zodlite.String, zodlite.Number)))),
		zodlite.F("type", zodlite.Nullish(zodlite.String)),
	)),
	zodlite.String,
)))

// elevenlabsErrorObjectSchema is the second union member ({error}).
var elevenlabsErrorObjectSchema = zodlite.Object(zodlite.F("error", zodlite.Object(
	zodlite.F("message", zodlite.String),
	zodlite.F("code", zodlite.Number),
)))

// elevenlabsErrorMessage is TS createJsonErrorResponseHandler with
// elevenlabsErrorDataSchema and elevenlabsErrorToMessage: the message of a
// body that matches the schema, else the HTTP status text. code is the
// numeric error.code of the older shape (Go ProviderError.ErrorCode).
func elevenlabsErrorMessage(statusCode int, body []byte) (message, code string) {
	var data map[string]interface{}
	// TS createJsonErrorResponseHandler reads response.text() (BOM dropped,
	// invalid UTF-8 replaced).
	body = []byte(textdecode.UTF8(body))
	if securejson.Unmarshal(body, &data) != nil || elevenlabsErrorDataSchema(data, true, nil) != nil {
		return http.StatusText(statusCode), ""
	}
	// zod's union yields the first member that matches: a body whose detail
	// does not match is read through its error object.
	if elevenlabsErrorDetailSchema(data, true, nil) != nil {
		return providerutils.NestedErrorMessage(statusCode, body)
	}
	detail := data["detail"]
	switch d := detail.(type) {
	case string:
		return d, ""
	case []interface{}:
		parts := make([]string, 0, len(d))
		for _, item := range d {
			issue, _ := item.(map[string]interface{})
			msg, _ := issue["msg"].(string)
			if loc, ok := issue["loc"].([]interface{}); ok && len(loc) > 0 {
				segs := make([]string, 0, len(loc))
				for _, l := range loc {
					if f, ok := l.(float64); ok {
						segs = append(segs, providerutils.JSNumberString(f))
					} else {
						segs = append(segs, fmt.Sprint(l))
					}
				}
				msg = strings.Join(segs, ".") + ": " + msg
			}
			parts = append(parts, msg)
		}
		return strings.Join(parts, "; "), ""
	case map[string]interface{}:
		msg, _ := d["message"].(string)
		if c, ok := d["code"].(string); ok {
			return msg + " (" + c + ")", ""
		}
		return msg, ""
	}
	return http.StatusText(statusCode), ""
}

// handleTranscriptionError converts an HTTP error into a ProviderError,
// parsing the ElevenLabs error shapes (TS elevenlabsFailedResponseHandler).
func handleTranscriptionError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := elevenlabsErrorMessage(statusErr.StatusCode, statusErr.Body)
		perr := providererrors.NewProviderErrorFromResponse("elevenlabs", 0, code, message, err)
		perr.Data = elevenlabsErrorData(statusErr.Body)
		return perr
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return internalhttp.UnwrapFetchRejection(err)
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

// elevenlabsErrorData is TS APICallError.data: the elevenlabsErrorDataSchema
// output for a matching body, else nil. The {detail} member keeps its loose
// detail as sent; the {error} member keeps message and code.
func elevenlabsErrorData(body []byte) interface{} {
	var data map[string]interface{}
	body = []byte(textdecode.UTF8(body))
	if securejson.Unmarshal(body, &data) != nil || elevenlabsErrorDataSchema(data, true, nil) != nil {
		return nil
	}
	if elevenlabsErrorDetailSchema(data, true, nil) == nil {
		return map[string]interface{}{"detail": data["detail"]}
	}
	e, _ := data["error"].(map[string]interface{})
	return map[string]interface{}{"error": map[string]interface{}{"message": e["message"], "code": e["code"]}}
}
