package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"reflect"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// RealtimeModel is the Gateway's realtime model, mirroring TS
// GatewayRealtimeModel (gateway-realtime-model.ts). The Gateway normalizes
// realtime exactly like it normalizes every other modality: the client
// speaks the normalized AI SDK realtime protocol and the Gateway translates
// to and from the upstream provider server-side, so this model is a thin
// identity codec over that normalized protocol — only the connection and
// authentication are Gateway-specific. It implements
// provider.Experimental_RealtimeModelV4 so it can be driven through
// ai.ConnectRealtime exactly like the OpenAI/Google/xAI realtime models.
type RealtimeModel struct {
	provider *Provider
	modelID  string
}

func (p *Provider) ExperimentalRealtime(modelID string) *RealtimeModel {
	return &RealtimeModel{provider: p, modelID: modelID}
}

// GetRealtimeToken mints a client secret for modelID, mirroring TS
// gateway.experimental_realtime.getToken.
func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	return p.ExperimentalRealtime(opts.Model).DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *RealtimeModel) SpecificationVersion() string { return "v4" }
func (m *RealtimeModel) Provider() string             { return "gateway.realtime" }
func (m *RealtimeModel) ModelID() string              { return m.modelID }

// DoCreateClientSecret mints a single-use, short-lived client secret
// (vcst_) the browser uses to open the realtime WebSocket without ever
// holding the long-lived Gateway credential, mirroring TS
// GatewayRealtimeModel#doCreateClientSecret. opts.SessionConfig is
// intentionally unused here, exactly like TS: the session config is applied
// later via the normalized session-update event, not at mint time.
func (m *RealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	params := MintRealtimeClientSecretParams{ModelID: m.modelID}
	if opts.ExpiresAfterSeconds != nil {
		params.ExpiresAfterSeconds = opts.ExpiresAfterSeconds
	}
	secret, err := m.provider.MintRealtimeClientSecret(ctx, params)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	wsURL, err := gatewayWebSocketURLChecked(m.provider.baseURL, "/realtime-model", m.modelID)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return provider.ClientSecretResult{
		Token:     secret.Token,
		URL:       wsURL,
		ExpiresAt: secret.ExpiresAt,
	}, nil
}

// GetWebSocketConfig mirrors TS GatewayRealtimeModel#getWebSocketConfig:
// the team scope is the provider's teamIdOrSlug setting (TS
// config.teamIdOrSlug), not a custom x-vercel-ai-gateway-team header (which
// only the streaming transcription and speech handshakes read).
func (m *RealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	return provider.WebSocketConfig{
		URL:       wsURL,
		Protocols: GetGatewayRealtimeProtocols(token, m.provider.config.TeamIDOrSlug),
	}
}

// ParseServerEvent mirrors TS GatewayRealtimeModel#parseServerEvent: the
// Gateway emits normalized AI SDK realtime events directly (`return raw as
// RealtimeModelV4ServerEvent`), so no provider-specific field mapping is
// needed — the wire JSON already uses the same field names as
// provider.RealtimeServerEvent. Raw is set to the full frame (rather than
// left at whatever the wire's own "raw" subfield decoded to), matching every
// other realtime model's convention (OpenAI/Google/xAI) of attaching the
// entire received frame.
func (m *RealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	var event provider.RealtimeServerEvent
	if err := jsonexact.Unmarshal(raw, &event); err != nil {
		return nil, err
	}
	event.Raw = raw
	return []provider.RealtimeServerEvent{event}, nil
}

