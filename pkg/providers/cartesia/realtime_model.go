package cartesia

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
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

// decodeCartesiaRealtimeEvent reads a server event as TS casts the parsed
// value: a field of another type does not drop the event (string fields read
// as the JS string of a scalar), is_final counts only when it is true and
// duration only when it is a number.
func decodeCartesiaRealtimeEvent(raw json.RawMessage) cartesiaRealtimeWireEvent {
	var parsed interface{}
	_ = jsonexact.Unmarshal(raw, &parsed)
	fields, _ := parsed.(map[string]interface{})
	str := func(key string) *string {
		v, ok := fields[key]
		if !ok || v == nil {
			return nil
		}
		s := cartesiaJSString(v)
		return &s
	}
	event := cartesiaRealtimeWireEvent{
		Type:      cartesiaJSString(fields["type"]),
		RequestID: str("request_id"),
		Transcrip: str("transcript"),
		Text:      str("text"),
		Message:   str("message"),
		ErrorCode: str("error_code"),
	}
	if b, ok := fields["is_final"].(bool); ok {
		event.IsFinal = &b
	}
	if d, ok := fields["duration"].(float64); ok {
		event.Duration = &d
	}
	return event
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

	// TS JSON.stringify({grants, ...expires_in}): grants first.
	body := jsonorder.New()
	body.Set("grants", map[string]interface{}{"stt": true})
	if opts.ExpiresAfterSeconds != nil {
		body.Set("expires_in", *opts.ExpiresAfterSeconds)
	}
	resp, err := m.postAccessToken(ctx, body)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cause := &internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body}
		return provider.ClientSecretResult{}, providererrors.NewProviderError("cartesia", 0, "",
			// TS response.text(): UTF-8 decoded, BOM dropped.
			fmt.Sprintf("Cartesia realtime client secret request failed: %d %s", resp.StatusCode, textdecode.UTF8(resp.Body)), cause)
	}
	// TS (await response.json()).token: a body that is not JSON rejects with
	// the parser's SyntaxError (Go's parser error here), and a null body
	// throws reading its token.
	var data interface{}
	if err := jsonexact.Unmarshal([]byte(textdecode.UTF8(resp.Body)), &data); err != nil {
		return provider.ClientSecretResult{}, err
	}
	if data == nil {
		return provider.ClientSecretResult{}, errors.New("Cannot read properties of null (reading 'token')") //nolint:staticcheck // matches the TS runtime error text
	}
	fields, _ := data.(map[string]interface{})
	token, _ := fields["token"].(string)
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
	// TS new URL(`${baseURL}${path}`) (WHATWG serialization, "Invalid URL"
	// for a base URL it rejects), then url.protocol = protocol === 'http:' ?
	// 'ws:' : 'wss:' (the setter leaves a non-special scheme unchanged).
	wsURL, err := urlnorm.Parse(base + path)
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	if wsURL.Scheme == "http" {
		wsURL.SetProtocol("ws")
	} else {
		wsURL.SetProtocol("wss")
	}
	u, err := url.Parse(wsURL.String())
	if err != nil {
		return provider.ClientSecretResult{}, err
	}
	// TS url.searchParams.set on new URL(...): the base URL's own parameters
	// first, insertion order, URLSearchParams escaping.
	query := internalhttp.ParseSearchParams(u.RawQuery)
	query.Set("model", m.modelID)
	query.Set("encoding", encoding)
	query.Set("sample_rate", strconv.Itoa(rate))
	query.Set("cartesia_version", m.provider.config.APIVersion)
	if !useTurnDetection && language != nil {
		query.Set("language", *language)
	}
	u.RawQuery = query.Encode()

	result := provider.ClientSecretResult{Token: token, URL: u.String()}
	if opts.ExpiresAfterSeconds != nil {
		expiresAt := m.now().Unix() + int64(*opts.ExpiresAfterSeconds)
		result.ExpiresAt = &expiresAt
	}
	return result, nil
}

// postAccessToken is the TS client secret request: the fetch function called
// directly (no postJsonToApi), with {...config headers, 'Content-Type':
// 'application/json'} (no runtime user-agent tag). A fetch rejection
// propagates as is instead of becoming the retryable "Cannot connect to API"
// APICallError.
func (m *RealtimeModel) postAccessToken(ctx context.Context, body *jsonorder.Object) (*internalhttp.Response, error) {
	headers, err := m.provider.client.RequestHeadersCtx(ctx)
	if err != nil {
		return nil, err // TS: resolve(config.headers) throws as is (LoadAPIKeyError)
	}
	payload, err := jsonstr.Marshal(body)
	if err != nil {
		return nil, err
	}
	tokenURL := m.provider.config.BaseURL + "/access-token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	httpResp, err := internalhttp.WithFetchRedirects(m.provider.client.HTTPClient()).Do(req)
	if err != nil {
		if cause, ok := internalhttp.FetchRejectionCause(err); ok {
			return nil, cause
		}
		return nil, err
	}
	defer httpResp.Body.Close() //nolint:errcheck
	respBody, err := fileutil.ReadResponseWithSizeLimit(httpResp, tokenURL, fileutil.DefaultMaxDownloadSize)
	if err != nil {
		return nil, err
	}
	return &internalhttp.Response{StatusCode: httpResp.StatusCode, Headers: httpResp.Header, Body: respBody}, nil
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
	// TS url.searchParams.set('access_token', token).
	u.RawQuery = internalhttp.SetSearchParam(u.RawQuery, "access_token", token)
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
		// TS convertBase64ToUint8Array maps base64url ('-', '_') to base64
		// first (uint8-utils.ts:6).
		audio, err := fileutil.ConvertBase64ToBytes(event.Audio)
		if err != nil {
			return nil, false, true, err
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
		return nil, false, true, fmt.Errorf("Cartesia Ink 2 does not support realtime client event \"%s\".", event.Type) //nolint:staticcheck // matches TS SDK's exact error text
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
	event := decodeCartesiaRealtimeEvent(raw)
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
	// TS raw is {event, transcriptEvents, duration} (in that order), with
	// the parsed events and a JS number.
	parsed := func(r json.RawMessage) interface{} {
		if tree, ok := jsonorder.Parse(r); ok {
			return tree
		}
		return r
	}
	transcriptEvents := make([]interface{}, len(state.events))
	for i, e := range state.events {
		transcriptEvents[i] = parsed(e)
	}
	combinedObj := jsonorder.New()
	combinedObj.Set("event", parsed(raw))
	combinedObj.Set("transcriptEvents", transcriptEvents)
	// JSON.stringify writes a non-finite sum (durations that overflow the
	// double range) as null.
	var duration interface{}
	if !math.IsInf(state.duration, 0) && !math.IsNaN(state.duration) {
		duration = json.Number(providerutils.JSNumberString(state.duration))
	}
	combinedObj.Set("duration", duration)
	combined, _ := json.Marshal(combinedObj)
	return []provider.RealtimeServerEvent{
		{Type: "audio-committed", ItemID: itemID, Raw: raw},
		{Type: "input-transcription-completed", ItemID: completedID, Transcript: state.text, Raw: combined},
	}
}
