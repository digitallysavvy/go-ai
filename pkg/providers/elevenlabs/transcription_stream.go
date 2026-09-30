package elevenlabs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"github.com/digitallysavvy/go-ai/pkg/version"
	"golang.org/x/net/websocket"
)

// finalCommitGracePeriod is the quiet window after a post-input-end commit
// event during which a paired ..._with_timestamps companion event is still
// awaited before finishing, mirroring TS finalCommitGracePeriodMs.
const finalCommitGracePeriod = 250 * time.Millisecond

// elevenLabsRealtimeErrorTypes are message_type values ElevenLabs' realtime
// endpoint uses for error conditions (TS elevenLabsRealtimeErrorTypes).
var elevenLabsRealtimeErrorTypes = map[string]bool{
	"auth_error":                  true,
	"chunk_size_exceeded":         true,
	"commit_throttled":            true,
	"error":                       true,
	"input_error":                 true,
	"insufficient_audio_activity": true,
	"queue_overflow":              true,
	"quota_exceeded":              true,
	"rate_limited":                true,
	"resource_exhausted":          true,
	"session_time_limit_exceeded": true,
	"transcriber_error":           true,
	"unaccepted_terms":            true,
}

// elevenLabsLateFinalizationErrorTypes are error message_types that, once at
// least one commit has been observed after the input audio ended, mean the
// server considers the session already finalized rather than failed (TS
// elevenLabsLateFinalizationErrorTypes).
var elevenLabsLateFinalizationErrorTypes = map[string]bool{
	"commit_throttled":            true,
	"input_error":                 true,
	"insufficient_audio_activity": true,
}

// isRealtimeTranscriptionModelID reports whether modelID is the realtime
// (WebSocket-only) Scribe v2 variant.
func isRealtimeTranscriptionModelID(modelID string) bool {
	return modelID == ModelScribeV2Realtime
}

// elevenLabsRealtimeAudioFormat maps an inputAudioFormat to ElevenLabs'
// `audio_format` query value and the sample rate to send with each chunk,
// mirroring TS getElevenLabsRealtimeAudioFormat.
func elevenLabsRealtimeAudioFormat(format provider.AudioFormat) (audioFormat string, sampleRate int, err error) {
	t := strings.ToLower(format.Type)

	if t == "audio/pcmu" {
		if format.Rate != nil && *format.Rate != 8000 {
			return "", 0, &providererrors.InvalidArgumentError{
				Field:   "inputAudioFormat",
				Message: "ElevenLabs only supports audio/pcmu at 8000 Hz",
			}
		}
		return "ulaw_8000", 8000, nil
	}

	supportedPCMRates := map[int]bool{8000: true, 16000: true, 22050: true, 24000: true, 44100: true, 48000: true}
	pcmRate := 16000
	if format.Rate != nil {
		pcmRate = *format.Rate
	}
	if t != "audio/pcm" || !supportedPCMRates[pcmRate] {
		return "", 0, &providererrors.InvalidArgumentError{
			Field: "inputAudioFormat",
			Message: "ElevenLabs realtime transcription supports audio/pcm at 8000, 16000, 22050, 24000, 44100, " +
				"or 48000 Hz, and audio/pcmu at 8000 Hz",
		}
	}
	return fmt.Sprintf("pcm_%d", pcmRate), pcmRate, nil
}

