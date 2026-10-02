package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// bearerTokenPattern extracts the token from an "Authorization: Bearer
// <token>" header value, mirroring TS getOpenAIRealtimeConnection's
// `/^bearer\s+(.+)$/i`: the "bearer" scheme name is matched
// case-insensitively (the HTTP auth scheme is case-insensitive) and any run
// of whitespace separates it from the token, not just a single space.
var bearerTokenPattern = regexp.MustCompile(`(?i)^bearer\s+(.+)$`)

// IsRealtimeTranscriptionModelID reports whether modelID streams over the
// OpenAI realtime WebSocket rather than the REST transcription endpoint.
// Prefix matching keeps dated snapshots (e.g. "gpt-realtime-whisper-2026-01-01")
// working, mirroring TS isRealtimeTranscriptionModelId.
func IsRealtimeTranscriptionModelID(modelID string) bool {
	return modelID == "gpt-realtime-whisper" || strings.HasPrefix(modelID, "gpt-realtime-whisper-")
}

// DoStream streams a transcript for live audio over the OpenAI realtime
// WebSocket (gpt-realtime-whisper and dated snapshots). Mirrors TS
// OpenAITranscriptionModel#doStream / createOpenAIRealtimeTranscriptionStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if !IsRealtimeTranscriptionModelID(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("streaming transcription with %s", m.modelID),
		}
	}

	streamOpts := TranscriptionStreamOpenAIOptions(opts.ProviderOptions)

	var warnings []types.Warning
	if streamOpts.HasInclude {
		warnings = append(warnings, UnsupportedStreamingTranscriptionWarning("include"))
	}
	if streamOpts.Prompt != "" {
		warnings = append(warnings, UnsupportedStreamingTranscriptionWarning("prompt"))
	}
	if streamOpts.HasTemperature {
		warnings = append(warnings, UnsupportedStreamingTranscriptionWarning("temperature"))
	}
	if len(streamOpts.TimestampGranularities) > 0 {
		warnings = append(warnings, UnsupportedStreamingTranscriptionWarning("timestampGranularities"))
	}

	headers := internalhttp.MergeHeaders(m.baseWSHeaders(), opts.Headers)
	sessionUpdate := BuildOpenAIRealtimeTranscriptionSession(m.modelID, opts.InputAudioFormat, streamOpts)
	wsURL := realtimeTranscriptionWebSocketURL(m.provider.config.BaseURL)

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := NewOpenAIRealtimeTranscriptionStream(abortCtx, OpenAIRealtimeTranscriptionStreamConfig{
		URL:              wsURL,
		Headers:          headers,
		SessionUpdate:    sessionUpdate,
		Language:         streamOpts.Language,
		Warnings:         warnings,
		Audio:            opts.Audio,
		IncludeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: sessionUpdate,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

// baseWSHeaders returns the provider's default headers (Authorization,
// organization, project, custom config headers, and the
// `ai-sdk-openai/VERSION` User-Agent tag) for the WebSocket handshake,
// mirroring TS openai-transcription-model.ts's doStream, which reuses
// `this.config.headers()` -- the same tagged getHeaders() closure used for
// REST calls -- rather than rebuilding an untagged header set.
func (m *TranscriptionModel) baseWSHeaders() map[string]string {
	return m.provider.client.Headers()
}

func UnsupportedStreamingTranscriptionWarning(option string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: "providerOptions.openai." + option,
		Details: fmt.Sprintf("OpenAI streaming transcription does not support %s.", option),
	}
}

func realtimeTranscriptionWebSocketURL(baseURL string) string {
	wsBase := strings.Replace(baseURL, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	return wsBase + "/realtime?intent=transcription"
}

// BuildOpenAIRealtimeTranscriptionSession mirrors TS
// BuildOpenAIRealtimeTranscriptionSession.
func BuildOpenAIRealtimeTranscriptionSession(modelID string, format provider.AudioFormat, opts TranscriptionStreamOpenAIOptionsValue) map[string]interface{} {
	inputFormat := map[string]interface{}{"type": format.Type}
	if format.Rate != nil {
		inputFormat["rate"] = *format.Rate
	}

	transcription := map[string]interface{}{"model": modelID}
	if opts.Language != "" {
		transcription["language"] = opts.Language
	}
	if opts.StreamingDelay != nil {
		transcription["delay"] = *opts.StreamingDelay
	}

	session := map[string]interface{}{
		"type": "transcription",
		"audio": map[string]interface{}{
			"input": map[string]interface{}{
				"format":         inputFormat,
				"transcription":  transcription,
				"turn_detection": nil,
			},
		},
	}
	if len(opts.StreamingInclude) > 0 {
		session["include"] = opts.StreamingInclude
	}
	return map[string]interface{}{"type": "session.update", "session": session}
}

// TranscriptionStreamOpenAIOptionsValue holds providerOptions.openai fields
// relevant to streaming transcription, plus flags for REST-only fields that
// warn when set during streaming.
type TranscriptionStreamOpenAIOptionsValue struct {
	Language               string
	StreamingDelay         *int
	StreamingInclude       []string
	HasInclude             bool
	Prompt                 string
	HasTemperature         bool
	TimestampGranularities []string
}

func TranscriptionStreamOpenAIOptions(providerOptions map[string]interface{}) TranscriptionStreamOpenAIOptionsValue {
	var result TranscriptionStreamOpenAIOptionsValue
	if providerOptions == nil {
		return result
	}
	openaiOpts, ok := providerOptions["openai"].(map[string]interface{})
	if !ok {
		return result
	}
	if v, ok := openaiOpts["language"].(string); ok {
		result.Language = v
	}
	if v, ok := openaiOpts["prompt"].(string); ok {
		result.Prompt = v
	}
	if _, ok := openaiOpts["temperature"]; ok {
		result.HasTemperature = true
	}
	if v, ok := openaiOpts["include"].([]interface{}); ok && len(v) > 0 {
		result.HasInclude = true
	}
	if v, ok := openaiOpts["timestampGranularities"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	if streaming, ok := openaiOpts["streaming"].(map[string]interface{}); ok {
		if v, ok := numericValue(streaming["delay"]); ok {
			d := int(v)
			result.StreamingDelay = &d
		}
		if v, ok := streaming["include"].([]interface{}); ok {
			for _, item := range v {
				if s, ok := item.(string); ok {
					result.StreamingInclude = append(result.StreamingInclude, s)
				}
			}
		}
	}
	return result
}

func numericValue(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// openAIRealtimeWSAuth mirrors TS getOpenAIRealtimeConnection: the bearer
// token rides the "openai-insecure-api-key" subprotocol (a plain WebSocket
// handshake cannot rely on an Authorization header being honored uniformly),
// and the Authorization header is stripped since OpenAI rejects handshakes
// carrying both auth channels.
func openAIRealtimeWSAuth(headers map[string]string) ([]string, map[string]string) {
	var token string
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") && v != "" {
			if m := bearerTokenPattern.FindStringSubmatch(v); m != nil {
				token = m[1]
			}
		}
	}
	if token == "" {
		return []string{"realtime"}, headers
	}
	filtered := make(map[string]string, len(headers))
	for k, v := range headers {
		if strings.EqualFold(k, "authorization") {
			continue
		}
		filtered[k] = v
	}
	return []string{"realtime", "openai-insecure-api-key." + token}, filtered
}

// OpenAIRealtimeTranscriptionStreamConfig bundles the inputs to
// NewOpenAIRealtimeTranscriptionStream.
type OpenAIRealtimeTranscriptionStreamConfig struct {
	URL              string
	Headers          map[string]string
	SessionUpdate    map[string]interface{}
	Language         string
	Warnings         []types.Warning
	Audio            provider.AudioStream
	IncludeRawChunks bool
}

// openAIRealtimeTranscriptionStream implements provider.TranscriptionStream
// over the OpenAI realtime WebSocket, mirroring TS
// createOpenAIRealtimeTranscriptionStream. The Next/Err/Close/emit/setErr
// plumbing is the shared wsutil.Session core; only the protocol-specific
// run()/pumpAudio()/dial()/send() below are local to OpenAI.
type openAIRealtimeTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func NewOpenAIRealtimeTranscriptionStream(parentCtx context.Context, cfg OpenAIRealtimeTranscriptionStreamConfig) *openAIRealtimeTranscriptionStream {
	s := &openAIRealtimeTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

func (s *openAIRealtimeTranscriptionStream) run(cfg OpenAIRealtimeTranscriptionStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason instead of only on an explicit Close() call, which a consumer
	// that only drains Next() to io.EOF may never make.
	defer s.CancelContext()

	conn, err := s.dial(cfg.URL, cfg.Headers)
	if err != nil {
		s.SetErr(err)
		cfg.Audio.Cancel(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck

	// Send the session update and start pumping audio before emitting
	// stream-start: setup must not wait for a consumer to be reading yet
	// (mirrors TS onOpen, where controller.enqueue does not block on a
	// reader before the socket send and sendAudio() call that follow it).
	payload, err := json.Marshal(cfg.SessionUpdate)
	if err != nil {
		s.SetErr(err)
		cfg.Audio.Cancel(err)
		return
	}
	if err := s.send(conn, payload); err != nil {
		s.SetErr(err)
		cfg.Audio.Cancel(err)
		return
	}

	audioErrCh := make(chan error, 1)
	go s.pumpAudio(conn, cfg.Audio, audioErrCh)

	if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.Warnings}) {
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	for {
		select {
		case <-s.Context().Done():
			cause := s.Context().Err()
			s.SetErr(cause)
			cfg.Audio.Cancel(cause)
			return

		case audioErr := <-audioErrCh:
			// Mirrors TS `void sendAudio(socket).catch(finishWithError)`: a
			// failure pumping audio (reading the caller's AudioStream, or
			// writing to the WebSocket) terminates the stream with an error
			// instead of being silently dropped.
			s.SetErr(audioErr)
			cfg.Audio.Cancel(audioErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if wsutil.IsCleanClose(res.Err) {
					// A clean close with no completed/error event yet is a
					// normal end of stream, not a failure (TS onClose calls
					// controller.close(), not controller.error(), when the
					// stream isn't already finished).
					cfg.Audio.Cancel(nil)
					return
				}
				realtimeErr := errors.New("OpenAI realtime transcription error")
				s.SetErr(realtimeErr)
				cfg.Audio.Cancel(realtimeErr)
				return
			}

			var raw map[string]interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &raw); jsonErr != nil {
				continue
			}
			if cfg.IncludeRawChunks {
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: raw}) {
					return
				}
			}

			eventType, _ := raw["type"].(string)
			switch eventType {
			case "conversation.item.input_audio_transcription.delta":
				itemID, _ := raw["item_id"].(string)
				delta, _ := raw["delta"].(string)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: itemID, Delta: delta}) {
					return
				}

			case "conversation.item.input_audio_transcription.completed":
				itemID, _ := raw["item_id"].(string)
				transcript, _ := raw["transcript"].(string)
				if itemID != "" {
					if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: itemID, Text: transcript}) {
						return
					}
				}
				langCopy := cfg.Language
				finish := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: transcript, Language: langCopy}
				s.Emit(finish)
				return

			case "error":
				message := "OpenAI realtime error"
				if errObj, ok := raw["error"].(map[string]interface{}); ok {
					if m, ok := errObj["message"].(string); ok && m != "" {
						message = m
					}
				}
				streamErr := errors.New(message)
				s.SetErr(streamErr)
				cfg.Audio.Cancel(streamErr)
				return
			}
		}
	}
}

