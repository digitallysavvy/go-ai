package xai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
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

// isKnownXAIInputAudioFormatType reports whether typ is one of xAI's
// recognized raw input audio encodings, mirroring TS
// isKnownInputAudioFormat.
func isKnownXAIInputAudioFormatType(typ string) bool {
	return typ == "audio/pcm" || typ == "audio/pcmu" || typ == "audio/pcma"
}

// xaiEncodingFromInputAudioFormat maps an inputAudioFormat.type to xAI's
// `encoding` query parameter, mirroring TS encodingFromInputAudioFormat.
func xaiEncodingFromInputAudioFormat(typ string) string {
	switch typ {
	case "audio/pcmu":
		return "mulaw"
	case "audio/pcma":
		return "alaw"
	default:
		return "pcm"
	}
}

// buildXAIStreamingTranscriptionURL builds the wss:// streaming
// transcription URL with query parameters, mirroring TS
// buildXaiStreamingTranscriptionUrl.
func buildXAIStreamingTranscriptionURL(baseURL string, inputAudioFormat provider.AudioFormat, opts XAITranscriptionProviderOptions) (*url.URL, error) {
	full := strings.TrimRight(baseURL, "/") + "/stt"
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

	q := u.Query()

	sampleRate := opts.SampleRate
	if sampleRate == nil {
		sampleRate = inputAudioFormat.Rate
	}
	if sampleRate != nil {
		q.Set("sample_rate", strconv.Itoa(*sampleRate))
	}

	encoding := xaiEncodingFromInputAudioFormat(inputAudioFormat.Type)
	if opts.AudioFormat != nil {
		encoding = *opts.AudioFormat
	}
	q.Set("encoding", encoding)

	if opts.Language != nil {
		q.Set("language", *opts.Language)
	}
	if opts.Diarize != nil {
		q.Set("diarize", strconv.FormatBool(*opts.Diarize))
	}
	if opts.FillerWords != nil {
		q.Set("filler_words", strconv.FormatBool(*opts.FillerWords))
	}
	if opts.Multichannel != nil {
		q.Set("multichannel", strconv.FormatBool(*opts.Multichannel))
	}
	if opts.Channels != nil {
		q.Set("channels", strconv.Itoa(*opts.Channels))
	}
	if opts.Streaming != nil {
		if opts.Streaming.InterimResults != nil {
			q.Set("interim_results", strconv.FormatBool(*opts.Streaming.InterimResults))
		}
		if opts.Streaming.Endpointing != nil {
			q.Set("endpointing", strconv.Itoa(*opts.Streaming.Endpointing))
		}
		if opts.Streaming.SmartTurn != nil {
			q.Set("smart_turn", strconv.FormatFloat(*opts.Streaming.SmartTurn, 'f', -1, 64))
		}
		if opts.Streaming.SmartTurnTimeout != nil {
			q.Set("smart_turn_timeout", strconv.Itoa(*opts.Streaming.SmartTurnTimeout))
		}
	}
	for _, kt := range xaiKeyterms(opts.Keyterm) {
		q.Add("keyterm", kt)
	}

	u.RawQuery = q.Encode()
	return u, nil
}

