package cartesia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const cartesiaDefaultRealtimeRequestID = "cartesia-realtime"

// RealtimeModel is the realtime Ink speech-to-text model over Cartesia's STT
// WebSocket APIs. Mirrors TS CartesiaRealtimeModel
// (packages/cartesia/src/cartesia-realtime-model.ts). Like the TS class it
// keeps per-connection parser state, so use one model per connection.
type RealtimeModel struct {
	provider *Provider
	modelID  string

	// now is overridable in tests (TS config._internal.currentDate).
	now func() time.Time

	mu               sync.Mutex
	manualTranscript cartesiaManualTranscript
	sessionCreated   bool
	useTurnDetection bool
}

type cartesiaManualTranscript struct {
	duration float64
	events   []json.RawMessage
	text     string
}

type cartesiaRealtimeWireEvent struct {
	Type      string   `json:"type"`
	RequestID *string  `json:"request_id"`
	Transcrip *string  `json:"transcript"`
	Text      *string  `json:"text"`
	IsFinal   *bool    `json:"is_final"`
	Duration  *float64 `json:"duration"`
	Message   *string  `json:"message"`
	ErrorCode *string  `json:"error_code"`
}

// NewRealtimeModel creates a Cartesia realtime Ink model.
func NewRealtimeModel(p *Provider, modelID string) *RealtimeModel {
	return &RealtimeModel{provider: p, modelID: modelID, now: time.Now, useTurnDetection: true}
}

// ExperimentalRealtimeModel mirrors TS provider.experimental_realtime(modelId).
func (p *Provider) ExperimentalRealtimeModel(modelID string) (provider.Experimental_RealtimeModelV4, error) {
	return NewRealtimeModel(p, modelID), nil
}

// RealtimeModel is an alias of ExperimentalRealtimeModel.
func (p *Provider) RealtimeModel(modelID string) (provider.Experimental_RealtimeModelV4, error) {
	return p.ExperimentalRealtimeModel(modelID)
}

// GetRealtimeToken mirrors TS provider.experimental_realtime.getToken().
func (p *Provider) GetRealtimeToken(ctx context.Context, opts provider.RealtimeFactoryGetTokenOptions) (provider.ClientSecretResult, error) {
	return NewRealtimeModel(p, opts.Model).DoCreateClientSecret(ctx, opts.ClientSecretOptions)
}

func (m *RealtimeModel) SpecificationVersion() string { return "v4" }
func (m *RealtimeModel) Provider() string             { return "cartesia.realtime" }
func (m *RealtimeModel) ModelID() string              { return m.modelID }

var (
	_ provider.Experimental_RealtimeModelV4    = (*RealtimeModel)(nil)
	_ provider.RealtimeClientSecretCreator     = (*RealtimeModel)(nil)
	_ provider.RealtimeWebSocketConfigProvider = (*RealtimeModel)(nil)
	_ provider.RealtimeRawEventSerializer      = (*RealtimeModel)(nil)
)

// BuildSessionConfig returns nil: Cartesia has no session configuration frame.
func (m *RealtimeModel) BuildSessionConfig(_ provider.RealtimeSessionConfig) any { return nil }

