package provider

import (
	"context"
	"encoding/json"
)

// Experimental_RealtimeModelV4 is the experimental provider contract for
// bidirectional realtime audio/text models. It mirrors the TypeScript AI SDK's
// Experimental_RealtimeModelV4 surface while using Go method names.
//
// DoCreateClientSecret and GetWebSocketConfig are NOT part of this interface
// (unlike an earlier version of this SDK): in TS's realtime-model-v4.ts both
// doCreateClientSecret and getWebSocketConfig are optional (`?`), like
// getServerWebSocketConfig and the WebRTC methods. Implement
// RealtimeClientSecretCreator and RealtimeWebSocketConfigProvider to opt in.
type Experimental_RealtimeModelV4 interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	BuildSessionConfig(config RealtimeSessionConfig) any
	ParseServerEvent(raw json.RawMessage) ([]RealtimeServerEvent, error)
	SerializeClientEvent(event RealtimeClientEvent) (json.RawMessage, error)
}

// RealtimeClientSecretCreator is an optional Experimental_RealtimeModelV4
// capability for server-side minting of an ephemeral client secret used to
// authenticate a browser-side WebSocket connection. Mirrors TS's optional
// doCreateClientSecret?() (realtime-model-v4.ts). A model that authenticates
// the connection itself (see RealtimeServerWebSocketConfigProvider) or that
// only supports WebRTC need not implement this, matching TS
// OpenAIRealtimeModelLive, which implements neither doCreateClientSecret nor
// getWebSocketConfig.
type RealtimeClientSecretCreator interface {
	DoCreateClientSecret(ctx context.Context, opts ClientSecretOptions) (ClientSecretResult, error)
}

// RealtimeWebSocketConfigProvider is an optional Experimental_RealtimeModelV4
// capability for browser-side construction of the WebSocket URL and
// subprotocols to use when connecting with a client secret minted via
// RealtimeClientSecretCreator. Mirrors TS's optional getWebSocketConfig?()
// (realtime-model-v4.ts). Not implemented by a model that only supports a
// server-authenticated connection (RealtimeServerWebSocketConfigProvider) or
// WebRTC.
type RealtimeWebSocketConfigProvider interface {
	GetWebSocketConfig(token, url string) WebSocketConfig
}

// RealtimeHealthCheckResponder is an optional realtime model extension. It
// mirrors TypeScript's optional getHealthCheckResponse method.
type RealtimeHealthCheckResponder interface {
	GetHealthCheckResponse(raw json.RawMessage) (json.RawMessage, bool)
}

// RealtimeRawEventSerializer is an optional Experimental_RealtimeModelV4
// capability for a provider whose client events sometimes need to bypass
// the default JSON-object serialization, mirroring TS's
// encodeRealtimeFrame: a string payload is sent over the WebSocket as-is
// (not re-JSON-encoded) and a binary payload (TS ArrayBuffer/typed
// array/Blob) is sent as a binary WS frame instead of text (audit row
// 8f89c25 / WG-MISC). When a model implements this and returns ok=true for
// an event, RealtimeSession.Send uses (Data, Binary) directly instead of
// calling SerializeClientEvent.
type RealtimeRawEventSerializer interface {
	SerializeClientEventRaw(event RealtimeClientEvent) (data []byte, binary bool, ok bool, err error)
}

// RealtimeRawEventParser is an optional Experimental_RealtimeModelV4
// capability for a provider that needs to receive frames the core session
// would otherwise silently drop for not being valid JSON — most notably a
// binary WS frame (e.g. raw audio), which TS passes straight to the
// model's parser rather than filtering by content (audit row 8f89c25 /
// WG-MISC). When a model implements this, RealtimeSession.Read calls it
// for every received frame (binary or text, valid JSON or not) instead of
// requiring the frame to be valid JSON before calling ParseServerEvent.
type RealtimeRawEventParser interface {
	ParseRawServerEvent(raw []byte, binary bool) ([]RealtimeServerEvent, error)
}

// RealtimeServerWebSocketConfigProvider is an optional realtime model
// capability for models whose connection is authenticated server-side with
// request headers (for example a provider API key) instead of a
// per-connection token minted via DoCreateClientSecret and sent as a
// WebSocket subprotocol. Mirrors the TypeScript SDK's
// getServerWebSocketConfig() (e.g. OpenAIRealtimeModelLive). When a
// realtime model implements this, callers should use it instead of
// DoCreateClientSecret/GetWebSocketConfig to establish the connection.
type RealtimeServerWebSocketConfigProvider interface {
	GetServerWebSocketConfig() (WebSocketConfig, error)
}

