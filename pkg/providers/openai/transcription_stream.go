package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// bearerTokenPattern extracts the token from an "Authorization: Bearer
// <token>" header value, mirroring TS getOpenAIRealtimeConnection's
// `/^bearer\s+(.+)$/i`: the "bearer" scheme name is matched
// case-insensitively (the HTTP auth scheme is case-insensitive) and any run
// of whitespace separates it from the token, not just a single space.
var bearerTokenPattern = regexp.MustCompile(`(?i)^bearer\s+(.+)$`)

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

// baseWSHeaders returns the provider's default headers (Authorization,
// organization, project, custom config headers, and the
// `ai-sdk/openai/VERSION` User-Agent tag) for the WebSocket handshake,
// mirroring TS openai-transcription-model.ts's doStream, which reuses
// `this.config.headers()` -- the same tagged getHeaders() closure used for
// REST calls -- rather than rebuilding an untagged header set.
func (m *TranscriptionModel) baseWSHeaders() map[string]string {
	return m.provider.client.Headers()
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
			if m := bearerTokenPattern.FindStringSubmatch(v); m != nil {
				token = m[1]
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
	// Release s.ctx's resources as soon as run() returns for any reason
	// instead of only on an explicit Close() call, which a consumer that
	// only drains Next() to io.EOF may never make.
	defer s.cancel()

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

	audioErrCh := make(chan error, 1)
	go s.pumpAudio(conn, cfg.audio, audioErrCh)

	if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.ctx, conn, msgCh)

	for {
		select {
		case <-s.ctx.Done():
			cause := s.ctx.Err()
			s.setErr(cause)
			cfg.audio.Cancel(cause)
			return

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`: a
			// failure pumping audio (reading the caller's AudioStream, or
			// writing to the WebSocket) terminates the stream with an error
			// instead of being silently dropped.
			s.setErr(audioErr)
			cfg.audio.Cancel(audioErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if wsutil.IsCleanClose(res.Err) {
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
				return
			}

			var raw map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
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
}

// pumpAudio reads chunks from audio and forwards them as
// input_audio_buffer.append messages, committing the buffer at EOF. Any other
// failure — reading from the AudioStream, or writing to the WebSocket — is
// reported on errCh, mirroring TS's `void sendAudio(socket).catch(finishWithError)`
// (a rejected `audioReader.read()` fails the stream exactly like a failed
// `socket.send`). A failure that stems from s.ctx already being cancelled is
// not reported here: run()'s own select on s.ctx.Done() already handles that
// case.
func (s *openAIRealtimeTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				if sendErr := s.send(conn, []byte(`{"type":"input_audio_buffer.commit"}`)); sendErr != nil {
					s.reportAudioError(errCh, sendErr)
				}
			} else if s.ctx.Err() == nil {
				s.reportAudioError(errCh, err)
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
			if s.ctx.Err() == nil {
				s.reportAudioError(errCh, err)
			}
			return
		}
	}
}

// reportAudioError delivers err to errCh, falling back to s.ctx.Done() so a
// send that no longer has a reader (run() already returned via a different
// path) cannot block pumpAudio forever.
func (s *openAIRealtimeTranscriptionStream) reportAudioError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-s.ctx.Done():
	}
}

func (s *openAIRealtimeTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	protocols, filteredHeaders := openAIRealtimeWSAuth(headers)
	return wsutil.Dial(s.ctx, wsURL, wsutil.DialOptions{Headers: filteredHeaders, Protocols: protocols})
}

func (s *openAIRealtimeTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.ctx, conn, string(message))
}

var (
	_ provider.TranscriptionModel    = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*openAIRealtimeTranscriptionStream)(nil)
)
