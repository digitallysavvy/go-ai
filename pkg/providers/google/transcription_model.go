package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// isLiveTranscriptionModelID reports whether modelID names a Gemini Live
// transcription variant (e.g. "gemini-3.5-transcribe-live"), which only
// supports streaming transcription (TS isLiveTranscriptionModelId).
func isLiveTranscriptionModelID(modelID string) bool {
	return strings.Contains(modelID, "-live")
}

// TranscriptionModel implements unary Gemini 3.5 Transcribe
// (TS GoogleTranscriptionModel.doGenerate): audio is transcribed through the
// Interactions API (https://ai.google.dev/gemini-api/docs/transcribe).
//
// The "-live" model variants only support streaming transcription over a
// WebSocket (TS doStream). The Go SDK's provider.TranscriptionModel
// interface has no streaming counterpart (no DoStream), so that half of TS
// GoogleTranscriptionModel is not implemented here; DoTranscribe rejects a
// live model ID with an explanatory error instead of silently doing the
// wrong thing.
type TranscriptionModel struct {
	prov    *Provider
	modelID string
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

func parseTranscriptionModelOptions(providerOptions map[string]interface{}) transcriptionModelOptions {
	var out transcriptionModelOptions
	google, _ := providerOptions["google"].(map[string]interface{})
	if google == nil {
		return out
	}
	if v, ok := google["languageCodes"].([]interface{}); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				out.LanguageCodes = append(out.LanguageCodes, str)
			}
		}
	} else if v, ok := google["languageCodes"].([]string); ok {
		out.LanguageCodes = v
	}
	if v, ok := google["customVocabulary"].([]interface{}); ok {
		for _, s := range v {
			if str, ok := s.(string); ok {
				out.CustomVocabulary = append(out.CustomVocabulary, str)
			}
		}
	} else if v, ok := google["customVocabulary"].([]string); ok {
		out.CustomVocabulary = v
	}
	if v, ok := google["wordTimestamp"].(bool); ok {
		out.WordTimestamp = &v
	}
	if v, ok := google["diarization"].(bool); ok {
		out.Diarization = &v
	}
	if v, ok := google["mode"].(string); ok {
		out.Mode = v
	}
	return out
}

// buildTranscriptionConfig builds the Interactions API transcription_config
// (snake_case wire shape) from provider options; returns nil when no options
// are set. Diarization and word timestamps are expressed inside the `mode`
// object, matching TS buildTranscriptionConfig exactly.
func buildTranscriptionConfig(opts transcriptionModelOptions) map[string]interface{} {
	config := map[string]interface{}{}
	if len(opts.LanguageCodes) > 0 {
		config["language_codes"] = opts.LanguageCodes
	}
	if len(opts.CustomVocabulary) > 0 {
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

// parseOffsetSeconds parses a Google duration offset such as "1s" or
// "9.400s" to seconds.
func parseOffsetSeconds(offset string) (float64, bool) {
	if offset == "" {
		return 0, false
	}
	trimmed := strings.TrimSuffix(offset, "s")
	v, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

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
		return nil, fmt.Errorf(
			"model '%s' only supports streaming transcription, which the Go SDK's TranscriptionModel does not yet expose (no DoStream); use a unary model such as '%s'",
			m.modelID, ModelGemini35Transcribe,
		)
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}

	audioBase64 := opts.AudioBase64
	if audioBase64 == "" {
		audioBase64 = base64.StdEncoding.EncodeToString(opts.Audio)
	}

	transcriptionOpts := parseTranscriptionModelOptions(opts.ProviderOptions)
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
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", "failed to transcribe audio: "+err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, providererrors.NewProviderError(m.Provider(), resp.StatusCode, "",
			fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(resp.Body)), nil)
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

	return &types.TranscriptionResult{
		Text:             text.String(),
		Segments:         segments,
		ProviderMetadata: providerMetadata,
		Response: &types.ResponseMetadata{
			ModelID: m.modelID,
			Headers: providerutils.ExtractHeaders(resp.Headers),
			Body:    json.RawMessage(resp.Body),
		},
	}, nil
}
