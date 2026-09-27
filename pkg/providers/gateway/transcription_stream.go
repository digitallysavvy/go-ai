package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/net/websocket"
)

// Gateway streaming transcription WebSocket envelope (v1) frame types (TS
// transcription-stream-envelope.ts). The client sends exactly one "start"
// TEXT frame, then raw audio as BINARY frames, then "audio-done"; the server
// replies with one JSON-serialized TranscriptionModelV4StreamPart per TEXT
// frame and closes 1000 after "finish" (non-1000 after an "error" part).
const (
	transcriptionStreamStartFrameType     = "transcription-stream.start"
	transcriptionStreamAudioDoneFrameType = "transcription-stream.audio-done"

	// maxTranscriptionAudioFrameBytes keeps audio frames under server
	// frame-size limits (envelope rule 7).
	maxTranscriptionAudioFrameBytes = 64 * 1024
)

// DoStream streams a transcript for live audio over the Gateway's streaming
// transcription WebSocket, using the ai-gateway-transcription.v1 subprotocol
// and the transcription-stream envelope. Mirrors TS
// GatewayTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}

	token, authMethod, err := m.provider.authResolver(ctx)
	if err != nil {
		return nil, err
	}

	headers := internalhttp.MergeHeaders(m.provider.headers, m.getModelConfigHeaders())
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)
	// The handshake carries auth on both channels, matching TS
	// createAuthHeaders: the raw Authorization/ai-gateway-auth-method headers
	// alongside the ai-gateway-auth.<token> subprotocol below.
	headers["Authorization"] = "Bearer " + token
	headers["ai-gateway-auth-method"] = authMethod

	startFrame := map[string]interface{}{
		"type":             transcriptionStreamStartFrameType,
		"inputAudioFormat": transcriptionAudioFormatWire(opts.InputAudioFormat),
	}
	if opts.ProviderOptions != nil {
		startFrame["providerOptions"] = opts.ProviderOptions
	}
	if opts.IncludeRawChunks {
		startFrame["includeRawChunks"] = true
	}

	wsURL := ToGatewayTranscriptionURL(m.provider.baseURL, m.modelID)
	protocols := GetGatewayTranscriptionProtocols(token, m.provider.config.TeamIDOrSlug)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGatewayTranscriptionStream(abortCtx, gatewayTranscriptionStreamConfig{
		url:        wsURL,
		protocols:  protocols,
		headers:    headers,
		startFrame: startFrame,
		audio:      opts.Audio,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: startFrame,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

func transcriptionAudioFormatWire(f provider.AudioFormat) map[string]interface{} {
	out := map[string]interface{}{"type": f.Type}
	if f.Rate != nil {
		out["rate"] = *f.Rate
	}
	return out
}

type gatewayTranscriptionStreamConfig struct {
	url        string
	protocols  []string
	headers    map[string]string
	startFrame map[string]interface{}
	audio      provider.AudioStream
}

// gatewayTranscriptionStream implements provider.TranscriptionStream over the
// AI Gateway's streaming transcription WebSocket.
type gatewayTranscriptionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.TranscriptionStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newGatewayTranscriptionStream(parentCtx context.Context, cfg gatewayTranscriptionStreamConfig) *gatewayTranscriptionStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &gatewayTranscriptionStream{ctx: ctx, cancel: cancel, parts: make(chan provider.TranscriptionStreamPart)}
	go s.run(cfg)
	return s
}

func (s *gatewayTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
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

func (s *gatewayTranscriptionStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *gatewayTranscriptionStream) Close() error {
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

func (s *gatewayTranscriptionStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *gatewayTranscriptionStream) emit(part provider.TranscriptionStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *gatewayTranscriptionStream) run(cfg gatewayTranscriptionStreamConfig) {
	defer close(s.parts)

	fail := func(err error) {
		s.setErr(err)
		cfg.audio.Cancel(err)
	}

	conn, err := s.dial(cfg.url, cfg.protocols, cfg.headers)
	if err != nil {
		fail(err)
		return
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	defer conn.Close() //nolint:errcheck

	startPayload, err := json.Marshal(cfg.startFrame)
	if err != nil {
		fail(err)
		return
	}
	if err := s.sendText(conn, string(startPayload)); err != nil {
		fail(err)
		return
	}

	// audioCtx (rather than s.ctx directly) lets a server "error" part stop
	// audio pumping while the receive loop below keeps running to observe
	// the server's terminal close (TS stopAudio()).
	audioCtx, stopAudio := context.WithCancel(s.ctx)
	defer stopAudio()
	go s.pumpAudio(audioCtx, conn, cfg.audio)

	var lastServerError interface{}
	hasServerError := false
	finished := false

	for {
		text, err := s.receive(conn)
		if err != nil {
			if finished {
				return
			}
			if s.ctx.Err() != nil {
				fail(s.ctx.Err())
			} else if hasServerError {
				fail(gatewayTranscriptionServerError(lastServerError))
			} else if isGatewaySocketError(err) {
				fail(errors.New("Connection error on AI Gateway transcription stream"))
			} else {
				fail(errors.New("AI Gateway transcription stream closed before a finish part was received"))
			}
			return
		}

		part, ok := parseGatewayTranscriptionStreamPart(text)
		if !ok {
			continue
		}

		if part.Type == provider.TranscriptionStreamPartTypeFinish {
			finished = true
			if !s.emit(*part) {
				return
			}
			return
		}

		if part.Type == provider.TranscriptionStreamPartTypeError {
			hasServerError = true
			lastServerError = part.Err
			// envelope rule 5: error parts are terminal — stop sending audio
			// while the server holds the connection open (e.g. for its final
			// billing flush).
			stopAudio()
			cfg.audio.Cancel(nil)
		}

		if !s.emit(*part) {
			return
		}
	}
}

// isGatewaySocketError reports whether err came from the underlying
// connection failing outright (as opposed to a clean close), matching TS's
// distinction between onSocketError and onClose with no error precedent.
// golang.org/x/net/websocket surfaces both as plain errors from
// websocket.Message.Receive with no close-code information to key on, so any
// error other than a clean io.EOF is treated as a socket-level error.
func isGatewaySocketError(err error) bool {
	return err != nil && !errors.Is(err, io.EOF)
}

func gatewayTranscriptionServerError(payload interface{}) error {
	if m, ok := payload.(map[string]interface{}); ok {
		if msg, ok := m["message"].(string); ok && msg != "" {
			return fmt.Errorf("AI Gateway transcription stream failed: %s", msg)
		}
	}
	return fmt.Errorf("AI Gateway transcription stream failed: %v", payload)
}

// pumpAudio reads chunks from audio and forwards them as binary frames (split
// to stay under the server frame-size limit), sending the audio-done TEXT
// frame at EOF.
func (s *gatewayTranscriptionStream) pumpAudio(ctx context.Context, conn *websocket.Conn, audio provider.AudioStream) {
	for {
		chunk, err := audio.Next(ctx)
		if err != nil {
			if err == io.EOF {
				_ = s.sendText(conn, fmt.Sprintf(`{"type":%q}`, transcriptionStreamAudioDoneFrameType))
			}
			return
		}
		for offset := 0; offset < len(chunk); offset += maxTranscriptionAudioFrameBytes {
			end := offset + maxTranscriptionAudioFrameBytes
			if end > len(chunk) {
				end = len(chunk)
			}
			if err := s.sendBinary(conn, chunk[offset:end]); err != nil {
				return
			}
		}
	}
}

func (s *gatewayTranscriptionStream) dial(wsURL string, protocols []string, headers map[string]string) (*websocket.Conn, error) {
	wsConfig, err := websocket.NewConfig(wsURL, "http://localhost/")
	if err != nil {
		return nil, err
	}
	wsConfig.Protocol = protocols
	wsConfig.Header = stdhttp.Header{}
	for k, v := range headers {
		if v != "" {
			wsConfig.Header.Set(k, v)
		}
	}

	// DialContext (rather than DialConfig, which always dials against
	// context.Background()) forces the pending TCP/TLS handshake to fail and
	// cleans up the socket when s.ctx is cancelled mid-dial, instead of
	// leaving an unread, unclosed connection behind if the dial completes
	// after we've already given up on it.
	return wsConfig.DialContext(s.ctx)
}

func (s *gatewayTranscriptionStream) sendText(conn *websocket.Conn, message string) error {
	done := make(chan error, 1)
	go func() {
		done <- websocket.Message.Send(conn, message)
	}()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case err := <-done:
		return err
	}
}

func (s *gatewayTranscriptionStream) sendBinary(conn *websocket.Conn, message []byte) error {
	done := make(chan error, 1)
	go func() {
		done <- websocket.Message.Send(conn, message)
	}()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case err := <-done:
		return err
	}
}

func (s *gatewayTranscriptionStream) receive(conn *websocket.Conn) (string, error) {
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
		return "", s.ctx.Err()
	case res := <-ch:
		if res.err != nil {
			return "", res.err
		}
		return res.msg, nil
	}
}

// parseGatewayTranscriptionStreamPart parses one server TEXT frame (a
// flattened TranscriptionModelV4StreamPart) into a provider.TranscriptionStreamPart.
// Unknown types are ignored (envelope rule 6); malformed JSON returns ok=false.
func parseGatewayTranscriptionStreamPart(text string) (*provider.TranscriptionStreamPart, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, false
	}
	rawType, _ := raw["type"].(string)

	part := &provider.TranscriptionStreamPart{Type: rawType}

	switch rawType {
	case provider.TranscriptionStreamPartTypeStreamStart:
		part.Warnings = warningsFromInterface(raw["warnings"])

	case provider.TranscriptionStreamPartTypeDelta:
		part.ID, _ = raw["id"].(string)
		part.Delta, _ = raw["delta"].(string)
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypePartial:
		part.ID, _ = raw["id"].(string)
		part.Text, _ = raw["text"].(string)
		part.StartSecond = float64PtrFromInterface(raw["startSecond"])
		part.DurationInSeconds = float64PtrFromInterface(raw["durationInSeconds"])
		part.ChannelIndex = intPtrFromInterface(raw["channelIndex"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypeFinal:
		part.ID, _ = raw["id"].(string)
		part.Text, _ = raw["text"].(string)
		part.StartSecond = float64PtrFromInterface(raw["startSecond"])
		part.EndSecond = float64PtrFromInterface(raw["endSecond"])
		part.ChannelIndex = intPtrFromInterface(raw["channelIndex"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypeResponseMetadata:
		part.ModelID, _ = raw["modelId"].(string)
		part.Headers = stringMapFromInterface(raw["headers"])
		if ts, ok := raw["timestamp"].(string); ok {
			if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				part.Timestamp = parsed
			}
		}

	case provider.TranscriptionStreamPartTypeFinish:
		part.FinishText, _ = raw["text"].(string)
		part.Language, _ = raw["language"].(string)
		part.DurationInSeconds = float64PtrFromInterface(raw["durationInSeconds"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])
		if segs, ok := raw["segments"].([]interface{}); ok {
			for _, s := range segs {
				segMap, ok := s.(map[string]interface{})
				if !ok {
					continue
				}
				seg := provider.TranscriptSegment{}
				seg.Text, _ = segMap["text"].(string)
				if v, ok := float64FromInterface(segMap["startSecond"]); ok {
					seg.StartSecond = v
				}
				if v, ok := float64FromInterface(segMap["endSecond"]); ok {
					seg.EndSecond = v
				}
				part.Segments = append(part.Segments, seg)
			}
		}

	case provider.TranscriptionStreamPartTypeRaw:
		part.RawValue = raw["rawValue"]

	case provider.TranscriptionStreamPartTypeError:
		part.Err = raw["error"]

	default:
		return nil, false
	}

	return part, true
}

func warningsFromInterface(v interface{}) []types.Warning {
	list, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]types.Warning, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		w := types.Warning{}
		w.Type, _ = m["type"].(string)
		w.Feature, _ = m["feature"].(string)
		w.Setting, _ = m["setting"].(string)
		w.Details, _ = m["details"].(string)
		w.Message, _ = m["message"].(string)
		out = append(out, w)
	}
	return out
}

func mapFromInterface(v interface{}) map[string]interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

func stringMapFromInterface(v interface{}) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

func float64FromInterface(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func float64PtrFromInterface(v interface{}) *float64 {
	if f, ok := float64FromInterface(v); ok {
		return &f
	}
	return nil
}

func intPtrFromInterface(v interface{}) *int {
	if f, ok := float64FromInterface(v); ok {
		i := int(f)
		return &i
	}
	return nil
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*gatewayTranscriptionStream)(nil)
)