// pumpAudio reads chunks from audio and forwards them as
// input_audio_buffer.append messages, committing the buffer at EOF. Any other
// failure — reading from the AudioStream, or writing to the WebSocket — is
// reported on errCh via the shared wsutil.PumpAudio loop, mirroring TS's
// `void sendAudio(socket).catch(finishWithError)` (a rejected
// `audioReader.read()` fails the stream exactly like a failed `socket.send`).
func (s *openAIRealtimeTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	wsutil.PumpAudio(s.Context(), audio,
		func(chunk []byte) error {
			msg, marshalErr := json.Marshal(map[string]string{
				"type":  "input_audio_buffer.append",
				"audio": base64.StdEncoding.EncodeToString(chunk),
			})
			if marshalErr != nil {
				return nil
			}
			return s.send(conn, msg)
		},
		func() error {
			return s.send(conn, []byte(`{"type":"input_audio_buffer.commit"}`))
		},
		errCh,
	)
}

func (s *openAIRealtimeTranscriptionStream) dial(wsURL string, headers map[string]string) (*websocket.Conn, error) {
	protocols, filteredHeaders := openAIRealtimeWSAuth(headers)
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: filteredHeaders, Protocols: protocols})
}

func (s *openAIRealtimeTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionModel    = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*openAIRealtimeTranscriptionStream)(nil)
)
