package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for OpenAI Whisper
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new OpenAI transcription model
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
	return m.provider.Name() + ".transcription"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if IsRealtimeTranscriptionModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("non-streaming transcription with %s", m.modelID),
		}
	}

	if err := validateTranscriptionProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	currentDate := time.Now()
	body, contentType, err := m.buildMultipartBody(opts)
	if err != nil {
		return nil, err
	}
	_, granularityWarnings := m.timestampGranularities(opts)

	// Use internal http client's Do method with custom headers
	req := internalhttp.Request{
		Method:  "POST",
		Path:    "/audio/transcriptions",
		Query:   m.provider.config.AudioQuery,
		Body:    body,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{"Content-Type": contentType}),
	}

	resp, err := m.provider.client.Do(ctx, req)
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause // TS: a rejected fetch wrapper or headers() resolution is returned unchanged
		}
		if pe, ok := m.provider.apiError(m.Provider(), err); ok {
			return nil, pe // TS openaiFailedResponseHandler / defaultOpenAICompatibleErrorStructure
		}
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, openAICompatStatusError("openai", resp.StatusCode, resp.Headers, resp.Body)
	}

	resp.Body = []byte(textdecode.UTF8(resp.Body)) // TS response.text(): BOM dropped, invalid UTF-8 replaced
	if err := providerutils.MediaResponseValidator(m.Provider(), providerutils.SchemaOpenAITranscr)(resp.StatusCode, resp.Headers, resp.Body); err != nil {
		return nil, err
	}

	var response openaiTranscriptionResponse
	if err := json.Unmarshal(resp.Body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode transcription response: %w", err)
	}
	var rawResponse interface{}
	_ = json.Unmarshal(resp.Body, &rawResponse)

	duration := transcriptionDuration(response)
	result := &types.TranscriptionResult{
		Text: response.Text,
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationOrZero(duration),
		},
		ProviderUsage: response.Usage,
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Headers),
			Body:      rawResponse,
		},
	}
	result.Language = openaiTranscriptionLanguage(response.Language, response.Languages)
	result.DurationInSeconds = duration
	result.Warnings = append(result.Warnings, granularityWarnings...)

	// TS 41465fd1b6: word timings are reported separately when the response
	// has any.
	if len(response.Words) > 0 {
		result.Words = make([]types.TranscriptionWord, len(response.Words))
		for i, w := range response.Words {
			result.Words[i] = types.TranscriptionWord{Text: w.Word, StartSecond: w.Start, EndSecond: w.End}
		}
	}

	// Timestamped segments/words (abb9ebf: diarized segments carry an extra
	// "speaker" field, surfaced separately via ProviderMetadata below).
	// TS: `segments ?? words ?? []` (an empty segments array wins over words).
	if response.Segments != nil {
		result.Segments = make([]types.TranscriptionTimestamp, len(response.Segments))
		for i, seg := range response.Segments {
			result.Segments[i] = types.TranscriptionTimestamp{
				Text:  seg.Text,
				Start: seg.Start,
				End:   seg.End,
			}
		}
	} else if response.Words != nil {
		result.Segments = make([]types.TranscriptionTimestamp, len(response.Words))
		for i, w := range response.Words {
			result.Segments[i] = types.TranscriptionTimestamp{
				Text:  w.Word,
				Start: w.Start,
				End:   w.End,
			}
		}
	}
	result.Timestamps = result.Segments

	// diarized_json (gpt-4o-transcribe-diarize): segments carry a "speaker"
	// field. Surface them separately via providerMetadata.openai.segments
	// (abb9ebf), matching TS's diarizedSegments.
	var diarizedSegments []map[string]interface{}
	for _, seg := range response.Segments {
		// TS `'speaker' in segment` after the schema union: only a
		// transcript.text.segment keeps its speaker ("" included).
		if seg.Speaker == nil || seg.Type != "transcript.text.segment" {
			continue
		}
		diarizedSegments = append(diarizedSegments, map[string]interface{}{
			"text":        seg.Text,
			"startSecond": seg.Start,
			"endSecond":   seg.End,
			"speaker":     *seg.Speaker,
		})
	}
	if len(diarizedSegments) > 0 || response.Languages != nil {
		openaiMeta := map[string]interface{}{}
		if len(diarizedSegments) > 0 {
			openaiMeta["segments"] = diarizedSegments
		}
		if response.Languages != nil {
			// Full detected-language list (multilingual results are not
			// collapsed to an arbitrary single language).
			languages := make([]map[string]interface{}, len(response.Languages))
			for i, l := range response.Languages {
				languages[i] = map[string]interface{}{"code": l.Code}
			}
			openaiMeta["languages"] = languages
		}
		result.ProviderMetadata = map[string]interface{}{"openai": openaiMeta}
	}

	return result, nil
}

