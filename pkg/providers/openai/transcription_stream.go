package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// bearerToken extracts the token from an "Authorization: Bearer <token>"
// header value like TS getOpenAIRealtimeConnection's
// `authorization.match(/^bearer\s+(.+)$/i)?.[1]`: the scheme name is matched
// case-insensitively, JS \s is any JS whitespace or line terminator (U+00A0,
// U+2003, U+FEFF, \v, ...; RE2 \s is ASCII only), and the token is the rest
// of the value, which must hold no line terminator (JS .). When only
// whitespace follows, the regexp backtracks and the token is its last
// character.
func bearerToken(v string) (string, bool) {
	const scheme = "bearer"
	if len(v) < len(scheme) || !strings.EqualFold(v[:len(scheme)], scheme) {
		return "", false
	}
	rest := v[len(scheme):]
	token := strings.TrimLeftFunc(rest, providerutils.IsJSWhiteSpace)
	if len(token) == len(rest) {
		return "", false // \s+ needs one character
	}
	if token == "" {
		_, size := utf8.DecodeLastRuneInString(rest)
		if size == len(rest) {
			return "", false // \s+ and .+ need a character each
		}
		token = rest[len(rest)-size:]
	}
	if strings.ContainsAny(token, "\n\r\u2028\u2029") {
		return "", false
	}
	return token, true
}

// IsRealtimeTranscriptionModelID reports whether modelID only streams over
// the OpenAI realtime WebSocket and has no REST transcription
// (gpt-realtime-whisper and, since TS 8375141ef4, gpt-live-transcribe, with
// their dated snapshots): DoTranscribe rejects these models. Models that
// support both, such as gpt-transcribe, are not included; use
// SupportsStreamingTranscription for the streaming check.
func IsRealtimeTranscriptionModelID(modelID string) bool {
	return getOpenAITranscriptionModelEntry(modelID).rest == transcriptionSupportNo
}

// DoStream streams a transcript for live audio over the OpenAI realtime
// WebSocket (gpt-realtime-whisper, gpt-live-transcribe, gpt-transcribe and
// dated snapshots). Mirrors TS OpenAITranscriptionModel#doStream /
// createOpenAIRealtimeTranscriptionStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if !SupportsStreamingTranscription(m.modelID) {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("streaming transcription with %s", m.modelID),
		}
	}

	if err := validateTranscriptionProviderOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	streamOpts := TranscriptionStreamOpenAIOptions(opts.ProviderOptions)

	warnings := StreamingTranscriptionWarnings(m.modelID, streamOpts)

	baseHeaders, err := m.baseWSHeaders(ctx)
	if err != nil {
		return nil, err
	}
	headers := internalhttp.MergeHeaders(baseHeaders, opts.Headers)
	sessionUpdate := BuildOpenAIRealtimeTranscriptionSession(m.modelID, opts.InputAudioFormat, streamOpts)
	wsURL, err := realtimeTranscriptionWebSocketURL(m.provider.config.BaseURL)
	if err != nil {
		return nil, err
	}

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
func (m *TranscriptionModel) baseWSHeaders(ctx context.Context) (map[string]string, error) {
	return m.provider.client.RequestHeadersCtx(ctx)
}

func UnsupportedStreamingTranscriptionWarning(option string) types.Warning {
	return types.Warning{
		Type:    "unsupported",
		Feature: "providerOptions.openai." + option,
		Details: fmt.Sprintf("OpenAI streaming transcription does not support %s.", option),
	}
}