// DoStream streams a transcript for live audio over xAI's `/stt` WebSocket
// endpoint. Mirrors TS XaiTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}

	xaiOpts, err := extractXAITranscriptionProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	if xaiOpts.Multichannel != nil && *xaiOpts.Multichannel && xaiOpts.Channels == nil {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "providerOptions.xai.channels is required when providerOptions.xai.multichannel is true",
		}
	}

	warnings := []types.Warning{}
	if xaiOpts.Format != nil {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.xai.format",
			Details: "xAI streaming transcription does not support format.",
		})
	}
	if xaiOpts.AudioFormat == nil && !isKnownXAIInputAudioFormatType(opts.InputAudioFormat.Type) {
		warnings = append(warnings, types.Warning{
			Type: "other",
			Message: fmt.Sprintf(
				"Unrecognized inputAudioFormat.type %q; falling back to raw PCM encoding. "+
					"Use audio/pcm, audio/pcmu, or audio/pcma, or set providerOptions.xai.audioFormat explicitly.",
				opts.InputAudioFormat.Type,
			),
		})
	}

	wsURL, err := buildXAIStreamingTranscriptionURL(m.provider.config.BaseURL, opts.InputAudioFormat, xaiOpts)
	if err != nil {
		return nil, err
	}

	// Reuse the provider's own tagged headers (Authorization, custom config
	// headers, and the `ai-sdk/xai/VERSION` User-Agent tag) instead of
	// rebuilding an untagged set, mirroring TS xai-transcription-model.ts's
	// doStream, which reuses `this.config.headers()`.
	headers := internalhttp.MergeHeaders(m.provider.client.Headers(), opts.Headers)

	expectedDoneCount := 1
	if xaiOpts.Multichannel != nil && *xaiOpts.Multichannel && xaiOpts.Channels != nil {
		expectedDoneCount = *xaiOpts.Channels
	}

	language := ""
	if xaiOpts.Language != nil {
		language = *xaiOpts.Language
	}

	currentDate := time.Now()

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newXAITranscriptionStream(abortCtx, xaiTranscriptionStreamConfig{
		url:               wsURL,
		headers:           headers,
		audio:             opts.Audio,
		warnings:          warnings,
		language:          language,
		expectedDoneCount: expectedDoneCount,
		includeRawChunks:  opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: wsURL.String(),
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: currentDate, ModelID: m.modelID},
	}, nil
}

type xaiTranscriptionStreamConfig struct {
	url               *url.URL
	headers           map[string]string
	audio             provider.AudioStream
	warnings          []types.Warning
	language          string
	expectedDoneCount int
	includeRawChunks  bool
}

// xaiTranscriptionStream implements provider.TranscriptionStream over xAI's
// `/stt` streaming WebSocket, mirroring TS
// createXaiStreamingTranscriptionStream.
type xaiTranscriptionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.TranscriptionStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newXAITranscriptionStream(parentCtx context.Context, cfg xaiTranscriptionStreamConfig) *xaiTranscriptionStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &xaiTranscriptionStream{ctx: ctx, cancel: cancel, parts: make(chan provider.TranscriptionStreamPart)}
	go s.run(cfg)
	return s
}

func (s *xaiTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
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

func (s *xaiTranscriptionStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *xaiTranscriptionStream) Close() error {
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

func (s *xaiTranscriptionStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *xaiTranscriptionStream) emit(part provider.TranscriptionStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

// receiveLoop continuously reads text frames from conn and forwards each one
// (or the terminal error) on out, until an error occurs or s.ctx is done.
func (s *xaiTranscriptionStream) receiveLoop(conn *websocket.Conn, out chan<- wsutil.Message) {
	wsutil.ReceiveLoop(s.ctx, conn, out)
}

// xaiStreamingTranscriptionEvent is a server->client streaming transcription
// event, mirroring TS XaiStreamingTranscriptionEvent.
type xaiStreamingTranscriptionEvent struct {
	Type         string   `json:"type,omitempty"`
	Text         string   `json:"text,omitempty"`
	IsFinal      bool     `json:"is_final,omitempty"`
	SpeechFinal  bool     `json:"speech_final,omitempty"`
	Start        *float64 `json:"start,omitempty"`
	Duration     *float64 `json:"duration,omitempty"`
	ChannelIndex *int     `json:"channel_index,omitempty"`
	Message      string   `json:"message,omitempty"`
}

// pumpAudio forwards raw audio chunks as binary WebSocket frames (xAI reads
// the socket payload directly, unlike Google's base64-in-JSON envelope), then
// sends an `audio.done` control message and signals audioEnded at EOF. A
// failed read or send is reported on sendErrCh so run()'s select loop can
// fail the stream, mirroring TS's `void sendAudio(socket).catch(finishWithError)`
// (TS's `await audioReader.read()` propagates a rejection out of `sendAudio`
// exactly like a failed `socket.send`, so both must fail the stream here too
// instead of ending it silently).
func (s *xaiTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, audioEnded chan<- struct{}, sendErrCh chan<- error) {
	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				_ = s.sendText(conn, `{"type":"audio.done"}`)
				select {
				case audioEnded <- struct{}{}:
				case <-s.ctx.Done():
				}
				return
			}
			select {
			case sendErrCh <- err:
			case <-s.ctx.Done():
			}
			return
		}
		if err := s.sendBinary(conn, chunk); err != nil {
			select {
			case sendErrCh <- err:
			case <-s.ctx.Done():
			}
			return
		}
	}
}

