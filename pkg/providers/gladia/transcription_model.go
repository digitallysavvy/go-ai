package gladia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// pollTimeout and pollInterval mirror GladiaTranscriptionModel.doGenerate's
// polling loop in the TypeScript SDK.
const (
	pollTimeout  = 60 * time.Second
	pollInterval = 1 * time.Second
)

// TranscriptionModel implements the provider.TranscriptionModel interface for
// Gladia's async pre-recorded transcription flow: upload the audio, start a
// transcription job referencing the uploaded audio URL, then poll the job's
// result_url until it completes (non-streaming; this "polling" is a batch job
// status poll, not real-time streaming).
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "gladia"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// extractOptions reads Gladia-specific options from the provider options map.
func extractOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["gladia"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var opts TranscriptionModelOptions
	if err := json.Unmarshal(b, &opts); err != nil {
		return nil
	}
	return &opts
}

// buildRequestBody builds the /v2/pre-recorded request body, mirroring
// GladiaTranscriptionModel.getArgs.
func buildRequestBody(gOpts *TranscriptionModelOptions) map[string]interface{} {
	body := map[string]interface{}{}
	if gOpts == nil {
		return body
	}

	setOptionalString(body, "context_prompt", gOpts.ContextPrompt)
	if gOpts.CustomVocabulary != nil {
		body["custom_vocabulary"] = gOpts.CustomVocabulary
	}
	setOptional(body, "detect_language", gOpts.DetectLanguage)
	setOptional(body, "enable_code_switching", gOpts.EnableCodeSwitching)
	setOptionalString(body, "language", gOpts.Language)
	setOptional(body, "callback", gOpts.Callback)
	setOptional(body, "subtitles", gOpts.Subtitles)
	setOptional(body, "diarization", gOpts.Diarization)
	setOptional(body, "translation", gOpts.Translation)
	setOptional(body, "summarization", gOpts.Summarization)
	setOptional(body, "moderation", gOpts.Moderation)
	setOptional(body, "named_entity_recognition", gOpts.NamedEntityRecognition)
	setOptional(body, "chapterization", gOpts.Chapterization)
	setOptional(body, "name_consistency", gOpts.NameConsistency)
	setOptional(body, "custom_spelling", gOpts.CustomSpelling)
	setOptional(body, "structured_data_extraction", gOpts.StructuredDataExtraction)
	setOptional(body, "sentiment_analysis", gOpts.SentimentAnalysis)
	setOptional(body, "audio_to_llm", gOpts.AudioToLlm)
	if len(gOpts.CustomMetadata) > 0 {
		body["custom_metadata"] = gOpts.CustomMetadata
	}
	setOptional(body, "sentences", gOpts.Sentences)
	setOptional(body, "display_mode", gOpts.DisplayMode)
	setOptional(body, "punctuation_enhanced", gOpts.PunctuationEnhanced)

	if gOpts.CustomVocabularyConfig != nil {
		cfg := map[string]interface{}{"vocabulary": gOpts.CustomVocabularyConfig.Vocabulary}
		setOptional(cfg, "default_intensity", gOpts.CustomVocabularyConfig.DefaultIntensity)
		body["custom_vocabulary_config"] = cfg
	}
	if gOpts.CodeSwitchingConfig != nil {
		body["code_switching_config"] = map[string]interface{}{"languages": gOpts.CodeSwitchingConfig.Languages}
	}
	if gOpts.CallbackConfig != nil {
		cfg := map[string]interface{}{"url": gOpts.CallbackConfig.URL}
		setOptionalString(cfg, "method", gOpts.CallbackConfig.Method)
		body["callback_config"] = cfg
	}
	if gOpts.SubtitlesConfig != nil {
		cfg := map[string]interface{}{}
		if len(gOpts.SubtitlesConfig.Formats) > 0 {
			cfg["formats"] = gOpts.SubtitlesConfig.Formats
		}
		setOptional(cfg, "minimum_duration", gOpts.SubtitlesConfig.MinimumDuration)
		setOptional(cfg, "maximum_duration", gOpts.SubtitlesConfig.MaximumDuration)
		setOptional(cfg, "maximum_characters_per_row", gOpts.SubtitlesConfig.MaximumCharactersPerRow)
		setOptional(cfg, "maximum_rows_per_caption", gOpts.SubtitlesConfig.MaximumRowsPerCaption)
		setOptionalString(cfg, "style", gOpts.SubtitlesConfig.Style)
		body["subtitles_config"] = cfg
	}
	if gOpts.DiarizationConfig != nil {
		cfg := map[string]interface{}{}
		setOptional(cfg, "number_of_speakers", gOpts.DiarizationConfig.NumberOfSpeakers)
		setOptional(cfg, "min_speakers", gOpts.DiarizationConfig.MinSpeakers)
		setOptional(cfg, "max_speakers", gOpts.DiarizationConfig.MaxSpeakers)
		setOptional(cfg, "enhanced", gOpts.DiarizationConfig.Enhanced)
		body["diarization_config"] = cfg
	}
	if gOpts.TranslationConfig != nil {
		cfg := map[string]interface{}{"target_languages": gOpts.TranslationConfig.TargetLanguages}
		setOptionalString(cfg, "model", gOpts.TranslationConfig.Model)
		setOptional(cfg, "match_original_utterances", gOpts.TranslationConfig.MatchOriginalUtterances)
		body["translation_config"] = cfg
	}
	if gOpts.SummarizationConfig != nil {
		cfg := map[string]interface{}{}
		setOptionalString(cfg, "type", gOpts.SummarizationConfig.Type)
		body["summarization_config"] = cfg
	}
	if gOpts.CustomSpellingConfig != nil {
		body["custom_spelling_config"] = map[string]interface{}{
			"spelling_dictionary": gOpts.CustomSpellingConfig.SpellingDictionary,
		}
	}
	if gOpts.StructuredDataExtractionConfig != nil {
		body["structured_data_extraction_config"] = map[string]interface{}{
			"classes": gOpts.StructuredDataExtractionConfig.Classes,
		}
	}
	if gOpts.AudioToLlmConfig != nil {
		body["audio_to_llm_config"] = map[string]interface{}{
			"prompts": gOpts.AudioToLlmConfig.Prompts,
		}
	}

	return body
}