// StreamingTranscriptionWarnings returns TS doStream's warnings for
// modelID: one "unsupported" warning per REST-only option that is set
// (include, languages, keywords, prompt, temperature,
// timestampGranularities, in that order), then, for a model without a
// streaming delay setting (gpt-transcribe), one for streaming.delay
// (TS 8375141ef4, 226b7daf82).
func StreamingTranscriptionWarnings(modelID string, opts TranscriptionStreamOpenAIOptionsValue) []types.Warning {
	var warnings []types.Warning
	for _, option := range []struct {
		name string
		set  bool
	}{
		{"include", opts.HasInclude},
		{"languages", opts.HasLanguages},
		{"keywords", opts.HasKeywords},
		{"prompt", opts.HasPrompt},
		{"temperature", opts.HasTemperature},
		{"timestampGranularities", opts.HasTimestampGranularities},
	} {
		if option.set {
			warnings = append(warnings, UnsupportedStreamingTranscriptionWarning(option.name))
		}
	}
	if !supportsStreamingTranscriptionDelay(modelID) && (opts.StreamingDelayLevel != "" || opts.StreamingDelay != nil) {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.openai.streaming.delay",
			Details: fmt.Sprintf("OpenAI streaming transcription with %s does not support delay.", modelID),
		})
	}
	return warnings
}

// supportsStreamingTranscriptionDelay is TS
// `capabilities.transcription.streamingDelay === true`.
func supportsStreamingTranscriptionDelay(modelID string) bool {
	return getOpenAITranscriptionModelEntry(modelID).streamingDelay == transcriptionSupportYes
}

func realtimeTranscriptionWebSocketURL(baseURL string) (string, error) {
	return openAIToWebSocketURL(baseURL + "/realtime?intent=transcription")
}

// openAIToWebSocketURL is TS toWebSocketUrl (new URL, http: to ws:, https:
// to wss:): the WHATWG serialization, "Invalid URL" for a URL it rejects.
func openAIToWebSocketURL(rawURL string) (string, error) {
	return wsutil.ToWebSocketURL(rawURL)
}

