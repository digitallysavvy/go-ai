package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// GatewayLiveModelIDs are the realtime model IDs that use the continuous
// Live API by default (TS gatewayLiveModelIds). Other IDs use the realtime
// API unless the Live API is selected explicitly.
var GatewayLiveModelIDs = []string{"openai/gpt-live-1", "openai/gpt-live-1.1"}

// Gateway realtime APIs a model ID can be routed to (TS
// GatewayRealtimeOptions.api).
const (
	RealtimeAPILive     = "live"
	RealtimeAPIRealtime = "realtime"
)

// RealtimeModelOptions selects the Gateway API for a model ID. Mirrors TS
// GatewayRealtimeOptions.
type RealtimeModelOptions struct {
	// API overrides model ID routing: "live" or "realtime". Nil (unset)
	// routes the GatewayLiveModelIDs to Live and everything else to the
	// realtime API; a non-nil value that is neither is rejected.
	API *string
}

func resolveGatewayRealtimeAPI(modelID string, opts RealtimeModelOptions) (string, error) {
	if opts.API != nil {
		if *opts.API != RealtimeAPILive && *opts.API != RealtimeAPIRealtime {
			return "", providererrors.NewArgumentError("api", `AI Gateway realtime api must be "live" or "realtime".`, nil)
		}
		return *opts.API, nil
	}
	for _, id := range GatewayLiveModelIDs {
		if id == modelID {
			return RealtimeAPILive, nil
		}
	}
	return RealtimeAPIRealtime, nil
}

// ExperimentalRealtimeModel creates a Gateway realtime model: a continuous
// Live model for the GatewayLiveModelIDs or an explicit api "live", the
// turn-based realtime model otherwise. Mirrors TS
// gateway.experimental_realtime(modelId, options). ExperimentalRealtime
// keeps returning the turn-based model for every ID.
func (p *Provider) ExperimentalRealtimeModel(modelID string, opts ...RealtimeModelOptions) (provider.Experimental_RealtimeModelV4, error) {
	var o RealtimeModelOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	api, err := resolveGatewayRealtimeAPI(modelID, o)
	if err != nil {
		return nil, err
	}
	if api == RealtimeAPILive {
		return p.ExperimentalLive(modelID), nil
	}
	return p.ExperimentalRealtime(modelID), nil
}

// ExperimentalLive creates the Gateway's continuous Live model (TS
// GatewayRealtimeModelLive).
func (p *Provider) ExperimentalLive(modelID string) *RealtimeModelLive {
	return &RealtimeModelLive{provider: p, modelID: modelID}
}

// RealtimeModelLive is the Gateway's Live model, mirroring TS
// GatewayRealtimeModelLive (7e55599fc7). Live is a continuous conversation
// API: one session-start opens the session, PCM audio streams in both
// directions, and session-close ends it with final usage. The client speaks
// the Gateway's normalized Live protocol on `/live-model`; the Gateway runs
// the provider mapping server-side, so server events arrive normalized.
type RealtimeModelLive struct {
	provider *Provider
	modelID  string
}

func (m *RealtimeModelLive) SpecificationVersion() string { return "v4" }
func (m *RealtimeModelLive) Provider() string             { return "gateway.live" }
func (m *RealtimeModelLive) ModelID() string              { return m.modelID }

// RealtimeConversation reports the continuous conversation kind (TS
// capabilities.conversation).
func (m *RealtimeModelLive) RealtimeConversation() string { return "continuous" }

// RealtimeConnections reports the supported connection kinds (TS
// capabilities.connections).
func (m *RealtimeModelLive) RealtimeConnections() []string {
	return []string{"client-secret-websocket", "server-websocket"}
}

// RealtimeLifecycle reports Live's explicit session-start / session-close
// framing (TS capabilities.startup / finalization).
func (m *RealtimeModelLive) RealtimeLifecycle() provider.RealtimeLifecycle {
	return provider.RealtimeLifecycle{StartupEventType: "session-start", FinalizationEventType: "session-close"}
}

// DoCreateClientSecret mints a short-lived client secret bound to this Live
// model (routeKind "live"). The browser opens the Live WebSocket with it
// through the ai-gateway-auth.<token> subprotocol; the session configuration
// is sent later in session-start.
func (m *RealtimeModelLive) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	params := MintRealtimeClientSecretParams{ModelID: m.modelID, RouteKind: "live"}
	if opts.ExpiresAfterSeconds != nil {
		params.ExpiresAfterSeconds = opts.ExpiresAfterSeconds
	}
	secret, err := m.provider.MintRealtimeClientSecret(ctx, params)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	wsURL, err := gatewayWebSocketURLChecked(m.provider.baseURL, "/live-model", m.modelID)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	return provider.ClientSecretResult{Token: secret.Token, URL: wsURL, ExpiresAt: secret.ExpiresAt}, nil
}

