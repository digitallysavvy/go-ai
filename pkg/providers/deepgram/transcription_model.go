package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for Deepgram
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Deepgram transcription model
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
	return "deepgram.transcription"
}

// ModelID returns the model ID
func (m *TranscriptionModel) ModelID() string {
	return m.modelID
}

// extractTranscriptionOptions reads Deepgram-specific transcription options
// from the provider options map.
func extractTranscriptionOptions(providerOptions map[string]interface{}) *TranscriptionModelOptions {
	if providerOptions == nil {
		return nil
	}
	raw, ok := providerOptions["deepgram"]
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

// buildQueryParams builds the /v1/listen query parameters, mirroring
// DeepgramTranscriptionModel.getArgs.
func (m *TranscriptionModel) buildQueryParams(opts *provider.TranscriptionOptions) map[string]string {
	query := map[string]string{"model": m.modelID}
	dgOpts := extractTranscriptionOptions(opts.ProviderOptions)
	rawOpts, _ := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["deepgram"].(map[string]interface{})

	// Shared Language/Timestamps options (Go's provider-agnostic surface).
	if opts.Language != "" {
		query["language"] = opts.Language
	}
	if opts.Timestamps {
		query["punctuate"] = "true"
		query["utterances"] = "true"
	}

	if dgOpts != nil {
		setQueryBool(query, "detect_entities", dgOpts.DetectEntities)
		// TS `value !== undefined` keeps an empty string ("language=").
		setQueryString(query, rawOpts, "language", "language", dgOpts.Language)
		setQueryBool(query, "detect_language", dgOpts.DetectLanguage)
		setQueryBool(query, "diarize", dgOpts.Diarize)
		setQueryBool(query, "filler_words", dgOpts.FillerWords)
		setQueryBool(query, "intents", dgOpts.Intents)
		setQueryString(query, rawOpts, "keyterm", "keyterm", dgOpts.Keyterm)
		setQueryBool(query, "paragraphs", dgOpts.Paragraphs)
		setQueryBool(query, "punctuate", dgOpts.Punctuate)
		if dgOpts.Redact != nil {
			query["redact"] = deepgramQueryStringValue(dgOpts.Redact)
		}
		setQueryString(query, rawOpts, "replace", "replace", dgOpts.Replace)
		setQueryString(query, rawOpts, "search", "search", dgOpts.Search)
		setQueryBool(query, "sentiment", dgOpts.Sentiment)
		setQueryBool(query, "smart_format", dgOpts.SmartFormat)
		if dgOpts.Summarize != nil {
			query["summarize"] = deepgramQueryStringValue(dgOpts.Summarize)
		}
		setQueryBool(query, "topics", dgOpts.Topics)
		setQueryBool(query, "utterances", dgOpts.Utterances)
		if dgOpts.UttSplit != nil {
			query["utt_split"] = providerutils.JSNumberString(*dgOpts.UttSplit)
		}
	}

	return query
}

// transcriptionQueryOrder is the TS getArgs body key order.
var transcriptionQueryOrder = []string{
	"model", "detect_entities", "detect_language", "diarize", "filler_words", "intents",
	"keyterm", "language", "paragraphs", "punctuate", "redact", "replace", "search",
	"sentiment", "smart_format", "summarize", "topics", "utterances", "utt_split",
}

// transcriptionSearchParams orders the /v1/listen query like TS; a key
// outside the TS body (none today) follows in sorted order.
func transcriptionSearchParams(query map[string]string) *internalhttp.SearchParams {
	p := internalhttp.NewSearchParams()
	seen := make(map[string]bool, len(query))
	for _, k := range transcriptionQueryOrder {
		if v, ok := query[k]; ok {
			p.Append(k, v)
			seen[k] = true
		}
	}
	rest := make([]string, 0)
	for k := range query {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		p.Append(k, query[k])
	}
	return p
}

// setQueryString sets query[key] to the string option when it is present in
// the raw provider options, including "" (TS keeps any non-undefined value).
func setQueryString(query map[string]string, raw map[string]interface{}, key, option, typed string) {
	if v, ok := raw[option].(string); ok {
		query[key] = v
	} else if typed != "" {
		query[key] = typed // typed options struct (no raw map): "" is indistinguishable from unset
	}
}

func setQueryBool(query map[string]string, key string, value *bool) {
	if value != nil {
		query[key] = strconv.FormatBool(*value)
	}
}

// deepgramQueryStringValue mirrors TS's `String(value)` coercion used when
// building /v1/listen query params (deepgram-transcription-model.ts:88-94):
// a JS array stringifies to its elements joined with commas (no brackets or
// spaces), unlike Go's fmt.Sprint("%v")-style formatting of a slice/
// []interface{}, which would otherwise produce "[a b]".
func deepgramQueryStringValue(value interface{}) string {
	if items, ok := value.([]interface{}); ok {
		parts := make([]string, len(items))
		for i, item := range items {
			parts[i] = fmt.Sprint(item)
		}
		return strings.Join(parts, ",")
	}
	if items, ok := value.([]string); ok {
		return strings.Join(items, ",")
	}
	return fmt.Sprint(value)
}

// DoTranscribe performs speech-to-text transcription
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts != nil {
		if err := validateTranscriptionOptions(opts.ProviderOptions); err != nil {
			return nil, err
		}
	}
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	// TS: currentDate is taken before the request is sent.
	currentDate := time.Now()
	query := m.buildQueryParams(opts)

	req := internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/listen",
		// TS postToApi body.content is options.audio: the bytes, or a base64
		// string audio as given.
		Body: transcriptionContent(opts),
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": opts.MimeType,
		}),
		// TS URLSearchParams over Object.entries(body): the getArgs key
		// order, not Go's sorted map keys.
		SearchParams: transcriptionSearchParams(query),
		// TS createJsonResponseHandler(deepgramTranscriptionResponseSchema).
		ValidateResponse: validateTranscriptionResponse,
	}

	var response deepgramTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, req, &response)
	if err != nil {
		return nil, handleError("deepgram", err)
	}

	result, err := m.convertResponse(response, httpResp, currentDate)
	if err != nil {
		return nil, err
	}
	// TS 5b89b2560c: batch transcription ignores the streaming options.
	if streaming, present := rawTranscriptionOptions(opts.ProviderOptions)["streaming"]; present && streaming != nil {
		result.Warnings = append(result.Warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.deepgram.streaming",
			Details: "Deepgram batch transcription does not support streaming options.",
		})
	}
	return result, nil
}

