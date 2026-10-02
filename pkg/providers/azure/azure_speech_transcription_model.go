package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// mediaTypeToExtension maps a media type to a file extension, mirroring TS
// provider-utils mediaTypeToExtension.
func mediaTypeToExtension(mediaType string) string {
	parts := strings.SplitN(strings.ToLower(mediaType), "/", 2)
	subtype := ""
	if len(parts) == 2 {
		subtype = parts[1]
	}
	switch subtype {
	case "mpeg":
		return "mp3"
	case "x-wav":
		return "wav"
	case "opus":
		return "ogg"
	case "mp4", "x-m4a":
		return "m4a"
	default:
		return subtype
	}
}

// AzureSpeechTranscriptionModel is the Azure Speech "Fast Transcription" file
// backend for MAI-Transcribe-1.5 and MAI-Transcribe-2, mirroring TS
// AzureSpeechTranscriptionModel (azure-speech-transcription-model.ts): POST
// /speechtotext/transcriptions:transcribe?api-version=2025-10-15 with
// multipart form data.
type AzureSpeechTranscriptionModel struct {
	modelID    string
	url        func() (string, error)
	headers    func(ctx context.Context) (map[string]string, error)
	httpClient *stdhttp.Client
}

func newAzureSpeechTranscriptionModel(modelID string, url func() (string, error), headers func(ctx context.Context) (map[string]string, error), httpClient *stdhttp.Client) *AzureSpeechTranscriptionModel {
	return &AzureSpeechTranscriptionModel{modelID: modelID, url: url, headers: headers, httpClient: httpClient}
}

func (m *AzureSpeechTranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *AzureSpeechTranscriptionModel) Provider() string             { return "azure.transcription" }
func (m *AzureSpeechTranscriptionModel) ModelID() string              { return m.modelID }

// azureSpeechTranscribeResponse mirrors TS azure-speech-transcription-model.ts's
// responseSchema.
type azureSpeechTranscribeResponse struct {
	CombinedPhrases []struct {
		Text string `json:"text"`
	} `json:"combinedPhrases"`
	DurationMilliseconds *float64            `json:"durationMilliseconds"`
	Phrases              []azureSpeechPhrase `json:"phrases"`
}

// AzureSpeechPhrase is one recognized phrase, exposed via
// providerMetadata["azure"]["phrases"] (TS AzureTranscriptionProviderMetadata).
type AzureSpeechPhrase struct {
	Text                 string            `json:"text"`
	OffsetMilliseconds   *float64          `json:"offsetMilliseconds,omitempty"`
	DurationMilliseconds *float64          `json:"durationMilliseconds,omitempty"`
	Locale               string            `json:"locale,omitempty"`
	Speaker              *int              `json:"speaker,omitempty"`
	Confidence           *float64          `json:"confidence,omitempty"`
	Words                []AzureSpeechWord `json:"words,omitempty"`
}

// AzureSpeechWord is one recognized word within an AzureSpeechPhrase.
type AzureSpeechWord struct {
	Text                 string   `json:"text"`
	OffsetMilliseconds   *float64 `json:"offsetMilliseconds,omitempty"`
	DurationMilliseconds *float64 `json:"durationMilliseconds,omitempty"`
}

type azureSpeechPhrase struct {
	Text                 string            `json:"text"`
	OffsetMilliseconds   *float64          `json:"offsetMilliseconds"`
	DurationMilliseconds *float64          `json:"durationMilliseconds"`
	Locale               *string           `json:"locale"`
	Speaker              *int              `json:"speaker"`
	Confidence           *float64          `json:"confidence"`
	Words                []azureSpeechWord `json:"words"`
}

type azureSpeechWord struct {
	Text                 string   `json:"text"`
	OffsetMilliseconds   *float64 `json:"offsetMilliseconds"`
	DurationMilliseconds *float64 `json:"durationMilliseconds"`
}