// BuildOpenAIRealtimeTranscriptionSession mirrors TS
// BuildOpenAIRealtimeTranscriptionSession.
func BuildOpenAIRealtimeTranscriptionSession(modelID string, format provider.AudioFormat, opts TranscriptionStreamOpenAIOptionsValue) map[string]interface{} {
	inputFormat := map[string]interface{}{"type": format.Type}
	if format.Rate != nil {
		inputFormat["rate"] = *format.Rate
	}

	transcription := map[string]interface{}{"model": modelID}
	// TS `providerOptions?.language != null`: a present "" is sent too.
	if opts.Language != "" || opts.HasLanguage {
		// gpt-live-transcribe and gpt-transcribe take a languages list
		// (TS 8375141ef4 languageField).
		if getOpenAITranscriptionModelEntry(modelID).languageField == "languages" {
			transcription["languages"] = []string{opts.Language}
		} else {
			transcription["language"] = opts.Language
		}
	}
	// TS: streaming.delay is an enum string (minimal|low|medium|high|xhigh),
	// sent only to models with a delay setting.
	if supportsStreamingTranscriptionDelay(modelID) {
		if opts.StreamingDelayLevel != "" {
			transcription["delay"] = opts.StreamingDelayLevel
		} else if opts.StreamingDelay != nil {
			transcription["delay"] = *opts.StreamingDelay
		}
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
	if opts.StreamingInclude != nil {
		session["include"] = opts.StreamingInclude
	}
	return map[string]interface{}{"type": "session.update", "session": session}
}

// TranscriptionStreamOpenAIOptionsValue holds providerOptions.openai fields
// relevant to streaming transcription, plus flags for REST-only fields that
// warn when set during streaming.
type TranscriptionStreamOpenAIOptionsValue struct {
	Language               string
	StreamingDelay         *int // Deprecated: TS streaming.delay is a string; use StreamingDelayLevel.
	StreamingDelayLevel    string
	StreamingInclude       []string
	HasInclude             bool
	Prompt                 string
	HasTemperature         bool
	TimestampGranularities []string
	// HasPrompt and HasTimestampGranularities mirror TS's `!= null` checks on
	// the raw options: an empty prompt or an empty array still warns.
	HasPrompt                 bool
	HasTimestampGranularities bool
	// HasLanguages and HasKeywords flag the REST-only languages and keywords
	// options (TS 226b7daf82).
	HasLanguages bool
	HasKeywords  bool
	// HasLanguage is true when providerOptions.openai.language holds a
	// string, "" included; BuildOpenAIRealtimeTranscriptionSession then sends
	// it like TS `language != null`.
	HasLanguage bool
}

func TranscriptionStreamOpenAIOptions(providerOptions map[string]interface{}) TranscriptionStreamOpenAIOptionsValue {
	var result TranscriptionStreamOpenAIOptionsValue
	if providerOptions == nil {
		return result
	}
	openaiOpts, ok := providerutils.NormalizeProviderOptionsObject(providerOptions["openai"])
	if !ok {
		return result
	}
	// Read Go-typed values ([]string, map[string]string, ...) like their JSON
	// form, the form TS parses (a []string streaming.include was dropped).
	openaiOpts = providerutils.NormalizeOptionsJSON(openaiOpts)
	if v, ok := openaiOpts["language"].(string); ok {
		result.Language, result.HasLanguage = v, true
	}
	if v, ok := openaiOpts["prompt"].(string); ok {
		result.Prompt = v
	}
	result.HasPrompt = openaiOpts["prompt"] != nil
	result.HasTemperature = openaiOpts["temperature"] != nil
	result.HasInclude = openaiOpts["include"] != nil
	result.HasTimestampGranularities = openaiOpts["timestampGranularities"] != nil
	result.HasLanguages = openaiOpts["languages"] != nil
	result.HasKeywords = openaiOpts["keywords"] != nil
	if v, ok := openaiOpts["timestampGranularities"].([]interface{}); ok {
		for _, item := range v {
			if s, ok := item.(string); ok {
				result.TimestampGranularities = append(result.TimestampGranularities, s)
			}
		}
	}
	if streaming, ok := openaiOpts["streaming"].(map[string]interface{}); ok {
		if level, ok := streaming["delay"].(string); ok {
			result.StreamingDelayLevel = level
		} else if v, ok := numericValue(streaming["delay"]); ok {
			d := int(v)
			result.StreamingDelay = &d
		}
		if v, ok := streaming["include"].([]interface{}); ok {
			result.StreamingInclude = []string{}
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
	return goNumber(v)
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
			if t, ok := bearerToken(v); ok {
				token = t
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
			cause := context.Cause(s.Context()) // TS onAbort(abortSignal.reason)
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

			// TS safeParseJSON: any JSON value (not only an object) is a
			// message; the raw part carries it as parsed.
			var rawValue interface{}
			if jsonErr := json.Unmarshal([]byte(res.Text), &rawValue); jsonErr != nil {
				continue
			}
			if cfg.IncludeRawChunks {
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: rawValue}) {
					return
				}
			}
			raw, _ := rawValue.(map[string]interface{})

			eventType, _ := raw["type"].(string)
			switch eventType {
			case "conversation.item.input_audio_transcription.delta":
				itemID, _ := raw["item_id"].(string)
				delta, _ := raw["delta"].(string)
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, ID: itemID, Delta: delta}) {
					return
				}

			case "conversation.item.input_audio_transcription.completed":
				// TS finish(text, id): the final part is sent when
				// item_id != null, an empty id included.
				itemID, hasItemID := raw["item_id"].(string)
				transcript, _ := raw["transcript"].(string)
				if hasItemID {
					if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: itemID, Text: transcript}) {
						return
					}
				}
				langCopy := cfg.Language
				finish := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinish, FinishText: transcript, Language: langCopy}
				s.Emit(finish)
				return

			case "error":
				// TS `raw.error?.message ?? 'OpenAI realtime error'`: an empty
				// message is kept.
				message := "OpenAI realtime error"
				if errObj, ok := raw["error"].(map[string]interface{}); ok {
					// new Error(x) takes String() of any non-null message.
					if m := errObj["message"]; m != nil {
						message = providerutils.JSString(m)
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