// RealtimeSessionIntent selects the realtime endpoint variant a server-side
// connection targets. Mirrors TS RealtimeModelV4SessionIntent
// ('conversation' | 'transcription', packages/provider/src/realtime-model/
// v4/realtime-model-v4-server-connection.ts). The empty value means
// RealtimeSessionIntentConversation, as an omitted TS intent does.
// Providers should fail fast when their endpoint and codec do not support
// an intent.
type RealtimeSessionIntent string

const (
	// RealtimeSessionIntentConversation is the conversational voice
	// endpoint most providers expose.
	RealtimeSessionIntentConversation RealtimeSessionIntent = "conversation"

	// RealtimeSessionIntentTranscription is a dedicated transcription
	// endpoint, which some providers expose at a different URL.
	RealtimeSessionIntentTranscription RealtimeSessionIntent = "transcription"
)

// RealtimeServerConnectionOptions are the options for
// RealtimeServerConnectionProvider.GetServerConnection (TS
// getServerWebSocketConfig `options?: { intent? }`).
type RealtimeServerConnectionOptions struct {
	// Intent selects the endpoint variant; empty means conversation.
	Intent RealtimeSessionIntent
}

// RealtimeServerConnectionProvider is an optional realtime model capability
// for a server-initiated upstream WebSocket connection that selects the
// endpoint variant by intent. It mirrors TS getServerWebSocketConfig?(
// { intent }) returning RealtimeModelV4ServerConnection
// (packages/provider/src/realtime-model/v4/realtime-model-v4.ts and
// realtime-model-v4-server-connection.ts): the returned WebSocketConfig
// carries the URL (with any provider query parameters), the handshake
// headers holding the long-lived provider credential, and optional
// subprotocols. It is meant for a trusted server or proxy that owns the
// socket; the context covers providers that need a round-trip (TS may
// return a promise).
//
// It is separate from RealtimeServerWebSocketConfigProvider on purpose: Go's
// ConnectRealtime prefers that older interface whenever a model implements
// it, so adding it to more models would silently switch existing
// client-secret callers to the long-lived key. This interface is used only
// when a caller asks for an intent. Callers should pass the same intent to
// RealtimeIntentCodec.
type RealtimeServerConnectionProvider interface {
	GetServerConnection(ctx context.Context, opts RealtimeServerConnectionOptions) (WebSocketConfig, error)
}

// RealtimeIntentCodec is an optional realtime model capability that builds
// session configuration and serializes client events for a given intent.
// It mirrors the TS `options?: { intent? }` argument of buildSessionConfig
// and serializeClientEvent (realtime-model-v4.ts). The
// Experimental_RealtimeModelV4 methods BuildSessionConfig and
// SerializeClientEvent keep the conversation behavior; server-side callers
// that opened a connection with RealtimeServerConnectionProvider should use
// these methods with the same intent. An unsupported intent returns an
// error.
type RealtimeIntentCodec interface {
	BuildSessionConfigForIntent(config RealtimeSessionConfig, intent RealtimeSessionIntent) (any, error)
	SerializeClientEventForIntent(event RealtimeClientEvent, intent RealtimeSessionIntent) (json.RawMessage, error)
}

// RealtimeLifecycle describes non-default session startup/finalization
// framing for a realtime model. Mirrors the relevant part of the TypeScript
// SDK's Experimental_RealtimeModelV4 capabilities object (`startup` /
// `finalization`). A zero value means the default GA framing: the session
// starts implicitly (a "session-update" client event configures it, but the
// connection is already live) and ends when the socket closes.
type RealtimeLifecycle struct {
	// StartupEventType is the client event type sent right after connecting
	// to configure and start the session. Empty means "session-update"
	// (default/GA framing). OpenAI Live uses "session-start": the session
	// does not exist until this event is sent.
	StartupEventType string

	// FinalizationEventType, when non-empty, is a client event type sent
	// before closing the connection to end the session gracefully (for
	// example OpenAI Live's "session-close"). Empty means no explicit
	// finalization event is sent; the session simply ends when the socket
	// closes.
	FinalizationEventType string
}

// RealtimeLifecycleProvider is an optional realtime model capability
// exposing non-default session startup/finalization framing. See
// RealtimeLifecycle.
type RealtimeLifecycleProvider interface {
	RealtimeLifecycle() RealtimeLifecycle
}

