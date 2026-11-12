package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// azureMaiSupportedSampleRates are the input sample rates MAI streaming
// transcription accepts, mirroring TS SUPPORTED_SAMPLE_RATES.
var azureMaiSupportedSampleRates = map[int]bool{16000: true, 24000: true}

const azureMaiDefaultSampleRate = 24000

// AzureMaiTranscriptionModel streams transcription over the MAI realtime
// WebSocket (wss://{resourceName}.services.ai.azure.com/mai/v1/realtime?intent=transcription),
// mirroring TS AzureMaiTranscriptionModel (azure-mai-transcription-model.ts).
// The protocol is OpenAI-realtime-compatible but MAI does not detect pauses
// server-side, so the stream commits once when the input audio ends.
type AzureMaiTranscriptionModel struct {
	modelID string
	url     func() (string, error)
	headers func(ctx context.Context) (map[string]string, error)
}

func newAzureMaiTranscriptionModel(modelID string, url func() (string, error), headers func(ctx context.Context) (map[string]string, error)) *AzureMaiTranscriptionModel {
	return &AzureMaiTranscriptionModel{modelID: modelID, url: url, headers: headers}
}

func (m *AzureMaiTranscriptionModel) SpecificationVersion() string { return "v4" }
func (m *AzureMaiTranscriptionModel) Provider() string             { return "azure.transcription" }
func (m *AzureMaiTranscriptionModel) ModelID() string              { return m.modelID }

// DoGenerate is unsupported: MAI streaming transcription has no file
// (non-realtime) transcription endpoint, mirroring TS
// AzureMaiTranscriptionModel#doGenerate.
func (m *AzureMaiTranscriptionModel) DoGenerate(_ context.Context, _ *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	return nil, &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("file transcription with %s (use streaming transcription)", m.modelID),
	}
}

