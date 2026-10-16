package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Gemini transcription model IDs on Vertex AI, mirroring
// pkg/providers/google's ModelGemini35Transcribe/ModelGemini35TranscribeLive
// (same model family, served through Vertex instead of the Developer API).
const (
	ModelGemini35Transcribe     = "gemini-3.5-transcribe"
	ModelGemini35TranscribeLive = "gemini-3.5-transcribe-live"
)

// isLiveGeminiTranscriptionModelID reports whether modelID names a live
// (streaming-only) Gemini transcription variant, mirroring TS
// isLiveTranscriptionModelId (google-vertex-gemini-transcription-model.ts).
func isLiveGeminiTranscriptionModelID(modelID string) bool {
	return strings.Contains(modelID, "-live")
}

// GeminiTranscriptionModel implements provider.TranscriptionModel (unary,
// this file) and provider.TranscriptionStreamer (live, see
// gemini_transcription_stream.go) for Gemini-family transcription models on
// Vertex AI. Unary variants transcribe via the same generateContent endpoint
// the chat models use; "-live" variants only support streaming over the
// Vertex Live API WebSocket. Mirrors TS GoogleVertexGeminiTranscriptionModel.
type GeminiTranscriptionModel struct {
	provider *Provider
	modelID  string

	// finishGrace overrides defaultGeminiFinishGraceDuration for tests
	// (mirrors TS config._internal.finishGraceMs). Zero means "use the
	// default".
	finishGrace time.Duration
}

// NewGeminiTranscriptionModel creates a Google Vertex AI Gemini transcription model.
func NewGeminiTranscriptionModel(p *Provider, modelID string) *GeminiTranscriptionModel {
	return &GeminiTranscriptionModel{provider: p, modelID: modelID}
}

func (m *GeminiTranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *GeminiTranscriptionModel) Provider() string             { return "google.vertex.transcription" }
func (m *GeminiTranscriptionModel) ModelID() string              { return m.modelID }

// vertexGeminiTranscriptionOptions mirrors
// googleVertexGeminiTranscriptionModelOptions (AudioTranscriptionConfig),
// shared by the unary (this file) and live (gemini_transcription_stream.go)
// variants.
type vertexGeminiTranscriptionOptions struct {
	LanguageCodes    []string
	CustomVocabulary []string
	WordTimestamp    *bool
	Diarization      *bool
	Mode             string // "SMART" | "VERBATIM"
}

// parseVertexGeminiTranscriptionOptions checks providerOptions under
// "googleVertex", then "vertex", then "google" -- matches TS's
// GoogleVertexGeminiTranscriptionModel.parseOptions loop order.
func parseVertexGeminiTranscriptionOptions(providerOptions map[string]interface{}) vertexGeminiTranscriptionOptions {
	out, _ := parseVertexGeminiTranscriptionOptionsChecked(providerOptions)
	return out
}

// parseVertexGeminiTranscriptionOptionsChecked is the zod-validating form: a
// present value of the wrong type (or a mode outside SMART/VERBATIM) fails
// with an InvalidArgumentError. Empty arrays stay non-nil because TS forwards
// `languageCodes: []`.
func parseVertexGeminiTranscriptionOptionsChecked(providerOptions map[string]interface{}) (vertexGeminiTranscriptionOptions, error) {
	invalid := func(cause error) error {
		return &providererrors.InvalidArgumentError{Field: "providerOptions", Message: "invalid google provider options", Cause: cause}
	}
	for _, key := range []string{"googleVertex", "vertex", "google"} {
		rawAny, present := providerOptions[key]
		if !present || rawAny == nil {
			continue
		}
		raw, ok := rawAny.(map[string]interface{})
		if !ok {
			return vertexGeminiTranscriptionOptions{}, invalid(fmt.Errorf("%s: expected object, received %T", key, rawAny))
		}
		var out vertexGeminiTranscriptionOptions
		strList := func(name string) ([]string, error) {
			v, has := raw[name]
			if !has || v == nil {
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
						return nil, invalid(fmt.Errorf("%s[%d]: expected string, received %T", name, i, item))
					}
					res = append(res, str)
				}
				return res, nil
			}
			return nil, invalid(fmt.Errorf("%s: expected array, received %T", name, v))
		}
		var err error
		if out.LanguageCodes, err = strList("languageCodes"); err != nil {
			return out, err
		}
		if out.CustomVocabulary, err = strList("customVocabulary"); err != nil {
			return out, err
		}
		for _, name := range []string{"wordTimestamp", "diarization"} {
			v, has := raw[name]
			if !has || v == nil {
				continue
			}
			b, isBool := v.(bool)
			if !isBool {
				return out, invalid(fmt.Errorf("%s: expected boolean, received %T", name, v))
			}
			if name == "wordTimestamp" {
				out.WordTimestamp = &b
			} else {
				out.Diarization = &b
			}
		}
		if v, has := raw["mode"]; has && v != nil {
			mode, isStr := v.(string)
			if !isStr || (mode != "SMART" && mode != "VERBATIM") {
				return out, invalid(fmt.Errorf("mode: invalid option %v, expected one of SMART, VERBATIM", v))
			}
			out.Mode = mode
		}
		return out, nil
	}
	return vertexGeminiTranscriptionOptions{}, nil
}