// GetWebSocketConfig carries the token and the team scope in WebSocket
// subprotocols.
func (m *RealtimeModelLive) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	return provider.WebSocketConfig{
		URL:       wsURL,
		Protocols: GetGatewayRealtimeProtocols(token, m.provider.config.TeamIDOrSlug),
	}
}

// GetServerConnection returns the live-model URL and the provider's defined
// request headers for a server connection (TS getServerWebSocketConfig).
// Conversation intent only. It is deliberately not a
// RealtimeServerWebSocketConfigProvider: the client-secret connection stays
// the default.
func (m *RealtimeModelLive) GetServerConnection(ctx context.Context, opts provider.RealtimeServerConnectionOptions) (provider.WebSocketConfig, error) {
	if err := gatewayLiveAssertConversationIntent(opts.Intent); err != nil {
		return provider.WebSocketConfig{}, err
	}
	wsURL, err := gatewayWebSocketURLChecked(m.provider.baseURL, "/live-model", m.modelID)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	headers, err := m.provider.gatewayServerHeaders(ctx)
	if err != nil {
		return provider.WebSocketConfig{}, err
	}
	return provider.WebSocketConfig{URL: wsURL, Headers: headers}, nil
}

func gatewayLiveAssertConversationIntent(intent provider.RealtimeSessionIntent) error {
	if intent != "" && intent != provider.RealtimeSessionIntentConversation {
		return fmt.Errorf("AI Gateway Live does not support the '%s' session intent.", intent) //nolint:staticcheck // matches the TS error text
	}
	return nil
}

// ParseServerEvent passes normalized Gateway events through (their own `raw`
// member is the provider payload). A frame that is not an object with a
// string type is an invalid_server_event error; a Gateway-originated error
// frame carries no provider payload, so the frame itself is kept as raw.
func (m *RealtimeModelLive) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	invalid := provider.RealtimeServerEvent{
		Type:    "error",
		Code:    "invalid_server_event",
		Message: "Invalid AI Gateway Live server event.",
		Raw:     raw,
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return []provider.RealtimeServerEvent{invalid}, nil
	}
	var probe interface{}
	if err := jsonexact.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	frame, ok := probe.(map[string]interface{})
	if !ok {
		return []provider.RealtimeServerEvent{invalid}, nil
	}
	typ, ok := frame["type"].(string)
	if !ok {
		return []provider.RealtimeServerEvent{invalid}, nil
	}
	var event provider.RealtimeServerEvent
	if err := jsonexact.Unmarshal(raw, &event); err != nil {
		return nil, err
	}
	if _, hasRaw := frame["raw"]; typ == "error" && !hasRaw {
		event.Raw = raw
	}
	return []provider.RealtimeServerEvent{event}, nil
}

func gatewayLiveUnsupportedEvent(typ string) error {
	return &providererrors.UnsupportedFunctionalityError{Functionality: "AI Gateway Live client event: " + typ}
}

// SerializeClientEvent maps a client event to the normalized Live event.
func (m *RealtimeModelLive) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	out := providerutils.OrderedMap{Vals: map[string]interface{}{"type": event.Type}}
	add := func(key string, v interface{}) {
		out.Keys = append(out.Keys, key)
		out.Vals[key] = v
	}
	out.Keys = []string{"type"}
	switch event.Type {
	case "session-start":
		config, err := gatewayLiveBuildSessionConfig(event.Config)
		if err != nil {
			return nil, err
		}
		add("modelId", m.modelID)
		add("config", config)
	case "session-close", "input-audio-mute", "input-audio-unmute":
	case "input-audio-append":
		add("audio", event.Audio)
	case "context-append":
		if err := gatewayLiveValidateContextOptions(event.ProviderOptions); err != nil {
			return nil, err
		}
		add("content", event.Content)
		if event.DelegationID != nil {
			add("delegationId", *event.DelegationID)
		} else {
			add("delegationId", nil)
		}
		if event.ProviderOptions != nil {
			add("providerOptions", event.ProviderOptions)
		}
	default:
		return nil, gatewayLiveUnsupportedEvent(event.Type)
	}
	if event.EventID != "" {
		add("eventId", event.EventID)
	}
	return jsonstr.Marshal(out)
}