// buildElevenLabsRealtimeURL builds the wss:// realtime transcription URL
// with query parameters, mirroring TS buildElevenLabsRealtimeTranscriptionUrl.
func buildElevenLabsRealtimeURL(baseURL, modelID, audioFormat string, languageCode *string, streaming *StreamingOptions) (*url.URL, error) {
	full := strings.TrimRight(baseURL, "/") + "/v1/speech-to-text/realtime"
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
	q.Set("model_id", modelID)
	q.Set("audio_format", audioFormat)

	includeDetailedCommit := streaming != nil &&
		(boolValue(streaming.IncludeTimestamps) || boolValue(streaming.IncludeLanguageDetection))

	if streaming != nil {
		if streaming.CommitStrategy != "" {
			q.Set("commit_strategy", streaming.CommitStrategy)
		}
		if streaming.EnableLogging != nil {
			q.Set("enable_logging", strconv.FormatBool(*streaming.EnableLogging))
		}
		if streaming.FilterBackgroundAudio != nil {
			q.Set("filter_background_audio", strconv.FormatBool(*streaming.FilterBackgroundAudio))
		}
		if streaming.IncludeLanguageDetection != nil {
			q.Set("include_language_detection", strconv.FormatBool(*streaming.IncludeLanguageDetection))
		}
	}
	if includeDetailedCommit {
		q.Set("include_timestamps", "true")
	} else if streaming != nil && streaming.IncludeTimestamps != nil {
		q.Set("include_timestamps", strconv.FormatBool(*streaming.IncludeTimestamps))
	}
	if languageCode != nil {
		q.Set("language_code", *languageCode)
	}
	if streaming != nil {
		if streaming.MinSilenceDurationMs != nil {
			q.Set("min_silence_duration_ms", strconv.Itoa(*streaming.MinSilenceDurationMs))
		}
		if streaming.MinSpeechDurationMs != nil {
			q.Set("min_speech_duration_ms", strconv.Itoa(*streaming.MinSpeechDurationMs))
		}
		if streaming.NoVerbatim != nil {
			q.Set("no_verbatim", strconv.FormatBool(*streaming.NoVerbatim))
		}
		if streaming.VadSilenceThresholdSecs != nil {
			q.Set("vad_silence_threshold_secs", formatFloatParam(*streaming.VadSilenceThresholdSecs))
		}
		if streaming.VadThreshold != nil {
			q.Set("vad_threshold", formatFloatParam(*streaming.VadThreshold))
		}
		for _, keyterm := range streaming.Keyterms {
			q.Add("keyterms", keyterm)
		}
		for _, lang := range streaming.SecondaryLanguages {
			q.Add("secondary_languages", lang)
		}
	}

	u.RawQuery = q.Encode()
	return u, nil
}

func formatFloatParam(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func boolValue(v *bool) bool { return v != nil && *v }

// stringValue dereferences an optional string, returning "" for nil. Used
// only for internal state (e.g. the detected-language seed) where "unset"
// and "" are already equivalent; wire-affecting call sites keep the pointer
// so an explicit "" is still distinguishable from "not set" (see
// TranscriptionModelOptions.LanguageCode / StreamingOptions.PreviousText).
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// DoStream streams a transcript for live audio over ElevenLabs' Scribe v2
// Realtime WebSocket endpoint. Mirrors TS
// ElevenLabsTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	if !isRealtimeTranscriptionModelID(m.modelID) {
		return nil, fmt.Errorf("%w: streaming transcription with %s", providererrors.ErrUnsupportedFeature, m.modelID)
	}

	elOpts, _, _ := extractTranscriptionOptions(opts.ProviderOptions)
	var streaming *StreamingOptions
	if elOpts != nil {
		streaming = elOpts.Streaming
	}

	warnings := []types.Warning{}
	if rawElevenlabs, ok := opts.ProviderOptions["elevenlabs"].(map[string]interface{}); ok {
		for _, optName := range []string{"diarize", "fileFormat", "numSpeakers", "tagAudioEvents", "timestampsGranularity"} {
			if v, present := rawElevenlabs[optName]; present && v != nil {
				warnings = append(warnings, types.Warning{
					Type:    "unsupported",
					Feature: "providerOptions.elevenlabs." + optName,
					Details: fmt.Sprintf("ElevenLabs realtime transcription does not support %s.", optName),
				})
			}
		}
	}

	includeTimestamps := streaming != nil && boolValue(streaming.IncludeTimestamps)
	includeLanguageDetection := streaming != nil && boolValue(streaming.IncludeLanguageDetection)

	// ElevenLabs documents filter_background_audio as incompatible with
	// include_timestamps. Language detection is delivered on the same
	// timestamp-bearing event, so it also requires include_timestamps here.
	if streaming != nil && boolValue(streaming.FilterBackgroundAudio) && (includeTimestamps || includeLanguageDetection) {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "providerOptions",
			Message: "providerOptions.elevenlabs.streaming.filterBackgroundAudio cannot be combined with includeTimestamps or includeLanguageDetection",
		}
	}

	audioFormat, sampleRate, err := elevenLabsRealtimeAudioFormat(opts.InputAudioFormat)
	if err != nil {
		return nil, err
	}

	var languageCode *string
	if elOpts != nil {
		languageCode = elOpts.LanguageCode
	}

	wsURL, err := buildElevenLabsRealtimeURL(m.provider.config.BaseURL, m.modelID, audioFormat, languageCode, streaming)
	if err != nil {
		return nil, err
	}

	// TS elevenlabs-transcription-model.ts's WebSocket connect reuses
	// `this.config.headers()` (combineHeaders(this.config.headers?.(),
	// options.headers) at elevenlabs-transcription-model.ts:313) -- the same
	// tagged getHeaders() used for regular HTTP requests, which carries the
	// `ai-sdk/elevenlabs/VERSION` tag. Match that here instead of building a
	// fresh, untagged header set.
	headers := version.WithUserAgentSuffix(internalhttp.MergeHeaders(map[string]string{
		"xi-api-key": m.provider.config.APIKey,
	}, opts.Headers), version.ProviderUserAgent("elevenlabs"))

	var previousText *string
	if streaming != nil {
		previousText = streaming.PreviousText
	}

	currentDate := time.Now()

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newElevenLabsRealtimeTranscriptionStream(abortCtx, elevenLabsRealtimeStreamConfig{
		url:                      wsURL,
		headers:                  headers,
		audio:                    opts.Audio,
		sampleRate:               sampleRate,
		previousText:             previousText,
		includeTimestamps:        includeTimestamps,
		includeLanguageDetection: includeLanguageDetection,
		includeRawChunks:         opts.IncludeRawChunks,
		language:                 stringValue(languageCode),
		warnings:                 warnings,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: wsURL.String(),
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: currentDate, ModelID: m.modelID},
	}, nil
}

