package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// isLiveTranscriptionModelID reports whether modelID names a Gemini Live
// transcription variant (e.g. "gemini-3.5-transcribe-live"), which only
// supports streaming transcription (TS isLiveTranscriptionModelId).
func isLiveTranscriptionModelID(modelID string) bool {
	return strings.Contains(modelID, "-live")
}

// TranscriptionModel implements Gemini 3.5 Transcribe (TS
// GoogleTranscriptionModel): unary audio is transcribed through the
// Interactions API (DoTranscribe, TS doGenerate); "-live" model variants
// (e.g. "gemini-3.5-transcribe-live") only support streaming transcription
// over the Gemini Live API WebSocket (DoStream, TS doStream, in
// transcription_stream.go). DoTranscribe rejects a live model ID and
// DoStream rejects a non-live model ID, matching TS's cross-rejection.
type TranscriptionModel struct {
	prov    *Provider
	modelID string

	// finishGrace overrides defaultFinishGraceDuration for tests
	// (mirrors TS config._internal.finishGraceMs). Zero means "use the
	// default".
	finishGrace time.Duration
}

// NewTranscriptionModel creates a Gemini 3.5 Transcribe model.
func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{prov: p, modelID: modelID}
}

func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *TranscriptionModel) Provider() string {
	return m.prov.Name() + ".transcription"
}
func (m *TranscriptionModel) ModelID() string { return m.modelID }

// transcriptionModelOptions mirrors TS GoogleTranscriptionModelOptions
// (google-transcription-model-options.ts), shared by the unary and live
// variants.
type transcriptionModelOptions struct {
	LanguageCodes    []string
	CustomVocabulary []string
	WordTimestamp    *bool
	Diarization      *bool
	Mode             string // "SMART" | "VERBATIM"
}

// parseTranscriptionModelOptionsChecked mirrors TS parseProviderOptions with
// googleTranscriptionModelOptions: a present value of the wrong type (or a
// mode outside SMART/VERBATIM) fails with an InvalidArgumentError. Empty
// arrays stay non-nil because TS forwards `languageCodes: []`.
func parseTranscriptionModelOptionsChecked(providerOptions map[string]interface{}) (transcriptionModelOptions, error) {
	var out transcriptionModelOptions
	raw, ok := providerOptions["google"]
	if !ok || raw == nil {
		return out, nil
	}
	google, isMap := raw.(map[string]interface{})
	if !isMap {
		return out, invalidGoogleFilesOptions(fmt.Errorf("google: expected object, received %T", raw))
	}
	strList := func(key string) ([]string, error) {
		v, present := google[key]
		if !present || v == nil {
			return nil, nil
		}
		switch list := v.(type) {
		case []string:
			return append([]string{}, list...), nil
		case []interface{}:
			res := make([]string, 0, len(list))
			for i, item := range list {
				str, isStr := item.(string)
				if !isStr {
					return nil, invalidGoogleFilesOptions(fmt.Errorf("%s[%d]: expected string, received %T", key, i, item))
				}
				res = append(res, str)
			}
			return res, nil
		}
		return nil, invalidGoogleFilesOptions(fmt.Errorf("%s: expected array, received %T", key, v))
	}
	var err error
	if out.LanguageCodes, err = strList("languageCodes"); err != nil {
		return out, err
	}
	if out.CustomVocabulary, err = strList("customVocabulary"); err != nil {
		return out, err
	}
	for _, key := range []string{"wordTimestamp", "diarization"} {
		v, present := google[key]
		if !present || v == nil {
			continue
		}
		b, isBool := v.(bool)
		if !isBool {
			return out, invalidGoogleFilesOptions(fmt.Errorf("%s: expected boolean, received %T", key, v))
		}
		if key == "wordTimestamp" {
			out.WordTimestamp = &b
		} else {
			out.Diarization = &b
		}
	}
	if v, present := google["mode"]; present && v != nil {
		mode, isStr := v.(string)
		if !isStr || (mode != "SMART" && mode != "VERBATIM") {
			return out, invalidGoogleFilesOptions(fmt.Errorf("mode: invalid option %v, expected one of SMART, VERBATIM", v))
		}
		out.Mode = mode
	}
	return out, nil
}

// buildTranscriptionConfig builds the Interactions API transcription_config
// (snake_case wire shape) from provider options; returns nil when no options
// are set. Diarization and word timestamps are expressed inside the `mode`
// object, matching TS buildTranscriptionConfig exactly.
func buildTranscriptionConfig(opts transcriptionModelOptions) map[string]interface{} {
	config := map[string]interface{}{}
	if opts.LanguageCodes != nil {
		config["language_codes"] = opts.LanguageCodes
	}
	if opts.CustomVocabulary != nil {
		config["custom_vocabulary"] = opts.CustomVocabulary
	}
	wantsMode := opts.Mode != "" || (opts.Diarization != nil && *opts.Diarization) || (opts.WordTimestamp != nil && *opts.WordTimestamp)
	if wantsMode {
		modeType := opts.Mode
		if modeType == "" {
			modeType = "VERBATIM"
		}
		mode := map[string]interface{}{"type": strings.ToLower(modeType)}
		if opts.Diarization != nil && *opts.Diarization {
			mode["diarization_mode"] = "speaker"
		}
		if opts.WordTimestamp != nil && *opts.WordTimestamp {
			mode["timestamp_granularities"] = []string{"word"}
		}
		config["mode"] = mode
	}
	if len(config) == 0 {
		return nil
	}
	return config
}