// DoStream streams a transcript for live audio over the MAI realtime
// WebSocket, mirroring TS AzureMaiTranscriptionModel#doStream.
func (m *AzureMaiTranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions, maiOpts AzureTranscriptionModelOptions, extraWarnings []types.Warning) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	rate := opts.InputAudioFormat.Rate
	resolvedRate := azureMaiDefaultSampleRate
	if rate != nil {
		resolvedRate = *rate
	}
	if opts.InputAudioFormat.Type != "audio/pcm" || !azureMaiSupportedSampleRates[resolvedRate] {
		rateSuffix := ""
		if rate != nil {
			rateSuffix = fmt.Sprintf(" at %d Hz", resolvedRate)
		}
		return nil, providererrors.NewArgumentError("inputAudioFormat", fmt.Sprintf(
			"MAI streaming transcription requires 16-bit mono PCM (audio/pcm) at 16000 or 24000 Hz, got %s%s.",
			opts.InputAudioFormat.Type, rateSuffix,
		), nil)
	}

	transcription := map[string]interface{}{"model": m.modelID}
	if maiOpts.Language != "" {
		transcription["language"] = maiOpts.Language
	}
	sessionUpdate := map[string]interface{}{
		"type": "session.update",
		"session": map[string]interface{}{
			"type": "transcription",
			"audio": map[string]interface{}{
				"input": map[string]interface{}{
					"format":          map[string]interface{}{"type": "audio/pcm", "rate": resolvedRate},
					"transcription":   transcription,
					"turn_detection":  nil,
					"noise_reduction": nil,
				},
			},
		},
	}

	urlStr, err := m.url()
	if err != nil {
		return nil, err
	}
	baseHeaders, err := m.headers(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	for k, v := range baseHeaders {
		headers[k] = v
	}
	for k, v := range opts.Headers {
		headers[k] = v
	}

	wsURL, err := toWebSocketURL(urlStr)
	if err != nil {
		return nil, err
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newAzureMaiTranscriptionStream(abortCtx, azureMaiTranscriptionStreamConfig{
		url:              wsURL,
		headers:          headers,
		sessionUpdate:    sessionUpdate,
		bytesPerSecond:   resolvedRate * 2,
		language:         maiOpts.Language,
		warnings:         extraWarnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: sessionUpdate,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

// toWebSocketURL is TS provider-utils toWebSocketUrl: new URL(url) with
// http: set to ws: and https: to wss: (the WHATWG serialization; "Invalid
// URL" for input it rejects).
func toWebSocketURL(rawURL string) (string, error) {
	return wsutil.ToWebSocketURL(rawURL)
}

type azureMaiTranscriptionStreamConfig struct {
	url              string
	headers          map[string]string
	sessionUpdate    map[string]interface{}
	bytesPerSecond   int
	language         string
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// azureMaiTranscriptionStream implements provider.TranscriptionStream over
// the MAI realtime WebSocket. The Next/Err/Close/Emit/SetErr plumbing is the
// shared wsutil.Session core; only the protocol-specific run()/pumpAudio()
// below are local to Azure MAI, mirroring the same split used by
// pkg/providers/openai/transcription_stream.go and
// pkg/providers/google/transcription_stream.go.
type azureMaiTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func newAzureMaiTranscriptionStream(parentCtx context.Context, cfg azureMaiTranscriptionStreamConfig) *azureMaiTranscriptionStream {
	s := &azureMaiTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// azureMaiRealtimeEvent is the subset of MAI realtime server messages
// relevant to streaming transcription, mirroring TS MaiRealtimeEvent.
type azureMaiRealtimeEvent struct {
	Type         string `json:"type"`
	ItemID       string `json:"item_id"`
	Delta        string `json:"delta"`
	Intermediate string `json:"intermediate"`
	// Transcript is a pointer: TS reads it with ??, so a present "" is a
	// value.
	Transcript *string `json:"transcript"`
	// Error is untyped: TS reads error?.code / error?.message from whatever
	// JSON the server sent and formats them with a template string.
	Error interface{} `json:"error"`
}

func (s *azureMaiTranscriptionStream) run(cfg azureMaiTranscriptionStreamConfig) {
	defer s.CloseParts()
	defer s.CancelContext()

	fail := func(err error) {
		s.SetErr(err)
		cfg.audio.Cancel(err)
	}

	closeInfo := &wsutil.CloseInfo{}
	conn, err := s.dial(cfg.url, cfg.headers, closeInfo)
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
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	var (
		finished     bool
		audioStarted bool
		committed    bool
		audioBytes   int
		deltas       string
	)

	audioEndedCh := make(chan struct{}, 1)
	audioErrCh := make(chan error, 1)

	sendAudio := func(socket *websocket.Conn) {
		wsutil.PumpAudio(s.Context(), cfg.audio,
			func(chunk []byte) error {
				b64 := base64.StdEncoding.EncodeToString(chunk)
				if len(b64) == 0 {
					return nil
				}
				audioBytes += len(chunk)
				msg, marshalErr := json.Marshal(map[string]string{"type": "input_audio_buffer.append", "audio": b64})
				if marshalErr != nil {
					return nil
				}
				return s.send(socket, msg)
			},
			func() error {
				select {
				case audioEndedCh <- struct{}{}:
				case <-s.Context().Done():
				}
				return nil
			},
			audioErrCh,
		)
	}

	finish := func(text string) {
		if finished {
			return
		}
		finished = true
		part := provider.TranscriptionStreamPart{
			Type:       provider.TranscriptionStreamPartTypeFinish,
			FinishText: text,
			Segments:   []provider.TranscriptSegment{},
			Language:   cfg.language,
		}
		if audioBytes > 0 && cfg.bytesPerSecond > 0 {
			d := float64(audioBytes) / float64(cfg.bytesPerSecond)
			part.DurationInSeconds = &d
		}
		s.Emit(part)
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.Context().Done():
			fail(context.Cause(s.Context())) // TS onAbort(abortSignal.reason)
			return

		case audioErr := <-audioErrCh:
			if finished {
				return
			}
			fail(audioErr)
			return

		case <-audioEndedCh:
			if finished {
				continue
			}
			if audioBytes == 0 {
				finish("")
				return
			}
			committed = true
			if sendErr := s.send(conn, []byte(`{"type":"input_audio_buffer.commit"}`)); sendErr != nil {
				fail(sendErr)
				return
			}

		case res := <-msgCh:
			if res.Err != nil {
				if finished {
					return
				}
				if wsutil.IsCleanClose(res.Err) {
					// A close before the transcript completed (before or
					// after the commit) is an abnormal end, as TS onClose
					// reports it, with " (code N: reason)"
					// (azure-mai-transcription-model.ts:349-355).
					fail(errors.New("Azure MAI transcription connection closed before the transcript completed" + closeInfo.Suffix())) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
					return
				}
				fail(errors.New("Azure MAI transcription WebSocket error")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				return
			}
			if finished {
				continue
			}

			// TS safeParseJSON: any JSON value is a message and is reported
			// as a raw part; only an object with known fields is an event.
			var rawValue interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &rawValue); jsonErr != nil {
				continue
			}
			if cfg.includeRawChunks {
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}
			var event azureMaiRealtimeEvent
			if jsonErr := jsonexact.Unmarshal([]byte(res.Text), &event); jsonErr != nil {
				continue
			}

			switch event.Type {
			case "session.created":
				payload, marshalErr := json.Marshal(cfg.sessionUpdate)
				if marshalErr == nil {
					if sendErr := s.send(conn, payload); sendErr != nil {
						fail(sendErr)
						return
					}
				}

			case "session.updated":
				if !audioStarted {
					audioStarted = true
					go sendAudio(conn)
				}

			case "conversation.item.input_audio_transcription.delta":
				deltas += event.Delta
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: event.ItemID, Delta: event.Delta}) {
					return
				}

			case "conversation.item.input_audio_transcription.intermediate":
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: event.ItemID, Text: event.Intermediate}) {
					return
				}

			case "conversation.item.input_audio_transcription.completed":
				transcript := deltas
				if event.Transcript != nil {
					transcript = *event.Transcript
				}
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: event.ItemID, Text: transcript}) {
					return
				}
				if committed {
					finish(transcript)
					return
				}

			case "conversation.item.input_audio_transcription.failed", "error":
				message := "unknown error"
				prefix := "Azure MAI transcription error"
				if errObj, ok := event.Error.(map[string]interface{}); ok {
					// TS `${event.error.code}` / `${event.error?.message ??
					// 'unknown error'}`: JS String() of a non-null value.
					if code := errObj["code"]; code != nil {
						prefix += " (" + providerutils.JSString(code) + ")"
					}
					if msg := errObj["message"]; msg != nil {
						message = providerutils.JSString(msg)
					}
				}
				fail(fmt.Errorf("%s: %s", prefix, message))
				return
			}
		}
	}
}

func (s *azureMaiTranscriptionStream) dial(wsURL string, headers map[string]string, closeInfo *wsutil.CloseInfo) (*websocket.Conn, error) {
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: headers, WrapConn: closeInfo.Wrap})
}

func (s *azureMaiTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionStream = (*azureMaiTranscriptionStream)(nil)
)