type elevenLabsRealtimeStreamConfig struct {
	url                      *url.URL
	headers                  map[string]string
	audio                    provider.AudioStream
	sampleRate               int
	previousText             *string
	includeTimestamps        bool
	includeLanguageDetection bool
	includeRawChunks         bool
	language                 string
	warnings                 []types.Warning
}

// elevenLabsRealtimeTranscriptionStream implements provider.TranscriptionStream
// over ElevenLabs' Scribe v2 Realtime WebSocket, mirroring TS
// createElevenLabsRealtimeTranscriptionStream. The Next/Err/Close/emit/setErr
// plumbing is the shared wsutil.Session core; pumpAudio's previous_text-on-
// first-chunk special case doesn't fit the shared wsutil.PumpAudio loop's
// uniform per-chunk callback cleanly, so it stays local.
type elevenLabsRealtimeTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func newElevenLabsRealtimeTranscriptionStream(parentCtx context.Context, cfg elevenLabsRealtimeStreamConfig) *elevenLabsRealtimeTranscriptionStream {
	s := &elevenLabsRealtimeTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

// elevenLabsRealtimeWord is one entry of a transcript event's `words` array.
type elevenLabsRealtimeWord struct {
	Text  string   `json:"text,omitempty"`
	Start *float64 `json:"start,omitempty"`
	End   *float64 `json:"end,omitempty"`
	Type  string   `json:"type,omitempty"`
}

// elevenLabsRealtimeEvent is a server->client realtime transcription event,
// mirroring TS ElevenLabsRealtimeTranscriptionEvent.
type elevenLabsRealtimeEvent struct {
	MessageType  string                   `json:"message_type,omitempty"`
	SessionID    string                   `json:"session_id,omitempty"`
	Text         string                   `json:"text,omitempty"`
	LanguageCode *string                  `json:"language_code,omitempty"`
	Words        []elevenLabsRealtimeWord `json:"words,omitempty"`
	Error        string                   `json:"error,omitempty"`
}

// pumpAudio forwards audio chunks as input_audio_chunk messages (the first
// chunk carries previous_text, when set), signalling audioEnded and stopping
// once the AudioStream is exhausted. The main run() loop owns sending the
// final commit message so only one goroutine ever writes to conn at a time
// after this returns. Any other failure — reading from the AudioStream, or
// writing to the WebSocket — is reported on errCh, mirroring TS's
// `void sendAudio(socket).catch(finishWithError)` (a rejected
// `audioReader.read()` fails the stream exactly like a failed `socket.send`).
// A failure that stems from the session's context already being cancelled is
// not reported here: run()'s own select on that context's Done() already
// handles that case.
func (s *elevenLabsRealtimeTranscriptionStream) pumpAudio(conn *websocket.Conn, cfg elevenLabsRealtimeStreamConfig, audioEnded chan<- struct{}, errCh chan<- error) {
	firstChunk := true
	for {
		chunk, err := cfg.audio.Next(s.Context())
		if err != nil {
			if err == io.EOF {
				select {
				case audioEnded <- struct{}{}:
				case <-s.Context().Done():
				}
			} else if s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, err)
			}
			return
		}
		msg := map[string]interface{}{
			"message_type":  "input_audio_chunk",
			"audio_base_64": base64.StdEncoding.EncodeToString(chunk),
			"commit":        false,
			"sample_rate":   cfg.sampleRate,
		}
		if firstChunk && cfg.previousText != nil {
			msg["previous_text"] = *cfg.previousText
		}
		firstChunk = false
		payload, marshalErr := json.Marshal(msg)
		if marshalErr != nil {
			continue
		}
		if err := s.send(conn, payload); err != nil {
			if s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, err)
			}
			return
		}
	}
}