func setOptional[T any](body map[string]interface{}, key string, value *T) {
	if value != nil {
		body[key] = *value
	}
}

func setOptionalString(body map[string]interface{}, key, value string) {
	if value != "" {
		body[key] = value
	}
}

// DoTranscribe performs the upload -> start job -> poll flow.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	currentDate := time.Now()

	// Step 1: upload the audio file.
	uploadBody, contentType, err := buildUploadMultipart(opts)
	if err != nil {
		return nil, err
	}
	var uploadResult struct {
		AudioURL string `json:"audio_url"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/upload",
		Body:    uploadBody,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &uploadResult)
	if err != nil {
		return nil, handleError(err)
	}

	// Step 2: start a pre-recorded transcription job.
	body := buildRequestBody(extractOptions(opts.ProviderOptions))
	body["audio_url"] = uploadResult.AudioURL

	var initResult struct {
		ResultURL string `json:"result_url"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/pre-recorded",
		Body:    body,
		Headers: opts.Headers,
	}, &initResult)
	if err != nil {
		return nil, handleError(err)
	}

	// Step 3: poll the job's result_url until it completes. The URL comes
	// from the provider response; validate it (and any redirect) as
	// untrusted input, trusting only the provider's own configured origin.
	raw, err := m.pollResult(ctx, initResult.ResultURL, opts.Headers)
	if err != nil {
		return nil, err
	}

	return convertPollResult(raw, currentDate), nil
}

