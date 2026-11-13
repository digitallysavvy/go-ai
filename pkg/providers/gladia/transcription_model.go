package gladia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"math"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// defaultPollTimeout and defaultPollInterval are TS DEFAULT_POLL_TIMEOUT_MS
// (10 minutes: long audio and add-ons take several minutes) and
// DEFAULT_POLL_INTERVAL_MS.
const (
	defaultPollTimeout  = 10 * time.Minute
	defaultPollInterval = 1 * time.Second
)

// gladiaPollSetting resolves a polling value: the per-call option (a
// validated positive integer of milliseconds), then the provider setting,
// then the default.
func gladiaPollSetting(g map[string]interface{}, key string, configured, fallback time.Duration) time.Duration {
	if ms, ok := g[key].(float64); ok && ms > 0 {
		if ms >= float64(math.MaxInt64/int64(time.Millisecond)) {
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(ms) * time.Millisecond
	}
	if configured > 0 {
		return configured
	}
	return fallback
}

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
	return "gladia.transcription"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// gladiaOptionsObject returns providerOptions["gladia"] as a JSON object
// read through its JSON form (a map, or a TranscriptionModelOptions value or
// pointer), so slices are []interface{} and numbers float64 as in the
// parsed TS options. Absent or null gives (nil, false).
func gladiaOptionsObject(providerOptions map[string]interface{}) (map[string]interface{}, bool) {
	raw, ok := providerOptions["gladia"]
	if !ok || raw == nil {
		return nil, false
	}
	b, err := json.Marshal(raw)
	if err != nil {
		m, isMap := raw.(map[string]interface{})
		return m, isMap
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil || m == nil {
		return nil, false
	}
	return m, true
}

// normalizeGladiaOptions replaces a typed TranscriptionModelOptions value
// with its JSON object so the option schema validates it like a map.
func normalizeGladiaOptions(providerOptions map[string]interface{}) map[string]interface{} {
	switch providerOptions["gladia"].(type) {
	case TranscriptionModelOptions, *TranscriptionModelOptions:
	default:
		return providerOptions
	}
	m, ok := gladiaOptionsObject(providerOptions)
	if !ok {
		return providerOptions
	}
	out := make(map[string]interface{}, len(providerOptions))
	for k, v := range providerOptions {
		out[k] = v
	}
	out["gladia"] = m
	return out
}

// gladiaPresent is TS `value != null` for an object member.
func gladiaPresent(m map[string]interface{}, key string) (interface{}, bool) {
	v, ok := m[key]
	return v, ok && v != nil
}

// copyPresent sets body[to] = m[from] when it is not null or absent (TS
// `x ?? undefined`).
func copyPresent(body, m map[string]interface{}, to, from string) {
	if v, ok := gladiaPresent(m, from); ok {
		body[to] = v
	}
}

// gladiaModelSent reports whether a model ID goes into the request body.
// Coordinator ruling (round 88, S6): "" and "default" are omitted (the
// Gladia default model), solaria-* IDs are sent as TS does, and any other ID
// keeps the v0.5.2 behavior (not sent) with an unsupported warning, because
// v0.5.2 documented that the model ID is not sent and its examples passed
// IDs Gladia rejects.
func gladiaModelSent(modelID string) bool {
	return strings.HasPrefix(modelID, "solaria-")
}

// gladiaEffectiveModelID is the model ID TS uses: gladia.transcription()
// with no ID is 'default'; Go's "" means the same.
func gladiaEffectiveModelID(modelID string) string {
	if modelID == "" {
		return "default"
	}
	return modelID
}

// getArgs builds the /v2/pre-recorded body (without audio_url) and the call
// warnings from the validated options object, mirroring TS
// GladiaTranscriptionModel.getArgs (c8875b71a3).
func (m *TranscriptionModel) getArgs(g map[string]interface{}) (map[string]interface{}, []types.Warning) {
	body := map[string]interface{}{}
	warnings := []types.Warning{}
	modelID := gladiaEffectiveModelID(m.modelID)

	if modelID != "default" {
		if gladiaModelSent(modelID) {
			body["model"] = modelID
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "modelId",
				Details: fmt.Sprintf("The model ID %q is not sent to Gladia; only solaria-* model IDs are forwarded, so the Gladia default model is used.", modelID),
			})
		}
	}

	if g != nil {
		diarizationConfig, _ := g["diarizationConfig"].(map[string]interface{})
		for _, u := range []struct {
			feature string
			value   interface{}
		}{
			{"contextPrompt", g["contextPrompt"]},
			{"moderation", g["moderation"]},
			{"chapterization", g["chapterization"]},
			{"nameConsistency", g["nameConsistency"]},
			{"structuredDataExtraction", g["structuredDataExtraction"]},
			{"structuredDataExtractionConfig", g["structuredDataExtractionConfig"]},
			{"displayMode", g["displayMode"]},
			{"diarizationConfig.enhanced", diarizationConfig["enhanced"]},
		} {
			if u.value != nil {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: u.feature,
					Details: "No longer supported by the Gladia v2 pre-recorded API; ignored.",
				})
			}
		}
		_, hasLanguageConfig := gladiaPresent(g, "languageConfig")
		for _, setting := range []string{"language", "detectLanguage", "enableCodeSwitching", "codeSwitchingConfig"} {
			if _, ok := gladiaPresent(g, setting); ok {
				message := "Use languageConfig instead."
				if hasLanguageConfig {
					message = "Ignored because languageConfig is provided; remove this deprecated option."
				}
				warnings = append(warnings, types.Warning{Type: "deprecated", Setting: setting, Message: message})
			}
		}

		customVocabulary, vocabularyFromArray, haveArrayVocabulary := resolveGladiaCustomVocabulary(g, &warnings)
		if customVocabulary != nil {
			body["custom_vocabulary"] = *customVocabulary
		}
		copyPresent(body, g, "callback", "callback")
		copyPresent(body, g, "subtitles", "subtitles")
		copyPresent(body, g, "diarization", "diarization")
		copyPresent(body, g, "translation", "translation")
		copyPresent(body, g, "summarization", "summarization")
		copyPresent(body, g, "named_entity_recognition", "namedEntityRecognition")
		copyPresent(body, g, "custom_spelling", "customSpelling")
		copyPresent(body, g, "sentiment_analysis", "sentimentAnalysis")
		copyPresent(body, g, "audio_to_llm", "audioToLlm")
		copyPresent(body, g, "pii_redaction", "piiRedaction")
		copyPresent(body, g, "custom_metadata", "customMetadata")
		copyPresent(body, g, "sentences", "sentences")
		copyPresent(body, g, "punctuation_enhanced", "punctuationEnhanced")

		customVocabularyConfig, _ := g["customVocabularyConfig"].(map[string]interface{})
		vocabulary, haveVocabulary := gladiaPresent(customVocabularyConfig, "vocabulary")
		if !haveVocabulary && haveArrayVocabulary {
			vocabulary, haveVocabulary = vocabularyFromArray, true
		}
		if items, ok := vocabulary.([]interface{}); haveVocabulary && ok {
			mapped := make([]interface{}, 0, len(items))
			for _, item := range items {
				obj, isObj := item.(map[string]interface{})
				if !isObj {
					mapped = append(mapped, item)
					continue
				}
				entry := map[string]interface{}{"value": obj["value"]}
				for _, k := range []string{"intensity", "pronunciations", "language"} {
					copyPresent(entry, obj, k, k)
				}
				mapped = append(mapped, entry)
			}
			cfg := map[string]interface{}{"vocabulary": mapped}
			copyPresent(cfg, customVocabularyConfig, "default_intensity", "defaultIntensity")
			body["custom_vocabulary_config"] = cfg
		}

		if languages, codeSwitching, ok := resolveGladiaLanguageConfig(g); ok {
			cfg := map[string]interface{}{}
			if languages != nil {
				cfg["languages"] = languages
			}
			if codeSwitching != nil {
				cfg["code_switching"] = codeSwitching
			}
			body["language_config"] = cfg
		}

		if c, ok := g["callbackConfig"].(map[string]interface{}); ok {
			cfg := map[string]interface{}{"url": c["url"]}
			copyPresent(cfg, c, "method", "method")
			body["callback_config"] = cfg
		}
		if c, ok := g["subtitlesConfig"].(map[string]interface{}); ok {
			cfg := map[string]interface{}{}
			copyPresent(cfg, c, "formats", "formats")
			copyPresent(cfg, c, "minimum_duration", "minimumDuration")
			copyPresent(cfg, c, "maximum_duration", "maximumDuration")
			copyPresent(cfg, c, "maximum_characters_per_row", "maximumCharactersPerRow")
			copyPresent(cfg, c, "maximum_rows_per_caption", "maximumRowsPerCaption")
			copyPresent(cfg, c, "style", "style")
			body["subtitles_config"] = cfg
		}
		if diarizationConfig != nil {
			cfg := map[string]interface{}{}
			copyPresent(cfg, diarizationConfig, "number_of_speakers", "numberOfSpeakers")
			copyPresent(cfg, diarizationConfig, "min_speakers", "minSpeakers")
			copyPresent(cfg, diarizationConfig, "max_speakers", "maxSpeakers")
			body["diarization_config"] = cfg
		}
		if c, ok := g["translationConfig"].(map[string]interface{}); ok {
			cfg := map[string]interface{}{"target_languages": c["targetLanguages"]}
			copyPresent(cfg, c, "model", "model")
			copyPresent(cfg, c, "match_original_utterances", "matchOriginalUtterances")
			copyPresent(cfg, c, "lipsync", "lipsync")
			copyPresent(cfg, c, "context_adaptation", "contextAdaptation")
			copyPresent(cfg, c, "context", "context")
			copyPresent(cfg, c, "informal", "informal")
			body["translation_config"] = cfg
		}
		if c, ok := g["summarizationConfig"].(map[string]interface{}); ok {
			cfg := map[string]interface{}{}
			copyPresent(cfg, c, "type", "type")
			body["summarization_config"] = cfg
		}
		if c, ok := g["customSpellingConfig"].(map[string]interface{}); ok {
			body["custom_spelling_config"] = map[string]interface{}{"spelling_dictionary": c["spellingDictionary"]}
		}
		if c, ok := g["audioToLlmConfig"].(map[string]interface{}); ok {
			cfg := map[string]interface{}{"prompts": c["prompts"]}
			copyPresent(cfg, c, "model", "model")
			body["audio_to_llm_config"] = cfg
		}
		if c, ok := g["piiRedactionConfig"].(map[string]interface{}); ok {
			if enabled, _ := g["piiRedaction"].(bool); !enabled {
				warnings = append(warnings, types.Warning{Type: "other", Message: "piiRedactionConfig has no effect unless piiRedaction is true."})
			}
			cfg := map[string]interface{}{}
			copyPresent(cfg, c, "entity_types", "entityTypes")
			copyPresent(cfg, c, "processed_text_type", "processedTextType")
			body["pii_redaction_config"] = cfg
		}
	}

	if modelID == "solaria-3" {
		languageConfig, _ := body["language_config"].(map[string]interface{})
		if languages, ok := languageConfig["languages"].([]interface{}); !ok || len(languages) != 1 {
			warnings = append(warnings, types.Warning{Type: "other", Message: `The "solaria-3" model supports a single language only. Pass exactly one language in languageConfig.languages; Gladia may reject the request.`})
		}
		if codeSwitching, _ := languageConfig["code_switching"].(bool); codeSwitching {
			warnings = append(warnings, types.Warning{Type: "other", Message: `The "solaria-3" model does not support code switching; Gladia may reject the request.`})
		}
	}

	return body, warnings
}

