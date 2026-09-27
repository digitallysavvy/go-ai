package google

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
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"golang.org/x/net/websocket"
)

// liveTranscriptionWebSocketPath is the Live API bidi service path for
// streaming transcription, mirroring TS's liveWebSocketPath in
// google-transcription-model.ts (distinct from the v1alpha realtime service
// path in realtime_model.go).
const liveTranscriptionWebSocketPath = "google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"

// defaultFinishGraceDuration is the quiet window after the input audio ends
// during which trailing transcripts are still accepted before the stream
// finishes with no terminal signal from the server (TS defaultFinishGraceMs).
const defaultFinishGraceDuration = 3 * time.Second

// getLiveTranscriptionWebSocketURL builds the wss:// Live API URL for
// streaming transcription, mirroring TS getLiveWebSocketURL: the base URL's
// trailing /v1beta or /v1alpha segment is stripped (googleRealtimeBaseURL),
// the scheme is upgraded to ws(s), and the API key rides the `key` query
// parameter.
func getLiveTranscriptionWebSocketURL(baseURL, apiKey string) string {
	u := googleRealtimeBaseURL(baseURL)
	u.Scheme = strings.Replace(u.Scheme, "http", "ws", 1)
	u.Path = strings.TrimRight(u.Path, "/") + "/ws/" + liveTranscriptionWebSocketPath
	q := u.Query()
	q.Set("key", apiKey)
	u.RawQuery = q.Encode()
	return u.String()
}

// baseTranscriptionHeaders rebuilds the provider's default headers
// (x-goog-api-key plus any configured custom headers), mirroring the
// Resolvable config.headers TS combines with per-call options.headers.
func (m *TranscriptionModel) baseTranscriptionHeaders() map[string]string {
	return internalhttp.MergeHeaders(map[string]string{
		"x-goog-api-key": m.prov.APIKey(),
	}, m.prov.config.Headers)
}

// extractGoogleAPIKeyHeader pulls the x-goog-api-key header value out of a
// header map case-insensitively and returns the remaining headers with any
// case-variant of that key removed, matching TS's webSocketHeaders filter.
// Within a single map holding more than one case-variant of the key, which
// one wins is unspecified (Go map iteration order); callers that need TS's
// deterministic "last case-variant wins, per-call overrides provider-level"
// behavior across the provider-level and per-call header sources should call
// this once per source, in precedence order, rather than on an
// already-merged map (see DoStream).
func extractGoogleAPIKeyHeader(headers map[string]string) (apiKey string, filtered map[string]string) {
	filtered = make(map[string]string, len(headers))
	for k, v := range headers {
		if strings.EqualFold(k, "x-goog-api-key") {
			if v != "" {
				apiKey = v
			}
			continue
		}
		filtered[k] = v
	}
	return apiKey, filtered
}

// validateLiveInputAudioFormat mirrors TS validateLiveInputAudioFormat: the
// Gemini Live transcription API only accepts 16kHz 16-bit PCM input audio.
func validateLiveInputAudioFormat(format provider.AudioFormat) error {
	if format.Type != "audio/pcm" || (format.Rate != nil && *format.Rate != 16000) {
		return &providererrors.InvalidArgumentError{
			Field:   "inputAudioFormat",
			Message: "The Gemini Live transcription API only supports 16kHz 16-bit PCM input audio.",
		}
	}
	return nil
}

// buildAudioTranscriptionConfig builds Google's Live API
// `inputAudioTranscription` config (camelCase wire, distinct from the
// snake_case Interactions API transcription_config built by
// buildTranscriptionConfig) from provider options; returns nil when no
// options are set. Mirrors TS buildAudioTranscriptionConfig exactly.
func buildAudioTranscriptionConfig(opts transcriptionModelOptions) map[string]interface{} {
	config := map[string]interface{}{}
	if len(opts.LanguageCodes) > 0 {
		config["languageCodes"] = opts.LanguageCodes
	}
	if len(opts.CustomVocabulary) > 0 {
		config["customVocabulary"] = opts.CustomVocabulary
	}
	if opts.WordTimestamp != nil {
		config["wordTimestamp"] = *opts.WordTimestamp
	}
	if opts.Diarization != nil {
		config["diarization"] = *opts.Diarization
	}
	if opts.Mode != "" {
		config["mode"] = opts.Mode
	}
	if len(config) == 0 {
		return nil
	}
	return config
}