func buildUploadMultipart(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("audio", "audio."+gladiaAudioExtension(opts.MimeType))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(opts.Audio); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func gladiaAudioExtension(mimeType string) string {
	switch mimeType {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return "wav"
	case "audio/webm":
		return "webm"
	case "audio/mp4", "audio/m4a":
		return "m4a"
	case "audio/flac":
		return "flac"
	case "audio/ogg":
		return "ogg"
	default:
		return "wav"
	}
}

// pollResult polls resultURL until the job reaches status "done" or "error",
// or pollTimeout elapses. It returns the raw decoded JSON response.
func (m *TranscriptionModel) pollResult(ctx context.Context, resultURL string, headers map[string]string) (map[string]interface{}, error) {
	trustedOrigin := m.provider.config.BaseURL
	pollOpts := fileutil.TrustedOriginDownloadOptions(trustedOrigin, m.provider.client.HTTPClient().Transport)
	pollOpts.Headers = internalhttp.MergeHeaders(map[string]string{"x-gladia-key": m.provider.config.APIKey}, headers)

	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if time.Since(start) > pollTimeout {
			return nil, fmt.Errorf("gladia: transcription job polling timed out")
		}

		var raw map[string]interface{}
		if _, err := fileutil.PollJSON(ctx, resultURL, pollOpts, &raw); err != nil {
			return nil, handleError(err)
		}

		status, _ := raw["status"].(string)
		switch status {
		case "done":
			return raw, nil
		case "error":
			return nil, fmt.Errorf("gladia: transcription job failed: %v", raw["error_code"])
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// convertPollResult maps a raw completed-job response into a
// TranscriptionResult, mirroring GladiaTranscriptionModel.doGenerate's return.
func convertPollResult(raw map[string]interface{}, currentDate time.Time) *types.TranscriptionResult {
	b, _ := json.Marshal(raw)
	var typed struct {
		Result *struct {
			Metadata struct {
				AudioDuration float64 `json:"audio_duration"`
			} `json:"metadata"`
			Transcription struct {
				FullTranscript string   `json:"full_transcript"`
				Languages      []string `json:"languages"`
				Utterances     []struct {
					Start float64 `json:"start"`
					End   float64 `json:"end"`
					Text  string  `json:"text"`
				} `json:"utterances"`
			} `json:"transcription"`
		} `json:"result"`
	}
	_ = json.Unmarshal(b, &typed)

	if typed.Result == nil {
		return &types.TranscriptionResult{
			Warnings: []types.Warning{},
			Response: &types.ResponseMetadata{
				Timestamp: currentDate,
				ModelID:   "default",
				Body:      raw,
			},
			ProviderMetadata: map[string]interface{}{"gladia": raw},
		}
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(typed.Result.Transcription.Utterances))
	for _, utt := range typed.Result.Transcription.Utterances {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  utt.Text,
			Start: utt.Start,
			End:   utt.End,
		})
	}

	var language string
	if len(typed.Result.Transcription.Languages) > 0 {
		language = typed.Result.Transcription.Languages[0]
	}

	duration := typed.Result.Metadata.AudioDuration

	return &types.TranscriptionResult{
		Text:              typed.Result.Transcription.FullTranscript,
		Segments:          segments,
		Timestamps:        segments,
		Language:          language,
		DurationInSeconds: &duration,
		Warnings:          []types.Warning{},
		Usage: types.TranscriptionUsage{
			DurationSeconds: duration,
		},
		// The whole poll response is surfaced under providerMetadata.gladia,
		// preserving per-utterance speaker/channel/confidence/words/language
		// and diarization info that the AI SDK's `segments` shape can't
		// represent (matches TS: providerMetadata: { gladia: transcriptionResult }).
		ProviderMetadata: map[string]interface{}{"gladia": raw},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			// Gladia's model ID is not selectable in the pre-recorded API;
			// the TS SDK always reports "default" here too.
			ModelID: "default",
			Body:    raw,
		},
	}
}

// handleError converts an HTTP error into a ProviderError, parsing Gladia's
// `{"error":{"message","code"}}` error shape when present. Errors surfaced by
// fileutil's polling helpers (DownloadError) are wrapped generically, since
// their response body is not retained.
func handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		var payload struct {
			Error struct {
				Message string `json:"message"`
				Code    int    `json:"code"`
			} `json:"error"`
		}
		message := string(statusErr.Body)
		code := ""
		if jsonErr := json.Unmarshal(statusErr.Body, &payload); jsonErr == nil && payload.Error.Message != "" {
			message = payload.Error.Message
			code = fmt.Sprintf("%d", payload.Error.Code)
		}
		return providererrors.NewProviderError("gladia", statusErr.StatusCode, code, message, err)
	}

	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) {
		return providererrors.NewProviderError("gladia", downloadErr.StatusCode, "", downloadErr.Error(), err)
	}

	return providererrors.NewProviderError("gladia", 0, "", err.Error(), err)
}
