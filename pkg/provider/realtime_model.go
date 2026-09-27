package provider

import (
	"context"
	"encoding/json"
)

// Experimental_RealtimeModelV4 is the experimental provider contract for
// bidirectional realtime audio/text models. It mirrors the TypeScript AI SDK's
// Experimental_RealtimeModelV4 surface while using Go method names.
type Experimental_RealtimeModelV4 interface {
	SpecificationVersion() string
	Provider() string
	ModelID() string

	DoCreateClientSecret(ctx context.Context, opts ClientSecretOptions) (ClientSecretResult, error)
	GetWebSocketConfig(token, url string) WebSocketConfig
	BuildSessionConfig(config RealtimeSessionConfig) any
	ParseServerEvent(raw json.RawMessage) ([]RealtimeServerEvent, error)
	SerializeClientEvent(event RealtimeClientEvent) (json.RawMessage, error)
}

// RealtimeHealthCheckResponder is an optional realtime model extension. It
// mirrors TypeScript's optional getHealthCheckResponse method.
type RealtimeHealthCheckResponder interface {
	GetHealthCheckResponse(raw json.RawMessage) (json.RawMessage, bool)
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
	// "realtime"). Ignored by factories with a single realtime API.
	API string `json:"api,omitempty"`
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

// RealtimeUsage reports cumulative realtime session usage (OpenAI Live
// session.usage.updated / session.closed).
type RealtimeUsage struct {
	Seconds float64 `json:"seconds"`
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
