package assemblyai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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
	return "assemblyai.transcription"
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
	rawOpts := rawProviderOptions(opts.ProviderOptions)
	setStr := func(dst map[string]interface{}, key string, path ...string) {
		if v, ok := stringOpt(rawOpts, path...); ok {
			dst[key] = v
		}
	}

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
		setStr(body, "boost_param", "boostParam")
		setOptional(body, "content_safety", aaiOpts.ContentSafety)
		setOptional(body, "content_safety_confidence", aaiOpts.ContentSafetyConfidence)
		if aaiOpts.CustomSpelling != nil {
			body["custom_spelling"] = aaiOpts.CustomSpelling
		}
		setOptional(body, "disfluencies", aaiOpts.Disfluencies)
		setOptional(body, "entity_detection", aaiOpts.EntityDetection)
		setOptional(body, "filter_profanity", aaiOpts.FilterProfanity)
		setOptional(body, "format_text", aaiOpts.FormatText)
		setOptional(body, "iab_categories", aaiOpts.IabCategories)
		if aaiOpts.KeytermsPrompt != nil {
			body["keyterms_prompt"] = aaiOpts.KeytermsPrompt
		}
		setStr(body, "language_code", "languageCode")
		setOptional(body, "language_confidence_threshold", aaiOpts.LanguageConfidenceThreshold)
		setOptional(body, "language_detection", aaiOpts.LanguageDetection)
		setOptional(body, "multichannel", aaiOpts.Multichannel)
		setStr(body, "prompt", "prompt")
		setOptional(body, "punctuate", aaiOpts.Punctuate)
		setOptional(body, "redact_pii", aaiOpts.RedactPii)
		setOptional(body, "redact_pii_audio", aaiOpts.RedactPiiAudio)
		setStr(body, "redact_pii_audio_quality", "redactPiiAudioQuality")
		if aaiOpts.RedactPiiPolicies != nil {
			body["redact_pii_policies"] = aaiOpts.RedactPiiPolicies
		}
		setOptional(body, "redact_pii_return_unredacted", aaiOpts.RedactPiiReturnUnredacted)
		setStr(body, "redact_pii_sub", "redactPiiSub")
		if aaiOpts.RedactStaticEntities != nil {
			body["redact_static_entities"] = aaiOpts.RedactStaticEntities
		}
		setStr(body, "remove_audio_tags", "removeAudioTags")
		setOptional(body, "sentiment_analysis", aaiOpts.SentimentAnalysis)
		setOptional(body, "speaker_labels", aaiOpts.SpeakerLabels)
		setOptional(body, "speakers_expected", aaiOpts.SpeakersExpected)
		setOptional(body, "speech_threshold", aaiOpts.SpeechThreshold)
		setOptional(body, "summarization", aaiOpts.Summarization)
		setStr(body, "summary_model", "summaryModel")
		setStr(body, "summary_type", "summaryType")
		setOptional(body, "temperature", aaiOpts.Temperature)
		setStr(body, "webhook_auth_header_name", "webhookAuthHeaderName")
		setStr(body, "webhook_auth_header_value", "webhookAuthHeaderValue")
		setStr(body, "webhook_url", "webhookUrl")
		if aaiOpts.WordBoost != nil {
			body["word_boost"] = aaiOpts.WordBoost
		}
		setStr(body, "domain", "domain")

		if aaiOpts.SpeakerOptions != nil {
			speakerOptions := map[string]interface{}{}
			setOptional(speakerOptions, "min_speakers_expected", aaiOpts.SpeakerOptions.MinSpeakersExpected)
			setOptional(speakerOptions, "max_speakers_expected", aaiOpts.SpeakerOptions.MaxSpeakersExpected)
			body["speaker_options"] = speakerOptions
		}

		if aaiOpts.LanguageDetectionOptions != nil {
			ldo := map[string]interface{}{}
			if aaiOpts.LanguageDetectionOptions.ExpectedLanguages != nil {
				ldo["expected_languages"] = aaiOpts.LanguageDetectionOptions.ExpectedLanguages
			}
			setStr(ldo, "fallback_language", "languageDetectionOptions", "fallbackLanguage")
			setOptional(ldo, "code_switching", aaiOpts.LanguageDetectionOptions.CodeSwitching)
			setOptional(ldo, "code_switching_confidence_threshold", aaiOpts.LanguageDetectionOptions.CodeSwitchingConfidenceThreshold)
			body["language_detection_options"] = ldo
		}

		if aaiOpts.RedactPiiAudioOptions != nil {
			rpao := map[string]interface{}{}
			setOptional(rpao, "return_redacted_no_speech_audio", aaiOpts.RedactPiiAudioOptions.ReturnRedactedNoSpeechAudio)
			setStr(rpao, "override_audio_redaction_method", "redactPiiAudioOptions", "overrideAudioRedactionMethod")
			body["redact_pii_audio_options"] = rpao
		}

		var deprecatedBoostOptions []string
		if aaiOpts.WordBoost != nil {
			deprecatedBoostOptions = append(deprecatedBoostOptions, "wordBoost")
		}
		if _, ok := stringOpt(rawOpts, "boostParam"); ok {
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
		if (aaiOpts.RedactPiiReturnUnredacted != nil || aaiOpts.RedactStaticEntities != nil) && !redactPiiEnabled {
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
		if _, ok := stringOpt(rawOpts, "languageCode"); ok && languageDetectionEnabled {
			warnings = append(warnings, types.Warning{
				Type:    "other",
				Message: "'languageDetection' cannot be combined with an explicit 'languageCode'; AssemblyAI rejects requests that set both.",
			})
		}
	}

	return body, warnings
}

