package google

import (
	"context"
	"encoding/base64"
	"encoding/binary"
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

	// Kind is an optional discriminator embedded in the serialized model's
	// Config["kind"] field, distinguishing this Gemini TTS model from a
	// sibling model type that shares the same Provider() tag. Google Vertex
	// tags both Chirp 3: HD voices (googlevertex.CloudTTSSpeechModel) and
	// Gemini TTS (this shared type) "google.vertex.speech" -- unlike TS,
	// where Vertex builds Gemini TTS by importing @ai-sdk/google's
	// GoogleSpeechModel class directly (so both classes still carry
	// distinct constructors that a workflow runtime -- external to this
	// SDK -- could distinguish at the object level). Go's registry
	// dispatches purely on the Provider() string, so the "kind" field lets
	// the Vertex deserializer rebuild the right Go type. Left empty for
	// Google's own "google.speech" tag, which is unambiguous (Google has
	// no Chirp speech model).
	Kind string

	// SerializableConfig returns a JSON-friendly copy of the constructing
	// provider's Config struct (credentials such as APIKey/AccessToken
	// stripped, matching provider.SerializableConfig everywhere else in
	// this SDK), used for workflow serialization. Set by the constructing
	// provider (NewSpeechModel here, or googlevertex.Provider.SpeechModel);
	// nil when a SpeechModel is built directly via
	// NewSpeechModelWithConfig without a provider back-reference, in which
	// case Serialize() returns a config containing only "kind" (if set).
	SerializableConfig func() map[string]interface{}
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
		SerializableConfig: func() map[string]interface{} {
			return provider.SerializableConfig(p.config)
		},
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
	requestBody, outputFormat, usesStructured, warnings, err := m.buildRequestBody(opts)
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
	// Older models return raw PCM, which needs a container for the default WAV
	// output. Gemini 3.8 returns WAV itself; another header would corrupt it.
	isPCM := geminiPCMMimeRE.MatchString(mimeType) || (mimeType == "" && !usesStructured)
	audio := pcm
	if outputFormat == "AUDIO_WAV" && isPCM && len(pcm) > 0 {
		audio = addGeminiWAVHeader(pcm, sampleRate)
	}
	if outputFormat == "AUDIO_L16" && len(pcm) > 0 && !usesStructured {
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
		Usage:    response.UsageMetadata,
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
		return gemini.GoogleHTTPError(m.Provider(), statusErr.StatusCode, statusErr.Headers, statusErr.Body, err)
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}

var geminiPCMMimeRE = regexp.MustCompile(`(?i)^audio/(?:l16|pcm)(?:;|$)`)

// buildRequestBody ports TS GoogleSpeechModel.getArgs: structured speech
// (turns, speechMetadata, response formats) for Gemini 3.8 and later, prompt
// based directions for the 2.5 / 3.1 families. It returns the request body,
// the API output format enum (AUDIO_WAV, AUDIO_L16, ...), whether the model
// uses structured speech, and any warnings.
func (m *SpeechModel) buildRequestBody(opts *provider.SpeechGenerateOptions) (map[string]interface{}, string, bool, []types.Warning, error) {
	warnings := []types.Warning{}
	googleOpts, err := m.extractProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, "", false, nil, err
	}

	voice := opts.Voice
	if voice == "" {
		voice = defaultGeminiTTSVoice
	}
	// Older Gemini families require prompt-based directions. Newer and custom
	// model IDs default to structured speech.
	usesStructured := !strings.HasPrefix(m.modelID, "gemini-2.5-") && !strings.HasPrefix(m.modelID, "gemini-3.1-")

	if strings.HasPrefix(voice, "voice_") || strings.HasPrefix(voice, "voicekey_") {
		return nil, "", false, nil, &providererrors.InvalidArgumentError{
			Field:   "voice",
			Message: "Custom voices are not supported. Use a prebuilt voice instead.",
		}
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
	if opts.Instructions != "" && !usesStructured {
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

	parts := []interface{}{map[string]interface{}{"text": promptText}}
	if usesStructured {
		if len(googleOpts.Turns) > 0 && googleOpts.SpeechMetadata != nil {
			return nil, "", false, nil, &providererrors.InvalidArgumentError{
				Field:   "providerOptions",
				Message: "Set speechMetadata on each turn when using turns.",
			}
		}
		if len(googleOpts.Turns) > 0 && opts.Text != "" {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "text",
				Details: "Google TTS turns replace the top-level text.",
			})
		}
		turns := googleOpts.Turns
		if len(turns) == 0 {
			turns = []GoogleSpeechTurn{{Text: opts.Text, SpeechMetadata: googleOpts.SpeechMetadata}}
		}
		parts = parts[:0]
		for _, turn := range turns {
			var style *string
			speaker := ""
			if turn.SpeechMetadata != nil {
				style = turn.SpeechMetadata.Style
				speaker = turn.SpeechMetadata.Speaker
			}
			if style == nil && opts.Instructions != "" {
				instructions := opts.Instructions
				style = &instructions
			}
			if googleOpts.MultiSpeakerVoiceConfig != nil && !googleSpeechHasSpeaker(googleOpts.MultiSpeakerVoiceConfig, speaker) {
				return nil, "", false, nil, &providererrors.InvalidArgumentError{
					Field:   "speechMetadata.speaker",
					Message: "Every multi-speaker turn must specify a speechMetadata.speaker matching a configured speaker.",
				}
			}
			part := map[string]interface{}{"text": turn.Text}
			if style != nil || speaker != "" {
				meta := map[string]interface{}{}
				if style != nil {
					meta["style"] = *style
				}
				if speaker != "" {
					meta["speaker"] = speaker
				}
				part["speechMetadata"] = meta
			}
			parts = append(parts, part)
		}
	} else if len(googleOpts.Turns) > 0 || googleOpts.SpeechMetadata != nil {
		return nil, "", false, nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "Structured speech metadata and turns require Gemini 3.8 TTS.",
		}
	}

	transcript := opts.Text
	if len(googleOpts.Turns) > 0 {
		var sb strings.Builder
		for _, turn := range googleOpts.Turns {
			sb.WriteString(turn.Text)
		}
		transcript = sb.String()
	}
	if transcript == "" {
		return nil, "", false, nil, &providererrors.InvalidArgumentError{
			Field:   "text",
			Message: "Speech input must contain a non-empty transcript.",
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

	formats := map[string]string{"wav": "AUDIO_WAV", "pcm": "AUDIO_L16"}
	if usesStructured {
		formats = map[string]string{
			"wav": "AUDIO_WAV", "audio/wav": "AUDIO_WAV",
			"pcm": "AUDIO_L16", "audio/l16": "AUDIO_L16",
			"mulaw": "AUDIO_MULAW", "audio/mulaw": "AUDIO_MULAW",
			"alaw": "AUDIO_ALAW", "audio/alaw": "AUDIO_ALAW",
		}
	}
	resolved := "wav"
	if _, ok := formats[opts.OutputFormat]; ok && opts.OutputFormat != "" {
		resolved = opts.OutputFormat
	} else if opts.OutputFormat != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unsupported output format: %s. Using wav instead.", opts.OutputFormat),
		})
	}

	generationConfig := map[string]interface{}{
		"responseModalities": []string{"AUDIO"},
		"speechConfig":       speechConfig,
	}
	if usesStructured && opts.OutputFormat != "" {
		generationConfig["responseFormat"] = map[string]interface{}{
			"audio": map[string]interface{}{"mimeType": formats[resolved]},
		}
	}

	return map[string]interface{}{
		"contents": []interface{}{
			map[string]interface{}{"role": "user", "parts": parts},
		},
		"generationConfig": generationConfig,
	}, formats[resolved], usesStructured, warnings, nil
}