// DoCreateClientSecret mints a short-lived access token and builds the STT
// WebSocket URL for the requested session settings.
func (m *RealtimeModel) DoCreateClientSecret(ctx context.Context, opts provider.ClientSecretOptions) (provider.ClientSecretResult, error) {
	if opts.ExpiresAfterSeconds != nil {
		if e := *opts.ExpiresAfterSeconds; e <= 0 || e > 3600 {
			return provider.ClientSecretResult{}, fmt.Errorf("Cartesia realtime client secrets must expire between 1 and 3600 seconds.") //nolint:staticcheck // matches TS SDK's exact error text
		}
	}
	var language *string
	var turnDetection *provider.RealtimeTurnDetection
	var inputFormat *provider.RealtimeAudioFormat
	if cfg := opts.SessionConfig; cfg != nil {
		if cfg.InputAudioTranscription != nil {
			language = cfg.InputAudioTranscription.Language
		}
		turnDetection = cfg.TurnDetection
		inputFormat = cfg.InputAudioFormat
	}
	if language != nil && *language != "en" {
		return provider.ClientSecretResult{}, fmt.Errorf("Cartesia Ink 2 currently supports English only.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	body := map[string]interface{}{"grants": map[string]interface{}{"stt": true}}
	if opts.ExpiresAfterSeconds != nil {
		body["expires_in"] = *opts.ExpiresAfterSeconds
	}
	resp, err := m.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/access-token",
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    body,
	})
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cause := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}
		return provider.ClientSecretResult{}, providererrors.NewProviderError("cartesia", 0, "",
			fmt.Sprintf("Cartesia realtime client secret request failed: %d %s", resp.StatusCode, string(resp.Body)), cause)
	}
	var data struct {
		Token interface{} `json:"token"`
	}
	_ = json.Unmarshal(resp.Body, &data)
	token, _ := data.Token.(string)
	if token == "" {
		return provider.ClientSecretResult{}, fmt.Errorf("Cartesia realtime client secret response did not include a token.") //nolint:staticcheck // matches TS SDK's exact error text
	}

	useTurnDetection := turnDetection == nil || turnDetection.Type != "disabled"
	encoding, err := resolveCartesiaEncoding(inputFormat)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	rate := 24000
	if inputFormat != nil && inputFormat.Rate != nil {
		rate = *inputFormat.Rate
	}

	base := strings.TrimRight(m.provider.config.BaseURL, "/")
	path := "/stt/websocket"
	if useTurnDetection {
		path = "/stt/turns/websocket"
	}
	u, err := url.Parse(base + path)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else {
		u.Scheme = "wss"
	}
	// URLSearchParams.set keeps insertion order.
	query := []string{
		"model=" + url.QueryEscape(m.modelID),
		"encoding=" + url.QueryEscape(encoding),
		"sample_rate=" + strconv.Itoa(rate),
		"cartesia_version=" + url.QueryEscape(m.provider.config.APIVersion),
	}
	if !useTurnDetection && language != nil {
		query = append(query, "language="+url.QueryEscape(*language))
	}
	u.RawQuery = strings.Join(query, "&")

	result := provider.ClientSecretResult{Token: token, URL: u.String()}
	if opts.ExpiresAfterSeconds != nil {
		expiresAt := m.now().Unix() + int64(*opts.ExpiresAfterSeconds)
		result.ExpiresAt = &expiresAt
	}
	return result, nil
}

// GetWebSocketConfig adds the access token to the STT URL and records whether
// the connection uses turn detection (which decides how commit is framed).
func (m *RealtimeModel) GetWebSocketConfig(token, wsURL string) provider.WebSocketConfig {
	u, err := url.Parse(wsURL)
	if err != nil {
		return provider.WebSocketConfig{URL: wsURL}
	}
	m.mu.Lock()
	m.useTurnDetection = strings.Contains(u.Path, "/stt/turns/websocket")
	m.mu.Unlock()
	var pairs []string
	if u.RawQuery != "" {
		for _, pair := range strings.Split(u.RawQuery, "&") {
			if pair != "" && !strings.HasPrefix(pair, "access_token=") {
				pairs = append(pairs, pair)
			}
		}
	}
	pairs = append(pairs, "access_token="+url.QueryEscape(token))
	u.RawQuery = strings.Join(pairs, "&")
	return provider.WebSocketConfig{URL: u.String()}
}

func resolveCartesiaEncoding(format *provider.RealtimeAudioFormat) (string, error) {
	if format == nil {
		return "pcm_s16le", nil
	}
	switch format.Type {
	case "audio/pcm":
		return "pcm_s16le", nil
	case "audio/pcmu":
		return "pcm_mulaw", nil
	case "audio/pcma":
		return "pcm_alaw", nil
	default:
		return "", fmt.Errorf("Unsupported Cartesia realtime audio format: %s", format.Type) //nolint:staticcheck // matches TS SDK's exact error text
	}
}

// SerializeClientEventRaw frames client events like TS: audio as a binary
// frame, a manual commit as the text frame "finalize", and ignored events as
// nothing.
func (m *RealtimeModel) SerializeClientEventRaw(event provider.RealtimeClientEvent) ([]byte, bool, bool, error) {
	switch event.Type {
	case "session-update", "input-audio-clear", "response-create":
		return nil, false, true, nil
	case "input-audio-append":
		audio, err := base64.StdEncoding.DecodeString(event.Audio)
		if err != nil {
			audio, err = base64.RawStdEncoding.DecodeString(event.Audio)
			if err != nil {
				return nil, false, true, err
			}
		}
		return audio, true, true, nil
	case "input-audio-commit":
		m.mu.Lock()
		turns := m.useTurnDetection
		m.mu.Unlock()
		if turns {
			return nil, false, true, nil
		}
		return []byte("finalize"), false, true, nil
	default:
		return nil, false, true, fmt.Errorf("Cartesia Ink 2 does not support realtime client event %q.", event.Type) //nolint:staticcheck // matches TS SDK's exact error text
	}
}