// rawProviderOptions returns the untyped "assemblyai" options so string
// options set to "" (kept by TS `?? undefined`) can be told apart from absent.
func rawProviderOptions(providerOptions map[string]interface{}) map[string]interface{} {
	raw, _ := providerOptions["assemblyai"].(map[string]interface{})
	if raw == nil {
		if v, ok := providerOptions["assemblyai"]; ok && v != nil {
			b, err := json.Marshal(v)
			if err == nil {
				_ = json.Unmarshal(b, &raw)
			}
		}
	}
	return raw
}

// stringOpt reports the string at raw[path...] and whether it is present.
func stringOpt(raw map[string]interface{}, path ...string) (string, bool) {
	var cur interface{} = raw
	for _, k := range path {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return "", false
		}
		cur = m[k]
	}
	s, ok := cur.(string)
	return s, ok
}

func setOptional[T any](body map[string]interface{}, key string, value *T) {
	if value != nil {
		body[key] = *value
	}
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	// TS: currentDate is taken before the upload.
	currentDate := time.Now()

	// Step 1: Upload audio.
	var uploadResult struct {
		UploadURL string `json:"upload_url"`
	}
	_, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaAssemblyAIUpload),
		Method:           http.MethodPost,
		Path:             "/upload",
		Body:             uploadContent(opts),
		// TS: { 'Content-Type': ..., ...combineHeaders(config, options.headers) },
		// so a caller header (any case matches in a map; exact key here) wins.
		Headers: internalhttp.MergeHeaders(map[string]string{
			"Content-Type": "application/octet-stream",
		}, opts.Headers),
	}, &uploadResult)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Step 2: Submit transcription job.
	if err := validateOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	body, warnings := m.buildRequestBody(opts)
	body["audio_url"] = uploadResult.UploadURL

	var submitResult struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaAssemblyAISubmit),
		Method:           http.MethodPost,
		Path:             "/transcript",
		Body:             orderedTranscriptBody(body),
		Headers:          opts.Headers,
	}, &submitResult)
	if err != nil {
		return nil, m.handleError(err)
	}

	// Step 3: Poll for completion.
	rawTranscript, httpResp, err := m.waitForCompletion(ctx, submitResult.ID, opts.Headers)
	if err != nil {
		return nil, err
	}

	return m.convertResponse(rawTranscript, currentDate, httpResp.Headers, warnings), nil
}

// uploadContent is TS postToApi's body.content (options.audio): the audio
// bytes, or a base64 string audio as given (TS sends the string itself).
func uploadContent(opts *provider.TranscriptionOptions) []byte {
	if len(opts.Audio) == 0 && opts.AudioBase64 != "" {
		return []byte(opts.AudioBase64)
	}
	return opts.Audio
}

// waitForCompletion polls the given transcript until it reaches a terminal
// status, mirroring AssemblyAITranscriptionModel.waitForCompletion.
func (m *TranscriptionModel) waitForCompletion(ctx context.Context, transcriptID string, headers map[string]string) (map[string]interface{}, *internalhttp.Response, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, nil, abortedError(ctx)
		default:
		}

		raw, resp, err := m.pollTranscript(ctx, transcriptID, headers)
		if err != nil {
			return nil, nil, err
		}

		status, _ := raw["status"].(string)
		switch status {
		case "completed":
			return raw, resp, nil
		case "error":
			// TS `transcript.error ?? 'Unknown error'`: only null/absent falls
			// back, an empty string is kept.
			errMsg, ok := raw["error"].(string)
			if !ok {
				errMsg = "Unknown error"
			}
			return nil, nil, fmt.Errorf("Transcription failed: %s", errMsg) //nolint:staticcheck // matches TS SDK's exact error text
		}

		select {
		case <-ctx.Done():
			return nil, nil, abortedError(ctx)
		case <-time.After(pollingInterval):
		}
	}
}