func (s *elevenLabsRealtimeTranscriptionStream) run(cfg elevenLabsRealtimeStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason instead of only on an explicit Close() call, which a consumer
	// that only drains Next() to io.EOF may never make.
	defer s.CancelContext()

	var finishTimer *time.Timer
	var finishTimerC <-chan time.Time
	stopFinishTimer := func() {
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
		stopFinishTimer()
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

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	audioEndedCh := make(chan struct{})
	audioErrCh := make(chan error, 1)

	var (
		finished                    bool
		detectedLanguage            = cfg.language
		endOfInput                  bool
		receivedPostInputCommit     bool
		segmentIndex                int
		sessionID                   string
		committedEventCount         int
		committedEventsAtEndOfInput int
		finalCommitEventCount       *int
		timestampedCommitCount      int
		finalSegments               []provider.TranscriptSegment
		finalTexts                  []string
	)
	expectDetailedCommit := cfg.includeTimestamps || cfg.includeLanguageDetection

	scheduleFinish := func() {
		stopFinishTimer()
		finishTimer = time.NewTimer(finalCommitGracePeriod)
		finishTimerC = finishTimer.C
	}

	segID := func() string {
		sid := sessionID
		if sid == "" {
			sid = "session"
		}
		return fmt.Sprintf("%s:%d", sid, segmentIndex)
	}

	finish := func() {
		if finished {
			return
		}
		finished = true
		stopFinishTimer()
		text := strings.TrimSpace(strings.Join(finalTexts, " "))
		segments := finalSegments
		if segments == nil {
			segments = []provider.TranscriptSegment{}
		}
		var duration *float64
		if len(finalSegments) > 0 {
			d := finalSegments[len(finalSegments)-1].EndSecond
			duration = &d
		}
		s.Emit(provider.TranscriptionStreamPart{
			Type:              provider.TranscriptionStreamPartTypeFinish,
			FinishText:        text,
			Segments:          segments,
			Language:          detectedLanguage,
			DurationInSeconds: duration,
		})
		cfg.audio.Cancel(nil)
	}

	for {
		select {
		case <-s.Context().Done():
			fail(s.Context().Err())
			return

		case <-finishTimerC:
			finishTimerC = nil
			finish()
			return

		case <-audioEndedCh:
			audioEndedCh = nil // consumed once; a nil channel blocks forever in future selects
			committedEventsAtEndOfInput = committedEventCount
			endOfInput = true
			payload, _ := json.Marshal(map[string]interface{}{
				"message_type": "input_audio_chunk", "audio_base_64": "", "commit": true, "sample_rate": cfg.sampleRate,
			})
			_ = s.send(conn, payload)

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
					// fails the stream, regardless of endOfInput/
					// receivedPostInputCommit — distinct from a clean close
					// (TS onClose), which finishes successfully once the
					// post-input commit has been observed.
					fail(errors.New("ElevenLabs realtime transcription error."))
				} else if endOfInput && receivedPostInputCommit {
					finish()
				} else {
					fail(errors.New("ElevenLabs realtime transcription stream closed before completion."))
				}
				return
			}
			if finished {
				continue
			}

			var raw elevenLabsRealtimeEvent
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

			if raw.MessageType != "" && elevenLabsRealtimeErrorTypes[raw.MessageType] {
				if endOfInput && (committedEventCount > 0 || timestampedCommitCount > 0) && elevenLabsLateFinalizationErrorTypes[raw.MessageType] {
					receivedPostInputCommit = true
					finish()
					return
				}
				msg := raw.Error
				if msg == "" {
					msg = "ElevenLabs realtime transcription error"
				}
				fail(errors.New(msg))
				return
			}

			switch raw.MessageType {
			case "session_started":
				sessionID = raw.SessionID
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
					return
				}
				go s.pumpAudio(conn, cfg, audioEndedCh, audioErrCh)

			case "partial_transcript":
				if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypePartial, ID: segID(), Text: raw.Text}) {
					return
				}

			case "final_transcript", "committed_transcript":
				committedEventCount++
				text := strings.TrimSpace(raw.Text)
				id := segID()
				if len(text) > 0 {
					finalTexts = append(finalTexts, text)
					segmentIndex++
					if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: id, Text: text}) {
						return
					}
				}
				// When timestamps are requested, ElevenLabs follows this event
				// with a ..._with_timestamps companion for the same commit.
				if endOfInput && committedEventCount > committedEventsAtEndOfInput {
					receivedPostInputCommit = true
					v := committedEventCount
					finalCommitEventCount = &v
					if !expectDetailedCommit || raw.MessageType == "final_transcript" {
						scheduleFinish()
					}
				}

			case "final_transcript_with_timestamps", "committed_transcript_with_timestamps":
				text := strings.TrimSpace(raw.Text)
				var timestampedWords []elevenLabsRealtimeWord
				for _, w := range raw.Words {
					if w.Start != nil && w.End != nil {
						timestampedWords = append(timestampedWords, w)
					}
				}
				// Unconditional except for a nil/absent language_code (TS:
				// `raw.language_code ?? detectedLanguage`): updated even when
				// this event carries no new text below.
				if raw.LanguageCode != nil {
					detectedLanguage = *raw.LanguageCode
				}

				// Normally this is paired with the preceding committed_transcript.
				// Still handle a timestamp-only server response defensively.
				if timestampedCommitCount >= len(finalTexts) && text != "" {
					id := segID()
					segmentIndex++
					finalTexts = append(finalTexts, text)
					part := provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeFinal, ID: id, Text: text}
					if cfg.includeTimestamps && len(timestampedWords) > 0 {
						start := *timestampedWords[0].Start
						end := *timestampedWords[len(timestampedWords)-1].End
						part.StartSecond = &start
						part.EndSecond = &end
					}
					if !s.Emit(part) {
						return
					}
				}
				timestampedCommitCount++
				if cfg.includeTimestamps {
					for _, w := range timestampedWords {
						finalSegments = append(finalSegments, provider.TranscriptSegment{Text: w.Text, StartSecond: *w.Start, EndSecond: *w.End})
					}
				}

				if endOfInput && timestampedCommitCount > committedEventsAtEndOfInput &&
					(finalCommitEventCount == nil || timestampedCommitCount >= *finalCommitEventCount) {
					receivedPostInputCommit = true
					scheduleFinish()
				}
			}
		}
	}
}

func (s *elevenLabsRealtimeTranscriptionStream) dial(wsURL *url.URL, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.Context(), wsURL.String(), wsutil.DialOptions{Headers: headers})
}

func (s *elevenLabsRealtimeTranscriptionStream) send(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, string(message))
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*elevenLabsRealtimeTranscriptionStream)(nil)
)
