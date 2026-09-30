package fal

import (
	"context"
	"encoding/base64"
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

// falTranscriptionStillInProgress is the special "not an error" detail the
// fal.ai queue API returns while a transcription job is still processing,
// matching fal-transcription-model.ts's failedResponseHandler check.
const falTranscriptionStillInProgress = "Request is still in progress"

const falTranscriptionPollTimeout = 60 * time.Second

// falTranscriptionPollInterval is a var (not const) so in-package tests can
// speed it up; production code never reassigns it.
var falTranscriptionPollInterval = 1 * time.Second

// TranscriptionModel implements the provider.TranscriptionModel interface
// for Fal.ai.
//
// It ports @ai-sdk/fal's FalTranscriptionModel (fal-transcription-model.ts):
// submits to the async queue at https://queue.fal.run/fal-ai/{modelId} and
// polls https://queue.fal.run/fal-ai/{modelId}/requests/{requestId} until
// the result is ready (or a 60s timeout elapses), independent of the
// provider's configured BaseURL (which only applies to the image model).
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new Fal.ai transcription model.
func NewTranscriptionModel(p *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns the specification version.
func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }

// Provider returns the provider name.
func (m *TranscriptionModel) Provider() string { return "fal.transcription" }

// ModelID returns the model ID.
func (m *TranscriptionModel) ModelID() string { return m.modelID }

type falTranscriptionQueueResponse struct {
	RequestID string `json:"request_id,omitempty"`
}

// falTranscriptionResultResponse mirrors falTranscriptionResponseSchema in
// fal-transcription-model.ts.
type falTranscriptionResultResponse struct {
	Text   string `json:"text"`
	Chunks []struct {
		Text      string    `json:"text"`
		Timestamp []float64 `json:"timestamp,omitempty"`
	} `json:"chunks,omitempty"`
	InferredLanguages []string `json:"inferred_languages,omitempty"`
}

// falTranscriptionParsedOptions mirrors falTranscriptionModelOptionsSchema
// in fal-transcription-model-options.ts, including its defaults.
type falTranscriptionParsedOptions struct {
	Language    string
	Diarize     bool
	ChunkLevel  string
	Version     string
	BatchSize   float64
	NumSpeakers *float64
}

// parseFalTranscriptionOptions applies the schema defaults from
// fal-transcription-model-options.ts (language: 'en', diarize: true,
// chunkLevel: 'segment', version: '3', batchSize: 64, numSpeakers: null).
// Defaults are only applied when providerOptions.fal is present (even as an
// empty object) — matching parseProviderOptions, which returns undefined
// (skipping the whole block in TS) when the key is entirely absent.
func parseFalTranscriptionOptions(raw interface{}) (*falTranscriptionParsedOptions, error) {
	out := &falTranscriptionParsedOptions{
		Language:   "en",
		Diarize:    true,
		ChunkLevel: "segment",
		Version:    "3",
		BatchSize:  64,
	}
	if raw == nil {
		return out, nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid fal provider options: expected object")
	}
	if v, ok := m["language"].(string); ok {
		out.Language = v
	}
	if v, ok := m["diarize"].(bool); ok {
		out.Diarize = v
	}
	if v, ok := m["chunkLevel"].(string); ok {
		out.ChunkLevel = v
	}
	if v, ok := m["version"].(string); ok {
		out.Version = v
	}
	if v, ok := toFalFloat64(m["batchSize"]); ok {
		out.BatchSize = v
	}
	if v, ok := toFalFloat64(m["numSpeakers"]); ok {
		out.NumSpeakers = &v
	}
	return out, nil
}

func toFalFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// buildRequestBody mirrors FalTranscriptionModel.getArgs in
// fal-transcription-model.ts.
func (m *TranscriptionModel) buildRequestBody(opts *provider.TranscriptionOptions) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"task":        "transcribe",
		"diarize":     true,
		"chunk_level": "word",
	}

	if opts.ProviderOptions != nil {
		if raw, ok := opts.ProviderOptions["fal"]; ok {
			parsed, err := parseFalTranscriptionOptions(raw)
			if err != nil {
				return nil, err
			}
			body["language"] = parsed.Language
			body["version"] = parsed.Version
			body["batch_size"] = parsed.BatchSize
			body["diarize"] = parsed.Diarize
			if parsed.ChunkLevel != "" {
				body["chunk_level"] = parsed.ChunkLevel
			}
			if parsed.NumSpeakers != nil {
				body["num_speakers"] = *parsed.NumSpeakers
			}
		}
	}

	return body, nil
}

