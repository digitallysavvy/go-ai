package lmnt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// SpeechModel represents an LMNT speech synthesis model
type SpeechModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version
func (m *SpeechModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *SpeechModel) Provider() string {
	return "lmnt"
}

// ModelID returns the model ID
func (m *SpeechModel) ModelID() string {
	return m.modelID
}

// SpeechModelOptions contains LMNT-specific speech provider options.
type SpeechModelOptions struct {
	Conversational *bool
	Length         *float64
	Seed           *int
	Speed          *float64
	Temperature    *float64
	TopP           *float64
	SampleRate     *int
}

// DoGenerate synthesizes speech from text
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	reqBody, warnings := m.buildRequestBody(opts)

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	currentDate := time.Now()

	// Create HTTP request
	url := fmt.Sprintf("%s/v1/ai/speech/bytes", m.provider.config.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", m.provider.config.APIKey)
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LAPI request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Read audio data
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Build result
	result := &types.SpeechResult{
		Audio:    audioData,
		Warnings: warnings,
		Request: &types.StepRequest{
			Body: string(jsonData),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Header),
			Body:      audioData,
		},
	}

	return result, nil
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, []types.Warning) {
	voice := opts.Voice
	if voice == "" {
		voice = "ava"
	}
	body := map[string]interface{}{
		"model":           m.modelID,
		"text":            opts.Text,
		"voice":           voice,
		"response_format": "mp3",
	}

	warnings := make([]types.Warning, 0)
	if opts.OutputFormat != "" {
		switch opts.OutputFormat {
		case "mp3", "aac", "mulaw", "raw", "wav":
			body["response_format"] = opts.OutputFormat
		default:
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: fmt.Sprintf("Unsupported output format: %s. Using mp3 instead.", opts.OutputFormat),
			})
		}
	}
	if opts.Speed != nil {
		body["speed"] = *opts.Speed
	}
	if lmntOptions, ok := extractSpeechModelOptions(opts.ProviderOptions); ok {
		addSpeechModelOptions(body, lmntOptions)
	}
	if opts.Language != "" {
		body["language"] = opts.Language
	}
	return body, warnings
}

func addSpeechModelOptions(body map[string]interface{}, opts SpeechModelOptions) {
	conversational := false
	if opts.Conversational != nil {
		conversational = *opts.Conversational
	}
	body["conversational"] = conversational

	if opts.Length != nil {
		body["length"] = *opts.Length
	}
	if opts.Seed != nil {
		body["seed"] = *opts.Seed
	}

	speed := 1.0
	if opts.Speed != nil {
		speed = *opts.Speed
	}
	body["speed"] = speed

	temperature := 1.0
	if opts.Temperature != nil {
		temperature = *opts.Temperature
	}
	body["temperature"] = temperature

	topP := 1.0
	if opts.TopP != nil {
		topP = *opts.TopP
	}
	body["top_p"] = topP

	sampleRate := 24000
	if opts.SampleRate != nil {
		sampleRate = *opts.SampleRate
	}
	body["sample_rate"] = sampleRate
}

func extractSpeechModelOptions(providerOptions map[string]interface{}) (SpeechModelOptions, bool) {
	if providerOptions == nil {
		return SpeechModelOptions{}, false
	}
	raw, ok := providerOptions["lmnt"]
	if !ok {
		return SpeechModelOptions{}, false
	}
	switch value := raw.(type) {
	case SpeechModelOptions:
		return value, true
	case *SpeechModelOptions:
		if value == nil {
			return SpeechModelOptions{}, true
		}
		return *value, true
	case map[string]interface{}:
		return parseSpeechModelOptionsMap(value), true
	default:
		return SpeechModelOptions{}, true
	}
}

func parseSpeechModelOptionsMap(raw map[string]interface{}) SpeechModelOptions {
	var opts SpeechModelOptions
	if v, ok := raw["conversational"].(bool); ok {
		opts.Conversational = &v
	}
	if v, ok := numberAsFloat(raw["length"]); ok {
		opts.Length = &v
	}
	if v, ok := numberAsInt(raw["seed"]); ok {
		opts.Seed = &v
	}
	if v, ok := numberAsFloat(raw["speed"]); ok {
		opts.Speed = &v
	}
	if v, ok := numberAsFloat(raw["temperature"]); ok {
		opts.Temperature = &v
	}
	if v, ok := numberAsFloat(raw["topP"]); ok {
		opts.TopP = &v
	}
	if v, ok := numberAsInt(raw["sampleRate"]); ok {
		opts.SampleRate = &v
	}
	return opts
}

func numberAsFloat(value interface{}) (float64, bool) {
	switch n := value.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

func numberAsInt(value interface{}) (int, bool) {
	switch n := value.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
