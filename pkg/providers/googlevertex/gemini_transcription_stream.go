package googlevertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"github.com/digitallysavvy/go-ai/pkg/version"
	"golang.org/x/net/websocket"
)

// liveGeminiTranscriptionWebSocketPath is the Vertex Live API bidi service
// path for streaming transcription, mirroring TS's liveWebSocketPath in
// gemini-transcription/google-vertex-gemini-transcription-model.ts (distinct
// from the Developer API's google.ai.generativelanguage...BidiGenerateContent
// path used by pkg/providers/google/transcription_stream.go).
const liveGeminiTranscriptionWebSocketPath = "google.cloud.aiplatform.v1.LlmBidiService/BidiGenerateContent"

// defaultGeminiFinishGraceDuration is the quiet window after the input audio
// ends during which trailing transcripts are still accepted before the
// stream finishes with no terminal signal from the server (TS
// defaultFinishGraceMs).
const defaultGeminiFinishGraceDuration = 3 * time.Second

// geminiLiveWebSocketURL builds the wss:// Vertex Live API URL for streaming
// transcription, mirroring TS's `wss://${vertexHost(location)}/ws/${liveWebSocketPath}`.
//
// When p.config.BaseURL was set explicitly (an httptest server URL in
// tests; TS instead injects a mock WebSocket constructor via
// config.webSocket, a DI point Go's wsutil.Dial has no equivalent for), the
// WS URL is derived from it (scheme swapped to ws/wss, same host) instead of
// the real vertexHost(location) production host, so tests can point the
// dial at a local WebSocket test server.
func geminiLiveWebSocketURL(p *Provider, location string) string {
	if p.config.BaseURL != "" {
		if u, err := url.Parse(p.config.BaseURL); err == nil && u.Host != "" {
			scheme := "wss"
			if u.Scheme == "http" {
				scheme = "ws"
			}
			return fmt.Sprintf("%s://%s/ws/%s", scheme, u.Host, liveGeminiTranscriptionWebSocketPath)
		}
	}
	return fmt.Sprintf("wss://%s/ws/%s", vertexHost(location), liveGeminiTranscriptionWebSocketPath)
}

// vertexModelResourcePath builds the fully-qualified Vertex model resource
// name used by the Live API setup message, mirroring TS:
// `projects/${project}/locations/${location}/publishers/google/models/${modelId}`
// (distinct from the relative "models/{id}" path
// pkg/providers/gemini.GetModelPath returns for the unary generateContent
// request).
func vertexModelResourcePath(project, location, modelID string) string {
	return fmt.Sprintf("projects/%s/locations/%s/publishers/google/models/%s", project, location, modelID)
}

// validateLiveInputAudioFormat mirrors TS validateLiveInputAudioFormat: the
// Vertex Live transcription API only accepts 16kHz 16-bit PCM input audio.
func validateLiveInputAudioFormat(format provider.AudioFormat) error {
	if format.Type != "audio/pcm" || (format.Rate != nil && *format.Rate != 16000) {
		return &providererrors.InvalidArgumentError{
			Field:   "inputAudioFormat",
			Message: "The Gemini Live transcription API only supports 16kHz 16-bit PCM input audio.",
		}
	}
	return nil
}