// resolveGladiaLanguageConfig is TS resolveLanguageConfig
// (gladia-transcription-options-resolution.ts): languageConfig wins as
// given; otherwise the deprecated language options are translated. ok is
// false when neither languages nor codeSwitching resolves.
func resolveGladiaLanguageConfig(g map[string]interface{}) (languages []interface{}, codeSwitching interface{}, ok bool) {
	if lc, present := gladiaPresent(g, "languageConfig"); present {
		obj, _ := lc.(map[string]interface{})
		if v, ok := gladiaPresent(obj, "languages"); ok {
			languages, _ = v.([]interface{})
		}
		if v, ok := gladiaPresent(obj, "codeSwitching"); ok {
			codeSwitching = v
		}
		return languages, codeSwitching, true
	}
	if v, ok := gladiaPresent(g, "enableCodeSwitching"); ok {
		codeSwitching = v
	}
	codeSwitchingConfig, _ := g["codeSwitchingConfig"].(map[string]interface{})
	switch {
	case codeSwitching == true && codeSwitchingConfig["languages"] != nil:
		languages, _ = codeSwitchingConfig["languages"].([]interface{})
		if languages == nil {
			languages = []interface{}{}
		}
	case g["language"] != nil:
		languages = []interface{}{g["language"]}
	case providerutils.JSTruthy(g["detectLanguage"]):
		languages = []interface{}{}
	}
	if languages == nil && codeSwitching == nil {
		return nil, nil, false
	}
	return languages, codeSwitching, true
}