// parseOffsetSeconds is TS Number.parseFloat for a Google duration offset such
// as "1s" or "9.400s": the longest leading decimal prefix, finite only.
func parseOffsetSeconds(offset string) (float64, bool) {
	m := leadingFloatRE.FindString(strings.TrimLeft(offset, " \t\r\n"))
	if m == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, false
	}
	return v, true
}

var leadingFloatRE = regexp.MustCompile(`^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?`)

type transcriptionWordAnnotation struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Speaker     string `json:"speaker,omitempty"`
	StartOffset string `json:"start_offset,omitempty"`
	EndOffset   string `json:"end_offset,omitempty"`
}

type transcriptionResponseContent struct {
	Type        string                        `json:"type"`
	Text        string                        `json:"text,omitempty"`
	Annotations []transcriptionWordAnnotation `json:"annotations,omitempty"`
}

type transcriptionResponseStep struct {
	Type    string                         `json:"type"`
	Content []transcriptionResponseContent `json:"content,omitempty"`
}

type transcriptionResponse struct {
	Status string                      `json:"status,omitempty"`
	Steps  []transcriptionResponseStep `json:"steps,omitempty"`
	Usage  map[string]interface{}      `json:"usage,omitempty"`
}

// DoTranscribe transcribes audio via the Interactions API
// (POST /interactions), matching TS GoogleTranscriptionModel.doGenerate.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if isLiveTranscriptionModelID(m.modelID) {
		return nil, &providererrors.InvalidArgumentError{
			Field: "modelId",
			Message: fmt.Sprintf(
				"Model '%s' only supports streaming transcription. Use ai.ExperimentalStreamTranscribe, or a unary model such as '%s'.",
				m.modelID, ModelGemini35Transcribe,
			),
		}
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}

	audioBase64 := opts.AudioBase64
	if audioBase64 == "" {
		audioBase64 = base64.StdEncoding.EncodeToString(opts.Audio)
	}

	transcriptionOpts, err := parseTranscriptionModelOptionsChecked(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	transcriptionConfig := buildTranscriptionConfig(transcriptionOpts)

	reqBody := map[string]interface{}{
		"model": m.modelID,
		"input": []map[string]interface{}{
			{
				"type":      "audio",
				"data":      audioBase64,
				"mime_type": opts.MimeType,
			},
		},
	}
	if transcriptionConfig != nil {
		reqBody["generation_config"] = map[string]interface{}{
			"transcription_config": transcriptionConfig,
		}
	}

	var response transcriptionResponse
	resp, err := m.prov.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/interactions",
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	var text strings.Builder
	var segments []types.TranscriptionTimestamp
	for _, step := range response.Steps {
		for _, content := range step.Content {
			if content.Type != "text" {
				continue
			}
			text.WriteString(content.Text)
			for _, annotation := range content.Annotations {
				if annotation.Type != "word_info" || annotation.Text == "" {
					continue
				}
				startSecond, ok1 := parseOffsetSeconds(annotation.StartOffset)
				endSecond, ok2 := parseOffsetSeconds(annotation.EndOffset)
				if !ok1 || !ok2 {
					continue
				}
				segments = append(segments, types.TranscriptionTimestamp{
					Text:  annotation.Text,
					Start: startSecond,
					End:   endSecond,
				})
			}
		}
	}

	var providerMetadata map[string]interface{}
	if len(response.Usage) > 0 {
		providerMetadata = map[string]interface{}{
			"google": map[string]interface{}{"usage": response.Usage},
		}
	}

	var providerUsage map[string]interface{}
	if len(response.Usage) > 0 {
		providerUsage = response.Usage
	}

	return &types.TranscriptionResult{
		Text:             text.String(),
		Segments:         segments,
		ProviderMetadata: providerMetadata,
		ProviderUsage:    providerUsage,
		Response: &types.ResponseMetadata{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      json.RawMessage(resp.Body),
		},
	}, nil
}

// handleError maps transport errors like TS googleFailedResponseHandler: a
// missing key stays a LoadAPIKeyError and HTTP failures keep status, headers
// and body for retry decisions.
func (m *TranscriptionModel) handleError(err error) error {
	var loadKeyErr *providererrors.LoadAPIKeyError
	if errors.As(err, &loadKeyErr) {
		return err
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return gemini.GoogleHTTPError(m.Provider(), statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}
