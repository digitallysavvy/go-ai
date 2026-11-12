package mistral

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/urlnorm"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// mistralRealtimeAudioChunkBytes keeps each input_audio.append message below
// Mistral's 256 KiB limit, including base64 expansion (TS 64 * 1024).
const mistralRealtimeAudioChunkBytes = 64 * 1024

// DoStream streams a transcript for live PCM audio over the Mistral realtime
// transcription WebSocket (voxtral-mini-transcribe-realtime-3 and -3-0).
// Mirrors TS MistralTranscriptionModel#doStream / streamMistralTranscription
// (b26902ea1a).
//
// The flow: connect to /v1/audio/transcriptions/realtime?model=<id>, send
// session.update (the snake_case session) on session.created, stream the
// audio as base64 input_audio.append messages of at most 64 KiB after
// session.updated, then input_audio.end. transcription.text.delta becomes a
// transcript-delta part, transcription.segment a transcript-final part, and
// transcription.done a response-metadata and a finish part.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	currentDate := time.Now()
	mistralOpts, err := parseMistralTranscriptionOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}
	rate := 16000
	if opts.InputAudioFormat.Rate != nil {
		rate = *opts.InputAudioFormat.Rate
	}
	if opts.InputAudioFormat.Type != "audio/pcm" || rate <= 0 {
		return nil, providererrors.NewArgumentError("inputAudioFormat", "Mistral realtime transcription requires audio/pcm (mono, signed 16-bit little-endian) with a positive integer sample rate.", nil)
	}

	warnings := []types.Warning{}
	if mistralOpts != nil {
		for _, option := range []struct {
			name string
			set  bool
		}{
			{"returnLanguage", mistralOpts.ReturnLanguage != nil},
			{"temperature", mistralOpts.Temperature != nil},
			{"timestampGranularities", mistralOpts.TimestampGranularities != nil},
		} {
			if option.set {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions.mistral." + option.name,
					Details: "This option is not supported for realtime transcription.",
				})
			}
		}
	}

	session := mistralRealtimeTranscriptionSession(rate, mistralOpts)

	// TS combineHeaders(await resolve(config.headers), options.headers); the
	// JSON Content-Type of the REST client is not a handshake header.
	baseHeaders, err := m.provider.client.RequestHeadersCtx(ctx)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	for k, v := range internalhttp.MergeHeaders(baseHeaders, opts.Headers) {
		if strings.EqualFold(k, "Content-Type") {
			if _, callSet := opts.Headers[k]; !callSet {
				continue
			}
		}
		headers[k] = v
	}

	wsURL, err := mistralRealtimeTranscriptionURL(m.provider.baseURL, m.modelID)
	if err != nil {
		return nil, err
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}
	stream := &mistralRealtimeTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](abortCtx)}
	go stream.run(mistralRealtimeTranscriptionConfig{
		url:              wsURL,
		headers:          headers,
		session:          session,
		modelID:          m.modelID,
		warnings:         warnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: wsURL,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: currentDate, ModelID: m.modelID},
	}, nil
}

// mistralRealtimeTranscriptionURL is TS
// toWebSocketUrl(`${baseURL}/audio/transcriptions/realtime`) with
// searchParams.set('model', modelId).
func mistralRealtimeTranscriptionURL(baseURL, modelID string) (string, error) {
	u, err := url.Parse(baseURL + "/v1/audio/transcriptions/realtime")
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	u.RawQuery = internalhttp.SetSearchParam(u.RawQuery, "model", modelID)
	u.ForceQuery = false
	if href, err := urlnorm.Href(u.String()); err == nil {
		return href, nil
	}
	return u.String(), nil
}

// mistralRealtimeSession is the TS `session` object
// (mistral-transcription-stream.ts 66-84), in its key order; undefined
// members are omitted.
type mistralRealtimeSession struct {
	AudioFormat            mistralRealtimeAudioFormat    `json:"audio_format"`
	TargetStreamingDelayMs *float64                      `json:"target_streaming_delay_ms,omitempty"`
	Language               *string                       `json:"language,omitempty"`
	ContextBias            *[]string                     `json:"context_bias,omitempty"`
	Diarize                *bool                         `json:"diarize,omitempty"`
	SpeakerReferences      *map[string]string            `json:"speaker_references,omitempty"`
	TurnDetection          *mistralRealtimeTurnDetection `json:"turn_detection,omitempty"`
}