// resolveGladiaCustomVocabulary is TS resolveCustomVocabulary: the
// deprecated array form is translated to custom_vocabulary: true plus a
// filtered vocabulary list, with its warnings; a boolean passes through.
func resolveGladiaCustomVocabulary(g map[string]interface{}, warnings *[]types.Warning) (customVocabulary *bool, vocabulary []interface{}, haveVocabulary bool) {
	t := true
	switch v := g["customVocabulary"].(type) {
	case bool:
		return &v, nil, false
	case []interface{}:
		*warnings = append(*warnings, types.Warning{Type: "deprecated", Setting: "customVocabulary", Message: "Use customVocabulary: true with customVocabularyConfig instead of an array."})
		if cfg, _ := g["customVocabularyConfig"].(map[string]interface{}); cfg["vocabulary"] != nil {
			*warnings = append(*warnings, types.Warning{Type: "other", Message: "customVocabularyConfig.vocabulary overrides the deprecated customVocabulary array."})
			return &t, nil, false
		}
		kept := make([]interface{}, 0, len(v))
		for _, item := range v {
			if gladiaVocabularyEntryValid(item) {
				kept = append(kept, item)
			}
		}
		if len(kept) != len(v) {
			*warnings = append(*warnings, types.Warning{Type: "other", Message: "Invalid customVocabulary entries were ignored; use strings or objects with a string value and correctly typed optional fields."})
		}
		if len(kept) > 0 {
			return &t, kept, true
		}
		return nil, nil, false
	}
	return nil, nil, false
}