// DoGenerate transcribes audio via the Azure Speech Fast Transcription API,
// mirroring TS AzureSpeechTranscriptionModel#doGenerate.
func (m *AzureSpeechTranscriptionModel) DoGenerate(ctx context.Context, opts *provider.TranscriptionOptions, azureOpts AzureSpeechTranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	timestamp := time.Now()

	maiModel, isMAI := getMAITranscribeModel(m.modelID)
	modelName := m.modelID
	if isMAI {
		modelName = maiModel.Name
	}
	timestamps := azureOpts.Timestamps
	if timestamps == "" && (!isMAI || maiModel.SupportsTimestamps) {
		timestamps = "segment"
	}

	definition := map[string]interface{}{
		"enhancedMode": map[string]interface{}{
			"enabled": true,
			"model":   modelName,
			"modelOptions": map[string]interface{}{
				"timestamps":      nilIfEmptyString(timestamps),
				"transcribeStyle": nilIfEmptyString(azureOpts.TranscribeStyle),
			},
		},
	}
	if azureOpts.Locales != nil {
		definition["locales"] = azureOpts.Locales
	}
	if azureOpts.Diarization != nil {
		definition["diarization"] = map[string]interface{}{"enabled": azureOpts.Diarization.Enabled}
	}
	if azureOpts.PhraseList != nil {
		definition["phraseList"] = map[string]interface{}{"phrases": azureOpts.PhraseList.Phrases}
	}

	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(opts.AudioBase64)
		if err != nil {
			return nil, &providererrors.InvalidArgumentError{Field: "audio", Message: "invalid base64 audio: " + err.Error()}
		}
		audio = decoded
	}

	body, contentType, err := buildAzureSpeechTranscribeMultipart(audio, opts.MimeType, definition)
	if err != nil {
		return nil, err
	}

	urlStr, err := m.url()
	if err != nil {
		return nil, err
	}
	headers, err := m.headers(ctx)
	if err != nil {
		return nil, err
	}

	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, urlStr, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range internalhttp.MergeHeaders(headers, opts.Headers) {
		req.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := fileutil.ReadResponseWithSizeLimit(resp, urlStr, fileutil.DefaultMaxDownloadSize)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != stdhttp.StatusOK {
		return nil, azureSpeechAPIError(m.Provider(), resp.StatusCode, resp.Header, respBody)
	}

	var parsed azureSpeechTranscribeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), resp.StatusCode, "", "failed to parse Azure Speech transcription response: "+err.Error(), err)
	}

	var textParts []string
	for _, phrase := range parsed.CombinedPhrases {
		textParts = append(textParts, phrase.Text)
	}

	var segments []types.TranscriptionTimestamp
	languages := map[string]bool{}
	phrases := make([]AzureSpeechPhrase, 0, len(parsed.Phrases))
	for _, phrase := range parsed.Phrases {
		if phrase.OffsetMilliseconds != nil && phrase.DurationMilliseconds != nil {
			segments = append(segments, types.TranscriptionTimestamp{
				Text:  phrase.Text,
				Start: *phrase.OffsetMilliseconds / 1000,
				End:   (*phrase.OffsetMilliseconds + *phrase.DurationMilliseconds) / 1000,
			})
		}
		locale := ""
		if phrase.Locale != nil {
			locale = *phrase.Locale
			base := strings.ToLower(strings.SplitN(locale, "-", 2)[0])
			languages[base] = true
		}
		words := make([]AzureSpeechWord, 0, len(phrase.Words))
		for _, w := range phrase.Words {
			words = append(words, AzureSpeechWord(w))
		}
		phrases = append(phrases, AzureSpeechPhrase{
			Text:                 phrase.Text,
			OffsetMilliseconds:   phrase.OffsetMilliseconds,
			DurationMilliseconds: phrase.DurationMilliseconds,
			Locale:               locale,
			Speaker:              phrase.Speaker,
			Confidence:           phrase.Confidence,
			Words:                words,
		})
	}

	// Only report a language when every phrase's locale shares the same
	// ISO-639-1 code; mixed results (e.g. en + fil) are reported as
	// undefined, mirroring TS's reportedLanguage logic.
	reportedLanguage := ""
	if len(languages) == 1 {
		for lang := range languages {
			if len(lang) == 2 {
				reportedLanguage = lang
			}
		}
	}

	var durationInSeconds *float64
	if parsed.DurationMilliseconds != nil {
		d := *parsed.DurationMilliseconds / 1000
		durationInSeconds = &d
	}

	return &types.TranscriptionResult{
		Text:              strings.Join(textParts, " "),
		Segments:          segments,
		Language:          reportedLanguage,
		DurationInSeconds: durationInSeconds,
		Warnings:          []types.Warning{},
		ProviderMetadata: map[string]interface{}{
			"azure": map[string]interface{}{"phrases": phrases},
		},
		Response: &types.ResponseMetadata{
			Timestamp: timestamp,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Header),
			Body:      respBody,
		},
	}, nil
}

func nilIfEmptyString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func buildAzureSpeechTranscribeMultipart(audio []byte, mimeType string, definition map[string]interface{}) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	extension := mediaTypeToExtension(mimeType)
	if extension == "" {
		extension = "bin"
	}
	part, err := writer.CreateFormFile("audio", "audio."+extension)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}

	definitionJSON, err := json.Marshal(definition)
	if err != nil {
		return nil, "", err
	}
	if err := writer.WriteField("definition", string(definitionJSON)); err != nil {
		return nil, "", err
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

// azureSpeechErrorBody mirrors the two shapes Azure Speech error bodies take:
// {"error":{"message":...}} or {"message":...}.
type azureSpeechErrorBody struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

// azureSpeechAPIError builds a *providererrors.ProviderError for a non-200
// Azure Speech response, mirroring TS's failedResponseHandler: a parsed JSON
// error message is used as-is; an empty 400 body (Azure's common failure
// mode for a bad voice, style, or option) gets an actionable message instead
// of a blank one.
func azureSpeechAPIError(providerName string, statusCode int, headers stdhttp.Header, body []byte) error {
	message := ""
	var parsed azureSpeechErrorBody
	if len(body) > 0 && json.Unmarshal(body, &parsed) == nil {
		if parsed.Error != nil && parsed.Error.Message != "" {
			message = parsed.Error.Message
		} else if parsed.Message != "" {
			message = parsed.Message
		}
	}
	if message == "" {
		if statusCode == stdhttp.StatusBadRequest {
			message = "Azure Speech request failed with status 400. Check the voice name, style, and output format."
		} else {
			message = "Azure Speech request failed with status " + strconv.Itoa(statusCode) + "."
		}
	}
	return &providererrors.ProviderError{
		Provider:        providerName,
		StatusCode:      statusCode,
		Message:         message,
		ResponseHeaders: providerutils.ExtractHeaders(headers),
		ResponseBody:    string(body),
	}
}