// DoStream streams a transcript for live audio over the Vertex Live API
// WebSocket ("gemini-3.5-transcribe-live" and other "-live" model variants).
// Mirrors TS GoogleVertexGeminiTranscriptionModel#doStream.
func (m *GeminiTranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}

	if !isLiveGeminiTranscriptionModelID(m.modelID) {
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

	transcriptionOpts := parseVertexGeminiTranscriptionOptions(opts.ProviderOptions)
	audioConfig := buildVertexAudioTranscriptionConfig(transcriptionOpts)
	if audioConfig == nil {
		audioConfig = map[string]interface{}{}
	}

	// The Vertex Live API authenticates with the same OAuth Bearer header
	// the HTTP surface uses (unlike the Developer API, which rides
	// x-goog-api-key on the query string) -- resolved once up front since
	// the handshake needs it synchronously, mirroring TS's
	// config.headers()/webSocketHeaders resolution before connectToWebSocket.
	token, err := m.provider.vertexAuthToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve Vertex auth token for streaming transcription: %w", err)
	}
	wsHeaders := map[string]string{"Authorization": "Bearer " + token}
	for k, v := range m.provider.config.Headers {
		wsHeaders[k] = v
	}
	// TS google-vertex-gemini-transcription-model.ts reuses
	// this.config.headers() -- the same tagged getHeaders() closure used for
	// REST calls -- so the WS handshake carries the `ai-sdk/google-vertex/
	// VERSION` tag too.
	wsHeaders = version.WithUserAgentSuffix(wsHeaders, version.ProviderUserAgent("google-vertex"))
	for k, v := range opts.Headers {
		wsHeaders[k] = v
	}

	location := m.provider.config.Location
	if location == "" {
		location = "global"
	}

	// NOTE: Google's GA announcement shows the setup with
	// `generationConfig: { responseModalities: ['TEXT'] }`, but sending it
	// suppresses the final `inputTranscription` segments on the current
	// endpoint (only interim partials arrive). Omit generationConfig -- the
	// empirically working shape -- until the endpoint honors the documented
	// form (mirrors the TS comment in
	// gemini-transcription/google-vertex-gemini-transcription-model.ts).
	setup := map[string]interface{}{
		"model":                   vertexModelResourcePath(m.provider.config.Project, location, m.modelID),
		"inputAudioTranscription": audioConfig,
	}

	inputAudioRate := 16000
	if opts.InputAudioFormat.Rate != nil {
		inputAudioRate = *opts.InputAudioFormat.Rate
	}

	finishGraceMs := m.finishGraceMs
	if finishGraceMs <= 0 {
		finishGraceMs = defaultGeminiFinishGraceDuration
	}

	wsURL := geminiLiveWebSocketURL(m.provider, location)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGeminiLiveTranscriptionStream(abortCtx, geminiLiveTranscriptionStreamConfig{
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

type geminiLiveTranscriptionStreamConfig struct {
	url              string
	headers          map[string]string
	setup            map[string]interface{}
	inputAudioRate   int
	finishGraceMs    time.Duration
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// geminiLiveTranscriptionStream implements provider.TranscriptionStream over
// the Vertex Live API WebSocket, mirroring TS
// createVertexLiveTranscriptionStream. Structurally identical to
// pkg/providers/google's googleLiveTranscriptionStream (same Live API
// message contract) other than auth, host, and setup.model shape, so both
// embed the shared wsutil.Session core for the Next/Err/Close/emit/setErr
// plumbing and the shared wsutil.PumpAudioAfterReady loop for pumpAudio;
// only the differing auth/host/setup construction and the message handling
// in run() stay provider-local.
type geminiLiveTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func newGeminiLiveTranscriptionStream(parentCtx context.Context, cfg geminiLiveTranscriptionStreamConfig) *geminiLiveTranscriptionStream {
	s := &geminiLiveTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// pumpAudio waits for the server's setupComplete acknowledgement (the Live
// API contract requires this before sending realtime input), then forwards
// audio chunks as realtimeInput.audio messages, committing
// realtimeInput.audioStreamEnd at EOF and signalling audioEnded, via the
// shared wsutil.PumpAudioAfterReady loop.
func (s *geminiLiveTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, setupComplete <-chan struct{}, rate int, audioEnded chan<- struct{}, errCh chan<- error) {
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

// geminiLiveServerMessage is the subset of Vertex Live API server messages
// relevant to streaming transcription, mirroring TS GoogleLiveServerMessage.
type geminiLiveServerMessage struct {
	SetupComplete json.RawMessage `json:"setupComplete,omitempty"`
	ServerContent *struct {
		InputTranscription        *geminiLiveTranscription `json:"inputTranscription,omitempty"`
		InterimInputTranscription *geminiLiveTranscription `json:"interimInputTranscription,omitempty"`
		TurnComplete              bool                     `json:"turnComplete,omitempty"`
		GenerationComplete        bool                     `json:"generationComplete,omitempty"`
		InteractionStatus         string                   `json:"interactionStatus,omitempty"`
	} `json:"serverContent,omitempty"`
	InputTranscription *geminiLiveTranscription `json:"inputTranscription,omitempty"`
	UsageMetadata      map[string]interface{}   `json:"usageMetadata,omitempty"`
	Error              *struct {
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

type geminiLiveTranscription struct {
	Text         string `json:"text,omitempty"`
	Finished     bool   `json:"finished,omitempty"`
	LanguageCode string `json:"languageCode,omitempty"`
	SpeakerLabel string `json:"speakerLabel,omitempty"`
}

func (s *geminiLiveTranscriptionStream) run(cfg geminiLiveTranscriptionStreamConfig) {
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
			audioEndedCh = nil
			schedulePendingFinish()

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
					fail(errors.New("Google Vertex Live transcription error")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				} else if audioEnded {
					finish()
				} else {
					fail(fmt.Errorf("Google Vertex Live transcription WebSocket closed unexpectedly before finishing")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				}
				return
			}
			if finished {
				continue
			}

			var message geminiLiveServerMessage
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
					msg = "Google Vertex Live API error"
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

func (s *geminiLiveTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: headers})
}

func (s *geminiLiveTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionStreamer = (*GeminiTranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*geminiLiveTranscriptionStream)(nil)
)
