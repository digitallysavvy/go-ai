package cartesia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"golang.org/x/net/websocket"
)

// cartesiaLinearPCMStreamingEncodings are the linear PCM encodings that
// legitimately widen generic `audio/pcm` input, mirroring TS
// LINEAR_PCM_STREAMING_ENCODINGS (the streaming encoding enum minus the
// G.711 companding laws, pcm_mulaw and pcm_alaw).
var cartesiaLinearPCMStreamingEncodings = map[string]bool{
	"pcm_f16le": true,
	"pcm_f32le": true,
	"pcm_s16le": true,
	"pcm_s32le": true,
}

// cartesiaEncodingFromInputAudioFormat mirrors TS
// cartesiaEncodingFromInputAudioFormat.
func cartesiaEncodingFromInputAudioFormat(inputType string) (string, error) {
	switch inputType {
	case "audio/pcm":
		return "pcm_s16le", nil
	case "audio/pcmu":
		return "pcm_mulaw", nil
	case "audio/pcma":
		return "pcm_alaw", nil
	default:
		return "", &providererrors.InvalidArgumentError{
			Field:   "inputAudioFormat",
			Message: fmt.Sprintf("Unsupported Cartesia streaming audio format: %s", inputType),
		}
	}
}

// buildCartesiaStreamingTranscriptionURL mirrors TS
// buildCartesiaStreamingTranscriptionUrl.
func buildCartesiaStreamingTranscriptionURL(baseURL, version, modelID, encoding string, sampleRate int, language *string, token string, useTurnDetection bool) (*url.URL, error) {
	path := "/stt/websocket"
	if useTurnDetection {
		path = "/stt/turns/websocket"
	}
	full := strings.TrimRight(baseURL, "/") + path
	u, err := url.Parse(full)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}

	q := url.Values{}
	q.Set("model", modelID)
	q.Set("encoding", encoding)
	q.Set("sample_rate", strconv.Itoa(sampleRate))
	q.Set("cartesia_version", version)
	q.Set("access_token", token)
	if !useTurnDetection && language != nil {
		q.Set("language", *language)
	}
	u.RawQuery = q.Encode()
	return u, nil
}

// createStreamingAccessToken mirrors TS
// CartesiaTranscriptionModel#createStreamingAccessToken (POST /access-token
// with {grants: {stt: true}}).
func (m *TranscriptionModel) createStreamingAccessToken(ctx context.Context, headers map[string]string) (string, error) {
	var resp struct {
		Token string `json:"token"`
	}
	if err := m.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/access-token",
		Body:    map[string]interface{}{"grants": map[string]interface{}{"stt": true}},
		Headers: headers,
	}, &resp); err != nil {
		return "", handleError(err)
	}
	return resp.Token, nil
}