// gladiaVocabularyEntryValid is the TS resolveCustomVocabulary entry filter.
func gladiaVocabularyEntryValid(item interface{}) bool {
	if _, ok := item.(string); ok {
		return true
	}
	obj, ok := item.(map[string]interface{})
	if !ok {
		return false
	}
	if _, ok := obj["value"].(string); !ok {
		return false
	}
	if v := obj["intensity"]; v != nil {
		f, ok := v.(float64)
		if !ok || math.IsInf(f, 0) || math.IsNaN(f) {
			return false
		}
	}
	if v := obj["pronunciations"]; v != nil {
		list, ok := v.([]interface{})
		if !ok {
			return false
		}
		for _, p := range list {
			if _, ok := p.(string); !ok {
				return false
			}
		}
	}
	if v := obj["language"]; v != nil {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// DoTranscribe performs the upload -> start job -> poll flow.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	currentDate := time.Now()

	// TS createGladia validates the polling settings; Go's New cannot fail,
	// so they are checked here, before any request.
	if err := validatePollingSettings(m.provider.config); err != nil {
		return nil, err
	}

	// TS getArgs (option parsing, validation and the body) runs before the
	// upload, so an invalid option sends no request.
	providerOptions := normalizeGladiaOptions(opts.ProviderOptions)
	if err := validateOptions(providerOptions); err != nil {
		return nil, err
	}
	gladiaOptions, _ := gladiaOptionsObject(providerOptions)
	body, warnings := m.getArgs(gladiaOptions)

	// Step 1: upload the audio file.
	uploadBody, contentType, err := buildUploadMultipart(opts)
	if err != nil {
		return nil, err
	}
	var uploadResult struct {
		AudioURL string `json:"audio_url"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaGladiaUpload),
		Method:           http.MethodPost,
		Path:             "/upload",
		Body:             uploadBody,
		Headers:          internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}, &uploadResult)
	if err != nil {
		return nil, handleError(err)
	}

	// Step 2: start a pre-recorded transcription job.
	body["audio_url"] = uploadResult.AudioURL

	var initResult struct {
		ResultURL string `json:"result_url"`
	}
	_, err = m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		ValidateResponse: providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaGladiaInit),
		Method:           http.MethodPost,
		Path:             "/pre-recorded",
		Body:             orderedGladiaBody(body),
		Headers:          opts.Headers,
	}, &initResult)
	if err != nil {
		return nil, handleError(err)
	}

	// Step 3: poll the job's result_url until it completes. The URL comes
	// from the provider response; validate it (and any redirect) as
	// untrusted input, trusting only the provider's own configured origin.
	interval := gladiaPollSetting(gladiaOptions, "pollIntervalMs", m.provider.config.PollInterval, defaultPollInterval)
	timeout := gladiaPollSetting(gladiaOptions, "pollTimeoutMs", m.provider.config.PollTimeout, defaultPollTimeout)
	raw, headers, err := m.pollResult(ctx, initResult.ResultURL, opts.Headers, interval, timeout)
	if err != nil {
		return nil, err
	}

	// TS AISDKError TranscriptionResultEmpty (cause: the result).
	if result, ok := raw["result"].(map[string]interface{}); !ok || result == nil {
		empty := providererrors.NewProviderError("gladia", 0, "TranscriptionResultEmpty", "Transcription result is empty", nil)
		empty.Data = gladiaSchemaResult(raw)
		return nil, empty
	}

	return convertPollResult(raw, headers, currentDate, gladiaEffectiveModelID(m.modelID), warnings), nil
}

func buildUploadMultipart(opts *provider.TranscriptionOptions) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	audio := opts.Audio
	if len(audio) == 0 && opts.AudioBase64 != "" {
		decoded, err := fileutil.ConvertBase64ToBytes(opts.AudioBase64)
		if err != nil {
			return nil, "", fmt.Errorf("gladia: failed to decode base64 audio: %w", err)
		}
		audio = decoded
	}

	part, err := providerutils.CreateTypedFormFile(writer, "audio", "audio."+gladiaAudioExtension(opts.MimeType), opts.MimeType)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buf, writer.FormDataContentType(), nil
}

func gladiaAudioExtension(mimeType string) string {
	return providerutils.MediaTypeToExtension(mimeType)
}

// pollResult polls resultURL until the job reaches status "done" or "error",
// or pollTimeout elapses (checked before each poll, after the abort check,
// as TS does). It returns the raw decoded JSON response along with
// that response's HTTP headers (TS: response.responseHeaders, surfaced on the
// final TranscriptionResult.Response.Headers).
func (m *TranscriptionModel) pollResult(ctx context.Context, resultURL string, headers map[string]string, pollInterval, pollTimeout time.Duration) (map[string]interface{}, map[string]string, error) {
	trustedOrigin := m.provider.config.BaseURL
	pollOpts := fileutil.TrustedOriginDownloadOptions(trustedOrigin, m.provider.client.HTTPClient().Transport)
	pollOpts.Headers = m.pollHeaders(resultURL, trustedOrigin, headers)

	start := time.Now()
	var last map[string]interface{}
	for {
		select {
		case <-ctx.Done():
			return nil, nil, context.Cause(ctx) // TS abortSignal.throwIfAborted(): the abort reason
		default:
		}
		if time.Since(start) > pollTimeout {
			// TS AISDKError TranscriptionJobPollingTimedOut (cause: the last
			// poll result).
			timedOut := providererrors.NewProviderError("gladia", 0, "TranscriptionJobPollingTimedOut",
				"Transcription job polling timed out after "+strconv.FormatInt(pollTimeout.Milliseconds(), 10)+" ms", nil)
			if last != nil {
				timedOut.Data = gladiaSchemaResult(last)
			}
			return nil, nil, timedOut
		}

		result, err := fileutil.DownloadWithMetadata(ctx, resultURL, pollOpts)
		if err != nil {
			// TS getFromApi handleFetchError: a network failure is the
			// retryable "Cannot connect to API" error, an abort stays as is.
			if cause, ok := fileutil.TransportFailure(err); ok {
				err = internalhttp.TransportErrorCtx(ctx, cause)
			}
			return nil, nil, handleError(err)
		}
		// TS createJsonResponseHandler(gladiaTranscriptionResultResponseSchema),
		// over response.text() (TextDecoder).
		result.Data = []byte(textdecode.UTF8(result.Data))
		if err := providerutils.MediaResponseValidator("gladia.transcription", providerutils.SchemaGladiaResult)(http.StatusOK, http.Header(result.Headers), result.Data); err != nil {
			return nil, nil, err
		}
		var raw map[string]interface{}
		if err := jsonexact.Unmarshal(result.Data, &raw); err != nil {
			return nil, nil, fmt.Errorf("gladia: failed to decode poll response: %w", err)
		}
		last = raw

		status, _ := raw["status"].(string)
		switch status {
		case "done":
			return raw, flattenHeaders(result.Headers), nil
		case "error":
			// TS AISDKError TranscriptionJobFailed (cause: the result); the
			// message names a reported error_code (TS 9207c2b1e0).
			message := "Transcription job failed"
			if code, ok := raw["error_code"].(float64); ok {
				message += " with error code " + providerutils.JSNumberString(code)
			}
			failure := providererrors.NewProviderError("gladia", 0, "TranscriptionJobFailed", message, nil)
			failure.Data = gladiaSchemaResult(raw)
			return nil, nil, failure
		}

		select {
		case <-ctx.Done():
			return nil, nil, context.Cause(ctx) // TS abortSignal.throwIfAborted(): the abort reason
		case <-time.After(pollInterval):
		}
	}
}

// pollHeaders is TS getFromApi({ credentialedOrigin }): caller and provider
// headers (the API key) are withheld unless the response-supplied URL is
// same-origin with the configured API; the user agent is still sent.
func (m *TranscriptionModel) pollHeaders(resultURL, trustedOrigin string, headers map[string]string) map[string]string {
	if !fileutil.IsSameOrigin(resultURL, trustedOrigin) {
		return version.WithUserAgentSuffix(nil, version.ProviderUserAgent("gladia"))
	}
	return version.WithUserAgentSuffix(mergeHeadersFold(map[string]string{
		"x-gladia-key":     providerutils.APIKeyOrEnv(m.provider.config.APIKey, "GLADIA_API_KEY"),
		"x-gladia-version": version.SDKUserAgent(),
	}, m.provider.config.Headers, headers), version.ProviderUserAgent("gladia"))
}

// flattenHeaders joins multi-value HTTP headers into single strings (the last
// Set-Cookie line wins, providerutils.FetchHeaderEntry), matching
// providerutils.ExtractHeaders's convention for other providers' response
// metadata.
func flattenHeaders(h map[string][]string) map[string]string {
	// TS extractResponseHeaders, IsomorphicDecode included.
	return providerutils.ExtractHeaders(http.Header(h))
}

// gladiaBodyKeyOrder is the TS getArgs assignment order of the
// /v2/pre-recorded body, then doGenerate's audio_url.
var gladiaBodyKeyOrder = []string{
	"model", "custom_vocabulary", "callback", "subtitles", "diarization", "translation",
	"summarization", "named_entity_recognition", "custom_spelling", "sentiment_analysis",
	"audio_to_llm", "pii_redaction", "custom_metadata", "sentences", "punctuation_enhanced",
	"custom_vocabulary_config", "language_config", "callback_config", "subtitles_config",
	"diarization_config", "translation_config", "summarization_config",
	"custom_spelling_config", "audio_to_llm_config", "pii_redaction_config", "audio_url",
}

// gladiaNestedKeyOrder is the TS literal order of the nested config objects.
var gladiaNestedKeyOrder = map[string][]string{
	"custom_vocabulary_config": {"vocabulary", "default_intensity"},
	"language_config":          {"languages", "code_switching"},
	"callback_config":          {"url", "method"},
	"subtitles_config":         {"formats", "minimum_duration", "maximum_duration", "maximum_characters_per_row", "maximum_rows_per_caption", "style"},
	"diarization_config":       {"number_of_speakers", "min_speakers", "max_speakers"},
	"translation_config":       {"target_languages", "model", "match_original_utterances", "lipsync", "context_adaptation", "context", "informal"},
	"summarization_config":     {"type"},
	"custom_spelling_config":   {"spelling_dictionary"},
	"audio_to_llm_config":      {"prompts", "model"},
	"pii_redaction_config":     {"entity_types", "processed_text_type"},
}

// gladiaOrdered copies the listed keys of m, in order, into an ordered
// object.
func gladiaOrdered(m map[string]interface{}, order []string) *jsonorder.Object {
	out := jsonorder.New()
	for _, k := range order {
		if v, ok := m[k]; ok {
			out.Set(k, v)
		}
	}
	return out
}

// orderedGladiaBody sends the body in the TS JSON.stringify order instead
// of Go's sorted map keys; vocabulary entries are {value, intensity,
// pronunciations, language}.
func orderedGladiaBody(body map[string]interface{}) *jsonorder.Object {
	out := jsonorder.New()
	for _, k := range gladiaBodyKeyOrder {
		v, ok := body[k]
		if !ok {
			continue
		}
		if order, nested := gladiaNestedKeyOrder[k]; nested {
			if m, isMap := v.(map[string]interface{}); isMap {
				if items, isList := m["vocabulary"].([]interface{}); isList && k == "custom_vocabulary_config" {
					entries := make([]interface{}, len(items))
					for i, item := range items {
						if entry, isObj := item.(map[string]interface{}); isObj {
							entries[i] = gladiaOrdered(entry, []string{"value", "intensity", "pronunciations", "language"})
						} else {
							entries[i] = item
						}
					}
					copied := make(map[string]interface{}, len(m))
					for mk, mv := range m {
						copied[mk] = mv
					}
					copied["vocabulary"] = entries
					m = copied
				}
				v = gladiaOrdered(m, order)
			}
		}
		out.Set(k, v)
	}
	return out
}

// convertPollResult maps a raw completed-job response into a
// TranscriptionResult, mirroring GladiaTranscriptionModel.doGenerate's return.
func convertPollResult(raw map[string]interface{}, headers map[string]string, currentDate time.Time, modelID string, warnings []types.Warning) *types.TranscriptionResult {
	if warnings == nil {
		warnings = []types.Warning{}
	}
	b, _ := jsonstr.Marshal(raw) // a provider ±Inf writes null (JSON.stringify)
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
	_ = jsonexact.Unmarshal(b, &typed)

	if typed.Result == nil {
		return &types.TranscriptionResult{
			Warnings: warnings,
			Response: &types.ResponseMetadata{
				Timestamp: currentDate,
				ModelID:   modelID,
				Headers:   headers,
				Body:      raw,
			},
			ProviderMetadata: map[string]interface{}{"gladia": gladiaSchemaResult(raw)},
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
		Warnings:          warnings,
		Usage: types.TranscriptionUsage{
			DurationSeconds: duration,
		},
		// The whole poll response is surfaced under providerMetadata.gladia,
		// preserving per-utterance speaker/channel/confidence/words/language
		// and diarization info that the AI SDK's `segments` shape can't
		// represent (matches TS: providerMetadata: { gladia: transcriptionResult }).
		ProviderMetadata: map[string]interface{}{"gladia": gladiaSchemaResult(raw)},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			// TS response.modelId is the model's ID ('default' when none
			// was given).
			ModelID: modelID,
			Headers: headers,
			Body:    raw,
		},
	}
}

// handleError converts an HTTP error into a ProviderError, parsing Gladia's
// `{"error":{"message","code"}}` error shape when present. This includes
// polling errors surfaced via fileutil.PollJSON/DownloadError, which now
// retains a bounded response body and headers for exactly this purpose (TS
// gladia-transcription-model.ts uses the same gladiaFailedResponseHandler for
// both the upload/init requests and the result_url polling GET).
func handleError(err error) error {
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
		message, code := parseGladiaErrorBody(statusErr.StatusCode, statusErr.Body)
		perr := providererrors.NewProviderErrorFromResponse("gladia", 0, code, message, err)
		// TS APICallError.data: the schema-parsed body ({error: {message,
		// code}}, undeclared keys stripped), else undefined.
		perr.Data = nil
		if code != "" {
			codeNumber, _ := strconv.ParseFloat(code, 64)
			perr.Data = map[string]interface{}{"error": map[string]interface{}{"message": message, "code": codeNumber}}
		}
		return perr
	}

	var downloadErr *providererrors.DownloadError
	if errors.As(err, &downloadErr) && downloadErr.StatusCode != 0 {
		// A handler message is final, an empty one included (TS APICallError
		// keeps ""). A download failure with no status never reached an HTTP
		// response and propagates unchanged below.
		// TS reads the failed body as text (TextDecoder).
		body := []byte(textdecode.UTF8(downloadErr.Body))
		message, code := parseGladiaErrorBody(downloadErr.StatusCode, body)
		providerErr := providererrors.NewProviderError("gladia", downloadErr.StatusCode, code, message, err)
		providerErr.ResponseHeaders = flattenHeaders(downloadErr.Headers)
		if len(body) > 0 {
			providerErr.ResponseBody = string(body)
		}
		if code != "" {
			// TS APICallError.data is the schema-parsed body (zod strips
			// every other key).
			codeNumber, _ := strconv.ParseFloat(code, 64)
			providerErr.Data = map[string]interface{}{"error": map[string]interface{}{"message": message, "code": codeNumber}}
		}
		return providerErr
	}

	// TS postJsonToApi / getFromApi let a transport failure (an abort, "Cannot
	// connect to API") propagate unchanged.
	return err
}

// parseGladiaErrorBody mirrors TS gladiaFailedResponseHandler: a body that
// matches {"error":{"message":string,"code":number}} gives that message
// (even an empty one) and the code; anything else (empty, not JSON, a missing
// or wrongly typed field) gives the HTTP status text as the message.
func parseGladiaErrorBody(statusCode int, body []byte) (message string, code string) {
	var payload struct {
		Error *struct {
			Message *string  `json:"message"`
			Code    *float64 `json:"code"`
		} `json:"error"`
	}
	if err := securejson.Unmarshal(body, &payload); err == nil && payload.Error != nil &&
		payload.Error.Message != nil && payload.Error.Code != nil {
		return *payload.Error.Message, providerutils.JSNumberString(*payload.Error.Code)
	}
	return http.StatusText(statusCode), ""
}

// gladiaSchemaResult applies the TS result schema (TS 9207c2b1e0): the top
// level is a plain z.object that keeps only status, error_code (null kept)
// and result, while result and its nested objects are loose, so every
// add-on (translation, summarization, audio_to_llm, sentences, subtitles,
// extra metadata, utterance keys) passes through as returned.
func gladiaSchemaResult(raw map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for _, k := range []string{"status", "error_code", "result"} {
		if v, ok := raw[k]; ok {
			out[k] = v
		}
	}
	return out
}
