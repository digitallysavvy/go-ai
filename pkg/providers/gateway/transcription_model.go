package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for AI Gateway.
type TranscriptionModel struct {
	provider *Provider
	modelID  string
}

// NewTranscriptionModel creates a new AI Gateway transcription model.
func NewTranscriptionModel(provider *Provider, modelID string) *TranscriptionModel {
	return &TranscriptionModel{provider: provider, modelID: modelID}
}

func (m *TranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *TranscriptionModel) Provider() string             { return "gateway" }
func (m *TranscriptionModel) ModelID() string              { return m.modelID }

// DoTranscribe transcribes audio via the Gateway transcription-model endpoint.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	body := map[string]interface{}{
		"audio":     base64.StdEncoding.EncodeToString(opts.Audio),
		"mediaType": opts.MimeType,
	}
	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := m.getModelConfigHeaders()
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)

	var response gatewayTranscriptionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/transcription-model",
		Body:    body,
		Headers: headers,
	}, &response)
	if err != nil {
		return nil, (&LanguageModel{provider: m.provider, modelID: m.modelID}).handleErrorWithContext(ctx, err)
	}

	segments := make([]types.TranscriptionTimestamp, 0, len(response.Segments))
	for _, segment := range response.Segments {
		segments = append(segments, types.TranscriptionTimestamp{
			Text:  segment.Text,
			Start: segment.StartSecond,
			End:   segment.EndSecond,
		})
	}

	warnings := response.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	return &types.TranscriptionResult{
		Text:              response.Text,
		Segments:          segments,
		Language:          response.Language,
		DurationInSeconds: response.DurationInSeconds,
		Timestamps:        segments,
		Warnings:          warnings,
		ProviderMetadata:  response.ProviderMetadata,
		Response: &types.ResponseMetadata{
			Timestamp: time.Now(),
			ModelID:   m.modelID,
			Headers:   flattenHeaders(httpResp.Headers),
			Body:      parseGatewayTranscriptionRawBody(httpResp.Body),
		},
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationValue(response.DurationInSeconds),
		},
	}, nil
}

func (m *TranscriptionModel) getModelConfigHeaders() map[string]string {
	return map[string]string{
		"ai-transcription-model-specification-version": "4",
		"ai-model-id": m.modelID,
	}
}

type gatewayTranscriptionResponse struct {
	Text              string                 `json:"text"`
	Segments          []gatewaySegment       `json:"segments,omitempty"`
	Language          string                 `json:"language,omitempty"`
	DurationInSeconds *float64               `json:"durationInSeconds,omitempty"`
	Warnings          []types.Warning        `json:"warnings,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
}

type gatewaySegment struct {
	Text        string  `json:"text"`
	StartSecond float64 `json:"startSecond"`
	EndSecond   float64 `json:"endSecond"`
}

func durationValue(duration *float64) float64 {
	if duration == nil {
		return 0
	}
	return *duration
}

func parseGatewayTranscriptionRawBody(body []byte) interface{} {
	var raw interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Sprintf("%s", body)
	}
	return raw
}