func (m *TranscriptionModel) convertResponse(response deepgramTranscriptionResponse, httpResp *internalhttp.Response, currentDate time.Time) (*types.TranscriptionResult, error) {
	if err := deepgramSegmentsAccessError(httpResp.Body); err != nil {
		return nil, err
	}
	var (
		text     string
		segments []types.TranscriptionTimestamp
		language string
	)
	if len(response.Results.Channels) > 0 {
		channel := response.Results.Channels[0]
		language = channel.DetectedLanguage
		if len(channel.Alternatives) > 0 {
			alt := channel.Alternatives[0]
			text = alt.Transcript
			for _, word := range alt.Words {
				segments = append(segments, types.TranscriptionTimestamp{
					Text:  word.Word,
					Start: word.Start,
					End:   word.End,
				})
			}
		}
	}
	if segments == nil {
		segments = []types.TranscriptionTimestamp{}
	}

	var durationInSeconds *float64
	if response.Metadata != nil {
		d := response.Metadata.Duration
		durationInSeconds = &d
	}

	result := &types.TranscriptionResult{
		Text:              text,
		Segments:          segments,
		Timestamps:        segments,
		Language:          language,
		DurationInSeconds: durationInSeconds,
		Warnings:          []types.Warning{},
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationValue(durationInSeconds),
		},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(httpResp.Headers),
			Body:      providerutils.RawJSONBody(httpResp.Body),
		},
	}
	// TS: `...(durationInSeconds != null ? { usage: { seconds: durationInSeconds } } : {})`.
	if durationInSeconds != nil {
		result.ProviderUsage = map[string]interface{}{"seconds": *durationInSeconds}
	}
	return result, nil
}

// deepgramSegmentsAccessError is the TypeError TS doGenerate throws when it
// maps the segments: `response.results?.channels[0].alternatives[0].words`
// reads past an empty channels or alternatives array when results is
// present, so the call fails. Results that are null or absent give no
// segments.
func deepgramSegmentsAccessError(body []byte) error {
	var raw struct {
		Results *struct {
			Channels []struct {
				Alternatives []json.RawMessage `json:"alternatives"`
			} `json:"channels"`
		} `json:"results"`
	}
	if len(body) == 0 || jsonexact.Unmarshal([]byte(textdecode.UTF8(body)), &raw) != nil || raw.Results == nil {
		return nil
	}
	if len(raw.Results.Channels) == 0 {
		return errors.New("Cannot read properties of undefined (reading 'alternatives')") //nolint:staticcheck // TS V8 TypeError text
	}
	if len(raw.Results.Channels[0].Alternatives) == 0 {
		return errors.New("Cannot read properties of undefined (reading 'words')") //nolint:staticcheck // TS V8 TypeError text
	}
	return nil
}

func durationValue(duration *float64) float64 {
	if duration == nil {
		return 0
	}
	return *duration
}

type deepgramTranscriptionResponse struct {
	Metadata *struct {
		TransactionKey string  `json:"transaction_key"`
		RequestID      string  `json:"request_id"`
		Duration       float64 `json:"duration"`
	} `json:"metadata"`
	Results struct {
		Channels []struct {
			DetectedLanguage string `json:"detected_language"`
			Alternatives     []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []struct {
					Word       string  `json:"word"`
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
				} `json:"words"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// transcriptionContent is TS options.audio as the request content: the audio
// bytes, or a base64 string audio sent as that string.
func transcriptionContent(opts *provider.TranscriptionOptions) []byte {
	if len(opts.Audio) == 0 && opts.AudioBase64 != "" {
		return []byte(opts.AudioBase64)
	}
	return opts.Audio
}
