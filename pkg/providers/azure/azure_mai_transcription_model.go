package azure

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
		return nil, &providererrors.InvalidArgumentError{
			Field: "inputAudioFormat",
			Message: fmt.Sprintf(
				"MAI streaming transcription requires 16-bit mono PCM (audio/pcm) at 16000 or 24000 Hz, got %s%s.",
				opts.InputAudioFormat.Type, rateSuffix,
			),
		}
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

// toWebSocketURL rewrites an http(s) URL to ws(s), mirroring TS
// provider-utils toWebSocketUrl.
func toWebSocketURL(rawURL string) (string, error) {
	switch {
	case hasPrefixFold(rawURL, "https://"):
		return "wss://" + rawURL[len("https://"):], nil
	case hasPrefixFold(rawURL, "http://"):
		return "ws://" + rawURL[len("http://"):], nil
	case hasPrefixFold(rawURL, "wss://"), hasPrefixFold(rawURL, "ws://"):
		return rawURL, nil
	default:
		return "", &providererrors.InvalidArgumentError{Field: "url", Message: "URL must use http(s) or ws(s) scheme: " + rawURL}
	}
}

func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c1, c2 := s[i], prefix[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
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
	Transcript   string `json:"transcript"`
	Error        *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func (s *azureMaiTranscriptionStream) run(cfg azureMaiTranscriptionStreamConfig) {
	defer s.CloseParts()
	defer s.CancelContext()

	fail := func(err error) {
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
			fail(s.Context().Err())
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
				if wsutil.IsCleanClose(res.Err) && committed {
					// A clean close after the commit with no completed event
					// yet is still an abnormal end: MAI should emit
					// .completed before closing. Fail explicitly rather than
					// finishing silently, mirroring TS onClose.
					fail(errors.New("Azure MAI transcription connection closed before the transcript completed")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
					return
				}
				fail(errors.New("Azure MAI transcription WebSocket error")) //nolint:staticcheck // leading proper noun (provider/brand name), not a capitalization issue
				return
			}
			if finished {
				continue
			}

			var event azureMaiRealtimeEvent
			if jsonErr := json.Unmarshal([]byte(res.Text), &event); jsonErr != nil {
				continue
			}
			if cfg.includeRawChunks {
				var rawValue interface{}
				_ = json.Unmarshal([]byte(res.Text), &rawValue)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
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
				transcript := event.Transcript
				if transcript == "" {
					transcript = deltas
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
				code := ""
				if event.Error != nil {
					if event.Error.Message != "" {
						message = event.Error.Message
					}
					code = event.Error.Code
				}
				prefix := "Azure MAI transcription error"
				if code != "" {
					prefix += " (" + code + ")"
				}
				fail(fmt.Errorf("%s: %s", prefix, message))
				return
			}
		}
	}
}

func (s *azureMaiTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: headers})
}

func (s *azureMaiTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionStream = (*azureMaiTranscriptionStream)(nil)
)