// timestampGranularities resolves the timestamp_granularities to send and
// the warnings (TS 41465fd1b6): providerOptions.openai.timestampGranularities,
// else the shared TimestampGranularities when non-nil (an empty slice
// suppresses the default), else the Go-only Timestamps flag (word, when no
// providerOptions.openai are given; v0.5 behavior), else segment for
// verbose_json. Only verbose_json accepts the field, so it is sent for that
// format alone; any other format warns for each granularity it cannot
// return (diarized_json always returns segments).
func (m *TranscriptionModel) timestampGranularities(opts *provider.TranscriptionOptions) ([]string, []types.Warning) {
	openaiOpts := transcriptionOpenAIOptions(opts.ProviderOptions)
	responseFormat := openaiOpts.ResponseFormat
	if responseFormat == "" {
		responseFormat = getOpenAITranscriptionModelEntry(m.modelID).responseFormat
	}
	var warnings []types.Warning
	granularities := openaiOpts.TimestampGranularities
	switch {
	case openaiOpts.HasTimestampGranularities:
	case opts.TimestampGranularities != nil:
		granularities = opts.TimestampGranularities
	case !openaiOpts.Present && opts.Timestamps:
		granularities = []string{"word"}
	case responseFormat == "verbose_json":
		granularities = []string{"segment"}
	}
	if openaiOpts.HasTimestampGranularities {
		for _, g := range opts.TimestampGranularities {
			if !stringListContains(openaiOpts.TimestampGranularities, g) {
				warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "timestampGranularities",
					Details: fmt.Sprintf("The \"%s\" timestamp granularity is excluded by providerOptions.openai.timestampGranularities.", g)})
			}
		}
	}
	feature := "timestampGranularities"
	if openaiOpts.HasTimestampGranularities {
		feature = "providerOptions.openai.timestampGranularities"
	}
	format := responseFormat
	if format == "" {
		format = "default JSON"
	}
	for _, g := range granularities {
		if responseFormat != "verbose_json" && (responseFormat != "diarized_json" || g != "segment") {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: feature,
				Details: fmt.Sprintf("OpenAI transcription with the %s response format does not support %s timestamps.", format, g)})
		}
	}
	if responseFormat != "verbose_json" {
		granularities = nil
	}
	return granularities, warnings
}

func stringListContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (m *TranscriptionModel) buildMultipartBody(opts *provider.TranscriptionOptions) (io.Reader, string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// TS appends model before file.
	if err := writer.WriteField("model", m.modelID); err != nil {
		return nil, "", err
	}

	// Add file
	fileHeader := textproto.MIMEHeader{}
	fileHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="audio.%s"`, getExtensionFromMimeType(opts.MimeType)))
	if opts.MimeType != "" {
		fileHeader.Set("Content-Type", opts.MimeType)
	} else {
		fileHeader.Set("Content-Type", "application/octet-stream")
	}
	part, err := writer.CreatePart(fileHeader)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(opts.Audio); err != nil {
		return nil, "", fmt.Errorf("failed to write audio data: %w", err)
	}

	openaiOpts := transcriptionOpenAIOptions(opts.ProviderOptions)
	entry := getOpenAITranscriptionModelEntry(m.modelID)

	// TS getArgs (9026c7de79, 226b7daf82): response_format defaults only for
	// the models that need one (whisper-1 verbose_json, the diarize model
	// diarized_json), and timestamp_granularities only for verbose_json, so
	// gpt-transcribe with provider options no longer gets verbose_json +
	// segment (a 400). The fields go in one ordered list: response_format,
	// include[], language, languages[], keywords[], prompt, temperature,
	// timestamp_granularities[], then chunking_strategy. temperature is the
	// schema default 0 whenever providerOptions.openai is present.
	responseFormat := openaiOpts.ResponseFormat
	if responseFormat == "" {
		responseFormat = entry.responseFormat
	}
	granularities, _ := m.timestampGranularities(opts)
	// TS sends every option that is `!= null`, so a present "" language or
	// prompt is sent as an empty form field.
	language, hasLanguage := openaiOpts.Language, openaiOpts.hasLanguage
	if !hasLanguage && opts.Language != "" {
		// Go extension: the top-level TranscriptionOptions.Language.
		language, hasLanguage = opts.Language, true
	}

	writeField := func(key, value string) error { return writer.WriteField(key, value) }
	writeList := func(key string, values []string) error {
		for _, v := range values {
			if err := writer.WriteField(key+"[]", v); err != nil {
				return err
			}
		}
		return nil
	}
	if responseFormat != "" {
		if err := writeField("response_format", responseFormat); err != nil {
			return nil, "", err
		}
	}
	if err := writeList("include", openaiOpts.Include); err != nil {
		return nil, "", err
	}
	if hasLanguage {
		if err := writeField("language", language); err != nil {
			return nil, "", err
		}
	}
	if err := writeList("languages", openaiOpts.Languages); err != nil {
		return nil, "", err
	}
	if err := writeList("keywords", openaiOpts.Keywords); err != nil {
		return nil, "", err
	}
	if openaiOpts.hasPrompt {
		if err := writeField("prompt", openaiOpts.Prompt); err != nil {
			return nil, "", err
		}
	}
	if openaiOpts.Present {
		temperature := openaiOpts.Temperature
		if !openaiOpts.HasTemperature {
			temperature = 0
		}
		if err := writeField("temperature", formatFloat(temperature)); err != nil {
			return nil, "", err
		}
	}
	if err := writeList("timestamp_granularities", granularities); err != nil {
		return nil, "", err
	}

	// Controls how the audio is split into chunks before transcription
	// (abb9ebf: default "auto" for the diarize model).
	chunkingStrategy := openaiOpts.ChunkingStrategy
	if chunkingStrategy == nil && entry.chunkingStrategy != "" {
		chunkingStrategy = entry.chunkingStrategy
	}
	if chunkingStrategy != nil {
		var value string
		switch cs := chunkingStrategy.(type) {
		case string:
			value = cs
		case map[string]interface{}:
			value = string(chunkingStrategyWireFormat(cs))
		}
		if value != "" {
			if err := writer.WriteField("chunking_strategy", value); err != nil {
				return nil, "", err
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return &buf, writer.FormDataContentType(), nil
}

// chunkingStrategyWireFormat maps the camelCase providerOptions keys to the
// snake_case server_vad object OpenAI expects, in TS key order
// (type, threshold, prefix_padding_ms, silence_duration_ms) and skipping
// null members like TS's `!= null` spreads.
func chunkingStrategyWireFormat(cs map[string]interface{}) json.RawMessage {
	var buf bytes.Buffer
	buf.WriteString(`{"type":`)
	typ, ok := cs["type"]
	if !ok || typ == nil {
		typ = "server_vad"
	}
	tb, _ := json.Marshal(typ)
	buf.Write(tb)
	for _, f := range [][2]string{{"threshold", "threshold"}, {"prefixPaddingMs", "prefix_padding_ms"}, {"silenceDurationMs", "silence_duration_ms"}} {
		if v, ok := cs[f[0]]; ok && v != nil {
			vb, _ := json.Marshal(v)
			buf.WriteString(`,"` + f[1] + `":`)
			buf.Write(vb)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes()
}

func formatFloat(f float64) string {
	encoded, _ := json.Marshal(f)
	return string(encoded)
}

// transcriptionOpenAIOptionsValue holds providerOptions.openai transcription
// settings (abb9ebf: openai-transcription-model-options.ts).
type transcriptionOpenAIOptionsValue struct {
	ResponseFormat         string
	ChunkingStrategy       interface{} // string ("auto") or map[string]interface{} (server_vad)
	Prompt                 string
	HasTemperature         bool
	Temperature            float64
	Include                []string
	TimestampGranularities []string
	// Languages and Keywords are the REST language and vocabulary hints
	// (TS 226b7daf82), sent as repeated languages[] / keywords[] fields.
	Languages []string
	Keywords  []string
	// Present is true when providerOptions.openai was supplied (TS
	// parseProviderOptions then applies the schema defaults).
	Present                   bool
	HasTimestampGranularities bool
	Language                  string
	// hasLanguage / hasPrompt: the key holds a string, "" included (TS
	// `value != null`).
	hasLanguage bool
	hasPrompt   bool
}

func transcriptionOpenAIOptions(providerOptions map[string]interface{}) transcriptionOpenAIOptionsValue {
	var result transcriptionOpenAIOptionsValue
	if providerOptions == nil {
		return result
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return result
	}
	result.Present = true
	if v, ok := openaiOpts["language"].(string); ok {
		result.Language, result.hasLanguage = v, true
	}
	if v, ok := openaiOpts["responseFormat"].(string); ok {
		result.ResponseFormat = v
	}
	if v, ok := openaiOpts["chunkingStrategy"]; ok {
		result.ChunkingStrategy = v
	}
	if v, ok := openaiOpts["prompt"].(string); ok {
		result.Prompt, result.hasPrompt = v, true
	}
	if v, ok := speechSpeedOption(openaiOpts["temperature"]); ok {
		result.Temperature = v
		result.HasTemperature = true
	}
	if v, ok := openaiOpts["include"].([]string); ok {
		result.Include = append(result.Include, v...)
	}
	if v, ok := openaiOpts["include"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.Include = append(result.Include, s)
			}
		}
	}
	result.Languages = transcriptionStringList(openaiOpts["languages"])
	result.Keywords = transcriptionStringList(openaiOpts["keywords"])
	if v, ok := openaiOpts["timestampGranularities"].([]string); ok {
		result.HasTimestampGranularities = true
		result.TimestampGranularities = append(result.TimestampGranularities, v...)
	}
	if v, ok := openaiOpts["timestampGranularities"].([]interface{}); ok {
		result.HasTimestampGranularities = true
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	return result
}

// transcriptionStringList reads a []string or []interface{} of strings.
func transcriptionStringList(v interface{}) []string {
	switch list := v.(type) {
	case []string:
		return append([]string(nil), list...)
	case []interface{}:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// transcriptionDuration ports TS doGenerate's durationInSeconds:
// `response.duration ?? (usage.type === 'duration' && typeof usage.seconds
// === 'number' ? usage.seconds : undefined)` (599a97f9bf: duration-billed
// json responses, e.g. gpt-transcribe, report the duration only in usage).
func transcriptionDuration(response openaiTranscriptionResponse) *float64 {
	if response.Duration != nil {
		d := *response.Duration
		return &d
	}
	if response.Usage != nil && response.Usage["type"] == "duration" {
		if seconds, ok := response.Usage["seconds"].(float64); ok {
			return &seconds
		}
	}
	return nil
}

func durationOrZero(d *float64) float64 {
	if d == nil {
		return 0
	}
	return *d
}

// getExtensionFromMimeType mirrors TS provider-utils mediaTypeToExtension.
func getExtensionFromMimeType(mimeType string) string {
	parts := strings.SplitN(strings.ToLower(mimeType), "/", 2)
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

// openaiTranscriptionLanguages maps the language names whisper returns to
// ISO-639-1 codes (TS languageMap).
var openaiTranscriptionLanguages = map[string]string{
	"afrikaans": "af", "arabic": "ar", "armenian": "hy", "azerbaijani": "az", "belarusian": "be",
	"bosnian": "bs", "bulgarian": "bg", "catalan": "ca", "chinese": "zh", "croatian": "hr",
	"czech": "cs", "danish": "da", "dutch": "nl", "english": "en", "estonian": "et",
	"finnish": "fi", "french": "fr", "galician": "gl", "german": "de", "greek": "el",
	"hebrew": "he", "hindi": "hi", "hungarian": "hu", "icelandic": "is", "indonesian": "id",
	"italian": "it", "japanese": "ja", "kannada": "kn", "kazakh": "kk", "korean": "ko",
	"latvian": "lv", "lithuanian": "lt", "macedonian": "mk", "malay": "ms", "marathi": "mr",
	"maori": "mi", "nepali": "ne", "norwegian": "no", "persian": "fa", "polish": "pl",
	"portuguese": "pt", "romanian": "ro", "russian": "ru", "serbian": "sr", "slovak": "sk",
	"slovenian": "sl", "spanish": "es", "swahili": "sw", "swedish": "sv", "tagalog": "tl",
	"tamil": "ta", "thai": "th", "turkish": "tr", "ukrainian": "uk", "urdu": "ur",
	"vietnamese": "vi", "welsh": "cy",
}

type openaiTranscriptionResponse struct {
	Text     string   `json:"text"`
	Duration *float64 `json:"duration"`
	Language string   `json:"language,omitempty"`
	// Languages is the detected-language list (nil when absent or null; an
	// empty non-nil slice when the API sent []).
	Languages []struct {
		Code string `json:"code"`
	} `json:"languages"`
	Usage    map[string]interface{} `json:"usage,omitempty"`
	Segments []struct {
		Text    string  `json:"text"`
		Start   float64 `json:"start"`
		End     float64 `json:"end"`
		// Type and Speaker identify a diarized segment (TS union member
		// {type: 'transcript.text.segment', ..., speaker: string}).
		Type    string  `json:"type,omitempty"`
		Speaker *string `json:"speaker,omitempty"`
	} `json:"segments"`
	Words []struct {
		Word  string  `json:"word"`
		Start float64 `json:"start"`
		End   float64 `json:"end"`
	} `json:"words"`
}

// ValidateTranscriptionProviderOptions validates providerOptions.openai for
// transcription calls (also used by the Azure OpenAI streaming delegate).
func ValidateTranscriptionProviderOptions(providerOptions map[string]interface{}) error {
	return validateTranscriptionProviderOptions(providerOptions)
}

// validateTranscriptionProviderOptions mirrors TS parseProviderOptions with
// openAITranscriptionModelOptions: a bad providerOptions.openai value throws
// InvalidArgumentError("invalid openai provider options").
func validateTranscriptionProviderOptions(providerOptions map[string]interface{}) error {
	raw, ok := providerOptions["openai"]
	if !ok || raw == nil {
		return nil
	}
	fail := func(msg string) error {
		return providererrors.NewArgumentError("providerOptions", "invalid openai provider options", errors.New(msg))
	}
	opts, ok := raw.(map[string]interface{})
	if !ok {
		return fail("Invalid input: expected object")
	}
	isStringList := func(v interface{}) bool {
		switch list := v.(type) {
		case []string:
			return true
		case []interface{}:
			for _, item := range list {
				if _, ok := item.(string); !ok {
					return false
				}
			}
			return true
		}
		return false
	}
	for _, key := range []string{"language", "prompt"} {
		if v, ok := opts[key]; ok && v != nil {
			if _, isString := v.(string); !isString {
				return fail("Invalid input: expected string at " + key)
			}
		}
	}
	for _, key := range []string{"include", "languages", "keywords"} {
		if v, ok := opts[key]; ok && v != nil && !isStringList(v) {
			return fail("Invalid input: expected array at " + key)
		}
	}
	if v, ok := opts["temperature"]; ok && v != nil {
		f, isNum := speechSpeedOption(v)
		if !isNum {
			return fail("Invalid input: expected number at temperature")
		}
		if f < 0 || f > 1 {
			return fail("Invalid input: expected number between 0 and 1 at temperature")
		}
	}
	if v, ok := opts["timestampGranularities"]; ok && v != nil {
		var items []string
		switch list := v.(type) {
		case []string:
			items = list
		case []interface{}:
			for _, item := range list {
				s, isString := item.(string)
				if !isString {
					return fail("Invalid option at timestampGranularities")
				}
				items = append(items, s)
			}
		default:
			return fail("Invalid input: expected array at timestampGranularities")
		}
		for _, item := range items {
			if item != "word" && item != "segment" {
				return fail(`Invalid option: expected one of "word"|"segment" at timestampGranularities`)
			}
		}
	}
	if v, ok := opts["responseFormat"]; ok && v != nil {
		s, _ := v.(string)
		if s != "json" && s != "verbose_json" && s != "diarized_json" {
			return fail(`Invalid option: expected one of "json"|"verbose_json"|"diarized_json" at responseFormat`)
		}
	}
	if v, ok := opts["chunkingStrategy"]; ok && v != nil {
		switch cs := v.(type) {
		case string:
			if cs != "auto" {
				return fail(`Invalid input at chunkingStrategy`)
			}
		case map[string]interface{}:
			if t, _ := cs["type"].(string); t != "server_vad" {
				return fail(`Invalid input: expected "server_vad" at chunkingStrategy.type`)
			}
			if t, ok := cs["threshold"]; ok && t != nil {
				f, isNum := speechSpeedOption(t)
				if !isNum || f < 0 || f > 1 {
					return fail("Invalid input: expected number between 0 and 1 at chunkingStrategy.threshold")
				}
			}
			for _, k := range []string{"prefixPaddingMs", "silenceDurationMs"} {
				if t, ok := cs[k]; ok && t != nil {
					f, isNum := speechSpeedOption(t)
					if !isNum || f < 0 || f != float64(int64(f)) {
						return fail("Invalid input: expected non-negative integer at chunkingStrategy." + k)
					}
				}
			}
		default:
			return fail(`Invalid input at chunkingStrategy`)
		}
	}
	if v, ok := opts["streaming"]; ok && v != nil {
		st, ok := v.(map[string]interface{})
		if !ok {
			return fail("Invalid input: expected object at streaming")
		}
		if d, ok := st["delay"]; ok && d != nil {
			s, _ := d.(string)
			switch s {
			case "minimal", "low", "medium", "high", "xhigh":
			default:
				return fail(`Invalid option: expected one of "minimal"|"low"|"medium"|"high"|"xhigh" at streaming.delay`)
			}
		}
		if i, ok := st["include"]; ok && i != nil && !isStringList(i) {
			return fail("Invalid input: expected array at streaming.include")
		}
	}
	return nil
}

var openaiLanguageCodeRE = regexp.MustCompile(`^[a-z]{2}$`)

func isOpenAILanguageCode(language string) bool { return openaiLanguageCodeRE.MatchString(language) }

// openaiTranscriptionLanguage mirrors the TS language resolution: a two-letter
// code is kept, a language name is mapped to its code, and otherwise a single
// detected language from `languages` is used. Multiple or malformed detected
// languages leave it empty.
func openaiTranscriptionLanguage(language string, languages []struct {
	Code string `json:"code"`
}) string {
	if isOpenAILanguageCode(language) {
		return language
	}
	if code, ok := openaiTranscriptionLanguages[language]; ok {
		return code
	}
	if len(languages) == 1 && isOpenAILanguageCode(languages[0].Code) {
		return languages[0].Code
	}
	return ""
}