// BuildSessionConfig returns the session configuration for session-start.
// It cannot report an invalid configuration (the interface has no error);
// SerializeClientEvent and BuildSessionConfigForIntent do.
func (m *RealtimeModelLive) BuildSessionConfig(config provider.RealtimeSessionConfig) any {
	return config
}

// BuildSessionConfigForIntent validates the configuration for Live and
// returns it (conversation only).
func (m *RealtimeModelLive) BuildSessionConfigForIntent(config provider.RealtimeSessionConfig, intent provider.RealtimeSessionIntent) (any, error) {
	if err := gatewayLiveAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return gatewayLiveBuildSessionConfig(config)
}

// SerializeClientEventForIntent is SerializeClientEvent for an intent
// (conversation only).
func (m *RealtimeModelLive) SerializeClientEventForIntent(event provider.RealtimeClientEvent, intent provider.RealtimeSessionIntent) (json.RawMessage, error) {
	if err := gatewayLiveAssertConversationIntent(intent); err != nil {
		return nil, err
	}
	return m.SerializeClientEvent(event)
}

var (
	_ provider.Experimental_RealtimeModelV4    = (*RealtimeModelLive)(nil)
	_ provider.RealtimeClientSecretCreator     = (*RealtimeModelLive)(nil)
	_ provider.RealtimeWebSocketConfigProvider = (*RealtimeModelLive)(nil)
	_ provider.RealtimeLifecycleProvider       = (*RealtimeModelLive)(nil)
	_ provider.RealtimeIntentCodec             = (*RealtimeModelLive)(nil)
	// Not RealtimeServerWebSocketConfigProvider: that interface would make
	// ConnectRealtime pick the server connection over the client secret.
	_ provider.RealtimeServerConnectionProvider = (*RealtimeModelLive)(nil)
)

var (
	gatewayLiveOpenAIOptions = map[string]bool{"delegation": true, "input": true, "store": true, "voice": true}
	gatewayLiveContextChans  = map[string]bool{"commentary": true, "instructions": true, "thinking": true}
	gatewayLiveOpenAIMessage = map[string]string{
		"delegation": `delegation must be { type: "client" } or null.`,
		"input":      `input must be at most 128 messages, each { type: "message", role, content: [{ type, text }] }.`,
		"store":      `store must be a boolean.`,
		"voice":      `voice must be { id: string } with a nonempty id.`,
	}
)

func gatewayLiveUnsupported(functionality string) error {
	return &providererrors.UnsupportedFunctionalityError{Functionality: functionality}
}

// gatewayLiveBuildSessionConfig is TS buildSessionConfig: a setting Live does
// not take, an invalid format or option, is an error; unset settings (nil)
// are dropped.
func gatewayLiveBuildSessionConfig(config provider.RealtimeSessionConfig) (provider.RealtimeSessionConfig, error) {
	for _, s := range []struct {
		key string
		set bool
	}{
		{"outputModalities", config.OutputModalities != nil},
		{"inputAudioTranscription", config.InputAudioTranscription != nil},
		{"outputAudioTranscription", config.OutputAudioTranscription != nil},
		{"turnDetection", config.TurnDetection != nil},
		{"tools", config.Tools != nil},
	} {
		if s.set {
			return config, gatewayLiveUnsupported("AI Gateway Live session setting: " + s.key)
		}
	}
	if err := gatewayLiveValidateSessionProviderOptions(config.ProviderOptions); err != nil {
		return config, err
	}
	input, output := config.InputAudioFormat, config.OutputAudioFormat
	for _, f := range []struct {
		argument string
		format   *provider.RealtimeAudioFormat
	}{{"inputAudioFormat", input}, {"outputAudioFormat", output}} {
		if f.format != nil && !gatewayLiveSupportedAudioFormat(*f.format) {
			rate := "an unspecified"
			if f.format.Rate != nil {
				rate = fmt.Sprint(*f.format.Rate)
			}
			return config, gatewayLiveUnsupported(fmt.Sprintf("AI Gateway Live %s %s at %s Hz; use audio/pcm at 16000 or 24000 Hz, or audio/pcma or audio/pcmu at 8000 Hz", f.argument, f.format.Type, rate))
		}
	}
	if input != nil && output != nil && (input.Type != output.Type || !sameRate(input.Rate, output.Rate)) {
		return config, providererrors.NewArgumentError("outputAudioFormat", "AI Gateway Live requires the same input and output audio format.", nil)
	}
	return config, nil
}

