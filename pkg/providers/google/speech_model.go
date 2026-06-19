package google

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	defaultGeminiTTSVoice      = "Kore"
	defaultGeminiTTSSampleRate = 24000
)

// SpeechModelConfig contains provider-specific settings for Gemini TTS.
type SpeechModelConfig struct {
	ProviderName        string
	MetadataKey         string
	ProviderOptionsKeys []string
	GeneratePath        func(modelID string) string
	Client              *internalhttp.Client
}

// SpeechModel implements Gemini text-to-speech for Google and Vertex.
type SpeechModel struct {
	modelID string
	cfg     SpeechModelConfig
}

// NewSpeechModel creates a Google Generative AI Gemini TTS model.
func NewSpeechModel(p *Provider, modelID string) *SpeechModel {
	return NewSpeechModelWithConfig(modelID, SpeechModelConfig{
		ProviderName:        p.Name() + ".speech",
		MetadataKey:         "google",
		ProviderOptionsKeys: []string{"google"},
		GeneratePath: func(id string) string {
			return fmt.Sprintf("/models/%s:generateContent", id)
		},
		Client: p.client,
	})
}

// NewSpeechModelWithConfig creates a Gemini TTS model with provider-specific
// routing/auth configuration.
func NewSpeechModelWithConfig(modelID string, cfg SpeechModelConfig) *SpeechModel {
	return &SpeechModel{modelID: modelID, cfg: cfg}
}

func (m *SpeechModel) SpecificationVersion() string { return "v4" }
func (m *SpeechModel) Provider() string             { return m.cfg.ProviderName }
func (m *SpeechModel) ModelID() string              { return m.modelID }

// DoGenerate synthesizes speech using Gemini :generateContent with AUDIO output.
func (m *SpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	requestBody, outputFormat, warnings, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}
	currentDate := time.Now()

	var response googleSpeechResponse
	httpResp, err := m.cfg.Client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    m.cfg.GeneratePath(m.modelID),
		Body:    requestBody,
		Headers: opts.Headers,
	}, &response)
	if err != nil {
		return nil, m.handleError(err)
	}

	base64Audio, mimeType := firstGoogleSpeechAudio(response)
	pcm := []byte{}
	if base64Audio != "" {
		pcm, err = base64.StdEncoding.DecodeString(base64Audio)
		if err != nil {
			return nil, fmt.Errorf("failed to decode Gemini TTS audio: %w", err)
		}
	}
	sampleRate := parseGeminiSampleRate(mimeType)
	if sampleRate == 0 {
		sampleRate = defaultGeminiTTSSampleRate
	}
	audio := pcm
	if outputFormat != "pcm" && len(pcm) > 0 {
		audio = addGeminiWAVHeader(pcm, sampleRate)
	} else if outputFormat == "pcm" && len(pcm) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Returning raw PCM audio (signed 16-bit little-endian, mono, %d Hz). These bytes have no container header and are not directly playable; see providerMetadata.%s for the sample rate and mime type.", sampleRate, m.cfg.MetadataKey),
		})
	}

	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Gemini TTS request metadata: %w", err)
	}
	responseBody, err := parseGoogleSpeechRawBody(httpResp.Body)
	if err != nil {
		return nil, err
	}

	return &types.SpeechResult{
		Audio:    audio,
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			m.cfg.MetadataKey: map[string]interface{}{
				"sampleRate": sampleRate,
				"mimeType":   nullableString(mimeType),
			},
		},
		Request: &types.StepRequest{
			Body: string(requestBytes),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      responseBody,
		},
	}, nil
}

func parseGoogleSpeechRawBody(body []byte) (interface{}, error) {
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini TTS response metadata: %w", err)
	}
	return raw, nil
}

func (m *SpeechModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
				Status  string `json:"status"`
			} `json:"error"`
		}
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil {
			if payload.Error.Message != "" {
				message = payload.Error.Message
			}
			code = payload.Error.Status
		}
		providerErr := providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		return providerErr
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, string, []types.Warning, error) {
	warnings := []types.Warning{}
	googleOpts, err := m.extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, "", nil, err
	}

	voice := opts.Voice
	if voice == "" {
		voice = defaultGeminiTTSVoice
	}
	var speechConfig map[string]interface{}
	if googleOpts.MultiSpeakerVoiceConfig != nil {
		speechConfig = map[string]interface{}{"multiSpeakerVoiceConfig": googleOpts.MultiSpeakerVoiceConfig}
	} else {
		speechConfig = map[string]interface{}{
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{"voiceName": voice},
			},
		}
	}

	promptText := opts.Text
	if opts.Instructions != "" {
		if googleOpts.MultiSpeakerVoiceConfig != nil {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "instructions",
				Details: "Google Gemini TTS ignores `instructions` when `multiSpeakerVoiceConfig` is set, because prepending them would break multi-speaker transcript parsing.",
			})
		} else {
			promptText = opts.Instructions + ": " + opts.Text
		}
	}
	if opts.Speed != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "speed",
			Details: "Google Gemini TTS models do not support the `speed` option. It was ignored.",
		})
	}
	if opts.Language != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "language",
			Details: "Google Gemini TTS models do not support the `language` option. Language is detected automatically from the input text.",
		})
	}

	outputFormat := "wav"
	switch opts.OutputFormat {
	case "", "wav":
	case "pcm":
		outputFormat = "pcm"
	default:
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unsupported output format: %s. Using wav instead.", opts.OutputFormat),
		})
	}

	return map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{"text": promptText},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"AUDIO"},
			"speechConfig":       speechConfig,
		},
	}, outputFormat, warnings, nil
}

