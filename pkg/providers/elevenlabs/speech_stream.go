package elevenlabs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

const (
	// maxHeldCodePoints bounds buffering for scripts without spaces (for
	// example CJK) or very long words (TS MAX_HELD_CODE_POINTS).
	maxHeldCodePoints = 100

	// maxSpeechServerFrameBytes bounds one server message the client will
	// buffer. Audio chunks are small; this only limits a misbehaving server.
	maxSpeechServerFrameBytes = 8 << 20

	// defaultSpeechKeepaliveInterval is the send inactivity after which a
	// keepalive is sent, ahead of the servers' default 20 s inactivity
	// timeout (TS KEEPALIVE_INTERVAL_MS).
	defaultSpeechKeepaliveInterval = 15 * time.Second
)

// speechKeepaliveInterval is a variable so tests can shorten it.
var speechKeepaliveInterval = defaultSpeechKeepaliveInterval

// countCodePoints counts Unicode code points, the unit ElevenLabs bills
// text-to-speech in.
func countCodePoints(text string) int { return utf8.RuneCountInString(text) }

// isWordBoundary reports whether r ends a word for the WebSockets, which
// expect text messages to end at whitespace. CJK sentence punctuation also
// counts, since those scripts have no spaces. Whitespace follows the JS \s
// class (not Go's unicode.IsSpace: JS excludes U+0085 and includes U+FEFF).
func isWordBoundary(r rune) bool {
	switch r {
	case '。', '、', '！', '？', '，', '；', '：':
		return true
	case 0x85:
		return false
	case 0xFEFF:
		return true
	}
	return unicode.IsSpace(r)
}

// takeCompleteWords splits buffered text at the last word boundary. It
// returns the text to send now (possibly empty) and the text to keep
// buffering.
func takeCompleteWords(buffered string) (send, hold string) {
	runes := []rune(buffered)
	for i := len(runes) - 1; i >= 0; i-- {
		if isWordBoundary(runes[i]) {
			return string(runes[:i+1]), string(runes[i+1:])
		}
	}
	if len(runes) >= maxHeldCodePoints {
		return buffered, ""
	}
	return "", buffered
}

// textSegment is text to send, with the speaker it belongs to.
type textSegment struct {
	text string
	// voice is the resolved voice (see streamProtocol.resolveVoice).
	voice string
	// newTurn forces a new dialogue turn even when the voice is unchanged.
	newTurn bool
}

// getNewTurn reads providerOptions.elevenlabs.newTurn from a text part.
func getNewTurn(part *provider.SpeechStreamTextPart) (bool, error) {
	opts, _ := part.ProviderOptions["elevenlabs"].(map[string]interface{})
	v, present := opts["newTurn"]
	if !present || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, &providererrors.InvalidArgumentError{
			Field:   "providerOptions.elevenlabs.newTurn",
			Message: "providerOptions.elevenlabs.newTurn must be a boolean.",
		}
	}
	return b, nil
}

// getElevenLabsAudioFormat maps an ElevenLabs output_format (for example
// mp3_44100_128, pcm_24000) to the format of the audio chunks it produces.
func getElevenLabsAudioFormat(outputFormat string) provider.AudioFormat {
	parts := strings.Split(outputFormat, "_")
	codec := parts[0]
	withRate := func(mediaType string) provider.AudioFormat {
		format := provider.AudioFormat{Type: mediaType}
		if len(parts) > 1 {
			// JS Number(): an empty string is 0, a non-number is NaN.
			if parts[1] == "" {
				zero := 0
				format.Rate = &zero
			} else if f, err := strconv.ParseFloat(parts[1], 64); err == nil && f == float64(int(f)) {
				rate := int(f)
				format.Rate = &rate
			}
		}
		return format
	}
	switch codec {
	case "mp3":
		return withRate("audio/mpeg")
	case "pcm":
		return withRate("audio/pcm")
	case "ulaw":
		return withRate("audio/pcmu")
	case "alaw":
		return withRate("audio/pcma")
	case "opus": // ElevenLabs Opus audio arrives in an Ogg container.
		return withRate("audio/ogg")
	case "wav":
		return withRate("audio/wav")
	default:
		return provider.AudioFormat{Type: "application/octet-stream"}
	}
}

