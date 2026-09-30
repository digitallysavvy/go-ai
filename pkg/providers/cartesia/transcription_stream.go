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
	"sync/atomic"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
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
// createCartesiaStreamingTranscriptionStream. The Next/Err/Close/emit/setErr
// plumbing is the shared wsutil.Session core; pumpAudio's turn-detection
// branching (a "close" control message vs. a bare "finalize" string,
// depending on cfg.useTurnDetection) doesn't fit the shared wsutil.PumpAudio
// loop's single onEnd callback cleanly, so it stays local.
type cartesiaTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]

	// finished mirrors the `finished` flag in TS: set once the stream has
	// reached a terminal state, checked by pumpAudio (a separate goroutine)
	// before sending the post-audio close/finalize control message.
	finished atomic.Bool
}

func newCartesiaTranscriptionStream(parentCtx context.Context, cfg cartesiaTranscriptionStreamConfig) *cartesiaTranscriptionStream {
	s := &cartesiaTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// send writes v (a string for a text frame, or []byte for a binary frame) to
// conn, unblocking early if the session's ctx is cancelled mid-write.
func (s *cartesiaTranscriptionStream) send(conn *websocket.Conn, v interface{}) error {
	return wsutil.Send(s.Context(), conn, v)
}

// pumpAudio forwards audio chunks as binary frames until the AudioStream is
// exhausted (io.EOF, per the AudioStream contract), then sends the
// close/finalize control message (unless the stream has already reached a
// terminal state), mirroring TS sendAudio. Any other failure — reading from
// the AudioStream, or writing to the WebSocket — is reported on errCh,
// mirroring `void sendAudio(socket).catch(finishWithError)`. A failure that
// stems from the session's context already being cancelled is not reported
// here: run()'s own select on that context's Done() already handles that
// case.
func (s *cartesiaTranscriptionStream) pumpAudio(conn *websocket.Conn, cfg cartesiaTranscriptionStreamConfig, errCh chan<- error) {
	for {
		chunk, err := cfg.audio.Next(s.Context())
		if err != nil {
			if err != io.EOF && s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, err)
			}
			break
		}
		if sendErr := s.send(conn, chunk); sendErr != nil {
			if s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, sendErr)
			}
			return
		}
	}

	if s.finished.Load() || s.Context().Err() != nil {
		return
	}
	if cfg.useTurnDetection {
		payload, marshalErr := json.Marshal(map[string]string{"type": "close"})
		if marshalErr != nil {
			wsutil.ReportError(s.Context(), errCh, marshalErr)
			return
		}
		if sendErr := s.send(conn, string(payload)); sendErr != nil && s.Context().Err() == nil {
			wsutil.ReportError(s.Context(), errCh, sendErr)
		}
	} else {
		if sendErr := s.send(conn, "finalize"); sendErr != nil && s.Context().Err() == nil {
			wsutil.ReportError(s.Context(), errCh, sendErr)
		}
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
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason instead of only on an explicit Close() call, which a consumer
	// that only drains Next() to io.EOF may never make.
	defer s.CancelContext()

	var finished bool

	fail := func(err error) {
		finished = true
		s.finished.Store(true)
		s.SetErr(err)
		cfg.audio.Cancel(err)
	}

	conn, err := s.dial(cfg.url)
	if err != nil {
		fail(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck

	if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	audioErrCh := make(chan error, 1)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)
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
		s.Emit(part)
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.Context().Done():
			fail(s.Context().Err())
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
			if res.Err != nil {
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
				if wsutil.IsCleanClose(res.Err) {
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
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
				continue
			}

			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.Text), &rawValue)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}

			switch raw.Type {
			case "turn.update", "turn.eager_end":
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: raw.RequestID, Text: raw.Transcript}) {
					return
				}

			case "turn.end":
				text := raw.Transcript
				finalTexts = append(finalTexts, text)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: raw.RequestID, Text: text}) {
					return
				}

			case "transcript":
				transcript := raw.Text
				if raw.IsFinal != nil && *raw.IsFinal {
					finalTexts = append(finalTexts, transcript)
					if raw.Duration != nil {
						durationSeconds += *raw.Duration
					}
					if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: raw.RequestID, Text: transcript}) {
						return
					}
				} else {
					part := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: raw.RequestID, Text: transcript}
					if raw.Duration != nil {
						d := *raw.Duration
						part.DurationInSeconds = &d
					}
					if !s.Emit(part) {
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
	return wsutil.Dial(s.Context(), wsURL.String(), wsutil.DialOptions{})
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*cartesiaTranscriptionStream)(nil)
)