func sameRate(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func gatewayLiveSupportedAudioFormat(f provider.RealtimeAudioFormat) bool {
	rate := -1
	if f.Rate != nil {
		rate = *f.Rate
	}
	if f.Type == "audio/pcm" {
		return rate == 16000 || rate == 24000
	}
	return (f.Type == "audio/pcma" || f.Type == "audio/pcmu") && rate == 8000
}

// gatewayLiveNormalize is the JSON value of a caller's provider options, so
// typed Go containers validate like the JS objects TS receives.
func gatewayLiveNormalize(v interface{}) (interface{}, error) {
	b, err := jsonstr.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func gatewayLiveSortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// gatewayLiveValidateSessionProviderOptions: gateway options stay
// service-owned; OpenAI Live options are WebSocket-only. A nil value is a JS
// null (Go has no undefined: leave the key out instead).
func gatewayLiveValidateSessionProviderOptions(providerOptions map[string]interface{}) error {
	if providerOptions == nil {
		return nil
	}
	normalized, err := gatewayLiveNormalize(providerOptions)
	if err != nil {
		return providererrors.NewArgumentError("providerOptions", "providerOptions must be an object.", nil)
	}
	options, _ := normalized.(map[string]interface{})
	for _, namespace := range gatewayLiveSortedKeys(options) {
		if namespace != "gateway" && namespace != "openai" {
			return gatewayLiveUnsupported("AI Gateway Live provider options namespace: " + namespace)
		}
		if _, ok := options[namespace].(map[string]interface{}); !ok {
			return providererrors.NewArgumentError("providerOptions."+namespace, fmt.Sprintf("providerOptions.%s must be an object.", namespace), nil)
		}
	}
	openai, ok := options["openai"].(map[string]interface{})
	if !ok {
		return nil
	}
	for _, key := range gatewayLiveSortedKeys(openai) {
		value := openai[key]
		if !gatewayLiveOpenAIOptions[key] {
			return gatewayLiveUnsupported("AI Gateway Live OpenAI option: " + key)
		}
		if key == "delegation" && value != nil {
			if m, ok := value.(map[string]interface{}); !ok || !gatewayLiveExactKeys(m, "type") || m["type"] != "client" {
				return gatewayLiveUnsupported(`AI Gateway Live delegation other than { type: "client" } or null`)
			}
		}
		if !gatewayLiveValidOpenAIOption(key, value) {
			return providererrors.NewArgumentError("providerOptions.openai."+key, gatewayLiveOpenAIMessage[key], nil)
		}
	}
	return nil
}

func gatewayLiveExactKeys(m map[string]interface{}, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

func gatewayLiveValidOpenAIOption(key string, value interface{}) bool {
	switch key {
	case "delegation":
		return true
	case "store":
		_, ok := value.(bool)
		return ok
	case "voice":
		m, ok := value.(map[string]interface{})
		if !ok || !gatewayLiveExactKeys(m, "id") {
			return false
		}
		id, ok := m["id"].(string)
		return ok && id != ""
	case "input":
		list, ok := value.([]interface{})
		if !ok || len(list) > 128 {
			return false
		}
		for _, message := range list {
			if !gatewayLiveInputMessage(message) {
				return false
			}
		}
		return true
	}
	return false
}

func gatewayLiveInputMessage(v interface{}) bool {
	message, ok := v.(map[string]interface{})
	if !ok || !gatewayLiveExactKeys(message, "type", "role", "content") || message["type"] != "message" {
		return false
	}
	content, ok := message["content"].([]interface{})
	if !ok || len(content) != 1 {
		return false
	}
	part, ok := content[0].(map[string]interface{})
	if !ok || !gatewayLiveExactKeys(part, "type", "text") {
		return false
	}
	if _, ok := part["text"].(string); !ok {
		return false
	}
	switch message["role"] {
	case "assistant":
		return part["type"] == "text" || part["type"] == "output_text"
	case "developer", "user":
		return part["type"] == "input_text"
	}
	return false
}

// gatewayLiveValidateContextOptions allows only openai.channel commentary,
// instructions or thinking.
func gatewayLiveValidateContextOptions(providerOptions map[string]interface{}) error {
	if providerOptions == nil {
		return nil
	}
	valid := false
	if normalized, err := gatewayLiveNormalize(providerOptions); err == nil {
		valid = true
		options, _ := normalized.(map[string]interface{})
		for namespace, entry := range options {
			inner, isMap := entry.(map[string]interface{})
			if namespace != "openai" || !isMap {
				valid = false
				break
			}
			for key, value := range inner {
				channel, isString := value.(string)
				if key != "channel" || !isString || !gatewayLiveContextChans[channel] {
					valid = false
				}
			}
		}
	}
	if !valid {
		return gatewayLiveUnsupported("AI Gateway Live context options other than openai.channel commentary, instructions, or thinking")
	}
	return nil
}
