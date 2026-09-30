package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// openAIRealtimeSpeechTranslationStreamConfig bundles the inputs to
// newOpenAIRealtimeSpeechTranslationStream.
type openAIRealtimeSpeechTranslationStreamConfig struct {
	url              string
	headers          map[string]string
	sessionUpdate    map[string]interface{}
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// openAIRealtimeSpeechTranslationStream implements
// provider.SpeechTranslationStream over the OpenAI realtime translations
// WebSocket, mirroring TS createOpenAIRealtimeSpeechTranslationStream.
type openAIRealtimeSpeechTranslationStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.SpeechTranslationStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newOpenAIRealtimeSpeechTranslationStream(parentCtx context.Context, cfg openAIRealtimeSpeechTranslationStreamConfig) *openAIRealtimeSpeechTranslationStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &openAIRealtimeSpeechTranslationStream{ctx: ctx, cancel: cancel, parts: make(chan provider.SpeechTranslationStreamPart)}
	go s.run(cfg)
	return s
}

func (s *openAIRealtimeSpeechTranslationStream) Next() (*provider.SpeechTranslationStreamPart, error) {
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

func (s *openAIRealtimeSpeechTranslationStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *openAIRealtimeSpeechTranslationStream) Close() error {
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

func (s *openAIRealtimeSpeechTranslationStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *openAIRealtimeSpeechTranslationStream) emit(part provider.SpeechTranslationStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *openAIRealtimeSpeechTranslationStream) run(cfg openAIRealtimeSpeechTranslationStreamConfig) {
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

	if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.ctx, conn, msgCh)

	var sourceText, translationText string

	// finish emits the accumulated final transcripts (if any) followed by
	// the finish part, mirroring TS finish().
	finish := func() {
		if sourceText != "" {
			if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal, Text: sourceText}) {
				return
			}
		}
		if translationText != "" {
			if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextFinal, Text: translationText}) {
				return
			}
		}
		s.emit(provider.SpeechTranslationStreamPart{
			Type:       provider.SpeechTranslationStreamPartTypeFinish,
			SourceText: sourceText,
			OutputText: translationText,
		})
	}

	for {
		select {
		case <-s.ctx.Done():
			cause := context.Cause(s.ctx)
			s.setErr(cause)
			cfg.audio.Cancel(cause)
			return

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`.
			s.setErr(audioErr)
			cfg.audio.Cancel(audioErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				// Any close reaching this select happens before a
				// session.closed event was ever processed (that branch
				// returns immediately below), so it is always an
				// unexpected termination — mirroring TS onClose. A clean
				// close and an abnormal disconnection (TS onSocketError)
				// both fail the stream; x/net/websocket does not expose
				// close codes/reasons to thread through the message like
				// TS does.
				var failErr error
				if wsutil.IsCleanClose(res.Err) {
					failErr = errors.New("OpenAI realtime translation WebSocket closed unexpectedly before finishing.")
				} else {
					failErr = errors.New("OpenAI realtime translation error")
				}
				s.setErr(failErr)
				cfg.audio.Cancel(failErr)
				return
			}

			var raw map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
				continue
			}
			if cfg.includeRawChunks {
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeRaw, RawValue: raw}) {
					return
				}
			}

			eventType, _ := raw["type"].(string)
			switch eventType {
			case "session.output_audio.delta":
				delta, _ := raw["delta"].(string)
				// skip empty deltas: an empty `audio` part carries no data
				if delta != "" {
					if audioBytes, decodeErr := base64.StdEncoding.DecodeString(delta); decodeErr == nil {
						if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: audioBytes}) {
							return
						}
					}
				}

			case "session.output_transcript.delta":
				delta, _ := raw["delta"].(string)
				translationText += delta
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, Delta: delta}) {
					return
				}

			case "session.input_transcript.delta":
				delta, _ := raw["delta"].(string)
				sourceText += delta
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta, Delta: delta}) {
					return
				}

			case "session.closed":
				finish()
				return

			case "error":
				message := "OpenAI realtime error"
				if errObj, ok := raw["error"].(map[string]interface{}); ok {
					if m, ok := errObj["message"].(string); ok && m != "" {
						message = m
					}
				}
				// Recoverable server errors are streamed as `error` parts
				// and do not terminate the stream, mirroring TS: only
				// `onSocketError`/`onClose`/a rejected `sendAudio` end it.
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeError, Err: errors.New(message)}) {
					return
				}
			}
		}
	}
}

// pumpAudio reads chunks from audio and forwards them as
// session.input_audio_buffer.append messages, sending session.close at EOF.
// Any other failure — reading from the AudioStream, or writing to the
// WebSocket — is reported on errCh, mirroring TS's `void
// sendAudio(socket).catch(finishWithError)`.
func (s *openAIRealtimeSpeechTranslationStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				if sendErr := s.send(conn, []byte(`{"type":"session.close"}`)); sendErr != nil {
					if s.ctx.Err() == nil {
						s.reportAudioError(errCh, sendErr)
					}
				}
			} else if s.ctx.Err() == nil {
				s.reportAudioError(errCh, err)
			}
			return
		}
		msg, marshalErr := json.Marshal(map[string]string{
			"type":  "session.input_audio_buffer.append",
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
func (s *openAIRealtimeSpeechTranslationStream) reportAudioError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-s.ctx.Done():
	}
}

func (s *openAIRealtimeSpeechTranslationStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	protocols, filteredHeaders := openAIRealtimeWSAuth(headers)
	return wsutil.Dial(s.ctx, wsURL, wsutil.DialOptions{Headers: filteredHeaders, Protocols: protocols})
}

func (s *openAIRealtimeSpeechTranslationStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.ctx, conn, string(message))
}

var (
	_ provider.SpeechTranslationModel  = (*SpeechTranslationModel)(nil)
	_ provider.SpeechTranslationStream = (*openAIRealtimeSpeechTranslationStream)(nil)
)