// defaultBaseURL is the API base URL New uses when Config.BaseURL is empty.
const defaultBaseURL = "https://api.assemblyai.com/v2"

// pollTranscript is one TS waitForCompletion poll. TS calls the fetch
// function directly (no getFromApi): the headers are combineHeaders(config
// headers, call headers) with no Content-Type and no runtime user-agent tag,
// and a fetch rejection propagates as is instead of becoming the retryable
// "Cannot connect to API" APICallError (so core does not retry the whole
// transcription). A non-ok response goes through
// assemblyaiFailedResponseHandler; the body is parsed with schema.parse, so
// a mismatch is the validation error itself, not "Invalid JSON response".
func (m *TranscriptionModel) pollTranscript(ctx context.Context, transcriptID string, headers map[string]string) (map[string]interface{}, *internalhttp.Response, error) {
	base := m.provider.config.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	pollURL := base + "/transcript/" + transcriptID
	configHeaders, err := m.provider.client.RequestHeadersCtx(ctx)
	if err != nil {
		return nil, nil, err // TS: resolve(config.headers) throws as is (LoadAPIKeyError)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
	if err != nil {
		return nil, nil, err
	}
	userContentType := false
	for k := range m.provider.config.Headers {
		if strings.EqualFold(k, "Content-Type") {
			userContentType = true
		}
	}
	for k, v := range configHeaders {
		// The client's JSON Content-Type default is for the POST requests;
		// the TS config headers have none.
		if strings.EqualFold(k, "Content-Type") && !userContentType {
			continue
		}
		req.Header.Set(k, v)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	httpResp, err := internalhttp.WithFetchRedirects(m.provider.client.HTTPClient()).Do(req)
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, nil, cause
		}
		return nil, nil, err
	}
	defer httpResp.Body.Close() //nolint:errcheck
	body, err := fileutil.ReadResponseWithSizeLimit(httpResp, pollURL, fileutil.DefaultMaxDownloadSize)
	if err != nil {
		return nil, nil, err
	}
	// TS response.text() / response.json(): UTF-8 decoded, BOM dropped.
	body = []byte(textdecode.UTF8(body))
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return nil, nil, m.handleError(&internalhttp.HTTPStatusError{StatusCode: httpResp.StatusCode, Headers: httpResp.Header, Body: body})
	}
	// TS `await response.json()` then schema.parse: plain JSON.parse keeps
	// a __proto__ key as an ordinary property.
	if err := providerutils.MediaSchemaParserPlain(providerutils.SchemaAssemblyAITranscript)(httpResp.StatusCode, httpResp.Header, body); err != nil {
		return nil, nil, err
	}
	var raw map[string]interface{}
	if err := jsonexact.UnmarshalPlain(body, &raw); err != nil {
		return nil, nil, err
	}
	return raw, &internalhttp.Response{StatusCode: httpResp.StatusCode, Headers: httpResp.Header, Body: body}, nil
}

// transcriptionAbortedError is TS waitForCompletion's
// Error('Transcription request was aborted') for an abort seen between
// polls (assemblyai-transcription-model.ts:289). It wraps the context error,
// so errors.Is(err, context.Canceled) keeps working.
type transcriptionAbortedError struct{ cause error }

func (e *transcriptionAbortedError) Error() string { return "Transcription request was aborted" }
func (e *transcriptionAbortedError) Unwrap() error { return e.cause }

func abortedError(ctx context.Context) error {
	return &transcriptionAbortedError{cause: ctx.Err()}
}

// transcriptBodyKeyOrder is the TS getArgs assignment order of the submit
// body, then doGenerate's audio_url.
var transcriptBodyKeyOrder = []string{
	"speech_model", "speech_models",
	"audio_end_at", "audio_start_from", "auto_chapters", "auto_highlights", "boost_param",
	"content_safety", "content_safety_confidence", "custom_spelling", "disfluencies",
	"entity_detection", "filter_profanity", "format_text", "iab_categories", "language_code",
	"language_confidence_threshold", "language_detection", "multichannel", "punctuate",
	"redact_pii", "redact_pii_audio", "redact_pii_audio_quality", "redact_pii_policies",
	"redact_pii_sub", "sentiment_analysis", "speaker_labels", "speakers_expected",
	"speech_threshold", "summarization", "summary_model", "summary_type",
	"webhook_auth_header_name", "webhook_auth_header_value", "webhook_url", "word_boost",
	"keyterms_prompt", "prompt", "temperature", "remove_audio_tags", "domain",
	"redact_pii_return_unredacted", "redact_static_entities",
	"speaker_options", "language_detection_options", "redact_pii_audio_options",
	"audio_url",
}