// SerializeClientEvent is the JSON fallback; core sessions use
// SerializeClientEventRaw. Audio has no JSON form.
func (m *RealtimeModel) SerializeClientEvent(event provider.RealtimeClientEvent) (json.RawMessage, error) {
	data, binary, _, err := m.SerializeClientEventRaw(event)
	if err != nil || len(data) == 0 || binary {
		return nil, err
	}
	return json.Marshal(string(data))
}

// ParseServerEvent maps a Cartesia STT event; the first event also yields a
// session-created event when the server did not send "connected".
func (m *RealtimeModel) ParseServerEvent(raw json.RawMessage) ([]provider.RealtimeServerEvent, error) {
	var event cartesiaRealtimeWireEvent
	_ = json.Unmarshal(raw, &event)
	m.mu.Lock()
	defer m.mu.Unlock()

	requestID := cartesiaDefaultRealtimeRequestID
	if event.RequestID != nil {
		requestID = *event.RequestID
	}
	custom := provider.RealtimeServerEvent{Type: "custom", RawType: event.Type, Raw: raw}

	var mapped []provider.RealtimeServerEvent
	switch event.Type {
	case "connected":
		m.sessionCreated = true
		sessionID := ""
		if event.RequestID != nil {
			sessionID = *event.RequestID
		}
		return []provider.RealtimeServerEvent{{Type: "session-created", SessionID: sessionID, Raw: raw}}, nil
	case "turn.start":
		mapped = []provider.RealtimeServerEvent{{Type: "speech-started", ItemID: requestID, Raw: raw}}
	case "turn.resume":
		mapped = []provider.RealtimeServerEvent{{Type: "speech-started", ItemID: requestID, Raw: raw}, custom}
	case "turn.end":
		transcript := ""
		if event.Transcrip != nil {
			transcript = *event.Transcrip
		}
		mapped = []provider.RealtimeServerEvent{
			{Type: "speech-stopped", ItemID: requestID, Raw: raw},
			{Type: "input-transcription-completed", ItemID: requestID, Transcript: transcript, Raw: raw},
		}
	case "transcript":
		if event.IsFinal != nil && *event.IsFinal {
			if event.Text != nil {
				m.manualTranscript.text += *event.Text
			}
			if event.Duration != nil {
				m.manualTranscript.duration += *event.Duration
			}
			m.manualTranscript.events = append(m.manualTranscript.events, raw)
		}
		mapped = []provider.RealtimeServerEvent{custom}
	case "flush_done":
		mapped = m.finishManualTranscript(event, raw)
	case "done":
		if len(m.manualTranscript.text) > 0 {
			mapped = append(m.finishManualTranscript(event, raw), custom)
		} else {
			mapped = []provider.RealtimeServerEvent{custom}
		}
	case "error":
		message := "Unknown Cartesia realtime error"
		if event.Message != nil {
			message = *event.Message
		}
		code := ""
		if event.ErrorCode != nil {
			code = *event.ErrorCode
		}
		mapped = []provider.RealtimeServerEvent{{Type: "error", Message: message, Code: code, Raw: raw}}
	default:
		mapped = []provider.RealtimeServerEvent{custom}
	}

	if m.sessionCreated {
		return mapped, nil
	}
	m.sessionCreated = true
	return append([]provider.RealtimeServerEvent{{Type: "session-created", SessionID: requestID, Raw: raw}}, mapped...), nil
}

// finishManualTranscript must be called with m.mu held.
func (m *RealtimeModel) finishManualTranscript(event cartesiaRealtimeWireEvent, raw json.RawMessage) []provider.RealtimeServerEvent {
	state := m.manualTranscript
	m.manualTranscript = cartesiaManualTranscript{}
	if state.text == "" {
		return []provider.RealtimeServerEvent{{Type: "custom", RawType: event.Type, Raw: raw}}
	}
	itemID := ""
	completedID := cartesiaDefaultRealtimeRequestID
	if event.RequestID != nil {
		itemID = *event.RequestID
		completedID = *event.RequestID
	}
	transcriptEvents := make([]json.RawMessage, len(state.events))
	copy(transcriptEvents, state.events)
	combined, _ := json.Marshal(map[string]interface{}{
		"event":            json.RawMessage(raw),
		"transcriptEvents": transcriptEvents,
		"duration":         state.duration,
	})
	return []provider.RealtimeServerEvent{
		{Type: "audio-committed", ItemID: itemID, Raw: raw},
		{Type: "input-transcription-completed", ItemID: completedID, Transcript: state.text, Raw: combined},
	}
}
