package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
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

// TranscriptionClientSecretOptions configures GetTranscriptionToken.
type TranscriptionClientSecretOptions struct {
	// ExpiresAfterSeconds is the token lifetime in seconds. Gateway default is 60s (max 300s).
	ExpiresAfterSeconds *int
}

// TranscriptionClientSecretResult is a minted transcription-bound client
// secret, mirroring TS GatewayTranscriptionFactoryGetTokenResult.
type TranscriptionClientSecretResult struct {
	// Token is the minted "vcst_" client secret.
	Token string
	// URL is the WebSocket URL of the streaming transcription surface for this model.
	URL string
	// ExpiresAt is the token expiry, epoch seconds.
	ExpiresAt *int64
}

// ExperimentalTranscription returns a transcription model bound to modelID.
// It mirrors the TypeScript SDK's callable
// gateway.experimental_transcription(modelId); use GetTranscriptionToken for
// its .getToken() counterpart.
func (p *Provider) ExperimentalTranscription(modelID string) *TranscriptionModel {
	return NewTranscriptionModel(p, modelID)
}

// GetTranscriptionToken mints a short-lived, transcription-bound client
// secret via the Gateway realtime client-secret route with routeKind set to
// "transcription" (TS gateway.experimental_transcription.getToken). The
// returned token is meant to be handed to a browser client, which connects
// with createGateway({apiKey: token}).transcription(modelID) — the token
// rides the same auth flow as an API key without exposing the long-lived
// Gateway credential.
func (p *Provider) GetTranscriptionToken(ctx context.Context, modelID string, opts *TranscriptionClientSecretOptions) (*TranscriptionClientSecretResult, error) {
	params := MintRealtimeClientSecretParams{ModelID: modelID, RouteKind: "transcription"}
	if opts != nil && opts.ExpiresAfterSeconds != nil {
		params.ExpiresAfterSeconds = opts.ExpiresAfterSeconds
	}
	secret, err := p.MintRealtimeClientSecret(ctx, params)
	if err != nil {
		return nil, err
	}
	return &TranscriptionClientSecretResult{
		Token:     secret.Token,
		URL:       ToGatewayTranscriptionURL(p.baseURL, modelID),
		ExpiresAt: secret.ExpiresAt,
	}, nil
}

// ToGatewayTranscriptionURL builds the Gateway streaming transcription
// WebSocket URL for modelID (TS toGatewayTranscriptionUrl,
// gateway-transcription-model.ts): the HTTP(S) base is upgraded to WS(S) and
// the model id is passed as the "ai-model-id" query parameter, since a
// browser WebSocket cannot set headers and slashes in qualified ids (e.g.
// "openai/gpt-realtime-whisper") must survive query encoding.
func ToGatewayTranscriptionURL(baseURL string, modelID string) string {
	u, err := url.Parse(strings.Replace(baseURL, "http", "ws", 1) + "/transcription-model")
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("ai-model-id", modelID)
	u.RawQuery = query.Encode()
	return u.String()
}

// DoTranscribe transcribes audio via the Gateway transcription-model endpoint.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionOptions{}
	}
	// TS sends a string audio input as given and encodes bytes.
	audio := base64.StdEncoding.EncodeToString(opts.Audio)
	if opts.AudioBase64 != "" {
		audio = opts.AudioBase64
	}
	body := map[string]interface{}{
		"audio":     audio,
		"mediaType": opts.MimeType,
	}
	if opts.ProviderOptions != nil {
		body["providerOptions"] = opts.ProviderOptions
	}

	headers := gatewayRequestHeaders(ctx, opts.Headers, m.getModelConfigHeaders())

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
	rawBody := parseGatewayTranscriptionRawBody(httpResp.Body)
	if isXaiTranscriptionModel(m.modelID) && requestsXaiDiarization(opts.ProviderOptions) && !containsSpeaker(rawBody) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.xai.diarize",
			Details: "AI Gateway does not currently expose xAI speaker diarization metadata.",
		})
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
			Body:      rawBody,
		},
		Usage: types.TranscriptionUsage{
			DurationSeconds: durationValue(response.DurationInSeconds),
		},
		ProviderUsage: response.Usage,
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
	Usage             map[string]interface{} `json:"usage,omitempty"`
	ProviderMetadata  map[string]interface{} `json:"providerMetadata,omitempty"`
}

type gatewaySegment struct {
	Text        string  `json:"text"`
	StartSecond float64 `json:"startSecond"`
	EndSecond   float64 `json:"endSecond"`
}

// isXaiTranscriptionModel reports whether modelID routes to an xAI
// transcription model (TS isXaiTranscriptionModel).
func isXaiTranscriptionModel(modelID string) bool {
	return strings.HasPrefix(modelID, "spacexai/") || strings.HasPrefix(modelID, "xai/")
}

// requestsXaiDiarization reports whether providerOptions asks for xAI
// speaker diarization under either the xai or spacexai namespace.
func requestsXaiDiarization(providerOptions map[string]interface{}) bool {
	for _, ns := range []string{"xai", "spacexai"} {
		if opts, ok := providerOptions[ns].(map[string]interface{}); ok {
			if diarize, ok := opts["diarize"].(bool); ok && diarize {
				return true
			}
		}
	}
	return false
}

// containsSpeaker reports whether value contains a non-null "speaker" key at
// any depth of the decoded response.
func containsSpeaker(value interface{}) bool {
	switch v := value.(type) {
	case []interface{}:
		for _, item := range v {
			if containsSpeaker(item) {
				return true
			}
		}
	case map[string]interface{}:
		for key, nested := range v {
			if (key == "speaker" && nested != nil) || containsSpeaker(nested) {
				return true
			}
		}
	}
	return false
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
		return string(body)
	}
	return raw
}