type mistralRealtimeAudioFormat struct {
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

type mistralRealtimeTurnDetection struct {
	StartThreshold    *float64 `json:"start_threshold,omitempty"`
	EagerEndThreshold *float64 `json:"eager_end_threshold,omitempty"`
	EndThreshold      *float64 `json:"end_threshold,omitempty"`
	MinSilenceMs      *float64 `json:"min_silence_ms,omitempty"`
	EndTimeoutMs      *float64 `json:"end_timeout_ms,omitempty"`
}

func mistralRealtimeTranscriptionSession(rate int, o *mistralTranscriptionOptions) mistralRealtimeSession {
	session := mistralRealtimeSession{AudioFormat: mistralRealtimeAudioFormat{Encoding: "pcm_s16le", SampleRate: rate}}
	if o == nil {
		return session
	}
	session.TargetStreamingDelayMs = o.TargetStreamingDelayMs
	if o.Language != "" {
		session.Language = &o.Language
	}
	if o.ContextBias != nil {
		session.ContextBias = &o.ContextBias
	}
	session.Diarize = o.Diarize
	if o.SpeakerReferences != nil {
		session.SpeakerReferences = &o.SpeakerReferences
	}
	if o.TurnDetection != nil {
		td := *o.TurnDetection
		session.TurnDetection = &td
	}
	return session
}

type mistralRealtimeTranscriptionConfig struct {
	url              string
	headers          map[string]string
	session          mistralRealtimeSession
	modelID          string
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
}

// mistralRealtimeTranscriptionStream implements provider.TranscriptionStream
// over the Mistral realtime transcription WebSocket.
type mistralRealtimeTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

// mistralRealtimeEvent is the TS transcriptionEventSchema union.
type mistralRealtimeSegment struct {
	Text      *string  `json:"text"`
	Start     *float64 `json:"start"`
	End       *float64 `json:"end"`
	SpeakerID *string  `json:"speaker_id"`
}

type mistralRealtimeEvent struct {
	Type string `json:"type"`
	mistralRealtimeSegment
	Model    *string                  `json:"model"`
	Language *string                  `json:"language"`
	Segments []mistralRealtimeSegment `json:"segments"`
	Usage    map[string]interface{}   `json:"usage"`
	Error    *struct {
		Message *string `json:"message"`
	} `json:"error"`
}

var mistralRealtimeHandledEvents = map[string]bool{
	"session.created":          true,
	"session.updated":          true,
	"transcription.text.delta": true,
	"transcription.segment":    true,
	"transcription.done":       true,
	"error":                    true,
}

// validate is TS safeValidateTypes(transcriptionEventSchema) for the members
// Go reads.
func (e *mistralRealtimeEvent) validate(raw map[string]interface{}) error {
	invalid := func(path string) error {
		return fmt.Errorf("Type validation failed: invalid %s in %s event", path, e.Type) //nolint:staticcheck // TS TypeValidationError prefix
	}
	isNum := func(v interface{}) bool { _, ok := v.(float64); return ok }
	isStr := func(v interface{}) bool { _, ok := v.(string); return ok }
	nullish := func(m map[string]interface{}, k string, ok func(interface{}) bool) bool {
		v, present := m[k]
		return !present || v == nil || ok(v)
	}
	checkSegment := func(m map[string]interface{}, path string) error {
		if !isStr(m["text"]) {
			return invalid(path + "text")
		}
		for _, k := range []string{"start", "end"} {
			if !nullish(m, k, isNum) {
				return invalid(path + k)
			}
		}
		if !nullish(m, "speaker_id", isStr) {
			return invalid(path + "speaker_id")
		}
		return nil
	}
	switch e.Type {
	case "transcription.text.delta":
		if !isStr(raw["text"]) {
			return invalid("text")
		}
	case "transcription.segment":
		return checkSegment(raw, "")
	case "transcription.done":
		if !isStr(raw["text"]) {
			return invalid("text")
		}
		for _, k := range []string{"model", "language"} {
			if !nullish(raw, k, isStr) {
				return invalid(k)
			}
		}
		if v, present := raw["segments"]; present && v != nil {
			list, ok := v.([]interface{})
			if !ok {
				return invalid("segments")
			}
			for i, item := range list {
				m, ok := item.(map[string]interface{})
				if !ok {
					return invalid(fmt.Sprintf("segments.%d", i))
				}
				if err := checkSegment(m, fmt.Sprintf("segments.%d.", i)); err != nil {
					return err
				}
			}
		}
		if v, present := raw["usage"]; present && v != nil {
			if _, ok := v.(map[string]interface{}); !ok {
				return invalid("usage")
			}
		}
	case "error":
		errObj, ok := raw["error"].(map[string]interface{})
		if !ok || !isStr(errObj["message"]) {
			return invalid("error.message")
		}
	}
	return nil
}

func (s *mistralRealtimeTranscriptionStream) run(cfg mistralRealtimeTranscriptionConfig) {
	defer s.CloseParts()
	defer s.CancelContext()

	var cancelOnce sync.Once
	cancelAudio := func(reason error) {
		if cfg.audio != nil {
			cancelOnce.Do(func() { cfg.audio.Cancel(reason) })
		}
	}
	fail := func(err error) {
		s.SetErr(err)
		cancelAudio(err)
	}

	// TS enqueues stream-start before connecting.
	if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		fail(context.Cause(s.Context()))
		return
	}

	closeInfo := &wsutil.CloseInfo{}
	conn, err := wsutil.Dial(s.Context(), cfg.url, wsutil.DialOptions{Headers: cfg.headers, WrapConn: closeInfo.Wrap})
	if err != nil {
		if ctxErr := context.Cause(s.Context()); ctxErr != nil {
			fail(ctxErr)
		} else {
			fail(errors.New("Mistral realtime transcription WebSocket error.")) //nolint:staticcheck // TS error text
		}
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck

	audioErrCh := make(chan error, 1)
	configured, sending := false, false

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	for {
		select {
		case <-s.Context().Done():
			fail(context.Cause(s.Context()))
			return

		case audioErr := <-audioErrCh:
			fail(audioErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if wsutil.IsCleanClose(res.Err) {
					code := strconv.Itoa(closeInfo.Code())
					reason := ""
					if r := closeInfo.Reason(); r != "" {
						reason = ", reason: " + r
					}
					fail(fmt.Errorf("Mistral realtime transcription connection closed before transcription.done (code: %s%s).", code, reason)) //nolint:staticcheck // TS error text
				} else {
					fail(errors.New("Mistral realtime transcription WebSocket error.")) //nolint:staticcheck // TS error text
				}
				return
			}

			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(res.Text), &raw); err != nil {
				fail(fmt.Errorf("JSON parsing failed: Text: %s.\nError message: %w", res.Text, err))
				return
			}
			eventType, ok := raw["type"].(string)
			if !ok {
				fail(errors.New("Type validation failed: expected an object with a string type")) //nolint:staticcheck // TS TypeValidationError prefix
				return
			}
			if cfg.includeRawChunks {
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: raw}) {
					fail(context.Cause(s.Context()))
					return
				}
			}
			// Unknown events (including turn events) remain available as raw
			// chunks.
			if !mistralRealtimeHandledEvents[eventType] {
				continue
			}
			var event mistralRealtimeEvent
			_ = jsonexact.Unmarshal([]byte(res.Text), &event)
			if err := event.validate(raw); err != nil {
				fail(err)
				return
			}

			switch eventType {
			case "session.created":
				if configured {
					continue
				}
				configured = true
				update, _ := jsonstr.Marshal(struct {
					Type    string                 `json:"type"`
					Session mistralRealtimeSession `json:"session"`
				}{"session.update", cfg.session})
				if err := wsutil.Send(s.Context(), conn, string(update)); err != nil {
					fail(err)
					return
				}

			case "session.updated":
				if !configured || sending {
					continue
				}
				sending = true
				go s.pumpAudio(conn, cfg.audio, audioErrCh)

			case "transcription.text.delta":
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeDelta, Delta: *event.Text}) {
					fail(context.Cause(s.Context()))
					return
				}

			case "transcription.segment":
				part := provider.TranscriptionStreamPart{
					Type:        provider.TranscriptionStreamPartTypeFinal,
					Text:        *event.Text,
					StartSecond: event.Start,
					EndSecond:   event.End,
				}
				if event.SpeakerID != nil {
					part.ProviderMetadata = map[string]interface{}{"mistral": map[string]interface{}{"speakerId": *event.SpeakerID}}
				}
				if !s.Emit(part) {
					fail(context.Cause(s.Context()))
					return
				}

			case "transcription.done":
				modelID := cfg.modelID
				if event.Model != nil {
					modelID = *event.Model
				}
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeResponseMetadata, ModelID: modelID, Body: raw}) {
					fail(context.Cause(s.Context()))
					return
				}
				s.Emit(mistralRealtimeFinishPart(event))
				cancelAudio(nil)
				_ = conn.WriteClose(1000)
				return

			case "error":
				fail(errors.New(*event.Error.Message))
				return
			}
		}
	}
}