// transcriptNestedKeyOrder is the TS literal order of the nested objects.
var transcriptNestedKeyOrder = map[string][]string{
	"speaker_options":            {"min_speakers_expected", "max_speakers_expected"},
	"language_detection_options": {"expected_languages", "fallback_language", "code_switching", "code_switching_confidence_threshold"},
	"redact_pii_audio_options":   {"return_redacted_no_speech_audio", "override_audio_redaction_method"},
}

// orderedTranscriptBody sends the submit body in the TS JSON.stringify
// order instead of Go's sorted map keys.
func orderedTranscriptBody(body map[string]interface{}) *jsonorder.Object {
	out := jsonorder.New()
	for _, k := range transcriptBodyKeyOrder {
		v, ok := body[k]
		if !ok {
			continue
		}
		if order, nested := transcriptNestedKeyOrder[k]; nested {
			if m, isMap := v.(map[string]interface{}); isMap {
				o := jsonorder.New()
				for _, nk := range order {
					if nv, has := m[nk]; has {
						o.Set(nk, nv)
					}
				}
				v = o
			}
		}
		out.Set(k, v)
	}
	return out
}

// convertResponse maps a raw completed transcript response into a
// TranscriptionResult, mirroring AssemblyAITranscriptionModel.doGenerate.
func (m *TranscriptionModel) convertResponse(raw map[string]interface{}, currentDate time.Time, headers http.Header, warnings []types.Warning) *types.TranscriptionResult {
	b, _ := jsonstr.Marshal(raw) // a provider ±Inf writes null (JSON.stringify)
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
	// The in-memory transcript of `await response.json()`: a __proto__ key
	// is an ordinary property there.
	_ = jsonexact.UnmarshalPlain(b, &transcript)

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

// assemblyAIErrorMessage is TS assemblyaiFailedResponseHandler (c87b19ee75):
// the documented flat `{"error": "<message>"}` shape gives that message; the
// older nested `{"error": {"message", "code"}}` shape is still accepted;
// anything else gives the HTTP status text.
func assemblyAIErrorMessage(statusCode int, body []byte) (message, code string) {
	var flat struct {
		Error json.RawMessage `json:"error"`
	}
	if securejson.Unmarshal(body, &flat) == nil && len(flat.Error) > 0 && flat.Error[0] == '"' {
		var text string
		if json.Unmarshal(flat.Error, &text) == nil {
			return text, ""
		}
	}
	return providerutils.NestedErrorMessage(statusCode, body)
}

// handleError converts an HTTP error into a ProviderError, parsing
// AssemblyAI's `{"error": "..."}` / `{"error":{"message","code"}}` error
// shapes when present.
func (m *TranscriptionModel) handleError(err error) error {
	if cause, ok := internalhttp.FetchRejectionCause(err); ok {
		return cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
	}
	if noResp, ok := internalhttp.NoResponseError(err); ok {
		return noResp // TS handleFetchError: an abort or timeout is returned unchanged
	}
	if providererrors.IsLoadAPIKeyError(err) {
		return err // TS throws LoadAPIKeyError unwrapped
	}
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message, code := assemblyAIErrorMessage(statusErr.StatusCode, statusErr.Body)
		perr := providererrors.NewProviderErrorFromResponse("assemblyai", 0, code, message, err)
		perr.Data = assemblyAIErrorData(statusErr.Body)
		return perr
	}
	var validationErr *providererrors.ValidationError
	if errors.As(err, &validationErr) {
		return err // TS throws the schema.parse failure itself
	}
	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// assemblyAIErrorData is the zod output of assemblyaiErrorDataSchema for a
// matching body ({error: string} or {error: {message, code}}, undeclared keys
// stripped), else nil (TS APICallError.data is undefined).
func assemblyAIErrorData(body []byte) interface{} {
	var obj map[string]interface{}
	if securejson.Unmarshal(body, &obj) != nil {
		return nil
	}
	switch e := obj["error"].(type) {
	case string:
		return map[string]interface{}{"error": e}
	case map[string]interface{}:
		message, okMessage := e["message"].(string)
		code, okCode := e["code"].(float64)
		if okMessage && okCode && !math.IsInf(code, 0) && !math.IsNaN(code) { // z.number() rejects Infinity
			return map[string]interface{}{"error": map[string]interface{}{"message": message, "code": code}}
		}
	}
	return nil
}