// streamEvent is one decoded server message, normalized across the
// WebSockets.
type streamEvent struct {
	audio            string
	err              string
	hasErr           bool
	isFinal          bool
	providerMetadata map[string]interface{}
}

// encodedText is the messages for a text segment and the characters they
// carry.
type encodedText struct {
	messages   []map[string]interface{}
	characters int
}

// streamProtocol is the wire protocol of one ElevenLabs streaming TTS
// WebSocket. resolveVoice applies the default voice to a text part and
// fails for a voice the socket cannot speak.
type streamProtocol interface {
	initMessage() map[string]interface{}
	// authFields are the init-message fields that authenticate the socket
	// with the API key.
	authFields(apiKey string) map[string]interface{}
	// keepaliveMessage resets the server's inactivity timeout without
	// generating audio.
	keepaliveMessage() map[string]interface{}
	resolveVoice(voice string) (string, error)
	encodeText(segment textSegment) encodedText
	endMessages() encodedText
	decodeEvent(raw map[string]interface{}) streamEvent
}

type speechStreamConfig struct {
	url              *url.URL
	headers          map[string]string
	apiKey           string
	audioFormat      provider.AudioFormat
	includeRawChunks bool
	// keepalive sends keepalives while the text stream is idle.
	keepalive         bool
	keepaliveInterval time.Duration
	protocol          streamProtocol
	text              provider.SpeechTextStream
	warnings          []types.Warning
}

// elevenLabsSpeechStream implements provider.SpeechStream over an
// ElevenLabs streaming speech WebSocket: an init message carrying the API
// key, text parts ending at word boundaries (held back per speaker),
// keepalives while idle, end-of-input messages, then audio until the final
// event. Mirrors TS createElevenLabsSpeechStream.
type elevenLabsSpeechStream struct {
	*wsutil.Session[provider.SpeechStreamPart]

	textExhausted   atomic.Bool
	inputEnded      atomic.Bool
	inputCharacters atomic.Int64

	// writes serializes socket writes (the text pump and the keepalive timer
	// both send) and owns the keepalive timer.
	writes    sync.Mutex
	keepAlive *time.Timer
}

