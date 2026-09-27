package openai

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// knownLiveModelIDs lists OpenAI Realtime model IDs that route to the Live
// API by default (no explicit api option required). Mirrors the TypeScript
// SDK's openai-realtime-factory.ts knownLiveModelIds.
var knownLiveModelIDs = map[string]bool{"gpt-live-1": true}

// OpenAIRealtimeModelLive implements the experimental OpenAI Live API: a
// continuous, server-managed WebSocket session (as opposed to the GA
// Realtime API's browser-direct, ephemeral-token connection implemented by
// OpenAIRealtimeModel). The connection is authenticated with the server's
// own API key via request headers, so short-lived client credentials are
// not supported; the browser-only WebRTC transport is TS-only and not
// implemented here.
type OpenAIRealtimeModelLive struct {
	provider *Provider
	modelID  string
}

// NewRealtimeModelLive creates an OpenAI Live realtime model.
func NewRealtimeModelLive(p *Provider, modelID string) *OpenAIRealtimeModelLive {
	return &OpenAIRealtimeModelLive{provider: p, modelID: modelID}
}

func (m *OpenAIRealtimeModelLive) SpecificationVersion() string { return "v4" }
func (m *OpenAIRealtimeModelLive) Provider() string             { return m.provider.Name() + ".live" }
func (m *OpenAIRealtimeModelLive) ModelID() string              { return m.modelID }

// DoCreateClientSecret always fails for Live: short-lived browser
// credentials are not supported over the server-WebSocket flow. Connect
// with a server-side API key via GetServerWebSocketConfig instead. Mirrors
// TS createOpenAIRealtimeFactory().getToken() rejecting
// OpenAIRealtimeModelLive.
func (m *OpenAIRealtimeModelLive) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	return provider.ClientSecretResult{}, &providererrors.UnsupportedFunctionalityError{
		Functionality: "Short-lived OpenAI credentials for the Live API. Use server WebSocket setup via GetServerWebSocketConfig() with a server-side API key instead.",
	}
}

// GetWebSocketConfig is required by provider.Experimental_RealtimeModelV4
// but unused for Live connections: pkg/ai.ConnectRealtime prefers
// GetServerWebSocketConfig when a model implements
// provider.RealtimeServerWebSocketConfigProvider, which this model does.
func (m *OpenAIRealtimeModelLive) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	cfg, _ := m.GetServerWebSocketConfig()
	return cfg
}

// GetServerWebSocketConfig builds the server-authenticated Live WebSocket
// connection: the provider's own request headers (Authorization,
// OpenAI-Organization, OpenAI-Project, and any custom headers), and no
// per-connection token/subprotocol. Mirrors TS
// OpenAIRealtimeModelLive.getServerWebSocketConfig().
func (m *OpenAIRealtimeModelLive) GetServerWebSocketConfig() (provider.WebSocketConfig, error) {
	base := m.provider.config.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/live/sessions"

	return provider.WebSocketConfig{URL: u.String(), Headers: m.provider.client.Headers()}, nil
}

// RealtimeLifecycle reports Live's non-default session framing: the session
// is started explicitly with "session.start" (it does not exist on
// connect) and ended gracefully with "session.close". Mirrors TS
// OpenAIRealtimeModelLive.capabilities.{startup,finalization}.
func (m *OpenAIRealtimeModelLive) RealtimeLifecycle() provider.RealtimeLifecycle {
	return provider.RealtimeLifecycle{StartupEventType: "session-start", FinalizationEventType: "session-close"}
}

func (m *OpenAIRealtimeModelLive) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	session, _ := buildOpenAILiveSessionConfig(config, m.modelID)
	return session
}

func (m *OpenAIRealtimeModelLive) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	return parseOpenAILiveServerEvent(raw), nil
}

func (m *OpenAIRealtimeModelLive) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	return serializeOpenAILiveClientEvent(event, m.modelID)
}