func xaiChannelID(channelIndex *int) string {
	if channelIndex == nil {
		return ""
	}
	return fmt.Sprintf("channel-%d", *channelIndex)
}

func (s *xaiTranscriptionStream) run(cfg xaiTranscriptionStreamConfig) {
	defer close(s.parts)
	// Release s.ctx's resources as soon as run() returns for any reason
	// instead of only on an explicit Close() call, which a consumer that
	// only drains Next() to io.EOF may never make.
	defer s.cancel()

	// cancelAudio guarantees cfg.audio.Cancel is invoked exactly once no
	// matter which path run() exits through, including a blocked emit()
	// unblocking on ctx cancellation (Close()) without going through fail()
	// or finish() below — otherwise an upstream audio producer piping into
	// cfg.audio could block forever, mirroring TS cleanup()'s unconditional
	// audio.cancel()/audioReader.cancel().
	var cancelAudioOnce sync.Once
	cancelAudio := func(err error) { cancelAudioOnce.Do(func() { cfg.audio.Cancel(err) }) }
	defer func() { cancelAudio(s.ctx.Err()) }()

	fail := func(err error) {
		s.setErr(err)
		cancelAudio(err)
	}

	conn, err := s.dial(cfg.url, cfg.headers)
	if err != nil {
		fail(err)
		return
	}
	s.connMu.Lock()
	s.conn = conn
	s.connMu.Unlock()
	defer conn.Close() //nolint:errcheck

	msgCh := make(chan wsutil.Message)
	go s.receiveLoop(conn, msgCh)

	audioEndedCh := make(chan struct{})
	sendErrCh := make(chan error, 1)

	// Transcript fragments are accumulated per channel: `transcript.done`'s
	// own `text` field is often empty, so the finished text is reconstructed
	// from finalized (speech_final) utterances plus any trailing unfinalized
	// text, mirroring TS's finalizedTexts/pendingTexts maps.
	var (
		finished       bool
		doneTexts      = map[int]string{}
		doneDuration   *float64
		finalizedTexts = map[int][]string{}
		pendingTexts   = map[int]string{}
	)

	finish := func() {
		if finished {
			return
		}
		finished = true
		keys := make([]int, 0, len(doneTexts))
		for k := range doneTexts {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		texts := make([]string, 0, len(keys))
		for _, k := range keys {
			texts = append(texts, doneTexts[k])
		}
		s.emit(provider.TranscriptionStreamPart{
			Type:              provider.TranscriptionStreamPartTypeFinish,
			FinishText:        strings.Join(texts, "\n"),
			Segments:          []provider.TranscriptSegment{},
			Language:          cfg.language,
			DurationInSeconds: doneDuration,
		})
		cancelAudio(nil)
	}

	for {
		select {
		case <-s.ctx.Done():
			fail(s.ctx.Err())
			return

		case <-audioEndedCh:
			audioEndedCh = nil // consumed once; a nil channel blocks forever in future selects

		case err := <-sendErrCh:
			if !finished {
				fail(err)
			}
			return

		case res, ok := <-msgCh:
			if !ok {
				return
			}
			if res.Err != nil {
				if finished {
					return
				}
				if wsutil.IsCleanClose(res.Err) {
					// A clean WebSocket close before every expected channel's
					// transcript.done arrived: mirror TS onClose, which ends
					// the stream without an error and without a synthetic
					// finish part.
					finished = true
					return
				}
				fail(errors.New("xAI streaming transcription error."))
				return
			}
			if finished {
				continue
			}

			var raw xaiStreamingTranscriptionEvent
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.Text), &rawValue)
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}

			switch raw.Type {
			case "transcript.created":
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
					return
				}
				go s.pumpAudio(conn, cfg.audio, audioEndedCh, sendErrCh)

			case "transcript.partial":
				channelIndex := 0
				if raw.ChannelIndex != nil {
					channelIndex = *raw.ChannelIndex
				}
				id := xaiChannelID(raw.ChannelIndex)
				// Only speech_final completes an utterance; is_final
				// fragments are re-sent/revised later, so they stay
				// partials.
				if raw.IsFinal && raw.SpeechFinal {
					if raw.Text != "" {
						finalizedTexts[channelIndex] = append(finalizedTexts[channelIndex], raw.Text)
					}
					delete(pendingTexts, channelIndex)
					part := provider.TranscriptionStreamPart{
						Type:         provider.TranscriptionStreamPartTypeFinal,
						ID:           id,
						Text:         raw.Text,
						ChannelIndex: raw.ChannelIndex,
					}
					if raw.Start != nil {
						start := *raw.Start
						part.StartSecond = &start
						if raw.Duration != nil {
							end := start + *raw.Duration
							part.EndSecond = &end
						}
					}
					if !s.emit(part) {
						return
					}
				} else {
					pendingTexts[channelIndex] = raw.Text
					part := provider.TranscriptionStreamPart{
						Type:              provider.TranscriptionStreamPartTypePartial,
						ID:                id,
						Text:              raw.Text,
						StartSecond:       raw.Start,
						DurationInSeconds: raw.Duration,
						ChannelIndex:      raw.ChannelIndex,
					}
					if !s.emit(part) {
						return
					}
				}

			case "transcript.done":
				channelIndex := 0
				if raw.ChannelIndex != nil {
					channelIndex = *raw.ChannelIndex
				}
				accumulated := append([]string(nil), finalizedTexts[channelIndex]...)
				if pending, ok := pendingTexts[channelIndex]; ok && pending != "" {
					accumulated = append(accumulated, pending)
				}
				text := raw.Text
				if text == "" {
					text = strings.Join(accumulated, " ")
				}
				doneTexts[channelIndex] = text
				if raw.Duration != nil {
					doneDuration = raw.Duration
				}
				if len(doneTexts) >= cfg.expectedDoneCount {
					finish()
					return
				}

			case "error":
				// xAI STT errors are terminal: surface the server message
				// instead of letting the socket close mask it.
				msg := raw.Message
				if msg == "" {
					msg = "xAI STT error"
				}
				fail(errors.New(msg))
				return
			}
		}
	}
}

func (s *xaiTranscriptionStream) dial(wsURL *url.URL, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.ctx, wsURL.String(), wsutil.DialOptions{Headers: headers})
}

func (s *xaiTranscriptionStream) sendText(conn *websocket.Conn, message string) error {
	return s.send(conn, message)
}

func (s *xaiTranscriptionStream) sendBinary(conn *websocket.Conn, data []byte) error {
	return s.send(conn, data)
}

// send writes v as a text frame (string) or binary frame ([]byte) to conn,
// mirroring golang.org/x/net/websocket.Message's type-based framing so raw
// audio chunks are sent unwrapped, exactly like TS's `socket.send(value)`.
func (s *xaiTranscriptionStream) send(conn *websocket.Conn, v interface{}) error {
	return wsutil.Send(s.ctx, conn, v)
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*xaiTranscriptionStream)(nil)
)