// mistralRealtimeFinishPart is the TS finish part of transcription.done.
func mistralRealtimeFinishPart(event mistralRealtimeEvent) provider.TranscriptionStreamPart {
	part := provider.TranscriptionStreamPart{
		Type:       provider.TranscriptionStreamPartTypeFinish,
		FinishText: *event.Text,
		Segments:   []provider.TranscriptSegment{},
		Usage:      event.Usage,
	}
	if event.Language != nil {
		part.Language = *event.Language
	}
	if seconds, ok := event.Usage["prompt_audio_seconds"].(float64); ok {
		part.DurationInSeconds = &seconds
	}
	metaSegments := make([]interface{}, 0, len(event.Segments))
	for _, seg := range event.Segments {
		if seg.Start != nil && seg.End != nil {
			part.Segments = append(part.Segments, provider.TranscriptSegment{Text: *seg.Text, StartSecond: *seg.Start, EndSecond: *seg.End})
		}
		m := map[string]interface{}{"text": *seg.Text}
		if seg.Start != nil {
			m["startSecond"] = *seg.Start
		}
		if seg.End != nil {
			m["endSecond"] = *seg.End
		}
		if seg.SpeakerID != nil {
			m["speakerId"] = *seg.SpeakerID
		}
		metaSegments = append(metaSegments, m)
	}
	mistral := map[string]interface{}{"segments": metaSegments}
	if event.Usage != nil {
		mistral["usage"] = event.Usage
	}
	part.ProviderMetadata = map[string]interface{}{"mistral": mistral}
	return part
}

// pumpAudio is TS sendAudio: each chunk goes out in input_audio.append
// messages of at most 64 KiB of audio, then input_audio.end.
func (s *mistralRealtimeTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	if audio == nil {
		if err := wsutil.Send(s.Context(), conn, `{"type":"input_audio.end"}`); err != nil && s.Context().Err() == nil {
			wsutil.ReportError(s.Context(), errCh, err)
		}
		return
	}
	wsutil.PumpAudio(s.Context(), audio,
		func(chunk []byte) error {
			for offset := 0; offset < len(chunk); offset += mistralRealtimeAudioChunkBytes {
				end := min(offset+mistralRealtimeAudioChunkBytes, len(chunk))
				msg, _ := json.Marshal(map[string]string{
					"type":  "input_audio.append",
					"audio": base64.StdEncoding.EncodeToString(chunk[offset:end]),
				})
				if err := wsutil.Send(s.Context(), conn, string(msg)); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			return wsutil.Send(s.Context(), conn, `{"type":"input_audio.end"}`)
		},
		errCh,
	)
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*mistralRealtimeTranscriptionStream)(nil)
)
