package hume

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// defaultVoiceID is Hume's default built-in voice.
const defaultVoiceID = "d8ab67c6-953d-4bd8-9370-8fa53a0f1453"

var humeOutputFormats = map[string]bool{"mp3": true, "pcm": true, "wav": true}

// SpeechModel implements provider.SpeechModel for Hume (POST /v0/tts/file).
// Hume has no selectable model ID; ModelID always returns "".
type SpeechModel struct {
	provider *Provider
}

// SpecificationVersion returns the specification version.
func (m *SpeechModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *SpeechModel) Provider() string { return "hume.speech" }

// ModelID returns the model ID. Hume has no selectable model, so this is
// always the empty string, matching the TypeScript SDK.
func (m *SpeechModel) ModelID() string { return "" }

// DoGenerate performs text-to-speech synthesis.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()

	requestBody, warnings := m.buildRequestBody(opts)
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/v0/tts/file",
		Body:    requestBytes,
		Headers: opts.Headers,
	})
	if err != nil {
		return nil, handleError(err)
	}
	if resp.StatusCode >= 400 {
		return nil, handleError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
	}

	return &types.SpeechResult{
		Audio:    resp.Body,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   "",
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      resp.Body,
		},
	}, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}
	humeOpts := extractSpeechModelOptions(opts.ProviderOptions)

	voice := opts.Voice
	if voice == "" {
		voice = defaultVoiceID
	}

	utterance := map[string]interface{}{
		"text": opts.Text,
		"voice": map[string]interface{}{
			"id":       voice,
			"provider": "HUME_AI",
		},
	}
	if opts.Speed != nil {
		utterance["speed"] = *opts.Speed
	}
	if opts.Instructions != "" {
		utterance["description"] = opts.Instructions
	}

	body := map[string]interface{}{
		"utterances": []interface{}{utterance},
		"format":     map[string]interface{}{"type": "mp3"},
	}

	outputFormat := opts.OutputFormat
	if outputFormat != "" {
		if humeOutputFormats[outputFormat] {
			body["format"] = map[string]interface{}{"type": outputFormat}
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: "Unsupported output format: " + outputFormat + ". Using mp3 instead.",
			})
		}
	}

	if humeOpts != nil && humeOpts.Context != nil {
		if humeOpts.Context.HasGenerationID() {
			body["context"] = map[string]interface{}{"generation_id": humeOpts.Context.GenerationID}
		} else if len(humeOpts.Context.Utterances) > 0 {
			utterances := make([]map[string]interface{}, 0, len(humeOpts.Context.Utterances))
			for _, u := range humeOpts.Context.Utterances {
				entry := map[string]interface{}{"text": u.Text}
				if u.Description != "" {
					entry["description"] = u.Description
				}
				if u.Speed != nil {
					entry["speed"] = *u.Speed
				}
				if u.TrailingSilence != nil {
					entry["trailing_silence"] = *u.TrailingSilence
				}
				if u.Voice != nil {
					voiceEntry := map[string]interface{}{}
					if u.Voice.ID != "" {
						voiceEntry["id"] = u.Voice.ID
					}
					if u.Voice.Name != "" {
						voiceEntry["name"] = u.Voice.Name
					}
					if u.Voice.Provider != "" {
						voiceEntry["provider"] = u.Voice.Provider
					}
					entry["voice"] = voiceEntry
				}
				utterances = append(utterances, entry)
			}
			body["context"] = map[string]interface{}{"utterances": utterances}
		}
	}

	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: "Hume speech models do not support language selection. Language parameter \"" + opts.Language + "\" was ignored.",
		})
	}

	return body, warnings
}