// SerializeClientEvent mirrors TS GatewayRealtimeModel#serializeClientEvent
// (`return event`): the Gateway accepts the normalized client event
// directly. Go's provider.RealtimeClientEvent is a single flat struct
// shared by every event type, so an unconditional marshal would also
// serialize its other, unrelated zero-valued fields (e.g. an empty "config"
// object on a non-session event) as extra wire keys a plain JS object
// never has; this instead emits only the fields the event's Type actually
// carries, matching the exact shape of the corresponding TS union member.
func (m *RealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	var out map[string]interface{}
	switch event.Type {
	case "session-update":
		out = map[string]interface{}{"type": event.Type, "config": m.BuildSessionConfig(event.Config)}
	case "input-audio-append":
		out = map[string]interface{}{"type": event.Type, "audio": event.Audio}
	case "input-audio-commit", "input-audio-clear", "response-cancel":
		out = map[string]interface{}{"type": event.Type}
	case "conversation-item-create":
		item := gatewayRealtimeConversationItem(event.Item)
		if item == nil {
			return nil, nil
		}
		out = map[string]interface{}{"type": event.Type, "item": item}
	case "conversation-item-truncate":
		out = map[string]interface{}{
			"type":         event.Type,
			"itemId":       event.ItemID,
			"contentIndex": event.ContentIndex,
			"audioEndMs":   event.AudioEndMs,
		}
	case "response-create":
		out = map[string]interface{}{"type": event.Type}
		if event.Options != nil {
			out["options"] = event.Options
		}
	case "session-start":
		out = map[string]interface{}{"type": event.Type, "config": m.BuildSessionConfig(event.Config)}
	case "session-close", "input-audio-mute", "input-audio-unmute":
		out = map[string]interface{}{"type": event.Type}
	case "context-append":
		out = map[string]interface{}{"type": event.Type, "content": event.Content}
		if event.DelegationID != nil {
			out["delegationId"] = *event.DelegationID
		} else {
			out["delegationId"] = nil
		}
		if len(event.ProviderOptions) > 0 {
			out["providerOptions"] = event.ProviderOptions
		}
	default:
		// TS returns the event itself (`return event`), so an event type
		// this SDK doesn't model still reaches the Gateway, carrying
		// whichever of its fields are set.
		if event.Type == "" {
			return nil, nil
		}
		out = map[string]interface{}{"type": event.Type}
		if !reflect.ValueOf(event.Config).IsZero() {
			out["config"] = event.Config
		}
		if event.Audio != "" {
			out["audio"] = event.Audio
		}
		if item := gatewayRealtimeConversationItem(event.Item); item != nil {
			out["item"] = item
		}
		if event.ItemID != "" {
			out["itemId"] = event.ItemID
		}
		if event.ContentIndex != 0 {
			out["contentIndex"] = event.ContentIndex
		}
		if event.AudioEndMs != 0 {
			out["audioEndMs"] = event.AudioEndMs
		}
		if event.Options != nil {
			out["options"] = event.Options
		}
		if event.Content != "" {
			out["content"] = event.Content
		}
		if event.DelegationID != nil {
			out["delegationId"] = *event.DelegationID
		}
		if len(event.ProviderOptions) > 0 {
			out["providerOptions"] = event.ProviderOptions
		}
	}
	// TS returns the event itself, so an optional eventId rides along.
	if event.EventID != "" {
		out["eventId"] = event.EventID
	}
	return jsonstr.Marshal(out)
}

// gatewayRealtimeConversationItem builds the wire object for one
// conversation item, mirroring the TS RealtimeModelV4ConversationItem
// discriminated union (only the fields relevant to item.Type, matching
// field names): text-message{type,role,text}, audio-message{type,role,audio},
// function-call-output{type,callId,name?,output}.
func gatewayRealtimeConversationItem(item provider.RealtimeConversationItem) map[string]interface{} {
	switch item.Type {
	case "text-message":
		return map[string]interface{}{"type": item.Type, "role": item.Role, "text": item.Text}
	case "audio-message":
		return map[string]interface{}{"type": item.Type, "role": item.Role, "audio": item.Audio}
	case "function-call-output":
		out := map[string]interface{}{"type": item.Type, "callId": item.CallID, "output": item.Output}
		if item.Name != nil {
			out["name"] = *item.Name
		}
		return out
	default:
		return nil
	}
}

// BuildSessionConfig mirrors TS GatewayRealtimeModel#buildSessionConfig
// (`return config`): the session config is already normalized; the Gateway
// maps it to the upstream provider's session payload server-side.
func (m *RealtimeModel) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	return config
}

var (
	_ provider.RealtimeServerConnectionProvider = (*RealtimeModel)(nil)
	_ provider.RealtimeIntentCodec              = (*RealtimeModel)(nil)
)

