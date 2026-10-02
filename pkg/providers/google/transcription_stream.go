package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
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

// baseTranscriptionHeaders returns the provider's default headers
// (x-goog-api-key, configured custom headers, and the `ai-sdk-google/VERSION`
// User-Agent tag), mirroring the Resolvable config.headers TS combines with
// per-call options.headers -- the same tagged getHeaders() closure used for
// REST calls, not a freshly rebuilt untagged header set.
func (m *TranscriptionModel) baseTranscriptionHeaders() map[string]string {
	return m.prov.client.Headers()
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
		return nil, errors.New("Google Generative AI API key is required for streaming transcription.") //nolint:staticcheck // matches TS SDK's exact error text
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

	finishGrace := m.finishGrace
	if finishGrace <= 0 {
		finishGrace = defaultFinishGraceDuration
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
		finishGrace:      finishGrace,
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
	finishGrace      time.Duration
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// googleLiveTranscriptionStream implements provider.TranscriptionStream over
// the Gemini Live API WebSocket, mirroring TS
// createGoogleLiveTranscriptionStream. The Next/Err/Close/emit/setErr
// plumbing is the shared wsutil.Session core; only the protocol-specific
// run()/pumpAudio()/dial()/send() below are local to Google.
type googleLiveTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func newGoogleLiveTranscriptionStream(parentCtx context.Context, cfg googleLiveTranscriptionStreamConfig) *googleLiveTranscriptionStream {
	s := &googleLiveTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// pumpAudio waits for the server's setupComplete acknowledgement (the Live
// API contract requires this before sending realtime input), then forwards
// audio chunks as realtimeInput.audio messages, committing
// realtimeInput.audioStreamEnd at EOF and signalling audioEnded. Any other
// failure — reading from the AudioStream, or writing to the WebSocket — is
// reported on errCh via the shared wsutil.PumpAudioAfterReady loop, mirroring
// TS's `void sendAudio(socket).catch(finishWithError)` (a rejected
// `audioReader.read()` fails the stream exactly like a failed `socket.send`).
func (s *googleLiveTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, setupComplete <-chan struct{}, rate int, audioEnded chan<- struct{}, errCh chan<- error) {
	wsutil.PumpAudioAfterReady(s.Context(), setupComplete, audio,
		func(chunk []byte) error {
			msg, marshalErr := json.Marshal(map[string]interface{}{
				"realtimeInput": map[string]interface{}{
					"audio": map[string]interface{}{
						"data":     base64.StdEncoding.EncodeToString(chunk),
						"mimeType": fmt.Sprintf("audio/pcm;rate=%d", rate),
					},
				},
			})
			if marshalErr != nil {
				return nil
			}
			return s.send(conn, msg)
		},
		func() error {
			if sendErr := s.send(conn, []byte(`{"realtimeInput":{"audioStreamEnd":true}}`)); sendErr != nil {
				return sendErr
			}
			select {
			case audioEnded <- struct{}{}:
			case <-s.Context().Done():
			}
			return nil
		},
		errCh,
	)
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
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason instead of only on an explicit Close() call, which a consumer
	// that only drains Next() to io.EOF may never make.
	defer s.CancelContext()

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
		s.SetErr(err)
		cfg.audio.Cancel(err)
	}

	conn, err := s.dial(cfg.url, cfg.headers)
	if err != nil {
		fail(err)
		return
	}
	s.SetConn(conn)
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

	if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

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
		ok := s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: segmentID(), Text: segmentBuffer})
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
		finishTimer = time.NewTimer(cfg.finishGrace)
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
		s.Emit(provider.TranscriptionStreamPart{
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
		case <-s.Context().Done():
			cause := s.Context().Err()
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

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`: a
			// failure pumping audio (reading the caller's AudioStream, or
			// writing to the WebSocket) terminates the stream with an error
			// instead of being silently dropped.
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
					// fails the stream, regardless of whether the input audio
					// had already ended — distinct from a clean close (TS
					// onClose), which finishes successfully once a finish is
					// pending.
					fail(errors.New("Google Live transcription error")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				} else if finishTimer != nil {
					// Gate on the pending finish-grace timer, not the
					// audioEnded boolean directly: schedulePendingFinish only
					// arms finishTimer once audioEnded is true (mirrors TS
					// onClose's audioEnded check exactly, since
					// schedulePendingFinish is invoked synchronously and
					// unconditionally as soon as audioEnded transitions to
					// true — see the matching gate in
					// speech_translation_stream.go, which this mirrors for
					// consistency across the two Live API streams).
					finish()
				} else {
					fail(fmt.Errorf("Google Live transcription WebSocket closed unexpectedly before finishing")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				}
				return
			}
			if finished {
				continue
			}

			var message googleLiveServerMessage
			if jsonErr := json.Unmarshal([]byte(res.Text), &message); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.Text), &rawValue)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
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
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: segmentID(), Text: latestInterim}) {
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
					if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: segmentID(), Delta: transcription.Text}) {
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
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: headers})
}

func (s *googleLiveTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*googleLiveTranscriptionStream)(nil)
)
