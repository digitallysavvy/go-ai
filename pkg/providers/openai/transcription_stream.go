package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/net/websocket"
)

// isRealtimeTranscriptionModelID reports whether modelID streams over the
// OpenAI realtime WebSocket rather than the REST transcription endpoint.
// Prefix matching keeps dated snapshots (e.g. "gpt-realtime-whisper-2026-01-01")
// working, mirroring TS isRealtimeTranscriptionModelId.
func isRealtimeTranscriptionModelID(modelID string) bool {
	return modelID == "gpt-realtime-whisper" || strings.HasPrefix(modelID, "gpt-realtime-whisper-")
}

// DoStream streams a transcript for live audio over the OpenAI realtime
// WebSocket (gpt-realtime-whisper and dated snapshots). Mirrors TS
// OpenAITranscriptionModel#doStream / createOpenAIRealtimeTranscriptionStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if !isRealtimeTranscriptionModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("streaming transcription with %s", m.modelID),
		}
	}

	streamOpts := transcriptionStreamOpenAIOptions(opts.ProviderOptions)

	var warnings []types.Warning
	if streamOpts.hasInclude {
		warnings = append(warnings, unsupportedStreamingTranscriptionWarning("include"))
	}
	if streamOpts.Prompt != "" {
		warnings = append(warnings, unsupportedStreamingTranscriptionWarning("prompt"))
	}
	if streamOpts.HasTemperature {
		warnings = append(warnings, unsupportedStreamingTranscriptionWarning("temperature"))
	}
	if len(streamOpts.TimestampGranularities) > 0 {
		warnings = append(warnings, unsupportedStreamingTranscriptionWarning("timestampGranularities"))
	}

	headers := internalhttp.MergeHeaders(m.baseWSHeaders(), opts.Headers)
	sessionUpdate := buildOpenAIRealtimeTranscriptionSession(m.modelID, opts.InputAudioFormat, streamOpts)
	wsURL := realtimeTranscriptionWebSocketURL(m.provider.config.BaseURL)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newOpenAIRealtimeTranscriptionStream(abortCtx, openAIRealtimeTranscriptionStreamConfig{
		url:              wsURL,
		headers:          headers,
		sessionUpdate:    sessionUpdate,
		language:         streamOpts.Language,
		warnings:         warnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: sessionUpdate,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

// baseWSHeaders rebuilds the provider's default headers (Authorization,
// organization, project, custom config headers) for the WebSocket handshake,
// mirroring the headers the internal HTTP client attaches to REST calls.
func (m *TranscriptionModel) baseWSHeaders() map[string]string {
	cfg := m.provider.config
	headers := map[string]string{}
	if cfg.APIKey != "" {
		headers["Authorization"] = "Bearer " + cfg.APIKey
	}
	if cfg.Organization != "" {
		headers["OpenAI-Organization"] = cfg.Organization
	}
	if cfg.Project != "" {
		headers["OpenAI-Project"] = cfg.Project
	}
	return internalhttp.MergeHeaders(headers, cfg.Headers)
}

func unsupportedStreamingTranscriptionWarning(option string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: "providerOptions.openai." + option,
		Details: fmt.Sprintf("OpenAI streaming transcription does not support %s.", option),
	}
}

func realtimeTranscriptionWebSocketURL(baseURL string) string {
	wsBase := strings.Replace(baseURL, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	return wsBase + "/realtime?intent=transcription"
}

// buildOpenAIRealtimeTranscriptionSession mirrors TS
// buildOpenAIRealtimeTranscriptionSession.
func buildOpenAIRealtimeTranscriptionSession(modelID string, format provider.AudioFormat, opts transcriptionStreamOpenAIOptionsValue) map[string]interface{} {
	inputFormat := map[string]interface{}{"type": format.Type}
	if format.Rate != nil {
		inputFormat["rate"] = *format.Rate
	}

	transcription := map[string]interface{}{"model": modelID}
	if opts.Language != "" {
		transcription["language"] = opts.Language
	}
	if opts.StreamingDelay != nil {
		transcription["delay"] = *opts.StreamingDelay
	}

	session := map[string]interface{}{
		"type": "transcription",
		"audio": map[string]interface{}{
			"input": map[string]interface{}{
				"format":         inputFormat,
				"transcription":  transcription,
				"turn_detection": nil,
			},
		},
	}
	if len(opts.StreamingInclude) > 0 {
		session["include"] = opts.StreamingInclude
	}
	return map[string]interface{}{"type": "session.update", "session": session}
}

// transcriptionStreamOpenAIOptionsValue holds providerOptions.openai fields
// relevant to streaming transcription, plus flags for REST-only fields that
// warn when set during streaming.
type transcriptionStreamOpenAIOptionsValue struct {
	Language               string
	StreamingDelay         *int
	StreamingInclude       []string
	hasInclude             bool
	Prompt                 string
	HasTemperature         bool
	TimestampGranularities []string
}

func transcriptionStreamOpenAIOptions(providerOptions map[string]interface{}) transcriptionStreamOpenAIOptionsValue {
	var result transcriptionStreamOpenAIOptionsValue
	if providerOptions == nil {
		return result
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return result
	}
	if v, ok := openaiOpts["language"].(string); ok {
		result.Language = v
	}
	if v, ok := openaiOpts["prompt"].(string); ok {
		result.Prompt = v
	}
	if _, ok := openaiOpts["temperature"]; ok {
		result.HasTemperature = true
	}
	if v, ok := openaiOpts["include"].([]interface{}); ok && len(v) > 0 {
		result.hasInclude = true
	}
	if v, ok := openaiOpts["timestampGranularities"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	if streaming, ok := openaiOpts["streaming"].(map[string]interface{}); ok {
		if v, ok := numericValue(streaming["delay"]); ok {
			d := int(v)
			result.StreamingDelay = &d
		}
		if v, ok := streaming["include"].([]interface{}); ok {
			for _, item := range v {
				if s, ok := item.(string); ok {
					result.StreamingInclude = append(result.StreamingInclude, s)
				}
			}
		}
	}
	return result
}

func numericValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// openAIRealtimeWSAuth mirrors TS getOpenAIRealtimeConnection: the bearer
// token rides the "openai-insecure-api-key" subprotocol (a plain WebSocket
// handshake cannot rely on an Authorization header being honored uniformly),
// and the Authorization header is stripped since OpenAI rejects handshakes
// carrying both auth channels.
func openAIRealtimeWSAuth(headers map[string]string) ([]string, map[string]string) {
	var token string
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") && v != "" {
			if rest, ok := strings.CutPrefix(v, "Bearer "); ok {
				token = rest
			} else if rest, ok := strings.CutPrefix(v, "bearer "); ok {
				token = rest
			}
		}
	}
	if token == "" {
		return []string{"realtime"}, headers
	}
	filtered := make(map[string]string, len(headers))
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") {
			continue
		}
		filtered[k] = v
	}
	return []string{"realtime", "openai-insecure-api-key." + token}, filtered
}

