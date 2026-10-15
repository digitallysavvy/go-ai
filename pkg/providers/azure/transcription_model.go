package azure

import (
	"context"
	"fmt"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// TranscriptionModel implements the provider.TranscriptionModel interface for Azure OpenAI
type TranscriptionModel struct {
	provider     *Provider
	deploymentID string
}

// NewTranscriptionModel creates a new Azure OpenAI transcription model
func NewTranscriptionModel(provider *Provider, deploymentID string) *TranscriptionModel {
	return &TranscriptionModel{
		provider:     provider,
		deploymentID: deploymentID,
	}
}

// SpecificationVersion returns the specification version
func (m *TranscriptionModel) SpecificationVersion() string {
	return "v4"
}

// Provider returns the provider name
func (m *TranscriptionModel) Provider() string {
	return "azure.transcription"
}

// ModelID returns the model ID (deployment ID for Azure)
func (m *TranscriptionModel) ModelID() string {
	return m.deploymentID
}

// DoTranscribe performs speech-to-text transcription through the OpenAI
// transcription model configured for this deployment (TS wraps
// OpenAITranscriptionModel), so providerOptions.openai, media type, headers,
// segments and response metadata match.
func (m *TranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	return openai.NewTranscriptionModel(m.provider.openaiDelegate(m.deploymentID), m.deploymentID).DoTranscribe(ctx, opts)
}

// DoStream streams a transcript for live audio over the OpenAI realtime
// WebSocket (gpt-realtime-whisper deployments). Mirrors TS
// AzureTranscriptionModel#doStream's default ("openai") branch, which wraps
// an OpenAITranscriptionModel instance constructed with Azure's own
// url/headers closures -- so this reuses the openai package's realtime
// transcription session (openai.NewOpenAIRealtimeTranscriptionStream) with
// Azure's own URL and headers, the same way DoTranscribe above reuses the
// REST wire format but through p.endpointPath/p.requestHeaders instead of
// OpenAI's.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if !openai.IsRealtimeTranscriptionModelID(m.deploymentID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("streaming transcription with %s", m.deploymentID),
		}
	}

	streamOpts := openai.TranscriptionStreamOpenAIOptions(opts.ProviderOptions)

	// options that only apply to the REST transcription endpoint, mirroring
	// TS's doStream check on the raw providerOptions.openai fields.
	var warnings []types.Warning
	if streamOpts.HasInclude {
		warnings = append(warnings, openai.UnsupportedStreamingTranscriptionWarning("include"))
	}
	if streamOpts.Prompt != "" {
		warnings = append(warnings, openai.UnsupportedStreamingTranscriptionWarning("prompt"))
	}
	if streamOpts.HasTemperature {
		warnings = append(warnings, openai.UnsupportedStreamingTranscriptionWarning("temperature"))
	}
	if len(streamOpts.TimestampGranularities) > 0 {
		warnings = append(warnings, openai.UnsupportedStreamingTranscriptionWarning("timestampGranularities"))
	}

	wsURL, err := m.realtimeWebSocketURL()
	if err != nil {
		return nil, err
	}

	// TS's getHeaders('openai') returns only the static api-key header (or
	// nothing when an AD token provider is configured -- that case's bearer
	// token is injected by a custom `fetch` wrapper at REST call time, which
	// a raw WebSocket dial never goes through either in TS or here), merged
	// with options.headers via combineHeaders.
	headers := internalhttp.MergeHeaders(m.provider.staticAuthHeaders(), opts.Headers)
	sessionUpdate := openai.BuildOpenAIRealtimeTranscriptionSession(m.deploymentID, opts.InputAudioFormat, streamOpts)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := openai.NewOpenAIRealtimeTranscriptionStream(abortCtx, openai.OpenAIRealtimeTranscriptionStreamConfig{
		URL:              wsURL,
		Headers:          headers,
		SessionUpdate:    sessionUpdate,
		Language:         streamOpts.Language,
		Warnings:         warnings,
		Audio:            opts.Audio,
		IncludeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: sessionUpdate,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.deploymentID},
	}, nil
}

// realtimeWebSocketURL builds the realtime WebSocket URL for the "openai"
// API path, mirroring TS's shared `url({path: '/realtime?intent=transcription',
// modelId})` closure -- the same URL builder used for Azure's
// Chat/Completion/Responses/Image models (p.responsesBaseURL +
// p.includeAPIVersionQuery) -- converted from http(s) to ws(s).
func (m *TranscriptionModel) realtimeWebSocketURL() (string, error) {
	target := m.provider.responsesBaseURL(m.deploymentID) + "/realtime?intent=transcription"
	if m.provider.includeAPIVersionQuery() {
		target += "&api-version=" + m.provider.config.APIVersion
	}
	return toWebSocketURL(target)
}

var (
	_ provider.TranscriptionModel    = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
)