func newElevenLabsSpeechStream(parentCtx context.Context, cfg speechStreamConfig) *elevenLabsSpeechStream {
	s := &elevenLabsSpeechStream{Session: wsutil.NewSession[provider.SpeechStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// send writes one JSON message and restarts the idle clock: a slow text
// producer (for example a reasoning model) would otherwise hit the server's
// inactivity timeout. No keepalive follows the end of input.
func (s *elevenLabsSpeechStream) send(conn *websocket.Conn, cfg speechStreamConfig, errCh chan<- error, message map[string]interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	s.writes.Lock()
	defer s.writes.Unlock()
	if err := wsutil.Send(s.Context(), conn, string(data)); err != nil {
		return err
	}
	if s.keepAlive != nil {
		s.keepAlive.Stop()
		s.keepAlive = nil
	}
	if !cfg.keepalive || s.inputEnded.Load() || s.Context().Err() != nil {
		return nil
	}
	s.keepAlive = time.AfterFunc(cfg.keepaliveInterval, func() {
		if s.inputEnded.Load() || s.Context().Err() != nil {
			return
		}
		if err := s.send(conn, cfg, errCh, cfg.protocol.keepaliveMessage()); err != nil && s.Context().Err() == nil {
			wsutil.ReportError(s.Context(), errCh, err)
		}
	})
	return nil
}

func (s *elevenLabsSpeechStream) stopKeepalive() {
	s.writes.Lock()
	if s.keepAlive != nil {
		s.keepAlive.Stop()
		s.keepAlive = nil
	}
	s.writes.Unlock()
}

func (s *elevenLabsSpeechStream) run(cfg speechStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason, not only on an explicit Close().
	defer s.CancelContext()
	defer s.stopKeepalive()

	var cancelOnce sync.Once
	cancelText := func(reason error) { cancelOnce.Do(func() { cfg.text.Cancel(reason) }) }
	// The provider owns the text stream and releases it on every terminal
	// path other than a fully-read normal finish.
	defer func() {
		if !s.textExhausted.Load() {
			cancelText(nil)
		}
	}()
	fail := func(err error) {
		s.SetErr(err)
		cancelText(err)
	}

	closeInfo := &wsutil.CloseInfo{}
	conn, err := wsutil.Dial(s.Context(), cfg.url.String(), wsutil.DialOptions{Headers: cfg.headers, WrapConn: closeInfo.Wrap})
	if err != nil {
		fail(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck
	conn.MaxPayloadBytes = maxSpeechServerFrameBytes

	errCh := make(chan error, 1)

	// The API key goes in the init message (not a header) so native
	// WebSockets work.
	initMsg := map[string]interface{}{}
	for k, v := range cfg.protocol.initMessage() {
		initMsg[k] = v
	}
	if cfg.apiKey != "" {
		for k, v := range cfg.protocol.authFields(cfg.apiKey) {
			initMsg[k] = v
		}
	}
	if err := s.send(conn, cfg, errCh, initMsg); err != nil {
		fail(err)
		return
	}
	go s.pumpText(conn, cfg, errCh)

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	if !s.Emit(provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		fail(context.Cause(s.Context()))
		return
	}

	for {
		select {
		case <-s.Context().Done():
			fail(context.Cause(s.Context()))
			return

		case err := <-errCh:
			fail(err)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if wsutil.IsCleanClose(res.Err) {
					// TS: "... closed before completion (code N: reason)."
					fail(errors.New("ElevenLabs speech stream closed before completion" + closeInfo.Suffix() + ".")) //nolint:staticcheck // matches TS SDK's exact error text
				} else {
					fail(errors.New("ElevenLabs speech stream error.")) //nolint:staticcheck // matches TS SDK's exact error text
				}
				return
			}
			done, err := s.handleMessage(cfg, res.Text)
			if err != nil {
				fail(err)
				return
			}
			if done {
				return
			}
		}
	}
}

// handleMessage processes one server message. It reports done after emitting
// the finish part.
func (s *elevenLabsSpeechStream) handleMessage(cfg speechStreamConfig, text string) (done bool, err error) {
	var raw map[string]interface{}
	if json.Unmarshal([]byte(text), &raw) != nil || raw == nil {
		return false, nil // not a JSON object: ignored
	}

	if cfg.includeRawChunks {
		if !s.Emit(provider.SpeechStreamPart{Type: provider.SpeechStreamPartTypeRaw, RawValue: raw}) {
			return false, context.Cause(s.Context())
		}
	}

	event := cfg.protocol.decodeEvent(raw)
	if event.hasErr {
		return false, fmt.Errorf("ElevenLabs speech stream error: %s", orderedErrorText(raw, text, event.err))
	}

	// Turn boundaries arrive as standalone frames, so they are emitted as
	// metadata-only (empty) audio parts.
	isTurnEnd := false
	if em, ok := event.providerMetadata["isFinalAudioForTurn"].(bool); ok && em {
		isTurnEnd = true
	}
	if event.audio != "" || isTurnEnd {
		var audio []byte
		if event.audio != "" {
			decoded, decodeErr := types.DecodeFileDataString(event.audio)
			if decodeErr != nil {
				return false, fmt.Errorf("ElevenLabs speech stream sent invalid audio: %w", decodeErr)
			}
			audio = decoded
		}
		part := provider.SpeechStreamPart{
			Type:        provider.SpeechStreamPartTypeAudio,
			Audio:       audio,
			AudioFormat: cfg.audioFormat,
		}
		if event.providerMetadata != nil {
			part.ProviderMetadata = map[string]interface{}{"elevenlabs": event.providerMetadata}
		}
		if !s.Emit(part) {
			return false, context.Cause(s.Context())
		}
	}

	if event.isFinal {
		characters := int(s.inputCharacters.Load())
		if !s.Emit(provider.SpeechStreamPart{
			Type:  provider.SpeechStreamPartTypeFinish,
			Usage: &provider.SpeechStreamUsage{InputCharacters: &characters},
		}) {
			return false, context.Cause(s.Context())
		}
		return true, nil
	}
	return false, nil
}

// orderedErrorText is the error text with a non-string `error` value written
// like JSON.stringify of the received object: keys in wire order, no HTML
// escaping (raw is a Go map, which sorts keys).
func orderedErrorText(raw map[string]interface{}, text, fallback string) string {
	if _, ok := raw["message"].(string); ok {
		return fallback
	}
	if _, ok := raw["error"].(string); ok || raw["error"] == nil {
		return fallback
	}
	tree, ok := jsonorder.Parse([]byte(text))
	if !ok {
		return fallback
	}
	if v, ok := jsonorder.Get(tree, "error"); ok {
		return jsonorder.Stringify(v)
	}
	return fallback
}

// pumpText reads the text stream, sends complete words per segment, and ends
// the input. Failures are reported on errCh unless they stem from the
// session's context being cancelled, which run() already handles.
func (s *elevenLabsSpeechStream) pumpText(conn *websocket.Conn, cfg speechStreamConfig, errCh chan<- error) {
	ctx := s.Context()
	report := func(err error) {
		if err != nil && ctx.Err() == nil {
			wsutil.ReportError(ctx, errCh, err)
		}
	}
	sendAll := func(encoded encodedText) error {
		s.inputCharacters.Add(int64(encoded.characters))
		for _, message := range encoded.messages {
			if err := s.send(conn, cfg, errCh, message); err != nil {
				return err
			}
		}
		return nil
	}

	// Text is held back per segment (speaker and turn): a part with another
	// voice or a new turn first flushes the held text of the previous one. An
	// empty text message ends the input, so never send one here.
	var held *textSegment
	flushHeld := func() error {
		if held != nil && len(held.text) > 0 {
			if err := sendAll(cfg.protocol.encodeText(*held)); err != nil {
				return err
			}
		}
		held = nil
		return nil
	}

	for {
		part, err := cfg.text.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			report(err)
			return
		}
		voice, err := cfg.protocol.resolveVoice(part.Voice)
		if err != nil {
			report(err)
			return
		}
		newTurn, err := getNewTurn(part)
		if err != nil {
			report(err)
			return
		}
		if held != nil && (held.voice != voice || newTurn) {
			if err := flushHeld(); err != nil {
				report(err)
				return
			}
		}
		segment := held
		if segment == nil {
			segment = &textSegment{voice: voice, newTurn: newTurn}
		}
		complete, hold := takeCompleteWords(segment.text + part.Text)
		if len(complete) > 0 {
			sent := *segment
			sent.text = complete
			if err := sendAll(cfg.protocol.encodeText(sent)); err != nil {
				report(err)
				return
			}
			// The rest of the segment continues the same turn.
			segment.newTurn = false
		}
		segment.text = hold
		held = segment
	}
	s.textExhausted.Store(true)

	if err := flushHeld(); err != nil {
		report(err)
		return
	}
	s.inputEnded.Store(true)
	s.stopKeepalive()
	report(sendAll(cfg.protocol.endMessages()))
}

var _ provider.SpeechStream = (*elevenLabsSpeechStream)(nil)