func (m *SpeechModel) extractProviderOptions(providerOptions map[string]interface{}) (GoogleSpeechModelOptions, error) {
	if providerOptions == nil {
		return GoogleSpeechModelOptions{}, nil
	}
	for _, key := range m.cfg.ProviderOptionsKeys {
		raw, ok := providerOptions[key]
		if !ok || raw == nil {
			continue
		}
		opts, err := parseGoogleSpeechOptions(raw)
		if err != nil {
			return GoogleSpeechModelOptions{}, &providererrors.InvalidArgumentError{
				Field:   "providerOptions",
				Message: fmt.Sprintf("invalid %s provider options", key),
				Cause:   err,
			}
		}
		return opts, nil
	}
	return GoogleSpeechModelOptions{}, nil
}

func parseGoogleSpeechOptions(raw interface{}) (GoogleSpeechModelOptions, error) {
	switch v := raw.(type) {
	case GoogleSpeechModelOptions:
		return validateGoogleSpeechModelOptions(v)
	case *GoogleSpeechModelOptions:
		if v == nil {
			return GoogleSpeechModelOptions{}, nil
		}
		return validateGoogleSpeechModelOptions(*v)
	case map[string]interface{}:
		out := GoogleSpeechModelOptions{}
		if rawConfig, ok := v["multiSpeakerVoiceConfig"]; ok {
			m, ok := rawConfig.(map[string]interface{})
			if !ok {
				return GoogleSpeechModelOptions{}, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig: expected object")
			}
			normalized, err := normalizeGoogleMultiSpeakerVoiceConfig(m)
			if err != nil {
				return GoogleSpeechModelOptions{}, err
			}
			out.MultiSpeakerVoiceConfig = normalized
		}
		return out, nil
	default:
		return GoogleSpeechModelOptions{}, fmt.Errorf("invalid google speech provider options: expected object")
	}
}

func validateGoogleSpeechModelOptions(opts GoogleSpeechModelOptions) (GoogleSpeechModelOptions, error) {
	if opts.MultiSpeakerVoiceConfig != nil {
		normalized, err := normalizeGoogleMultiSpeakerVoiceConfig(opts.MultiSpeakerVoiceConfig)
		if err != nil {
			return opts, err
		}
		opts.MultiSpeakerVoiceConfig = normalized
	}
	return opts, nil
}

func normalizeGoogleMultiSpeakerVoiceConfig(config map[string]interface{}) (map[string]interface{}, error) {
	rawSpeakers, ok := config["speakerVoiceConfigs"]
	if !ok {
		return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig: missing speakerVoiceConfigs")
	}
	var speakers []map[string]interface{}
	switch v := rawSpeakers.(type) {
	case []interface{}:
		speakers = make([]map[string]interface{}, len(v))
		for i, raw := range v {
			speakerConfig, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d]: expected object", i)
			}
			speakers[i] = speakerConfig
		}
	case []map[string]interface{}:
		speakers = v
	default:
		return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs: expected array")
	}
	normalizedSpeakers := make([]interface{}, 0, len(speakers))
	for i, speakerConfig := range speakers {
		speaker, ok := speakerConfig["speaker"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d].speaker: expected string", i)
		}
		voiceConfig, ok := speakerConfig["voiceConfig"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d].voiceConfig: expected object", i)
		}
		prebuilt, ok := voiceConfig["prebuiltVoiceConfig"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d].voiceConfig.prebuiltVoiceConfig: expected object", i)
		}
		voiceName, ok := prebuilt["voiceName"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d].voiceConfig.prebuiltVoiceConfig.voiceName: expected string", i)
		}
		normalizedSpeakers = append(normalizedSpeakers, map[string]interface{}{
			"speaker": speaker,
			"voiceConfig": map[string]interface{}{
				"prebuiltVoiceConfig": map[string]interface{}{
					"voiceName": voiceName,
				},
			},
		})
	}
	return map[string]interface{}{"speakerVoiceConfigs": normalizedSpeakers}, nil
}

type googleSpeechResponse struct {
	Candidates []struct {
		Content *struct {
			Parts []struct {
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func firstGoogleSpeechAudio(response googleSpeechResponse) (string, string) {
	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil && part.InlineData.Data != "" {
				return part.InlineData.Data, part.InlineData.MimeType
			}
		}
	}
	return "", ""
}

var geminiRateRE = regexp.MustCompile(`rate=(\d+)`)

func parseGeminiSampleRate(mimeType string) int {
	match := geminiRateRE.FindStringSubmatch(mimeType)
	if len(match) != 2 {
		return 0
	}
	rate, _ := strconv.Atoi(match[1])
	return rate
}

func addGeminiWAVHeader(pcm []byte, sampleRate int) []byte {
	const (
		numChannels   = 1
		bitsPerSample = 16
		headerSize    = 44
	)
	blockAlign := numChannels * bitsPerSample / 8
	byteRate := sampleRate * blockAlign
	dataSize := len(pcm)
	out := make([]byte, headerSize+dataSize)
	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+dataSize))
	copy(out[8:], "WAVE")
	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], numChannels)
	binary.LittleEndian.PutUint32(out[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:], bitsPerSample)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(dataSize))
	copy(out[headerSize:], pcm)
	return out
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
