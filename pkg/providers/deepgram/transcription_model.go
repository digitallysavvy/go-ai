package deepgram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
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
	return "deepgram"
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
		if dgOpts.Language != "" {
			query["language"] = dgOpts.Language
		}
		setQueryBool(query, "detect_language", dgOpts.DetectLanguage)
		setQueryBool(query, "diarize", dgOpts.Diarize)
		setQueryBool(query, "filler_words", dgOpts.FillerWords)
		setQueryBool(query, "intents", dgOpts.Intents)
		if dgOpts.Keyterm != "" {
			query["keyterm"] = dgOpts.Keyterm
		}
		setQueryBool(query, "paragraphs", dgOpts.Paragraphs)
		setQueryBool(query, "punctuate", dgOpts.Punctuate)
		if dgOpts.Redact != nil {
			query["redact"] = deepgramQueryStringValue(dgOpts.Redact)
		}
		if dgOpts.Replace != "" {
			query["replace"] = dgOpts.Replace
		}
		if dgOpts.Search != "" {
			query["search"] = dgOpts.Search
		}
		setQueryBool(query, "sentiment", dgOpts.Sentiment)
		setQueryBool(query, "smart_format", dgOpts.SmartFormat)
		if dgOpts.Summarize != nil {
			query["summarize"] = deepgramQueryStringValue(dgOpts.Summarize)
		}
		setQueryBool(query, "topics", dgOpts.Topics)
		setQueryBool(query, "utterances", dgOpts.Utterances)
		if dgOpts.UttSplit != nil {
			query["utt_split"] = strconv.FormatFloat(*dgOpts.UttSplit, 'f', -1, 64)
		}
	}

	return query
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
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	query := m.buildQueryParams(opts)

	req := internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/v1/listen",
		Body:   opts.Audio,
		Headers: internalhttp.MergeHeaders(opts.Headers, map[string]string{
			"Content-Type": opts.MimeType,
		}),
		Query: query,
	}

	var response deepgramTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, req, &response)
	if err != nil {
		return nil, handleError("deepgram", err)
	}

	return m.convertResponse(response, httpResp), nil
}

func (m *TranscriptionModel) convertResponse(response deepgramTranscriptionResponse, httpResp *internalhttp.Response) *types.TranscriptionResult {
	currentDate := time.Now()

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

	return &types.TranscriptionResult{
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
			Body:      response,
		},
	}
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