// gatewayRealtimeAssertConversationIntent mirrors TS assertConversationIntent
// (892af4dab7): the normalized realtime route is conversational only. The
// empty intent is conversation.
func gatewayRealtimeAssertConversationIntent(intent provider.RealtimeSessionIntent) error {
	if intent != "" && intent != provider.RealtimeSessionIntentConversation {
		return fmt.Errorf("AI Gateway realtime does not support the '%s' session intent on the normalized realtime route.", intent) //nolint:staticcheck // matches the TS error text
	}
	return nil
}

// GetServerConnection returns the server-side WebSocket connection (TS
// getServerWebSocketConfig, 892af4dab7): the realtime-model route with the
// provider's resolved HTTP auth headers (Authorization, auth method,
// protocol version, team and custom headers). Conversation intent only.
// ConnectRealtime uses it only when RealtimeSessionOptions.Intent is set
// (coordinator ruling 1).
func (m *RealtimeModel) GetServerConnection(ctx context.Context, opts provider.RealtimeServerConnectionOptions) (provider.WebSocketConfig, error) {
	if err := gatewayRealtimeAssertConversationIntent(opts.Intent); err != nil {
		return provider.WebSocketConfig{}, err
	}
	token, authMethod, err := m.provider.authResolver(ctx)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	headers := mergeHeadersFold(
		map[string]string{"Authorization": "Bearer " + token, "ai-gateway-auth-method": authMethod},
		m.provider.headers,
	)
	// TS getHeaders goes through withUserAgentSuffix, which lower-cases the
	// header names; removeUndefinedEntries drops unset values.
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if v != "" {
			out[strings.ToLower(k)] = v
		}
	}
	wsURL, err := gatewayWebSocketURLChecked(m.provider.baseURL, "/realtime-model", m.modelID)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	return provider.WebSocketConfig{URL: wsURL, Headers: out}, nil
}

// BuildSessionConfigForIntent is BuildSessionConfig for an intent
// (conversation only).
func (m *RealtimeModel) BuildSessionConfigForIntent(config provider.RealtimeSessionConfig, intent provider.RealtimeSessionIntent) (any, error) {
	if err := gatewayRealtimeAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.BuildSessionConfig(config), nil
}

// SerializeClientEventForIntent is SerializeClientEvent for an intent
// (conversation only).
func (m *RealtimeModel) SerializeClientEventForIntent(event provider.RealtimeClientEvent, intent provider.RealtimeSessionIntent) (json.RawMessage, error) {
	if err := gatewayRealtimeAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.SerializeClientEvent(event)
}

var _ provider.Experimental_RealtimeModelV4 = (*RealtimeModel)(nil)

// Compile-time checks that *RealtimeModel still satisfies the optional
// client-secret-WebSocket capabilities now that DoCreateClientSecret and
// GetWebSocketConfig moved off Experimental_RealtimeModelV4 into
// RealtimeClientSecretCreator/RealtimeWebSocketConfigProvider (hand-off:
// "realtime optional capabilities"). GetRealtimeToken above calls
// DoCreateClientSecret directly on the concrete type rather than through a
// type assertion, so losing either method would otherwise only surface as
// a runtime failure elsewhere.
var (
	_ provider.RealtimeClientSecretCreator     = (*RealtimeModel)(nil)
	_ provider.RealtimeWebSocketConfigProvider = (*RealtimeModel)(nil)
)

// ToGatewayRealtimeURL builds the Gateway realtime WebSocket URL. The
// HTTP(S) base URL is upgraded to WS(S) and the model id rides the
// ?ai-model-id= query — the WS transport of the ai-model-id header the HTTP
// routes use, since a browser WebSocket cannot set headers. The model id is
// passed through verbatim; the Gateway owns resolution (including the bare
// -> "openai/" qualification), exactly like the non-realtime routes. The URL
// is the WHATWG serialization of TS new URL(...); it is "" when the base URL
// does not parse (TS throws "Invalid URL").
func ToGatewayRealtimeURL(baseURL string, modelID string) string {
	return gatewayWebSocketURL(baseURL, "/realtime-model", modelID)
}