// googleSpeechHasSpeaker reports whether speaker names one of the configured
// multi-speaker voices.
func googleSpeechHasSpeaker(config map[string]interface{}, speaker string) bool {
	speakers, _ := config["speakerVoiceConfigs"].([]interface{})
	for _, raw := range speakers {
		if c, ok := raw.(map[string]interface{}); ok && c["speaker"] == speaker && speaker != "" {
			return true
		}
	}
	return false
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
		if rawMeta, ok := v["speechMetadata"]; ok && rawMeta != nil {
			meta, err := parseGoogleSpeechMetadata(rawMeta)
			if err != nil {
				return GoogleSpeechModelOptions{}, err
			}
			out.SpeechMetadata = meta
		}
		if rawTurns, ok := v["turns"]; ok && rawTurns != nil {
			turns, err := parseGoogleSpeechTurns(rawTurns)
			if err != nil {
				return GoogleSpeechModelOptions{}, err
			}
			out.Turns = turns
		}
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

func parseGoogleSpeechMetadata(raw interface{}) (*GoogleSpeechMetadata, error) {
	switch v := raw.(type) {
	case GoogleSpeechMetadata:
		return validateGoogleSpeechMetadata(v)
	case *GoogleSpeechMetadata:
		if v == nil {
			return nil, nil
		}
		return validateGoogleSpeechMetadata(*v)
	case map[string]interface{}:
		out := GoogleSpeechMetadata{}
		if sp, ok := v["speaker"]; ok && sp != nil {
			s, ok := sp.(string)
			if !ok {
				return nil, fmt.Errorf("invalid google speech speechMetadata.speaker: expected string")
			}
			out.Speaker = s
			if s == "" {
				return nil, fmt.Errorf("invalid google speech speechMetadata.speaker: expected a non-empty string")
			}
		}
		if st, ok := v["style"]; ok && st != nil {
			s, ok := st.(string)
			if !ok {
				return nil, fmt.Errorf("invalid google speech speechMetadata.style: expected string")
			}
			out.Style = &s
		}
		return &out, nil
	default:
		return nil, fmt.Errorf("invalid google speech speechMetadata: expected object")
	}
}

func validateGoogleSpeechMetadata(m GoogleSpeechMetadata) (*GoogleSpeechMetadata, error) {
	return &m, nil
}

func parseGoogleSpeechTurns(raw interface{}) ([]GoogleSpeechTurn, error) {
	var list []interface{}
	switch v := raw.(type) {
	case []GoogleSpeechTurn:
		if len(v) == 0 {
			return nil, fmt.Errorf("invalid google speech turns: expected at least one turn")
		}
		return v, nil
	case []interface{}:
		list = v
	case []map[string]interface{}:
		for _, t := range v {
			list = append(list, t)
		}
	default:
		return nil, fmt.Errorf("invalid google speech turns: expected array")
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("invalid google speech turns: expected at least one turn")
	}
	turns := make([]GoogleSpeechTurn, 0, len(list))
	for i, item := range list {
		switch t := item.(type) {
		case GoogleSpeechTurn:
			turns = append(turns, t)
		case map[string]interface{}:
			text, ok := t["text"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid google speech turns[%d].text: expected string", i)
			}
			turn := GoogleSpeechTurn{Text: text}
			if rawMeta, ok := t["speechMetadata"]; ok && rawMeta != nil {
				meta, err := parseGoogleSpeechMetadata(rawMeta)
				if err != nil {
					return nil, err
				}
				turn.SpeechMetadata = meta
			}
			turns = append(turns, turn)
		default:
			return nil, fmt.Errorf("invalid google speech turns[%d]: expected object", i)
		}
	}
	return turns, nil
}

func validateGoogleSpeechModelOptions(opts GoogleSpeechModelOptions) (GoogleSpeechModelOptions, error) {
	if len(opts.Turns) == 0 {
		opts.Turns = nil
	}
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
		if v, present := voiceConfig["voice"]; present && v != nil {
			return nil, fmt.Errorf("invalid google speech multiSpeakerVoiceConfig.speakerVoiceConfigs[%d].voiceConfig.voice: not supported; use prebuiltVoiceConfig", i)
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
	UsageMetadata map[string]interface{} `json:"usageMetadata,omitempty"`
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
	// strconv.Atoi clamps to math.MaxInt64/MinInt64 (with a non-nil, here
	// ignored, error) rather than returning 0 when match[1] overflows an
	// int, so an out-of-range value must be rejected explicitly instead of
	// being treated as a real sample rate.
	rate, err := strconv.Atoi(match[1])
	if err != nil || rate < 0 || rate > math.MaxUint32 {
		return 0
	}
	return rate
}

func addGeminiWAVHeader(pcm []byte, sampleRate int) []byte {
	const (
		numChannels   = 1
		bitsPerSample = 16
		headerSize    = 44
	)
	// sampleRate is parsed from the API-provided mimeType (see
	// parseGeminiSampleRate) via strconv.Atoi, which is architecture-sized
	// and can exceed uint32's range (or, on a parse overflow, be clamped to
	// math.MaxInt64). Guard the narrowing conversion below explicitly so an
	// out-of-range value can't silently truncate into a bogus WAV header
	// field instead of being treated as "unknown".
	if sampleRate < 0 || sampleRate > math.MaxUint32 {
		sampleRate = 0
	}
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