// DoStream streams a transcript for live audio over Cartesia's Ink 2
// realtime WebSocket endpoint (/stt/websocket or /stt/turns/websocket).
// Mirrors TS CartesiaTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	if !isStreamingTranscriptionModelID(m.modelID) {
		return nil, fmt.Errorf("%w: streaming transcription with %s", providererrors.ErrUnsupportedFeature, m.modelID)
	}

	currentDate := time.Now()
	warnings := []types.Warning{}
	cartesiaOpts := extractTranscriptionModelOptions(opts.ProviderOptions)

	var language *string
	if cartesiaOpts != nil && cartesiaOpts.Language != "" {
		l := cartesiaOpts.Language
		language = &l
	}
	if language != nil && *language != "en" {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "Cartesia Ink 2 currently supports English only.",
		}
	}

	if cartesiaOpts != nil && len(cartesiaOpts.TimestampGranularities) > 0 {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.cartesia.timestampGranularities",
			Details: "Cartesia streaming transcription does not support timestamp granularities.",
		})
	}

	// Always validate the declared media type — an explicit
	// streaming.encoding override must not bypass the allowlist.
	inferredEncoding, err := cartesiaEncodingFromInputAudioFormat(opts.InputAudioFormat.Type)
	if err != nil {
		return nil, err
	}

	var streaming *StreamingOptions
	if cartesiaOpts != nil {
		streaming = cartesiaOpts.Streaming
	}

	encoding := inferredEncoding
	if streaming != nil && streaming.Encoding != "" {
		encoding = streaming.Encoding
	}
	// `audio/pcm` is generic linear PCM, so widening its default 16-bit
	// interpretation is the intended use of the option. Overriding a G.711
	// media type (or turning generic PCM into G.711) contradicts the
	// declared format; surface that instead of sending it silently.
	if encoding != inferredEncoding {
		widening := inferredEncoding == "pcm_s16le" && cartesiaLinearPCMStreamingEncodings[encoding]
		if !widening {
			warnings = append(warnings, types.Warning{
				Type: "other",
				Message: fmt.Sprintf(
					"providerOptions.cartesia.streaming.encoding '%s' contradicts inputAudioFormat.type '%s' (inferred '%s'); sending '%s'.",
					encoding, opts.InputAudioFormat.Type, inferredEncoding, encoding,
				),
			})
		}
	}

	token, err := m.createStreamingAccessToken(ctx, opts.Headers)
	if err != nil {
		return nil, err
	}

	useTurnDetection := streaming == nil || streaming.TurnDetection == nil || *streaming.TurnDetection

	version := m.provider.config.APIVersion
	if version == "" {
		version = DefaultAPIVersion
	}

	sampleRate := 24000
	if opts.InputAudioFormat.Rate != nil {
		sampleRate = *opts.InputAudioFormat.Rate
	}

	var urlLanguage *string
	if !useTurnDetection && language != nil {
		urlLanguage = language
	}

	wsURL, err := buildCartesiaStreamingTranscriptionURL(m.provider.config.BaseURL, version, m.modelID, encoding, sampleRate, urlLanguage, token, useTurnDetection)
	if err != nil {
		return nil, err
	}

	// The request body echoes the connection URL with the access token
	// stripped, matching TS's `requestUrl.searchParams.delete('access_token')`.
	requestURL := *wsURL
	strippedQuery := requestURL.Query()
	strippedQuery.Del("access_token")
	requestURL.RawQuery = strippedQuery.Encode()

	resolvedLanguage := "en"
	if language != nil {
		resolvedLanguage = *language
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newCartesiaTranscriptionStream(abortCtx, cartesiaTranscriptionStreamConfig{
		url:              wsURL,
		warnings:         warnings,
		language:         resolvedLanguage,
		useTurnDetection: useTurnDetection,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: requestURL.String(),
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: currentDate, ModelID: m.modelID},
	}, nil
}

type cartesiaTranscriptionStreamConfig struct {
	url              *url.URL
	warnings         []types.Warning
	language         string
	useTurnDetection bool
	audio            provider.AudioStream
	includeRawChunks bool
}

// cartesiaTranscriptionStream implements provider.TranscriptionStream over
// Cartesia's Ink 2 realtime WebSocket, mirroring TS
// createCartesiaStreamingTranscriptionStream.
type cartesiaTranscriptionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.TranscriptionStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn

	// finished mirrors the `finished` flag in TS: set once the stream has
	// reached a terminal state, checked by pumpAudio (a separate goroutine)
	// before sending the post-audio close/finalize control message.
	finished atomic.Bool
}

func newCartesiaTranscriptionStream(parentCtx context.Context, cfg cartesiaTranscriptionStreamConfig) *cartesiaTranscriptionStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &cartesiaTranscriptionStream{ctx: ctx, cancel: cancel, parts: make(chan provider.TranscriptionStreamPart)}
	go s.run(cfg)
	return s
}

