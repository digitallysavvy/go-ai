package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

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
// WebSocket, mirroring TS createOpenAIRealtimeSpeechTranslationStream. The
// Next/Err/Close/emit/setErr plumbing is the shared wsutil.Session core;
// only the protocol-specific run()/pumpAudio()/dial()/send() below are
// local to OpenAI.
type openAIRealtimeSpeechTranslationStream struct {
	*wsutil.Session[provider.SpeechTranslationStreamPart]
}

func newOpenAIRealtimeSpeechTranslationStream(parentCtx context.Context, cfg openAIRealtimeSpeechTranslationStreamConfig) *openAIRealtimeSpeechTranslationStream {
	s := &openAIRealtimeSpeechTranslationStream{Session: wsutil.NewSession[provider.SpeechTranslationStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

func (s *openAIRealtimeSpeechTranslationStream) run(cfg openAIRealtimeSpeechTranslationStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason instead of only on an explicit Close() call, which a consumer
	// that only drains Next() to io.EOF may never make.
	defer s.CancelContext()

	conn, err := s.dial(cfg.url, cfg.headers)
	if err != nil {
		s.SetErr(err)
		cfg.audio.Cancel(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck

	// Send the session update and start pumping audio before emitting
	// stream-start: setup must not wait for a consumer to be reading yet
	// (mirrors TS onOpen, where controller.enqueue does not block on a
	// reader before the socket send and sendAudio() call that follow it).
	payload, err := json.Marshal(cfg.sessionUpdate)
	if err != nil {
		s.SetErr(err)
		cfg.audio.Cancel(err)
		return
	}
	if err := s.send(conn, payload); err != nil {
		s.SetErr(err)
		cfg.audio.Cancel(err)
		return
	}

	audioErrCh := make(chan error, 1)
	go s.pumpAudio(conn, cfg.audio, audioErrCh)

	if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	var sourceText, translationText string

	// finish emits the accumulated final transcripts (if any) followed by
	// the finish part, mirroring TS finish().
	finish := func() {
		if sourceText != "" {
			if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal, Text: sourceText}) {
				return
			}
		}
		if translationText != "" {
			if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextFinal, Text: translationText}) {
				return
			}
		}
		s.Emit(provider.SpeechTranslationStreamPart{
			Type:       provider.SpeechTranslationStreamPartTypeFinish,
			SourceText: sourceText,
			OutputText: translationText,
		})
	}

	for {
		select {
		case <-s.Context().Done():
			cause := context.Cause(s.Context())
			s.SetErr(cause)
			cfg.audio.Cancel(cause)
			return

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`.
			s.SetErr(audioErr)
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
				s.SetErr(failErr)
				cfg.audio.Cancel(failErr)
				return
			}

			var raw map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
				continue
			}
			if cfg.includeRawChunks {
				if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeRaw, RawValue: raw}) {
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
						if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, AudioData: audioBytes}) {
							return
						}
					}
				}

			case "session.output_transcript.delta":
				delta, _ := raw["delta"].(string)
				translationText += delta
				if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, Delta: delta}) {
					return
				}

			case "session.input_transcript.delta":
				delta, _ := raw["delta"].(string)
				sourceText += delta
				if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta, Delta: delta}) {
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
				if !s.Emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeError, Err: errors.New(message)}) {
					return
				}
			}
		}
	}
}

// pumpAudio reads chunks from audio and forwards them as
// session.input_audio_buffer.append messages, sending session.close at EOF.
// Any other failure — reading from the AudioStream, or writing to the
// WebSocket — is reported on errCh via the shared wsutil.PumpAudio loop,
// mirroring TS's `void sendAudio(socket).catch(finishWithError)`.
func (s *openAIRealtimeSpeechTranslationStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	wsutil.PumpAudio(s.Context(), audio,
		func(chunk []byte) error {
			msg, marshalErr := json.Marshal(map[string]string{
				"type":  "session.input_audio_buffer.append",
				"audio": base64.StdEncoding.EncodeToString(chunk),
			})
			if marshalErr != nil {
				return nil
			}
			return s.send(conn, msg)
		},
		func() error {
			return s.send(conn, []byte(`{"type":"session.close"}`))
		},
		errCh,
	)
}

func (s *openAIRealtimeSpeechTranslationStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	protocols, filteredHeaders := openAIRealtimeWSAuth(headers)
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: filteredHeaders, Protocols: protocols})
}

func (s *openAIRealtimeSpeechTranslationStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.SpeechTranslationModel  = (*SpeechTranslationModel)(nil)
	_ provider.SpeechTranslationStream = (*openAIRealtimeSpeechTranslationStream)(nil)
)