// RealtimeServerEventParserFactory is an optional realtime model capability
// for creating a fresh, potentially stateful raw-event parser scoped to one
// connection, instead of the stateless ParseServerEvent. Mirrors the
// TypeScript SDK's optional createServerEventParser().
type RealtimeServerEventParserFactory interface {
	NewServerEventParser() func(raw json.RawMessage) ([]RealtimeServerEvent, error)
}

type WebSocketConfig struct {
	URL       string            `json:"url"`
	Protocols []string          `json:"protocols,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type ClientSecretOptions struct {
	ExpiresAfterSeconds *int                   `json:"expiresAfterSeconds,omitempty"`
	SessionConfig       *RealtimeSessionConfig `json:"sessionConfig,omitempty"`
}

type RealtimeFactoryGetTokenOptions struct {
	Model string `json:"model"`
	ClientSecretOptions

	// API optionally overrides model ID routing for factories with
	// multiple realtime APIs sharing one namespace (e.g. OpenAI's "live" vs
	// "realtime"). Ignored by factories with a single realtime API. Nil
	// means unset (routing defaults apply); a non-nil value that the
	// factory doesn't recognize is an error, distinguishing "not passed"
	// from "passed and invalid" the way TS's optional `api?: string` does.
	API *string `json:"api,omitempty"`
}

type ClientSecretResult struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt *int64 `json:"expiresAt,omitempty"`
}

type RealtimeAudioFormat struct {
	Type string `json:"type"`
	Rate *int   `json:"rate,omitempty"`
}

type RealtimeAudioTranscriptionConfig struct {
	Model    *string `json:"model,omitempty"`
	Language *string `json:"language,omitempty"`
	Prompt   *string `json:"prompt,omitempty"`
}

type RealtimeTurnDetection struct {
	Type              string   `json:"type"`
	Threshold         *float64 `json:"threshold,omitempty"`
	SilenceDurationMs *int     `json:"silenceDurationMs,omitempty"`
	PrefixPaddingMs   *int     `json:"prefixPaddingMs,omitempty"`
}

type RealtimeToolDefinition struct {
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Description *string                `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type RealtimeSessionConfig struct {
	Instructions             *string                           `json:"instructions,omitempty"`
	Voice                    *string                           `json:"voice,omitempty"`
	OutputModalities         []string                          `json:"outputModalities,omitempty"`
	InputAudioFormat         *RealtimeAudioFormat              `json:"inputAudioFormat,omitempty"`
	InputAudioTranscription  *RealtimeAudioTranscriptionConfig `json:"inputAudioTranscription,omitempty"`
	OutputAudioTranscription *RealtimeAudioTranscriptionConfig `json:"outputAudioTranscription,omitempty"`
	OutputAudioFormat        *RealtimeAudioFormat              `json:"outputAudioFormat,omitempty"`
	TurnDetection            *RealtimeTurnDetection            `json:"turnDetection,omitempty"`
	Tools                    []RealtimeToolDefinition          `json:"tools,omitempty"`
	ProviderOptions          map[string]interface{}            `json:"providerOptions,omitempty"`
}

type RealtimeConversationItem struct {
	Type   string  `json:"type"`
	Role   string  `json:"role,omitempty"`
	Text   string  `json:"text,omitempty"`
	Audio  string  `json:"audio,omitempty"`
	CallID string  `json:"callId,omitempty"`
	Name   *string `json:"name,omitempty"`
	Output string  `json:"output,omitempty"`
}

type RealtimeResponseCreateOptions struct {
	Modalities   []string               `json:"modalities,omitempty"`
	Instructions *string                `json:"instructions,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

type RealtimeClientEvent struct {
	Type         string                         `json:"type"`
	Config       RealtimeSessionConfig          `json:"config,omitempty"`
	Audio        string                         `json:"audio,omitempty"`
	Item         RealtimeConversationItem       `json:"item,omitempty"`
	ItemID       string                         `json:"itemId,omitempty"`
	ContentIndex int                            `json:"contentIndex,omitempty"`
	AudioEndMs   int                            `json:"audioEndMs,omitempty"`
	Options      *RealtimeResponseCreateOptions `json:"options,omitempty"`

	// EventID optionally tags a client event so the corresponding server
	// acknowledgment (command-acknowledged / error) can be correlated with
	// it. Used by OpenAI Live.
	EventID string `json:"eventId,omitempty"`

	// Content and DelegationID carry a "context-append" event's payload
	// (OpenAI Live: append instructions/thinking/commentary context mid
	// session). ProviderOptions selects the channel via
	// providerOptions.openai.channel ("instructions" | "thinking" |
	// "commentary"; default "thinking").
	Content         string                 `json:"content,omitempty"`
	DelegationID    *string                `json:"delegationId,omitempty"`
	ProviderOptions map[string]interface{} `json:"providerOptions,omitempty"`
}

// RealtimeUsage reports realtime usage on completion events or provider
// snapshots, mirroring TS RealtimeModelV4Usage (packages/provider/src/
// realtime-model/v4/realtime-model-v4-usage.ts). Providers populate only
// what they report; snapshots without a response association must not be
// summed as increments. Token counts are the provider's raw counts: the
// input buckets include cached tokens, and the cached portions are also
// reported in the CachedInput* fields.
//
// Token counts are *float64, the JS number TS reports (a fractional wire
// value is kept, as TS does).
//
// Seconds stays a plain float64 for v0.5.2 compatibility (OpenAI Live
// session.usage.updated / session.closed set it). JSON output always
// includes "seconds" unless the usage is token-only: at least one token
// field is set, Seconds is 0 and seconds was not marked present with
// SetSeconds (or decoded from JSON alongside token fields). The zero value
// marshals as {"seconds":0}, as in v0.5.2.
type RealtimeUsage struct {
	// InputTokens is the gross input tokens across all reported
	// modalities; includes cached tokens.
	InputTokens *float64 `json:"inputTokens,omitempty"`
	// OutputTokens is the gross output tokens across all reported
	// modalities.
	OutputTokens *float64 `json:"outputTokens,omitempty"`
	// CachedInputTokens is the cached portion of InputTokens.
	CachedInputTokens *float64 `json:"cachedInputTokens,omitempty"`
	// InputTextTokens is the input text tokens (includes cached text).
	InputTextTokens *float64 `json:"inputTextTokens,omitempty"`
	// InputAudioTokens is the input audio tokens (includes cached audio).
	InputAudioTokens *float64 `json:"inputAudioTokens,omitempty"`
	// OutputTextTokens is the output text tokens.
	OutputTextTokens *float64 `json:"outputTextTokens,omitempty"`
	// OutputAudioTokens is the output audio tokens.
	OutputAudioTokens *float64 `json:"outputAudioTokens,omitempty"`
	// CachedInputTextTokens is the cached portion of InputTextTokens.
	CachedInputTextTokens *float64 `json:"cachedInputTextTokens,omitempty"`
	// CachedInputAudioTokens is the cached portion of InputAudioTokens.
	CachedInputAudioTokens *float64 `json:"cachedInputAudioTokens,omitempty"`

	// Seconds is the audio duration in seconds (duration-billed sessions
	// and transcription).
	Seconds float64 `json:"seconds"`

	// secondsSet marks Seconds as reported even when it is 0, so a usage
	// with token fields still marshals "seconds".
	secondsSet bool
}

// SetSeconds sets Seconds and marks it as reported, so it is marshaled even
// when it is 0 and token fields are set.
func (u *RealtimeUsage) SetSeconds(seconds float64) {
	u.Seconds = seconds
	u.secondsSet = true
}

// HasSeconds reports whether Seconds is part of this usage: it was set with
// SetSeconds, is non-zero, or no token field is set (the v0.5.2 shape).
func (u RealtimeUsage) HasSeconds() bool {
	return u.secondsSet || u.Seconds != 0 || !u.hasTokens()
}

func (u RealtimeUsage) hasTokens() bool {
	return u.InputTokens != nil || u.OutputTokens != nil ||
		u.CachedInputTokens != nil || u.InputTextTokens != nil ||
		u.InputAudioTokens != nil || u.OutputTextTokens != nil ||
		u.OutputAudioTokens != nil || u.CachedInputTextTokens != nil ||
		u.CachedInputAudioTokens != nil
}

// realtimeUsageTokens is RealtimeUsage without Seconds or methods.
type realtimeUsageTokens struct {
	InputTokens            *float64 `json:"inputTokens,omitempty"`
	OutputTokens           *float64 `json:"outputTokens,omitempty"`
	CachedInputTokens      *float64 `json:"cachedInputTokens,omitempty"`
	InputTextTokens        *float64 `json:"inputTextTokens,omitempty"`
	InputAudioTokens       *float64 `json:"inputAudioTokens,omitempty"`
	OutputTextTokens       *float64 `json:"outputTextTokens,omitempty"`
	OutputAudioTokens      *float64 `json:"outputAudioTokens,omitempty"`
	CachedInputTextTokens  *float64 `json:"cachedInputTextTokens,omitempty"`
	CachedInputAudioTokens *float64 `json:"cachedInputAudioTokens,omitempty"`
}

func (u RealtimeUsage) tokens() realtimeUsageTokens {
	return realtimeUsageTokens{
		InputTokens:            u.InputTokens,
		OutputTokens:           u.OutputTokens,
		CachedInputTokens:      u.CachedInputTokens,
		InputTextTokens:        u.InputTextTokens,
		InputAudioTokens:       u.InputAudioTokens,
		OutputTextTokens:       u.OutputTextTokens,
		OutputAudioTokens:      u.OutputAudioTokens,
		CachedInputTextTokens:  u.CachedInputTextTokens,
		CachedInputAudioTokens: u.CachedInputAudioTokens,
	}
}

// MarshalJSON omits "seconds" only for token-only usage (see RealtimeUsage).
func (u RealtimeUsage) MarshalJSON() ([]byte, error) {
	if !u.HasSeconds() {
		return json.Marshal(u.tokens())
	}
	return json.Marshal(struct {
		realtimeUsageTokens
		Seconds float64 `json:"seconds"`
	}{u.tokens(), u.Seconds})
}

// UnmarshalJSON decodes RealtimeUsage. A "seconds" key decoded alongside
// token fields marks Seconds present, so the value round-trips; a
// seconds-only payload decodes to the same value as a struct literal.
func (u *RealtimeUsage) UnmarshalJSON(data []byte) error {
	var raw struct {
		realtimeUsageTokens
		Seconds *float64 `json:"seconds"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t := raw.realtimeUsageTokens
	*u = RealtimeUsage{
		InputTokens:            t.InputTokens,
		OutputTokens:           t.OutputTokens,
		CachedInputTokens:      t.CachedInputTokens,
		InputTextTokens:        t.InputTextTokens,
		InputAudioTokens:       t.InputAudioTokens,
		OutputTextTokens:       t.OutputTextTokens,
		OutputAudioTokens:      t.OutputAudioTokens,
		CachedInputTextTokens:  t.CachedInputTextTokens,
		CachedInputAudioTokens: t.CachedInputAudioTokens,
	}
	if raw.Seconds != nil {
		u.Seconds = *raw.Seconds
		if u.Seconds == 0 && u.hasTokens() {
			u.secondsSet = true
		}
	}
	return nil
}

type RealtimeServerEvent struct {
	Type           string          `json:"type"`
	SessionID      string          `json:"sessionId,omitempty"`
	ItemID         string          `json:"itemId,omitempty"`
	PreviousItemID string          `json:"previousItemId,omitempty"`
	Item           interface{}     `json:"item,omitempty"`
	Transcript     string          `json:"transcript,omitempty"`
	ResponseID     string          `json:"responseId,omitempty"`
	Status         string          `json:"status,omitempty"`
	Delta          string          `json:"delta,omitempty"`
	Text           string          `json:"text,omitempty"`
	CallID         string          `json:"callId,omitempty"`
	Name           string          `json:"name,omitempty"`
	Arguments      string          `json:"arguments,omitempty"`
	Message        string          `json:"message,omitempty"`
	Code           string          `json:"code,omitempty"`
	RawType        string          `json:"rawType,omitempty"`
	Raw            json.RawMessage `json:"raw"`

	// The following fields are populated by OpenAI Live events only.

	// Usage is cumulative session usage, present on session-usage and
	// session-closed events.
	Usage *RealtimeUsage `json:"usage,omitempty"`
	// ContextWindowUsageRatio is the fraction (0-1) of the context window
	// used, present on some session-usage events.
	ContextWindowUsageRatio *float64 `json:"contextWindowUsageRatio,omitempty"`
	// Reason explains why the session closed (session-closed).
	Reason string `json:"reason,omitempty"`
	// DelegationMode is "client" or "provider" (session-started).
	DelegationMode string `json:"delegationMode,omitempty"`
	// DelegationID and Target/OffsetMs describe a delegation-created event.
	DelegationID string `json:"delegationId,omitempty"`
	Target       string `json:"target,omitempty"`
	OffsetMs     *int   `json:"offsetMs,omitempty"`
	// Speaker, StartMs and EndMs describe a transcript-fragment event.
	Speaker string `json:"speaker,omitempty"`
	StartMs *int   `json:"startMs,omitempty"`
	EndMs   *int   `json:"endMs,omitempty"`
	// Command and ClientEventID describe a command-acknowledged event, or
	// tag the client event an error responds to.
	Command       string `json:"command,omitempty"`
	ClientEventID string `json:"clientEventId,omitempty"`
}