// openAIRealtimeTranscriptionStreamConfig bundles the inputs to
// newOpenAIRealtimeTranscriptionStream.
type openAIRealtimeTranscriptionStreamConfig struct {
	url              string
	headers          map[string]string
	sessionUpdate    map[string]interface{}
	language         string
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// openAIRealtimeTranscriptionStream implements provider.TranscriptionStream
// over the OpenAI realtime WebSocket, mirroring TS
// createOpenAIRealtimeTranscriptionStream.
type openAIRealtimeTranscriptionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.TranscriptionStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newOpenAIRealtimeTranscriptionStream(parentCtx context.Context, cfg openAIRealtimeTranscriptionStreamConfig) *openAIRealtimeTranscriptionStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &openAIRealtimeTranscriptionStream{ctx: ctx, cancel: cancel, parts: make(chan provider.TranscriptionStreamPart)}
	go s.run(cfg)
	return s
}

func (s *openAIRealtimeTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
	part, ok := <-s.parts
	if !ok {
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, io.EOF
	}
	return &part, nil
}

func (s *openAIRealtimeTranscriptionStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *openAIRealtimeTranscriptionStream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		s.connMu.Lock()
		if s.conn != nil {
			_ = s.conn.Close()
		}
		s.connMu.Unlock()
	})
	return nil
}

func (s *openAIRealtimeTranscriptionStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *openAIRealtimeTranscriptionStream) emit(part provider.TranscriptionStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *openAIRealtimeTranscriptionStream) run(cfg openAIRealtimeTranscriptionStreamConfig) {
	defer close(s.parts)

	conn, err := s.dial(cfg.url, cfg.headers)
	if err != nil {
		s.setErr(err)
		cfg.audio.Cancel(err)
		return
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	defer conn.Close() //nolint:errcheck

	// Send the session update and start pumping audio before emitting
	// stream-start: setup must not wait for a consumer to be reading yet
	// (mirrors TS onOpen, where controller.enqueue does not block on a
	// reader before the socket send and sendAudio() call that follow it).
	payload, err := json.Marshal(cfg.sessionUpdate)
	if err != nil {
		s.setErr(err)
		cfg.audio.Cancel(err)
		return
	}
	if err := s.send(conn, payload); err != nil {
		s.setErr(err)
		cfg.audio.Cancel(err)
		return
	}

	go s.pumpAudio(conn, cfg.audio)

	if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	for {
		msg, err := s.receive(conn)
		if err != nil {
			select {
			case <-s.ctx.Done():
				cause := s.ctx.Err()
				s.setErr(cause)
				cfg.audio.Cancel(cause)
			default:
				if errors.Is(err, io.EOF) {
					// A clean close with no completed/error event yet is a
					// normal end of stream, not a failure (TS onClose calls
					// controller.close(), not controller.error(), when the
					// stream isn't already finished).
					cfg.audio.Cancel(nil)
					return
				}
				realtimeErr := errors.New("OpenAI realtime transcription error")
				s.setErr(realtimeErr)
				cfg.audio.Cancel(realtimeErr)
			}
			return
		}

		var raw map[string]interface{}
		if jsonErr := json.Unmarshal(msg, &raw); jsonErr != nil {
			continue
		}
		if cfg.includeRawChunks {
			if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: raw}) {
				return
			}
		}

		eventType, _ := raw["type"].(string)
		switch eventType {
		case "conversation.item.input_audio_transcription.delta":
			itemID, _ := raw["item_id"].(string)
			delta, _ := raw["delta"].(string)
			if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: itemID, Delta: delta}) {
				return
			}

		case "conversation.item.input_audio_transcription.completed":
			itemID, _ := raw["item_id"].(string)
			transcript, _ := raw["transcript"].(string)
			if itemID != "" {
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: itemID, Text: transcript}) {
					return
				}
			}
			langCopy := cfg.language
			finish := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: transcript, Language: langCopy}
			s.emit(finish)
			return

		case "error":
			message := "OpenAI realtime error"
			if errObj, ok := raw["error"].(map[string]interface{}); ok {
				if m, ok := errObj["message"].(string); ok && m != "" {
					message = m
				}
			}
			streamErr := errors.New(message)
			s.setErr(streamErr)
			cfg.audio.Cancel(streamErr)
			return
		}
	}
}

// pumpAudio reads chunks from audio and forwards them as
// input_audio_buffer.append messages, committing the buffer at EOF. Errors
// close the connection with the pump's error path, mirroring TS sendAudio's
// finally-driven cleanup.
func (s *openAIRealtimeTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream) {
	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				_ = s.send(conn, []byte(`{"type":"input_audio_buffer.commit"}`))
			}
			return
		}
		msg, marshalErr := json.Marshal(map[string]string{
			"type":  "input_audio_buffer.append",
			"audio": base64.StdEncoding.EncodeToString(chunk),
		})
		if marshalErr != nil {
			continue
		}
		if err := s.send(conn, msg); err != nil {
			return
		}
	}
}

func (s *openAIRealtimeTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	protocols, filteredHeaders := openAIRealtimeWSAuth(headers)
	wsConfig, err := websocket.NewConfig(wsURL, "http://localhost/")
	if err != nil {
		return nil, err
	}
	wsConfig.Protocol = protocols
	wsConfig.Header = stdhttp.Header{}
	for k, v := range filteredHeaders {
		if v != "" {
			wsConfig.Header.Set(k, v)
		}
	}

	// DialContext (rather than DialConfig, which always dials against
	// context.Background()) forces the pending handshake to fail and cleans
	// up the socket when s.ctx is cancelled mid-dial, instead of leaving an
	// unread, unclosed connection behind if the dial completes after we've
	// already given up on it.
	return wsConfig.DialContext(s.ctx)
}

func (s *openAIRealtimeTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	done := make(chan error, 1)
	go func() {
		done <- websocket.Message.Send(conn, string(message))
	}()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case err := <-done:
		return err
	}
}

func (s *openAIRealtimeTranscriptionStream) receive(conn *websocket.Conn) ([]byte, error) {
	type result struct {
		msg string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var msg string
		err := websocket.Message.Receive(conn, &msg)
		ch <- result{msg: msg, err: err}
	}()
	select {
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return []byte(res.msg), nil
	}
}

var (
	_ provider.TranscriptionModel    = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*openAIRealtimeTranscriptionStream)(nil)
)
