package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const (
	// defaultChirp3HDVoice/defaultChirp3HDLanguage mirror TS DEFAULT_VOICE /
	// DEFAULT_LANGUAGE.
	defaultChirp3HDVoice    = "Kore"
	defaultChirp3HDLanguage = "en-US"

	// chirp3HDVoiceInfix: Chirp 3: HD voice names are
	// "<locale>-Chirp3-HD-<voice>", e.g. "en-US-Chirp3-HD-Kore".
	// https://cloud.google.com/text-to-speech/docs/chirp3-hd
	chirp3HDVoiceInfix = "Chirp3-HD"
)

// CloudTTSSpeechModel implements Chirp 3: HD voices on the Google Cloud
// Text-to-Speech API (TS GoogleVertexCloudTTSSpeechModel). Unlike the Gemini
// TTS models (served through Vertex's generateContent endpoint), Chirp 3: HD
// voices are served by the dedicated Cloud Text-to-Speech text:synthesize
// endpoint, reusing the provider's Google Cloud (OAuth) credentials.
type CloudTTSSpeechModel struct {
	prov    *Provider
	modelID string
}

// NewCloudTTSSpeechModel creates a Chirp 3: HD speech model.
func NewCloudTTSSpeechModel(p *Provider, modelID string) *CloudTTSSpeechModel {
	return &CloudTTSSpeechModel{prov: p, modelID: modelID}
}

func (m *CloudTTSSpeechModel) SpecificationVersion() string { return "v4" }
func (m *CloudTTSSpeechModel) Provider() string             { return "google.vertex.speech" }
func (m *CloudTTSSpeechModel) ModelID() string              { return m.modelID }

// chirp3HDVoiceName composes the languageCode/name pair Cloud TTS expects,
// mirroring TS's inline composition logic exactly: a fully-qualified Chirp
// 3: HD voice name is passed through verbatim (with its locale prefix, if
// any, as the language code); otherwise the name is composed from the
// language (BCP-47 locale) and the plain voice name.
func chirp3HDVoiceName(voice, language string) (voiceName, languageCode string) {
	if strings.Contains(voice, chirp3HDVoiceInfix) {
		voiceName = voice
		// The locale prefix may be missing (e.g. "Chirp3-HD-Kore"), in which
		// case the extracted prefix is empty and the default language is used.
		localePrefix := strings.TrimSuffix(strings.SplitN(voice, chirp3HDVoiceInfix, 2)[0], "-")
		switch {
		case language != "":
			languageCode = language
		case localePrefix != "":
			languageCode = localePrefix
		default:
			languageCode = defaultChirp3HDLanguage
		}
		return voiceName, languageCode
	}

	if language != "" {
		languageCode = language
	} else {
		languageCode = defaultChirp3HDLanguage
	}
	voiceName = fmt.Sprintf("%s-%s-%s", languageCode, chirp3HDVoiceInfix, voice)
	return voiceName, languageCode
}

// DoGenerate synthesizes speech via the Cloud Text-to-Speech
// text:synthesize endpoint.
func (m *CloudTTSSpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	currentDate := time.Now()
	var warnings []types.Warning

	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}

	voice := opts.Voice
	if voice == "" {
		voice = defaultChirp3HDVoice
	}
	voiceName, languageCode := chirp3HDVoiceName(voice, opts.Language)

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Google Cloud Text-to-Speech Chirp 3: HD voices do not support the `instructions` option. It was ignored.",
		})
	}
	// LINEAR16 responses are WAV (RIFF) files, so only "wav" is supported.
	if opts.OutputFormat != "" && opts.OutputFormat != "wav" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "outputFormat",
			Details: fmt.Sprintf("Unsupported output format: %s. Using wav instead.", opts.OutputFormat),
		})
	}

	audioConfig := map[string]interface{}{"audioEncoding": "LINEAR16"}
	if opts.Speed != nil {
		audioConfig["speakingRate"] = *opts.Speed
	}
	reqBody := map[string]interface{}{
		"input":       map[string]interface{}{"text": opts.Text},
		"voice":       map[string]interface{}{"languageCode": languageCode, "name": voiceName},
		"audioConfig": audioConfig,
	}

	if m.prov.cloudTTSClient == nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "",
			"google Vertex Chirp speech models require standard Google Cloud credentials (Express Mode API keys are not supported)", nil)
	}

	resp, err := m.prov.cloudTTSClient.Do(ctx, internalhttp.Request{
		Method:  "POST",
		Headers: opts.Headers,
		Body:    reqBody,
	})
	if err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to synthesize speech: "+err.Error(), err)
	}
	if resp.StatusCode >= 400 {
		return nil, cloudTTSError(resp.StatusCode, resp.Body, resp.Headers)
	}

	var parsed struct {
		AudioContent string `json:"audioContent"`
	}
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to parse response: "+err.Error(), err)
	}

	// Empty audio is returned as-is so the core layer throws
	// NoSpeechGeneratedError.
	audio := []byte{}
	if parsed.AudioContent != "" {
		decoded, err := base64.StdEncoding.DecodeString(parsed.AudioContent)
		if err != nil {
			return nil, providererrors.NewProviderError("google-vertex", 0, "", "failed to decode audio: "+err.Error(), err)
		}
		audio = decoded
	}

	reqBodyJSON, _ := json.Marshal(reqBody)

	return &types.SpeechResult{
		Audio:    audio,
		Warnings: warnings,
		ProviderMetadata: map[string]interface{}{
			"google": map[string]interface{}{"mimeType": "audio/wav"},
		},
		Request: &types.StepRequest{Body: string(reqBodyJSON)},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      json.RawMessage(resp.Body),
		},
	}, nil
}

// cloudTTSErrorData mirrors TS googleVertexErrorDataSchema
// (google-vertex-error.ts): {"error":{"code","message","status"}}.
type cloudTTSErrorData struct {
	Error struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// cloudTTSError builds a ProviderError from a failed Cloud Text-to-Speech
// response, mirroring TS googleVertexFailedResponseHandler
// (createJsonErrorResponseHandler with errorToMessage: data => data.error.message):
// the message is the parsed Google error's `message` field alone, not the
// raw response body, falling back to the raw body when it doesn't parse.
func cloudTTSError(statusCode int, body []byte, headers http.Header) error {
	message := string(body)
	var data cloudTTSErrorData
	if err := json.Unmarshal(body, &data); err == nil && data.Error.Message != "" {
		message = data.Error.Message
	}
	perr := providererrors.NewProviderError("google-vertex", statusCode, data.Error.Status, message, nil)
	perr.ResponseBody = string(body)
	if len(headers) > 0 {
		perr.ResponseHeaders = providerutils.ExtractHeaders(headers)
	}
	return perr
}