func (s *cartesiaTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
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

func (s *cartesiaTranscriptionStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *cartesiaTranscriptionStream) Close() error {
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

func (s *cartesiaTranscriptionStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *cartesiaTranscriptionStream) emit(part provider.TranscriptionStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

type cartesiaWSResult struct {
	msg string
	err error
}

// receiveLoop continuously reads text frames from conn and forwards each one
// (or the terminal error) on out, until an error occurs or s.ctx is done.
func (s *cartesiaTranscriptionStream) receiveLoop(conn *websocket.Conn, out chan<- cartesiaWSResult) {
	for {
		var msg string
		err := websocket.Message.Receive(conn, &msg)
		select {
		case out <- cartesiaWSResult{msg: msg, err: err}:
		case <-s.ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// send writes v (a string for a text frame, or []byte for a binary frame) to
// conn, unblocking early if s.ctx is cancelled mid-write.
func (s *cartesiaTranscriptionStream) send(conn *websocket.Conn, v interface{}) error {
	done := make(chan error, 1)
	go func() {
		done <- websocket.Message.Send(conn, v)
	}()
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	case err := <-done:
		return err
	}
}

// pumpAudio forwards audio chunks as binary frames until the AudioStream is
// exhausted (io.EOF, per the AudioStream contract), then sends the
// close/finalize control message (unless the stream has already reached a
// terminal state), mirroring TS sendAudio. Any other failure — reading from
// the AudioStream, or writing to the WebSocket — is reported on errCh,
// mirroring `void sendAudio(socket).catch(finishWithError)`. A failure that
// stems from s.ctx already being cancelled is not reported here: run()'s own
// select on s.ctx.Done() already handles that case.
func (s *cartesiaTranscriptionStream) pumpAudio(conn *websocket.Conn, cfg cartesiaTranscriptionStreamConfig, errCh chan<- error) {
	for {
		chunk, err := cfg.audio.Next(s.ctx)
		if err != nil {
			if err != io.EOF && s.ctx.Err() == nil {
				s.reportAudioError(errCh, err)
			}
			break
		}
		if sendErr := s.send(conn, chunk); sendErr != nil {
			if s.ctx.Err() == nil {
				s.reportAudioError(errCh, sendErr)
			}
			return
		}
	}

	if s.finished.Load() || s.ctx.Err() != nil {
		return
	}
	if cfg.useTurnDetection {
		payload, marshalErr := json.Marshal(map[string]string{"type": "close"})
		if marshalErr != nil {
			s.reportAudioError(errCh, marshalErr)
			return
		}
		if sendErr := s.send(conn, string(payload)); sendErr != nil && s.ctx.Err() == nil {
			s.reportAudioError(errCh, sendErr)
		}
	} else {
		if sendErr := s.send(conn, "finalize"); sendErr != nil && s.ctx.Err() == nil {
			s.reportAudioError(errCh, sendErr)
		}
	}
}

// reportAudioError delivers err to errCh, falling back to s.ctx.Done() so a
// send that no longer has a reader (run() already returned via a different
// path) cannot block pumpAudio forever.
func (s *cartesiaTranscriptionStream) reportAudioError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-s.ctx.Done():
	}
}

// cartesiaStreamingTranscriptionEvent is a server->client realtime
// transcription event, mirroring TS CartesiaStreamingTranscriptionEvent.
type cartesiaStreamingTranscriptionEvent struct {
	Type       string   `json:"type,omitempty"`
	RequestID  string   `json:"request_id,omitempty"`
	Transcript string   `json:"transcript,omitempty"`
	Text       string   `json:"text,omitempty"`
	IsFinal    *bool    `json:"is_final,omitempty"`
	Duration   *float64 `json:"duration,omitempty"`
	Message    string   `json:"message,omitempty"`
	ErrorCode  string   `json:"error_code,omitempty"`
}

func (s *cartesiaTranscriptionStream) run(cfg cartesiaTranscriptionStreamConfig) {
	defer close(s.parts)
	// Release s.ctx's resources as soon as run() returns for any reason
	// instead of only on an explicit Close() call, which a consumer that
	// only drains Next() to io.EOF may never make.
	defer s.cancel()

	var finished bool

	fail := func(err error) {
		finished = true
		s.finished.Store(true)
		s.setErr(err)
		cfg.audio.Cancel(err)
	}

	conn, err := s.dial(cfg.url)
	if err != nil {
		fail(err)
		return
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	defer conn.Close() //nolint:errcheck

	if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan cartesiaWSResult)
	audioErrCh := make(chan error, 1)
	go s.receiveLoop(conn, msgCh)
	go s.pumpAudio(conn, cfg, audioErrCh)

	var (
		finalTexts      []string
		durationSeconds float64
	)

	finish := func() {
		if finished {
			return
		}
		finished = true
		s.finished.Store(true)
		sep := ""
		if cfg.useTurnDetection {
			sep = " "
		}
		part := provider.TranscriptionStreamPart{
			Type:       provider.TranscriptionStreamPartTypeFinish,
			FinishText: strings.Join(finalTexts, sep),
			Segments:   []provider.TranscriptSegment{},
			Language:   cfg.language,
		}
		if durationSeconds > 0 {
			d := durationSeconds
			part.DurationInSeconds = &d
		}
		s.emit(part)
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.ctx.Done():
			fail(s.ctx.Err())
			return

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`: a
			// failure pumping audio (reading the caller's AudioStream, or
			// writing to the WebSocket) terminates the stream with an error.
			if finished {
				return
			}
			fail(audioErr)
			return

		case res := <-msgCh:
			if res.err != nil {
				// A received WebSocket close frame (io.EOF from
				// golang.org/x/net/websocket's frame reader) mirrors TS
				// connectToWebSocket's onClose: an implicit, silent finish if
				// the stream hasn't already reached a terminal state. Any
				// other read error (connection reset, protocol violation,
				// etc.) mirrors onSocketError: an abnormal disconnection
				// that must surface as an error, not a quiet success.
				if finished {
					return
				}
				if errors.Is(res.err, io.EOF) {
					finish()
				} else {
					fail(errors.New("Cartesia streaming transcription error"))
				}
				return
			}
			if finished {
				continue
			}

			var raw cartesiaStreamingTranscriptionEvent
			if jsonErr := json.Unmarshal([]byte(res.msg), &raw); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.msg), &rawValue)
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}

			switch raw.Type {
			case "turn.update", "turn.eager_end":
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: raw.RequestID, Text: raw.Transcript}) {
					return
				}

			case "turn.end":
				text := raw.Transcript
				finalTexts = append(finalTexts, text)
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: raw.RequestID, Text: text}) {
					return
				}

			case "transcript":
				transcript := raw.Text
				if raw.IsFinal != nil && *raw.IsFinal {
					finalTexts = append(finalTexts, transcript)
					if raw.Duration != nil {
						durationSeconds += *raw.Duration
					}
					if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: raw.RequestID, Text: transcript}) {
						return
					}
				} else {
					part := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: raw.RequestID, Text: transcript}
					if raw.Duration != nil {
						d := *raw.Duration
						part.DurationInSeconds = &d
					}
					if !s.emit(part) {
						return
					}
				}

			case "flush_done":
				_ = s.send(conn, "close")

			case "done":
				finish()
				return

			case "error":
				msg := raw.Message
				if msg == "" {
					msg = raw.ErrorCode
				}
				if msg == "" {
					msg = "Cartesia streaming transcription error"
				}
				fail(errors.New(msg))
				return
			}
		}
	}
}

func (s *cartesiaTranscriptionStream) dial(wsURL *url.URL) (*websocket.Conn, error) {
	wsConfig, err := websocket.NewConfig(wsURL.String(), "http://localhost/")
	if err != nil {
		return nil, err
	}

	// DialContext (rather than DialConfig, which always dials against
	// context.Background()) forces the pending handshake to fail and cleans
	// up the socket when s.ctx is cancelled mid-dial, instead of leaving an
	// unread, unclosed connection behind if the dial completes after we've
	// already given up on it.
	return wsConfig.DialContext(s.ctx)
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*cartesiaTranscriptionStream)(nil)
)
