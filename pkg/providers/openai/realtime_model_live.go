package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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

// Compile-time checks that *OpenAIRealtimeModelLive still implements the
// capabilities it authenticates and frames its session with. There is no
// compile-time way to assert the *absence* of RealtimeClientSecretCreator /
// RealtimeWebSocketConfigProvider (see the comment below); that half is
// covered at runtime by
// TestOpenAIRealtimeModelLive_DoesNotImplementClientSecretOrWebSocketConfig.
var (
	_ provider.Experimental_RealtimeModelV4          = (*OpenAIRealtimeModelLive)(nil)
	_ provider.RealtimeServerWebSocketConfigProvider = (*OpenAIRealtimeModelLive)(nil)
	_ provider.RealtimeLifecycleProvider             = (*OpenAIRealtimeModelLive)(nil)
	_ provider.RealtimeServerConnectionProvider      = (*OpenAIRealtimeModelLive)(nil)
	_ provider.RealtimeIntentCodec                   = (*OpenAIRealtimeModelLive)(nil)
)

// liveConversationIntent rejects every intent but conversation (TS
// assertConversationIntent, 892af4dab7).
func liveConversationIntent(intent provider.RealtimeSessionIntent) error {
	if intent != "" && intent != provider.RealtimeSessionIntentConversation {
		return fmt.Errorf("OpenAI Live session intent '%s' is not supported.", intent)
	}
	return nil
}

// GetServerConnection is GetServerWebSocketConfig for an intent; Live has
// the conversation endpoint only (TS getServerWebSocketConfig({intent})).
func (m *OpenAIRealtimeModelLive) GetServerConnection(ctx context.Context, opts provider.RealtimeServerConnectionOptions) (provider.WebSocketConfig, error) {
	if err := liveConversationIntent(opts.Intent); err != nil {
		return provider.WebSocketConfig{}, err
	}
	return m.serverWebSocketConfig(func() (map[string]string, error) { return m.provider.client.RequestHeadersCtx(ctx) })
}

// BuildSessionConfigForIntent is BuildSessionConfig for an intent.
func (m *OpenAIRealtimeModelLive) BuildSessionConfigForIntent(config provider.RealtimeSessionConfig, intent provider.RealtimeSessionIntent) (any, error) {
	if err := liveConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.BuildSessionConfig(config), nil
}

// SerializeClientEventForIntent is SerializeClientEvent for an intent.
func (m *OpenAIRealtimeModelLive) SerializeClientEventForIntent(event provider.RealtimeClientEvent, intent provider.RealtimeSessionIntent) (json.RawMessage, error) {
	if err := liveConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.SerializeClientEvent(event)
}

// DoCreateClientSecret and GetWebSocketConfig are intentionally NOT
// implemented: short-lived browser credentials are not supported over the
// server-WebSocket flow. Connect with a server-side API key via
// GetServerWebSocketConfig instead. Mirrors TS OpenAIRealtimeModelLive,
// which implements neither doCreateClientSecret nor getWebSocketConfig
// (both optional in RealtimeModelV4); createOpenAIRealtimeFactory().getToken()
// rejects OpenAIRealtimeModelLive with an explicit instanceof check before
// ever calling doCreateClientSecret (see (*Provider).GetRealtimeToken's
// RealtimeClientSecretCreator type assertion, the Go equivalent). pkg/ai's
// ConnectRealtime prefers GetServerWebSocketConfig when a model implements
// provider.RealtimeServerWebSocketConfigProvider, which this model does, so
// GetWebSocketConfig is never needed for Live connections either.

// GetServerWebSocketConfig builds the server-authenticated Live WebSocket
// connection: the provider's own request headers (Authorization,
// OpenAI-Organization, OpenAI-Project, and any custom headers), and no
// per-connection token/subprotocol. Mirrors TS
// OpenAIRealtimeModelLive.getServerWebSocketConfig().
func (m *OpenAIRealtimeModelLive) GetServerWebSocketConfig() (provider.WebSocketConfig, error) {
	return m.serverWebSocketConfig(m.provider.client.RequestHeaders)
}

func (m *OpenAIRealtimeModelLive) serverWebSocketConfig(requestHeaders func() (map[string]string, error)) (provider.WebSocketConfig, error) {
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
	// TS `${baseURL}/live/sessions`: the base URL lost one trailing "/" in New.
	u.Path += "/live/sessions"

	// TS getServerWebSocketConfig reads config.headers(): a missing API key
	// is a LoadAPIKeyError, not a handshake without credentials.
	headers, err := requestHeaders()
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	return provider.WebSocketConfig{URL: u.String(), Headers: headers}, nil
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
