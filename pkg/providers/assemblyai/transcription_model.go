package assemblyai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// docsURL is linked from deprecation/nudge warnings about model selection.
const docsURL = "https://www.assemblyai.com/docs/pre-recorded-audio/select-the-speech-model"

// pollingInterval mirrors the TypeScript SDK's POLLING_INTERVAL_MS.
const pollingInterval = 3 * time.Second

// TranscriptionModel implements the provider.TranscriptionModel interface for AssemblyAI
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new AssemblyAI transcription model
func NewTranscriptionModel(provider *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{
		provider: provider,
		modelID:  modelID,
	}
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "assemblyai"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// extractOptions reads AssemblyAI-specific options from the provider options map.
func extractOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["assemblyai"]
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

// buildRequestBody builds the /v2/transcript request body and warnings for
// the configured model and options. Mirrors AssemblyAITranscriptionModel.getArgs.
func (m *TranscriptionModel) buildRequestBody(opts *provider.TranscriptionOptions) (map[string]interface{}, []types.Warning) {
	warnings := []types.Warning{}
	aaiOpts := extractOptions(opts.ProviderOptions)

	body := map[string]interface{}{}

	// The legacy `best` model is selected via the deprecated singular
	// `speech_model` parameter. All other models (e.g. `universal-2`,
	// `universal-3-pro`, `universal-3-5-pro`) are only accessible via the
	// `speech_models` array and are rejected by `speech_model`.
	if m.modelID == ModelBest || m.modelID == "" {
		body["speech_model"] = ModelBest
		warnings = append(warnings, types.Warning{
			Type:    "deprecated",
			Setting: fmt.Sprintf("model '%s'", ModelBest),
			Message: "The 'best' model is a legacy AssemblyAI model. Use 'universal-3-5-pro' instead. See documentation: " + docsURL,
		})
	} else {
		body["speech_models"] = []string{m.modelID}

		// Forward-looking nudge: universal-3-5-pro is AssemblyAI's latest
		// flagship and is set to replace universal-3-pro. Not a deprecation —
		// both models still work — so this is an informational warning only.
		switch m.modelID {
		case ModelUniversal3Pro:
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: fmt.Sprintf("'universal-3-5-pro' is AssemblyAI's latest flagship model and is set to replace '%s'. See %s", ModelUniversal3Pro, docsURL),
			})
		case ModelUniversal2:
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "'universal-3-5-pro' is AssemblyAI's latest flagship model. See " + docsURL,
			})
		}
	}

	// The shared TranscriptionOptions.Language field maps to language_code;
	// provider-specific languageCode (if set) takes precedence.
	if opts.Language != "" {
		body["language_code"] = opts.Language
	}

	if aaiOpts != nil {
		setOptional(body, "audio_end_at", aaiOpts.AudioEndAt)
		setOptional(body, "audio_start_from", aaiOpts.AudioStartFrom)
		setOptional(body, "auto_chapters", aaiOpts.AutoChapters)
		setOptional(body, "auto_highlights", aaiOpts.AutoHighlights)
		setOptionalString(body, "boost_param", aaiOpts.BoostParam)
		setOptional(body, "content_safety", aaiOpts.ContentSafety)
		setOptional(body, "content_safety_confidence", aaiOpts.ContentSafetyConfidence)
		if len(aaiOpts.CustomSpelling) > 0 {
			body["custom_spelling"] = aaiOpts.CustomSpelling
		}
		setOptional(body, "disfluencies", aaiOpts.Disfluencies)
		setOptional(body, "entity_detection", aaiOpts.EntityDetection)
		setOptional(body, "filter_profanity", aaiOpts.FilterProfanity)
		setOptional(body, "format_text", aaiOpts.FormatText)
		setOptional(body, "iab_categories", aaiOpts.IabCategories)
		if len(aaiOpts.KeytermsPrompt) > 0 {
			body["keyterms_prompt"] = aaiOpts.KeytermsPrompt
		}
		if aaiOpts.LanguageCode != "" {
			body["language_code"] = aaiOpts.LanguageCode
		}
		setOptional(body, "language_confidence_threshold", aaiOpts.LanguageConfidenceThreshold)
		setOptional(body, "language_detection", aaiOpts.LanguageDetection)
		setOptional(body, "multichannel", aaiOpts.Multichannel)
		setOptionalString(body, "prompt", aaiOpts.Prompt)
		setOptional(body, "punctuate", aaiOpts.Punctuate)
		setOptional(body, "redact_pii", aaiOpts.RedactPii)
		setOptional(body, "redact_pii_audio", aaiOpts.RedactPiiAudio)
		setOptionalString(body, "redact_pii_audio_quality", aaiOpts.RedactPiiAudioQuality)
		if len(aaiOpts.RedactPiiPolicies) > 0 {
			body["redact_pii_policies"] = aaiOpts.RedactPiiPolicies
		}
		setOptional(body, "redact_pii_return_unredacted", aaiOpts.RedactPiiReturnUnredacted)
		setOptionalString(body, "redact_pii_sub", aaiOpts.RedactPiiSub)
		if len(aaiOpts.RedactStaticEntities) > 0 {
			body["redact_static_entities"] = aaiOpts.RedactStaticEntities
		}
		setOptionalString(body, "remove_audio_tags", aaiOpts.RemoveAudioTags)
		setOptional(body, "sentiment_analysis", aaiOpts.SentimentAnalysis)
		setOptional(body, "speaker_labels", aaiOpts.SpeakerLabels)
		setOptional(body, "speakers_expected", aaiOpts.SpeakersExpected)
		setOptional(body, "speech_threshold", aaiOpts.SpeechThreshold)
		setOptional(body, "summarization", aaiOpts.Summarization)
		setOptionalString(body, "summary_model", aaiOpts.SummaryModel)
		setOptionalString(body, "summary_type", aaiOpts.SummaryType)
		setOptional(body, "temperature", aaiOpts.Temperature)
		setOptionalString(body, "webhook_auth_header_name", aaiOpts.WebhookAuthHeaderName)
		setOptionalString(body, "webhook_auth_header_value", aaiOpts.WebhookAuthHeaderValue)
		setOptionalString(body, "webhook_url", aaiOpts.WebhookUrl)
		if len(aaiOpts.WordBoost) > 0 {
			body["word_boost"] = aaiOpts.WordBoost
		}
		setOptionalString(body, "domain", aaiOpts.Domain)

		if aaiOpts.SpeakerOptions != nil {
			speakerOptions := map[string]interface{}{}
			setOptional(speakerOptions, "min_speakers_expected", aaiOpts.SpeakerOptions.MinSpeakersExpected)
			setOptional(speakerOptions, "max_speakers_expected", aaiOpts.SpeakerOptions.MaxSpeakersExpected)
			body["speaker_options"] = speakerOptions
		}

		if aaiOpts.LanguageDetectionOptions != nil {
			ldo := map[string]interface{}{}
			if len(aaiOpts.LanguageDetectionOptions.ExpectedLanguages) > 0 {
				ldo["expected_languages"] = aaiOpts.LanguageDetectionOptions.ExpectedLanguages
			}
			setOptionalString(ldo, "fallback_language", aaiOpts.LanguageDetectionOptions.FallbackLanguage)
			setOptional(ldo, "code_switching", aaiOpts.LanguageDetectionOptions.CodeSwitching)
			setOptional(ldo, "code_switching_confidence_threshold", aaiOpts.LanguageDetectionOptions.CodeSwitchingConfidenceThreshold)
			body["language_detection_options"] = ldo
		}

		if aaiOpts.RedactPiiAudioOptions != nil {
			rpao := map[string]interface{}{}
			setOptional(rpao, "return_redacted_no_speech_audio", aaiOpts.RedactPiiAudioOptions.ReturnRedactedNoSpeechAudio)
			setOptionalString(rpao, "override_audio_redaction_method", aaiOpts.RedactPiiAudioOptions.OverrideAudioRedactionMethod)
			body["redact_pii_audio_options"] = rpao
		}

		var deprecatedBoostOptions []string
		if len(aaiOpts.WordBoost) > 0 {
			deprecatedBoostOptions = append(deprecatedBoostOptions, "wordBoost")
		}
		if aaiOpts.BoostParam != "" {
			deprecatedBoostOptions = append(deprecatedBoostOptions, "boostParam")
		}
		if len(deprecatedBoostOptions) > 0 {
			setting := deprecatedBoostOptions[0]
			for _, s := range deprecatedBoostOptions[1:] {
				setting += ", " + s
			}
			warnings = append(warnings, types.Warning{
				Type:    "deprecated",
				Setting: setting,
				Message: "'wordBoost' and 'boostParam' are deprecated and are rejected by 'universal-3-pro' / 'universal-3-5-pro' and 'slam-1'. Use 'keytermsPrompt' instead.",
			})
		}

		// The following options only take effect alongside a prerequisite
		// option; without it AssemblyAI either rejects the request (400) or
		// silently ignores the option. Warn rather than mutate user input.
		redactPiiEnabled := aaiOpts.RedactPii != nil && *aaiOpts.RedactPii
		if (aaiOpts.RedactPiiReturnUnredacted != nil || len(aaiOpts.RedactStaticEntities) > 0) && !redactPiiEnabled {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "'redactPiiReturnUnredacted' and 'redactStaticEntities' require 'redactPii' to be enabled; AssemblyAI rejects the request otherwise.",
			})
		}
		redactPiiAudioEnabled := aaiOpts.RedactPiiAudio != nil && *aaiOpts.RedactPiiAudio
		if aaiOpts.RedactPiiAudioOptions != nil && !redactPiiAudioEnabled {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "'redactPiiAudioOptions' only applies when 'redactPiiAudio' is enabled; it is otherwise ignored.",
			})
		}
		languageDetectionEnabled := aaiOpts.LanguageDetection != nil && *aaiOpts.LanguageDetection
		if aaiOpts.LanguageCode != "" && languageDetectionEnabled {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "'languageDetection' cannot be combined with an explicit 'languageCode'; AssemblyAI rejects requests that set both.",
			})
		}
	}

	return body, warnings
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

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}

	// Step 1: Upload audio.
	var uploadResult struct {
		UploadURL string `json:"upload_url"`
	}
	_, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/upload",
		Body:   opts.Audio,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": "application/octet-stream",
		}),
	}, &uploadResult)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Step 2: Submit transcription job.
	body, warnings := m.buildRequestBody(opts)
	body["audio_url"] = uploadResult.UploadURL

	var submitResult struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/transcript",
		Body:    body,
		Headers: opts.Headers,
	}, &submitResult)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Step 3: Poll for completion.
	currentDate := time.Now()
	rawTranscript, httpResp, err := m.waitForCompletion(ctx, submitResult.ID, opts.Headers)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(rawTranscript, currentDate, httpResp.Headers, warnings), nil
}