// DoStream streams a transcript for live audio over the Gemini Live API
// WebSocket (gemini-3.5-transcribe-live and other "-live" model variants).
// Mirrors TS GoogleTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}

	if !isLiveTranscriptionModelID(m.modelID) {
		return nil, &providererrors.InvalidArgumentError{
			Field: "modelId",
			Message: fmt.Sprintf(
				"Model '%s' does not support streaming transcription. Use a live model such as '%s'.",
				m.modelID, ModelGemini35TranscribeLive,
			),
		}
	}

	if err := validateLiveInputAudioFormat(opts.InputAudioFormat); err != nil {
		return nil, err
	}

	transcriptionOpts := parseTranscriptionModelOptions(opts.ProviderOptions)

	// Resolve the API key from the provider-level and per-call header maps
	// separately, with the per-call value taking precedence, before merging
	// them: extracting it from the already-merged map would make "last
	// case-variant wins" depend on Go's unspecified map iteration order
	// whenever a per-call header overrides a differently-cased provider
	// header (TS's combineHeaders is deterministic here because JS object
	// spread preserves insertion order).
	baseAPIKey, filteredBaseHeaders := extractGoogleAPIKeyHeader(m.baseTranscriptionHeaders())
	callAPIKey, filteredCallHeaders := extractGoogleAPIKeyHeader(opts.Headers)
	apiKey := baseAPIKey
	if callAPIKey != "" {
		apiKey = callAPIKey
	}
	if apiKey == "" {
		return nil, errors.New("Google Generative AI API key is required for streaming transcription.")
	}
	wsHeaders := internalhttp.MergeHeaders(filteredBaseHeaders, filteredCallHeaders)

	audioConfig := buildAudioTranscriptionConfig(transcriptionOpts)
	if audioConfig == nil {
		audioConfig = map[string]interface{}{}
	}

	// NOTE: Google's GA announcement shows the setup with
	// `generationConfig: { responseModalities: ['TEXT'] }`, but sending it
	// suppresses the final `inputTranscription` segments on the current
	// endpoint (only interim partials arrive). Omit generationConfig — the
	// empirically working shape — until the endpoint honors the documented
	// form (mirrors the TS comment in google-transcription-model.ts).
	setup := map[string]interface{}{
		"model":                   gemini.GetModelPath(m.modelID),
		"inputAudioTranscription": audioConfig,
	}

	inputAudioRate := 16000
	if opts.InputAudioFormat.Rate != nil {
		inputAudioRate = *opts.InputAudioFormat.Rate
	}

	finishGraceMs := m.finishGraceMs
	if finishGraceMs <= 0 {
		finishGraceMs = defaultFinishGraceDuration
	}

	wsURL := getLiveTranscriptionWebSocketURL(m.prov.config.BaseURL, apiKey)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGoogleLiveTranscriptionStream(abortCtx, googleLiveTranscriptionStreamConfig{
		url:              wsURL,
		headers:          wsHeaders,
		setup:            setup,
		inputAudioRate:   inputAudioRate,
		finishGraceMs:    finishGraceMs,
		warnings:         []types.Warning{},
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: setup,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

type googleLiveTranscriptionStreamConfig struct {
	url              string
	headers          map[string]string
	setup            map[string]interface{}
	inputAudioRate   int
	finishGraceMs    time.Duration
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// googleLiveTranscriptionStream implements provider.TranscriptionStream over
// the Gemini Live API WebSocket, mirroring TS
// createGoogleLiveTranscriptionStream.
type googleLiveTranscriptionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	parts  chan provider.TranscriptionStreamPart

	mu  sync.Mutex
	err error

	closeOnce sync.Once
	connMu    sync.Mutex
	conn      *websocket.Conn
}

func newGoogleLiveTranscriptionStream(parentCtx context.Context, cfg googleLiveTranscriptionStreamConfig) *googleLiveTranscriptionStream {
	ctx, cancel := context.WithCancel(parentCtx)
	s := &googleLiveTranscriptionStream{ctx: ctx, cancel: cancel, parts: make(chan provider.TranscriptionStreamPart)}
	go s.run(cfg)
	return s
}

func (s *googleLiveTranscriptionStream) Next() (*provider.TranscriptionStreamPart, error) {
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

func (s *googleLiveTranscriptionStream) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *googleLiveTranscriptionStream) Close() error {
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

func (s *googleLiveTranscriptionStream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

// emit sends part on s.parts, returning false when the stream's ctx is done
// (so a blocked send unblocks instead of leaking when Close cancels ctx).
func (s *googleLiveTranscriptionStream) emit(part provider.TranscriptionStreamPart) bool {
	select {
	case s.parts <- part:
		return true
	case <-s.ctx.Done():
		return false
	}
}

type googleLiveWSResult struct {
	msg string
	err error
}

// receiveLoop continuously reads text frames from conn and forwards each one
// (or the terminal error) on out, until an error occurs or s.ctx is done.
// A dedicated goroutine (rather than one-shot receives from run()) lets the
// caller's select multiplex incoming messages against the finish-grace timer.
func (s *googleLiveTranscriptionStream) receiveLoop(conn *websocket.Conn, out chan<- googleLiveWSResult) {
	for {
		var msg string
		err := websocket.Message.Receive(conn, &msg)
		select {
		case out <- googleLiveWSResult{msg: msg, err: err}:
		case <-s.ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// pumpAudio waits for the server's setupComplete acknowledgement (the Live
// API contract requires this before sending realtime input), then forwards
// audio chunks as realtimeInput.audio messages, committing
// realtimeInput.audioStreamEnd at EOF and signalling audioEnded.
func (s *googleLiveTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, setupComplete <-chan struct{}, rate int, audioEnded chan<- struct{}) {
	select {
	case <-setupComplete:
	case <-s.ctx.Done():
		return
	}

	for {
		chunk, err := audio.Next(s.ctx)
		if err != nil {
			if err == io.EOF {
				_ = s.send(conn, []byte(`{"realtimeInput":{"audioStreamEnd":true}}`))
				select {
				case audioEnded <- struct{}{}:
				case <-s.ctx.Done():
				}
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
			return
		}
	}
}

// googleLiveServerMessage is the subset of Google Live API server messages
// relevant to streaming transcription, mirroring TS GoogleLiveServerMessage.
type googleLiveServerMessage struct {
	SetupComplete json.RawMessage `json:"setupComplete,omitempty"`
	ServerContent *struct {
		InputTranscription        *googleLiveTranscription `json:"inputTranscription,omitempty"`
		InterimInputTranscription *googleLiveTranscription `json:"interimInputTranscription,omitempty"`
		TurnComplete              bool                     `json:"turnComplete,omitempty"`
		GenerationComplete        bool                     `json:"generationComplete,omitempty"`
		InteractionStatus         string                   `json:"interactionStatus,omitempty"`
	} `json:"serverContent,omitempty"`
	InputTranscription *googleLiveTranscription `json:"inputTranscription,omitempty"`
	UsageMetadata      map[string]interface{}   `json:"usageMetadata,omitempty"`
	Error              *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type googleLiveTranscription struct {
	Text         string `json:"text,omitempty"`
	Finished     bool   `json:"finished,omitempty"`
	LanguageCode string `json:"languageCode,omitempty"`
	SpeakerLabel string `json:"speakerLabel,omitempty"`
}

func (s *googleLiveTranscriptionStream) run(cfg googleLiveTranscriptionStreamConfig) {
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
	go s.pumpAudio(conn, cfg.audio, setupCompleteCh, cfg.inputAudioRate, audioEndedCh)

	if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan googleLiveWSResult)
	go s.receiveLoop(conn, msgCh)

	// Transcription fragments arrive incrementally and are accumulated per
	// segment; a `finished: true` transcription or `turnComplete` finalizes
	// the current segment. Google Live messages carry no response/item IDs,
	// so a segment counter generates consistent synthetic IDs.
	var (
		finished       bool
		segmentCounter int
		segmentBuffer  string
		fullText       string
		latestInterim  string
		language       string
		audioEnded     bool
		usageMetadata  map[string]interface{}
	)

	segmentID := func() string { return fmt.Sprintf("google-segment-%d", segmentCounter) }

	// completeSegment finalizes segmentBuffer (or, absent one, the latest
	// revisable interim text) as a transcript-final part.
	completeSegment := func() bool {
		if segmentBuffer == "" {
			if latestInterim == "" {
				return true
			}
			segmentBuffer = latestInterim
		}
		latestInterim = ""
		ok := s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: segmentID(), Text: segmentBuffer})
		if fullText == "" {
			fullText = segmentBuffer
		} else {
			fullText = fullText + " " + segmentBuffer
		}
		segmentBuffer = ""
		segmentCounter++
		return ok
	}

	// Trailing transcripts can arrive after the input audio ends; without a
	// terminal signal, finish after a quiet grace window. Transcript
	// activity reschedules the timer.
	schedulePendingFinish := func() {
		if finished || !audioEnded {
			return
		}
		cancelPendingFinish()
		finishTimer = time.NewTimer(cfg.finishGraceMs)
		finishTimerC = finishTimer.C
	}

	finish := func() {
		if finished {
			return
		}
		completeSegment()
		finished = true
		cancelPendingFinish()
		var providerMetadata map[string]interface{}
		if usageMetadata != nil {
			providerMetadata = map[string]interface{}{"google": map[string]interface{}{"usageMetadata": usageMetadata}}
		}
		s.emit(provider.TranscriptionStreamPart{
			Type:             provider.TranscriptionStreamPartTypeFinish,
			FinishText:       fullText,
			Segments:         []provider.TranscriptSegment{},
			Language:         language,
			ProviderMetadata: providerMetadata,
		})
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.ctx.Done():
			cause := s.ctx.Err()
			fail(cause)
			return

		case <-finishTimerC:
			finishTimerC = nil
			finish()
			return

		case <-audioEndedCh:
			audioEnded = true
			audioEndedCh = nil // consumed once; a nil channel blocks forever in future selects
			schedulePendingFinish()

		case res := <-msgCh:
			if res.err != nil {
				if finished {
					return
				}
				if audioEnded {
					finish()
				} else {
					fail(fmt.Errorf("Google Live transcription WebSocket closed unexpectedly before finishing"))
				}
				return
			}
			if finished {
				continue
			}

			var message googleLiveServerMessage
			if jsonErr := json.Unmarshal([]byte(res.msg), &message); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.msg), &rawValue)
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}

			if len(message.SetupComplete) > 0 && string(message.SetupComplete) != "null" {
				resolveSetupComplete()
			}

			if message.UsageMetadata != nil {
				usageMetadata = message.UsageMetadata
			}

			if message.Error != nil {
				msg := message.Error.Message
				if msg == "" {
					msg = "Google Live API error"
				}
				fail(errors.New(msg))
				return
			}

			serverContent := message.ServerContent

			if serverContent != nil && serverContent.InterimInputTranscription != nil && serverContent.InterimInputTranscription.Text != "" {
				schedulePendingFinish()
				latestInterim = serverContent.InterimInputTranscription.Text
				if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: segmentID(), Text: latestInterim}) {
					return
				}
			}

			transcription := message.InputTranscription
			if serverContent != nil && serverContent.InputTranscription != nil {
				transcription = serverContent.InputTranscription
			}
			if transcription != nil {
				if transcription.LanguageCode != "" {
					language = transcription.LanguageCode
				}
				if transcription.Text != "" {
					schedulePendingFinish()
					latestInterim = ""
					segmentBuffer += transcription.Text
					if !s.emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: segmentID(), Delta: transcription.Text}) {
						return
					}
				}
				if transcription.Finished {
					if !completeSegment() {
						return
					}
				}
			}

			if serverContent != nil && serverContent.TurnComplete {
				if !completeSegment() {
					return
				}
			}

			// interactionStatus idle (REQUIRES_ACTION in the EAP builds) is
			// the definitive all-processing-complete signal: finish as soon
			// as the input audio has ended.
			if serverContent != nil && audioEnded {
				status := serverContent.InteractionStatus
				if status == "IDLE" || status == "REQUIRES_ACTION" || (serverContent.TurnComplete && status == "") {
					finish()
					return
				}
			}
		}
	}
}

func (s *googleLiveTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	wsConfig, err := websocket.NewConfig(wsURL, "http://localhost/")
	if err != nil {
		return nil, err
	}
	wsConfig.Header = stdhttp.Header{}
	for k, v := range headers {
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

func (s *googleLiveTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
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

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*googleLiveTranscriptionStream)(nil)
)