// buildVertexAudioTranscriptionConfig builds Google's AudioTranscriptionConfig
// (camelCase wire shape, used by both the unary generateContent request and
// the Live API `inputAudioTranscription` setup) from provider options;
// returns nil when no options are set. Mirrors TS
// buildAudioTranscriptionConfig exactly.
func buildVertexAudioTranscriptionConfig(opts vertexGeminiTranscriptionOptions) map[string]interface{} {
	config := map[string]interface{}{}
	if opts.LanguageCodes != nil {
		config["languageCodes"] = opts.LanguageCodes
	}
	if opts.CustomVocabulary != nil {
		config["customVocabulary"] = opts.CustomVocabulary
	}
	if opts.WordTimestamp != nil {
		config["wordTimestamp"] = *opts.WordTimestamp
	}
	if opts.Diarization != nil {
		config["diarization"] = *opts.Diarization
	}
	if opts.Mode != "" {
		config["mode"] = opts.Mode
	}
	if len(config) == 0 {
		return nil
	}
	return config
}

type vertexGeminiTranscriptionWord struct {
	Word        string `json:"word,omitempty"`
	StartOffset string `json:"startOffset,omitempty"`
	EndOffset   string `json:"endOffset,omitempty"`
}

type vertexGeminiAudioTranscription struct {
	Text         string                          `json:"text,omitempty"`
	LanguageCode string                          `json:"languageCode,omitempty"`
	SpeakerLabel string                          `json:"speakerLabel,omitempty"`
	Words        []vertexGeminiTranscriptionWord `json:"words,omitempty"`
}

type vertexGeminiTranscriptionResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text               string                          `json:"text,omitempty"`
				AudioTranscription *vertexGeminiAudioTranscription `json:"audioTranscription,omitempty"`
			} `json:"parts,omitempty"`
		} `json:"content,omitempty"`
	} `json:"candidates,omitempty"`
	UsageMetadata map[string]interface{} `json:"usageMetadata,omitempty"`
}

// DoTranscribe transcribes audio via Vertex generateContent, matching TS
// GoogleVertexGeminiTranscriptionModel.doGenerate. Live model IDs are
// rejected here (they only support streaming transcription, via DoStream in
// gemini_transcription_stream.go), matching TS's cross-rejection between
// doGenerate and doStream.
func (m *GeminiTranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if isLiveGeminiTranscriptionModelID(m.modelID) {
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

	transcriptionOpts, err := parseVertexGeminiTranscriptionOptionsChecked(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	audioConfig := buildVertexAudioTranscriptionConfig(transcriptionOpts)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]interface{}{
					{
						"inlineData": map[string]interface{}{
							"mimeType": opts.MimeType,
							"data":     audioBase64,
						},
					},
				},
			},
		},
	}
	if audioConfig != nil {
		reqBody["generationConfig"] = map[string]interface{}{
			"audioTranscriptionConfig": audioConfig,
		}
	}

	var response vertexGeminiTranscriptionResponse
	resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    fmt.Sprintf("/%s:generateContent", gemini.GetModelPath(m.modelID)),
		Body:    reqBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}
	if resp.StatusCode >= 400 {
		return nil, gemini.GoogleHTTPError(m.Provider(), resp.StatusCode, resp.Headers, resp.Body, nil)
	}

	var plainText, transcriptionText strings.Builder
	var segments []types.TranscriptionTimestamp
	var language string
	if len(response.Candidates) > 0 {
		for _, part := range response.Candidates[0].Content.Parts {
			plainText.WriteString(part.Text)
			if part.AudioTranscription != nil {
				transcriptionText.WriteString(part.AudioTranscription.Text)
				if language == "" {
					language = part.AudioTranscription.LanguageCode
				}
				for _, word := range part.AudioTranscription.Words {
					startSecond, ok1 := parseGoogleDuration(word.StartOffset)
					endSecond, ok2 := parseGoogleDuration(word.EndOffset)
					if word.Word == "" || !ok1 || !ok2 {
						continue
					}
					segments = append(segments, types.TranscriptionTimestamp{
						Text:  word.Word,
						Start: startSecond,
						End:   endSecond,
					})
				}
			}
		}
	}

	// Prefer the plain generated-text parts; fall back to the
	// audioTranscription text when the model only returned transcription
	// parts -- matches TS `plainText !== '' ? plainText : transcriptionText`.
	text := plainText.String()
	if text == "" {
		text = transcriptionText.String()
	}

	var providerMetadata map[string]interface{}
	if len(response.UsageMetadata) > 0 {
		providerMetadata = map[string]interface{}{
			"google": map[string]interface{}{"usageMetadata": response.UsageMetadata},
		}
	}

	var providerUsage map[string]interface{}
	if len(response.UsageMetadata) > 0 {
		providerUsage = response.UsageMetadata
	}

	return &types.TranscriptionResult{
		Text:             text,
		Segments:         segments,
		Timestamps:       segments,
		Language:         language,
		Warnings:         []types.Warning{},
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

// handleError maps transport errors like TS googleVertexFailedResponseHandler:
// a missing key/credential error passes through and HTTP failures keep
// status, headers and body for retry decisions.
func (m *GeminiTranscriptionModel) handleError(err error) error {
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
