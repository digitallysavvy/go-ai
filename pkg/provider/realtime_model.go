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

type WebSocketConfig struct {
	URL       string   `json:"url"`
	Protocols []string `json:"protocols,omitempty"`
}

type ClientSecretOptions struct {
	ExpiresAfterSeconds *int                   `json:"expiresAfterSeconds,omitempty"`
	SessionConfig       *RealtimeSessionConfig `json:"sessionConfig,omitempty"`
}

type RealtimeFactoryGetTokenOptions struct {
	Model string `json:"model"`
	ClientSecretOptions
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
}