// DoTranscribe submits an audio transcription job and polls until complete.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	body, err := m.buildRequestBody(opts)
	if err != nil {
		return nil, err
	}

	base64Audio := opts.AudioBase64
	if base64Audio == "" {
		base64Audio = base64.StdEncoding.EncodeToString(opts.Audio)
	}
	body["audio_url"] = fmt.Sprintf("data:%s;base64,%s", opts.MimeType, base64Audio)

	currentDate := time.Now()

	submitPath := fmt.Sprintf("%s/fal-ai/%s", m.provider.queueHost, m.modelID)

	var submitResp falTranscriptionQueueResponse
	_, err = m.provider.absClient.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    submitPath,
		Body:    body,
		Headers: opts.Headers,
	}, &submitResp)
	if err != nil {
		return nil, m.handleError(err)
	}

	if submitResp.RequestID == "" {
		return nil, providererrors.NewInvalidResponseDataError(submitResp, "fal transcription queue response missing request_id")
	}

	resultPath := fmt.Sprintf("%s/fal-ai/%s/requests/%s", m.provider.queueHost, m.modelID, submitResp.RequestID)

	result, responseHeaders, rawBody, err := m.pollForResult(ctx, resultPath, opts.Headers)
	if err != nil {
		return nil, err
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(result.Chunks))
	for _, chunk := range result.Chunks {
		start := 0.0
		end := 0.0
		if len(chunk.Timestamp) > 0 {
			start = chunk.Timestamp[0]
		}
		if len(chunk.Timestamp) > 1 {
			end = chunk.Timestamp[1]
		}
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  chunk.Text,
			Start: start,
			End:   end,
		})
	}

	// durationInSeconds: chunks.at(-1)?.timestamp?.at(1) — the LAST chunk's
	// end timestamp specifically, not the max across all chunks.
	var duration *float64
	if n := len(result.Chunks); n > 0 {
		last := result.Chunks[n-1]
		if len(last.Timestamp) > 1 {
			d := last.Timestamp[1]
			duration = &d
		}
	}

	language := ""
	if len(result.InferredLanguages) > 0 {
		language = result.InferredLanguages[0]
	}

	usage := types.TranscriptionUsage{}
	if duration != nil {
		usage.DurationSeconds = *duration
	}

	var rawValue interface{}
	_ = json.Unmarshal(rawBody, &rawValue)

	return &types.TranscriptionResult{
		Text:              result.Text,
		Segments:          segments,
		Timestamps:        segments,
		Language:          language,
		DurationInSeconds: duration,
		Usage:             usage,
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   responseHeaders,
			Body:      rawValue,
		},
	}, nil
}

// pollForResult polls the fal.ai queue result endpoint every 1s (up to a
// 60s timeout), treating a `{"detail":"Request is still in progress"}` body
// as "keep polling" rather than a failure, matching
// FalTranscriptionModel.doGenerate's polling loop.
func (m *TranscriptionModel) pollForResult(ctx context.Context, resultPath string, headers map[string]string) (*falTranscriptionResultResponse, map[string]string, []byte, error) {
	deadline := time.Now().Add(falTranscriptionPollTimeout)

	for {
		resp, err := m.provider.absClient.Do(ctx, internalhttp.Request{
			Method:  http.MethodGet,
			Path:    resultPath,
			Headers: headers,
		})
		if err != nil {
			return nil, nil, nil, m.handleError(err)
		}

		if resp.StatusCode >= 400 {
			var detail struct {
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal(resp.Body, &detail)
			if detail.Detail == falTranscriptionStillInProgress {
				if time.Now().After(deadline) {
					return nil, nil, nil, providererrors.NewProviderError(
						m.Provider(), 0, "", "Transcription request timed out after 60 seconds", nil,
					)
				}
				select {
				case <-ctx.Done():
					return nil, nil, nil, ctx.Err()
				case <-time.After(falTranscriptionPollInterval):
				}
				continue
			}
			return nil, nil, nil, m.handleError(&internalhttp.HTTPStatusError{
				StatusCode: resp.StatusCode,
				Headers:    resp.Headers,
				Body:       resp.Body,
			})
		}

		var result falTranscriptionResultResponse
		if err := json.Unmarshal(resp.Body, &result); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to decode fal transcription response: %w", err)
		}
		return &result, providerutils.ExtractHeaders(resp.Headers), resp.Body, nil
	}
}

func (m *TranscriptionModel) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		message := parseFalErrorMessage(statusErr.Body, string(statusErr.Body))
		providerErr := providererrors.NewProviderError(m.Provider(), statusErr.StatusCode, "", message, err)
		providerErr.ResponseHeaders = providerutils.ExtractHeaders(statusErr.Headers)
		return providerErr
	}
	return providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
}
