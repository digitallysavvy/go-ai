package google

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// googleLiveSpeechTranslationStreamConfig bundles the inputs to
// newGoogleLiveSpeechTranslationStream.
type googleLiveSpeechTranslationStreamConfig struct {
	url              string
	headers          map[string]string
	setup            map[string]interface{}
	inputAudioRate   int
	finishGraceMs    time.Duration
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// googleLiveSpeechTranslationStream implements provider.SpeechTranslationStream
// over the Gemini Live API WebSocket, mirroring TS
// createGoogleLiveSpeechTranslationStream.
type googleLiveSpeechTranslationStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.SpeechTranslationStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newGoogleLiveSpeechTranslationStream(parentCtx context.Context, cfg googleLiveSpeechTranslationStreamConfig) *googleLiveSpeechTranslationStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &googleLiveSpeechTranslationStream{ctx: ctx, cancel: cancel, parts: make(chan provider.SpeechTranslationStreamPart)}
	go s.run(cfg)
	return s
}

func (s *googleLiveSpeechTranslationStream) Next() (*provider.SpeechTranslationStreamPart, error) {
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

func (s *googleLiveSpeechTranslationStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *googleLiveSpeechTranslationStream) Close() error {
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

func (s *googleLiveSpeechTranslationStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *googleLiveSpeechTranslationStream) emit(part provider.SpeechTranslationStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *googleLiveSpeechTranslationStream) receiveLoop(conn *websocket.Conn, out chan<- wsutil.Message) {
	wsutil.ReceiveLoop(s.ctx, conn, out)
}

// pumpAudio waits for the server's setupComplete acknowledgement (the Live
// API contract requires this before sending realtime input), then forwards
// audio chunks as realtimeInput.audio messages, committing
// realtimeInput.audioStreamEnd at EOF and signalling audioEnded. Mirrors TS
// sendAudio.
func (s *googleLiveSpeechTranslationStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, setupComplete <-chan struct{}, rate int, audioEnded chan<- struct{}, errCh chan<- error) {
	select {
	case <-setupComplete:
	case <-s.ctx.Done():
		return
	}

	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				if sendErr := s.send(conn, []byte(`{"realtimeInput":{"audioStreamEnd":true}}`)); sendErr != nil {
					if s.ctx.Err() == nil {
						s.reportAudioError(errCh, sendErr)
					}
					return
				}
				select {
				case audioEnded <- struct{}{}:
				case <-s.ctx.Done():
				}
			} else if s.ctx.Err() == nil {
				s.reportAudioError(errCh, err)
			}
			return
		}
		msg, marshalErr := json.Marshal(map[string]interface{}{
			"realtimeInput": map[string]interface{}{
				"audio": map[string]interface{}{
					"data":     base64.StdEncoding.EncodeToString(chunk),
					"mimeType": fmt.Sprintf("audio/pcm;rate=%d", rate),
				},
			},
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
func (s *googleLiveSpeechTranslationStream) reportAudioError(errCh chan<- error, err error) {
	select {
	case errCh <- err:
	case <-s.ctx.Done():
	}
}

// googleLiveSpeechTranslationServerMessage is the subset of Google Live API
// server messages relevant to streaming speech translation, mirroring TS
// GoogleLiveServerMessage in google-speech-translation-model.ts.
type googleLiveSpeechTranslationServerMessage struct {
	SetupComplete json.RawMessage `json:"setupComplete,omitempty"`
	ServerContent *struct {
		ModelTurn *struct {
			Parts []struct {
				InlineData *struct {
					Data string `json:"data,omitempty"`
				} `json:"inlineData,omitempty"`
			} `json:"parts,omitempty"`
		} `json:"modelTurn,omitempty"`
		OutputTranscription *googleLiveTranscription `json:"outputTranscription,omitempty"`
		InputTranscription  *googleLiveTranscription `json:"inputTranscription,omitempty"`
		TurnComplete        bool                     `json:"turnComplete,omitempty"`
	} `json:"serverContent,omitempty"`
	InputTranscription *googleLiveTranscription             `json:"inputTranscription,omitempty"`
	UsageMetadata      *googleLiveTranslationUsageMetadata  `json:"usageMetadata,omitempty"`
	Error              *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type googleLiveTranslationTokenDetail struct {
	Modality   string `json:"modality,omitempty"`
	TokenCount *int   `json:"tokenCount,omitempty"`
}

type googleLiveTranslationUsageMetadata struct {
	PromptTokensDetails   []googleLiveTranslationTokenDetail `json:"promptTokensDetails,omitempty"`
	ResponseTokensDetails []googleLiveTranslationTokenDetail `json:"responseTokensDetails,omitempty"`
}

// accumulateGoogleLiveTranslationUsage folds a usageMetadata delta into
// usage, mirroring TS accumulateGoogleLiveUsage: Live Translation emits
// periodic usage deltas, and only the billable AUDIO modality (input and
// output) is aggregated — the TEXT prompt detail is internal translation
// context, not part of the public audio-only input.
func accumulateGoogleLiveTranslationUsage(usage *provider.SpeechTranslationUsage, meta *googleLiveTranslationUsageMetadata) *provider.SpeechTranslationUsage {
	if meta == nil {
		return usage
	}
	var inputAudioTokens, outputAudioTokens *int
	if usage != nil {
		inputAudioTokens = usage.InputAudioTokens
		outputAudioTokens = usage.OutputAudioTokens
	}
	for _, d := range meta.PromptTokensDetails {
		if d.Modality == "AUDIO" && d.TokenCount != nil {
			v := intOrZero(inputAudioTokens) + *d.TokenCount
			inputAudioTokens = &v
		}
	}
	for _, d := range meta.ResponseTokensDetails {
		if d.Modality == "AUDIO" && d.TokenCount != nil {
			v := intOrZero(outputAudioTokens) + *d.TokenCount
			outputAudioTokens = &v
		}
	}
	if inputAudioTokens == nil && outputAudioTokens == nil {
		return usage
	}
	out := &provider.SpeechTranslationUsage{}
	if usage != nil {
		*out = *usage
	}
	out.InputAudioTokens = inputAudioTokens
	out.OutputAudioTokens = outputAudioTokens
	return out
}

func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// pcm16SilenceDurationMs decodes base64Audio as little-endian PCM16 samples
// and, when every sample's absolute amplitude is at or below
// pcm16SilenceAmplitudeThreshold, returns the chunk's duration in
// milliseconds at googleLiveOutputAudioRate. Mirrors TS
// getPcm16SilenceDurationMs.
func pcm16SilenceDurationMs(base64Audio string) (float64, bool) {
	raw, err := base64.StdEncoding.DecodeString(base64Audio)
	if err != nil {
		return 0, false
	}
	if len(raw) < 2 {
		return 0, false
	}
	sampleCount := len(raw) / 2
	for i := 0; i < sampleCount; i++ {
		sample := int16(binary.LittleEndian.Uint16(raw[i*2 : i*2+2]))
		if abs16(sample) > pcm16SilenceAmplitudeThreshold {
			return 0, false
		}
	}
	return float64(sampleCount) / float64(googleLiveOutputAudioRate) * 1000, true
}

func abs16(v int16) int16 {
	if v < 0 {
		return -v
	}
	return v
}

// run drives the Live API WebSocket session: dial, send the setup message,
// pump audio, and translate server messages into
// provider.SpeechTranslationStreamPart values, mirroring TS
// createGoogleLiveSpeechTranslationStream's ReadableStream `start`.
func (s *googleLiveSpeechTranslationStream) run(cfg googleLiveSpeechTranslationStreamConfig) {
	defer close(s.parts)
	// Release s.ctx's resources as soon as run() returns for any reason
	// instead of only on an explicit Close() call, which a consumer that
	// only drains Next() to io.EOF may never make.
	defer s.cancel()

	var finishTimer *time.Timer
	var finishTimerC <-chan time.Time
	cancelPendingFinish := func() {
		if finishTimer != nil {
			finishTimer.Stop()
			finishTimer = nil
			finishTimerC = nil
		}
	}

	// fail stops any pending finish-grace timer (mirroring TS cleanup(),
	// which finishWithError also calls) before recording the error and
	// cancelling the caller's AudioStream.
	fail := func(err error) {
		cancelPendingFinish()
		s.setErr(err)
		cfg.audio.Cancel(err)
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

	payload, err := json.Marshal(map[string]interface{}{"setup": cfg.setup})
	if err != nil {
		fail(err)
		return
	}
	// Setup is sent (and stream-start emitted) before waiting on a consumer
	// to start reading, mirroring TS onOpen where controller.enqueue does not
	// block on a reader before the socket send that follows it.
	if err := s.send(conn, payload); err != nil {
		fail(err)
		return
	}

	setupCompleteCh := make(chan struct{})
	var setupCompleteOnce sync.Once
	resolveSetupComplete := func() { setupCompleteOnce.Do(func() { close(setupCompleteCh) }) }

	audioEndedCh := make(chan struct{})
	audioErrCh := make(chan error, 1)
	go s.pumpAudio(conn, cfg.audio, setupCompleteCh, cfg.inputAudioRate, audioEndedCh, audioErrCh)

	if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go s.receiveLoop(conn, msgCh)

	// Google Live messages carry no response/item IDs; a turn counter
	// generates consistent synthetic IDs (like the realtime event mapper).
	// Transcription fragments arrive incrementally and are accumulated per
	// turn; `turnComplete` finalizes the current turn.
	var (
		finished              bool
		turnCounter           int
		sourceText            string
		sourceTurnBuffer      string
		translationText       string
		translationTurnBuffer string
		audioEnded            bool
		usage                 *provider.SpeechTranslationUsage

		// Live Translation is a continuous pipeline rather than a turn-based
		// model. After audioStreamEnd it keeps sending PCM silence
		// indefinitely and does not emit turnComplete. Drain translated
		// speech, then finish after enough trailing silence. Keep
		// turnComplete handling as a fallback for compatible server
		// implementations and test doubles.
		openTurn          bool
		sawTurnComplete   bool
		trailingSilenceMs float64
	)

	itemID := func() string { return fmt.Sprintf("google-item-%d", turnCounter) }

	onTurnActivity := func() {
		openTurn = true
		trailingSilenceMs = 0
		cancelPendingFinish()
	}

	completeTurn := func() bool {
		ok := true
		if sourceTurnBuffer != "" {
			if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptFinal, ID: itemID(), Text: sourceTurnBuffer}) {
				ok = false
			}
			sourceText += sourceTurnBuffer
			sourceTurnBuffer = ""
		}
		if translationTurnBuffer != "" {
			if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextFinal, ID: itemID(), Text: translationTurnBuffer}) {
				ok = false
			}
			translationText += translationTurnBuffer
			translationTurnBuffer = ""
		}
		turnCounter++
		return ok
	}

	schedulePendingFinish := func() {
		if finished || finishTimer != nil {
			return
		}
		finishTimer = time.NewTimer(cfg.finishGraceMs)
		finishTimerC = finishTimer.C
	}

	finish := func() {
		if finished {
			return
		}
		if sourceTurnBuffer != "" || translationTurnBuffer != "" {
			completeTurn()
		}
		finished = true
		cancelPendingFinish()
		s.emit(provider.SpeechTranslationStreamPart{
			Type:       provider.SpeechTranslationStreamPartTypeFinish,
			SourceText: sourceText,
			OutputText: translationText,
			Usage:      usage,
		})
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.ctx.Done():
			fail(context.Cause(s.ctx))
			return

		case <-finishTimerC:
			finishTimerC = nil
			finish()
			return

		case <-audioEndedCh:
			audioEnded = true
			audioEndedCh = nil // consumed once; a nil channel blocks forever in future selects
			if sawTurnComplete && !openTurn {
				schedulePendingFinish()
			}

		case audioErr := <-audioErrCh:
			if finished {
				return
			}
			fail(audioErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if finished {
					return
				}
				if !wsutil.IsCleanClose(res.Err) {
					// An abnormal disconnection (TS onSocketError) always
					// fails the stream.
					fail(errors.New("Google Live translation error"))
				} else if finishTimer != nil {
					// A close while a finish is pending confirms that no
					// further turn activity follows (TS onClose's
					// `finishTimer != null` branch).
					finish()
				} else {
					fail(errors.New("Google Live translation WebSocket closed unexpectedly before finishing."))
				}
				return
			}
			if finished {
				continue
			}

			var message googleLiveSpeechTranslationServerMessage
			if jsonErr := json.Unmarshal([]byte(res.Text), &message); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.Text), &rawValue)
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}

			if len(message.SetupComplete) > 0 && string(message.SetupComplete) != "null" {
				resolveSetupComplete()
			}

			if message.UsageMetadata != nil {
				usage = accumulateGoogleLiveTranslationUsage(usage, message.UsageMetadata)
			}

			if message.Error != nil {
				msg := message.Error.Message
				if msg == "" {
					msg = "Google Live API error"
				}
				fail(errors.New(msg))
				return
			}

			inputText := ""
			if message.ServerContent != nil && message.ServerContent.InputTranscription != nil {
				inputText = message.ServerContent.InputTranscription.Text
			} else if message.InputTranscription != nil {
				inputText = message.InputTranscription.Text
			}
			if inputText != "" {
				onTurnActivity()
				sourceTurnBuffer += inputText
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeSourceTranscriptDelta, ID: itemID(), Delta: inputText}) {
					return
				}
			}

			serverContent := message.ServerContent
			if serverContent == nil {
				continue
			}

			if serverContent.ModelTurn != nil {
				stop := false
				for _, part := range serverContent.ModelTurn.Parts {
					if part.InlineData == nil || part.InlineData.Data == "" {
						continue
					}
					audioBytes, decodeErr := base64.StdEncoding.DecodeString(part.InlineData.Data)
					if decodeErr != nil {
						continue
					}
					if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeAudio, ID: itemID(), AudioData: audioBytes}) {
						stop = true
						break
					}

					silenceMs, isSilence := pcm16SilenceDurationMs(part.InlineData.Data)
					if audioEnded && isSilence {
						trailingSilenceMs += silenceMs
						if trailingSilenceMs >= float64(cfg.finishGraceMs.Milliseconds()) {
							finish()
							return
						}
					} else {
						onTurnActivity()
					}
				}
				if stop {
					return
				}
			}

			if serverContent.OutputTranscription != nil && serverContent.OutputTranscription.Text != "" {
				onTurnActivity()
				translationTurnBuffer += serverContent.OutputTranscription.Text
				if !s.emit(provider.SpeechTranslationStreamPart{Type: provider.SpeechTranslationStreamPartTypeOutputTextDelta, ID: itemID(), Delta: serverContent.OutputTranscription.Text}) {
					return
				}
			}

			if serverContent.TurnComplete {
				if !completeTurn() {
					return
				}
				openTurn = false
				sawTurnComplete = true
				if audioEnded {
					schedulePendingFinish()
				}
			}
		}
	}
}

func (s *googleLiveSpeechTranslationStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.ctx, wsURL, wsutil.DialOptions{Headers: headers})
}

func (s *googleLiveSpeechTranslationStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.ctx, conn, string(message))
}

var (
	_ provider.SpeechTranslationModel  = (*SpeechTranslationModel)(nil)
	_ provider.SpeechTranslationStream = (*googleLiveSpeechTranslationStream)(nil)
)