// waitForCompletion polls the given transcript until it reaches a terminal
// status, mirroring AssemblyAITranscriptionModel.waitForCompletion.
func (m *TranscriptionModel) waitForCompletion(ctx context.Context, transcriptID string, headers map[string]string) (map[string]interface{}, *internalhttp.Response, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		default:
		}

		var raw map[string]interface{}
		resp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
			Method:  http.MethodGet,
			Path:    "/transcript/" + transcriptID,
			Headers: headers,
		}, &raw)
		if err != nil {
			return nil, nil, m.handleError(err)
		}

		status, _ := raw["status"].(string)
		switch status {
		case "completed":
			return raw, resp, nil
		case "error":
			errMsg, _ := raw["error"].(string)
			if errMsg == "" {
				errMsg = "Unknown error"
			}
			return nil, nil, fmt.Errorf("assemblyai: transcription failed: %s", errMsg)
		}

		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(pollingInterval):
		}
	}
}

// convertResponse maps a raw completed transcript response into a
// TranscriptionResult, mirroring AssemblyAITranscriptionModel.doGenerate.
func (m *TranscriptionModel) convertResponse(raw map[string]interface{}, currentDate time.Time, headers http.Header, warnings []types.Warning) *types.TranscriptionResult {
	b, _ := json.Marshal(raw)
	var transcript struct {
		Text          string   `json:"text"`
		LanguageCode  string   `json:"language_code"`
		AudioDuration *float64 `json:"audio_duration"`
		Words         []struct {
			Text  string  `json:"text"`
			Start float64 `json:"start"`
			End   float64 `json:"end"`
		} `json:"words"`
	}
	_ = json.Unmarshal(b, &transcript)

	segments := make([]types.TranscriptionTimestamp, 0, len(transcript.Words))
	for _, word := range transcript.Words {
		segments = append(segments, types.TranscriptionTimestamp{
			Text: word.Text,
			// AssemblyAI returns word timings in milliseconds; the AI SDK
			// reports segment timings in seconds.
			Start: word.Start / 1000,
			End:   word.End / 1000,
		})
	}

	durationInSeconds := transcript.AudioDuration
	if durationInSeconds == nil && len(transcript.Words) > 0 {
		last := transcript.Words[len(transcript.Words)-1].End / 1000
		durationInSeconds = &last
	}

	// Surface diarization and audio-intelligence results that the AI SDK's
	// `segments` shape can't represent, keyed under `assemblyai`. NOTE:
	// timings inside these objects (e.g. utterances[].start) are in
	// milliseconds, matching the AssemblyAI API — unlike the top-level
	// segments, whose start/end are in seconds.
	metadata := map[string]interface{}{}
	if v, ok := raw["utterances"]; ok && v != nil {
		metadata["utterances"] = v
	}
	if v, ok := raw["sentiment_analysis_results"]; ok && v != nil {
		metadata["sentimentAnalysisResults"] = v
	}
	if v, ok := raw["entities"]; ok && v != nil {
		metadata["entities"] = v
	}
	if v, ok := raw["content_safety_labels"]; ok && v != nil {
		metadata["contentSafetyLabels"] = v
	}
	if v, ok := raw["iab_categories_result"]; ok && v != nil {
		metadata["iabCategoriesResult"] = v
	}
	if v, ok := raw["auto_highlights_result"]; ok && v != nil {
		metadata["autoHighlightsResult"] = v
	}

	result := &types.TranscriptionResult{
		Text:              transcript.Text,
		Segments:          segments,
		Timestamps:        segments,
		Language:          transcript.LanguageCode,
		DurationInSeconds: durationInSeconds,
		Warnings:          warnings,
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationValue(durationInSeconds),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(headers),
			Body:      raw,
		},
	}
	if len(metadata) > 0 {
		result.ProviderMetadata = map[string]interface{}{
			"assemblyai": metadata,
		}
	}
	return result
}

func durationValue(duration *float64) float64 {
	if duration == nil {
		return 0
	}
	return *duration
}

// handleError converts an HTTP error into a ProviderError, parsing
// AssemblyAI's `{"error":{"message","code"}}` error shape when present.
func (m *TranscriptionModel) handleError(err error) error {
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
		providerErr := providererrors.NewProviderError("assemblyai", statusErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		providerErr.ResponseBody = string(statusErr.Body)
		return providerErr
	}
	return providererrors.NewProviderError("assemblyai", 0, "", err.Error(), err)
}
