package deepgram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/websocket"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// deepgramKeepAliveInterval is how often a KeepAlive message goes out while
// the stream is open: Deepgram closes connections that are idle for ten
// seconds (TS setInterval(..., 4000)).
var deepgramKeepAliveInterval = 4 * time.Second

// deepgramStreamingEncodings maps the supported input media types to the
// Deepgram live encodings (TS streamDeepgramTranscription).
var deepgramStreamingEncodings = map[string]string{
	"audio/pcm":  "linear16",
	"audio/pcmu": "mulaw",
	"audio/pcma": "alaw",
}

// deepgramStreamingOptionParams is the TS `mappings` table, in the TS
// schema's key order (the order parsed provider options iterate in).
var deepgramStreamingOptionParams = []struct{ option, param string }{
	{"language", "language"},
	{"detectLanguage", ""},
	{"smartFormat", "smart_format"},
	{"punctuate", "punctuate"},
	{"paragraphs", ""},
	{"summarize", ""},
	{"topics", ""},
	{"intents", ""},
	{"sentiment", ""},
	{"detectEntities", ""},
	{"redact", "redact"},
	{"replace", "replace"},
	{"search", "search"},
	{"keyterm", "keyterm"},
	{"diarize", "diarize"},
	{"utterances", ""},
	{"uttSplit", ""},
	{"fillerWords", "filler_words"},
}

// deepgramStreamingParams is the TS `streamingMappings` table in schema
// order.
var deepgramStreamingParams = []struct{ option, param string }{
	{"interimResults", "interim_results"},
	{"endpointing", "endpointing"},
	{"channels", "channels"},
	{"multichannel", "multichannel"},
	{"vadEvents", "vad_events"},
	{"utteranceEndMs", "utterance_end_ms"},
}

var deepgramAuthorizationPattern = regexp.MustCompile(`(?i)^(Token|Bearer) (\S+)$`)

// DoStream transcribes live audio over Deepgram's /v1/listen WebSocket,
// mirroring TS streamDeepgramTranscription (5b89b2560c). The credential
// travels as the subprotocols [scheme, token] (and the handshake headers),
// never in the URL.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}
	if strings.HasPrefix(m.modelID, "flux") {
		return nil, &providererrors.UnsupportedFunctionalityError{Functionality: "Deepgram Flux streaming transcription (/v2/listen)"}
	}
	encoding, ok := deepgramStreamingEncodings[opts.InputAudioFormat.Type]
	if !ok || opts.InputAudioFormat.Rate == nil || *opts.InputAudioFormat.Rate <= 0 {
		return nil, providererrors.NewArgumentError("inputAudioFormat",
			"Deepgram streaming requires audio/pcm (16-bit little-endian), audio/pcmu, or audio/pcma and a positive integer sample rate.", nil)
	}
	rate := *opts.InputAudioFormat.Rate

	if err := validateTranscriptionOptions(opts.ProviderOptions); err != nil {
		return nil, err
	}
	rawOpts := rawTranscriptionOptions(opts.ProviderOptions)

	warnings := []types.Warning{}
	wsURL, err := deepgramStreamingURL(m.provider.baseURL(), m.modelID, encoding, rate, rawOpts, &warnings)
	if err != nil {
		return nil, err
	}

	headers, protocols, err := m.streamingHandshake(opts.Headers)
	if err != nil {
		return nil, err
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}
	timestamp := time.Now()
	var language string
	if v, ok := rawOpts["language"].(string); ok {
		language = v
	}

	stream := &deepgramTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](abortCtx)}
	go stream.run(deepgramStreamConfig{
		url:              wsURL,
		headers:          headers,
		protocols:        protocols,
		warnings:         warnings,
		audio:            opts.Audio,
		includeRawChunks: opts.IncludeRawChunks,
		modelID:          m.modelID,
		timestamp:        timestamp,
		language:         language,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: wsURL,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: timestamp, ModelID: m.modelID},
	}, nil
}

// deepgramStreamingURL builds the TS listen URL: ws(s)://<base>/v1/listen
// with model, encoding and sample_rate, then the mapped provider options in
// schema order (arrays appended once per entry; any other non-null option
// warns unless it is false), then the streaming options.
func deepgramStreamingURL(baseURL, modelID, encoding string, rate int, rawOpts map[string]interface{}, warnings *[]types.Warning) (string, error) {
	// TS toWebSocketUrl(config.url({path: '/v1/listen'})): new URL (WHATWG
	// serialization, "Invalid URL" for a base URL it rejects), http: to ws:,
	// https: to wss:.
	normalized, err := wsutil.ToWebSocketURL(baseURL + "/v1/listen")
	if err != nil {
		return "", err
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	q := internalhttp.ParseSearchParams(u.RawQuery)
	q.Set("model", modelID)
	q.Set("encoding", encoding)
	q.Set("sample_rate", strconv.Itoa(rate))

	for _, o := range deepgramStreamingOptionParams {
		value, present := rawOpts[o.option]
		if !present || value == nil {
			continue
		}
		if o.param == "" {
			if value == false {
				continue
			}
			*warnings = append(*warnings, types.Warning{
				Type:    "unsupported",
				Feature: "providerOptions.deepgram." + o.option,
				Details: "This option is not supported for Deepgram streaming transcription.",
			})
			continue
		}
		if list, ok := value.([]interface{}); ok {
			for _, entry := range list {
				q.Append(o.param, deepgramJSString(entry))
			}
			continue
		}
		q.Append(o.param, deepgramJSString(value))
	}
	streaming, _ := rawOpts["streaming"].(map[string]interface{})
	for _, o := range deepgramStreamingParams {
		if value, present := streaming[o.option]; present && value != nil {
			q.Set(o.param, deepgramJSString(value))
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// deepgramJSString is JS String(value) for a parsed JSON option value.
func deepgramJSString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return providerutils.JSNumberString(v)
	case []interface{}:
		return deepgramQueryStringValue(v)
	}
	return fmt.Sprint(value)
}

// rawTranscriptionOptions returns providerOptions["deepgram"] as a JSON
// object (a typed TranscriptionModelOptions through its JSON form).
func rawTranscriptionOptions(providerOptions map[string]interface{}) map[string]interface{} {
	raw, ok := providerOptions["deepgram"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		m, _ := raw.(map[string]interface{})
		return m
	}
	var m map[string]interface{}
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// streamingHandshake resolves the TS handshake headers (provider headers,
// then call headers, names lower-cased) and the credential subprotocols
// [scheme, token] from the authorization header, which must use the Token
// or Bearer scheme.
func (m *TranscriptionModel) streamingHandshake(callHeaders map[string]string) (map[string]string, []string, error) {
	cfg := m.provider.config
	auth, err := providerutils.RequestAuthHeader(cfg.Headers, cfg.APIKey, "DEEPGRAM_API_KEY", "Deepgram", "Authorization", "Token ")
	if err != nil {
		return nil, nil, err
	}
	providerHeaders := version.WithUserAgentSuffix(internalhttp.MergeHeaders(auth, cfg.Headers), version.ProviderUserAgent("deepgram"))
	headers := map[string]string{}
	for _, source := range []map[string]string{providerHeaders, callHeaders} {
		for name, value := range source {
			headers[strings.ToLower(name)] = value
		}
	}
	authorization, hasAuthorization := headers["authorization"]
	if !hasAuthorization {
		return headers, nil, nil
	}
	credentials := deepgramAuthorizationPattern.FindStringSubmatch(authorization)
	if credentials == nil {
		return nil, nil, providererrors.NewArgumentError("headers", "Deepgram streaming authorization must use the Token or Bearer scheme.", nil)
	}
	return headers, []string{strings.ToLower(credentials[1]), credentials[2]}, nil
}

type deepgramStreamConfig struct {
	url              string
	headers          map[string]string
	protocols        []string
	warnings         []types.Warning
	audio            provider.AudioStream
	includeRawChunks bool
	modelID          string
	timestamp        time.Time
	language         string
}

// deepgramTranscriptionStream implements provider.TranscriptionStream over
// Deepgram's live WebSocket.
type deepgramTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]

	// finished and inputEnded mirror the TS flags; the audio pump reads
	// them from its own goroutine.
	finished   atomic.Bool
	inputEnded atomic.Bool
}

type deepgramFinalText struct {
	text         string
	startSecond  float64
	channelIndex float64
}

func (s *deepgramTranscriptionStream) run(cfg deepgramStreamConfig) {
	defer s.CloseParts()
	defer s.CancelContext()

	var cancelOnce sync.Once
	cancelAudio := func(reason error) {
		if cfg.audio != nil {
			cancelOnce.Do(func() { cfg.audio.Cancel(reason) })
		}
	}
	fail := func(err error) {
		if s.finished.Swap(true) {
			return
		}
		s.SetErr(err)
		cancelAudio(err)
	}

	closeInfo := &wsutil.CloseInfo{}
	conn, err := wsutil.Dial(s.Context(), cfg.url, wsutil.DialOptions{Headers: cfg.headers, Protocols: cfg.protocols, WrapConn: closeInfo.Wrap})
	if err != nil {
		if ctxErr := context.Cause(s.Context()); ctxErr != nil {
			fail(ctxErr)
		} else {
			fail(errors.New("Deepgram streaming WebSocket error.")) //nolint:staticcheck // TS error text
		}
		return
	}
	s.SetConn(conn)
	defer func() {
		_ = conn.WriteClose(1000)
		_ = conn.Close()
	}()

	// TS onOpen enqueues stream-start (never blocking), then starts the
	// keepalive and the audio sender; the senders start first here so a
	// consumer that has not read stream-start yet does not hold up the audio.
	errCh := make(chan error, 2)
	inputEndedCh := make(chan struct{})
	go s.keepAlive(conn, errCh, inputEndedCh)
	go s.pumpAudio(conn, cfg.audio, errCh, inputEndedCh)

	if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeStreamStart, Warnings: cfg.warnings}) {
		fail(context.Cause(s.Context()))
		return
	}

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	var (
		duration         *float64
		reportedDuration *float64
		language         = cfg.language
		finalTexts       []deepgramFinalText
		segments         = []provider.TranscriptSegment{}
	)
	finish := func() {
		if s.finished.Swap(true) {
			return
		}
		sort.SliceStable(finalTexts, func(i, j int) bool {
			if finalTexts[i].startSecond != finalTexts[j].startSecond {
				return finalTexts[i].startSecond < finalTexts[j].startSecond
			}
			return finalTexts[i].channelIndex < finalTexts[j].channelIndex
		})
		texts := make([]string, len(finalTexts))
		for i, t := range finalTexts {
			texts[i] = t.text
		}
		sort.SliceStable(segments, func(i, j int) bool { return segments[i].StartSecond < segments[j].StartSecond })
		part := provider.TranscriptionStreamPart{
			Type:              provider.TranscriptionStreamPartTypeFinish,
			FinishText:        strings.Join(texts, " "),
			Segments:          segments,
			Language:          language,
			DurationInSeconds: duration,
		}
		if reportedDuration != nil {
			part.Usage = map[string]interface{}{"seconds": *reportedDuration}
		}
		s.Emit(part)
		cancelAudio(nil)
	}

	for {
		select {
		case <-s.Context().Done():
			fail(context.Cause(s.Context()))
			return

		case err := <-errCh:
			fail(err)
			return

		case res := <-msgCh:
			if res.Err != nil {
				if s.finished.Load() {
					return
				}
				if !wsutil.IsCleanClose(res.Err) {
					fail(errors.New("Deepgram streaming WebSocket error.")) //nolint:staticcheck // TS error text
					return
				}
				code := closeInfo.Code()
				if !s.inputEnded.Load() || code != 1000 {
					suffix := "."
					if reason := closeInfo.Reason(); reason != "" {
						suffix = ": " + reason
					}
					fail(fmt.Errorf("Deepgram streaming connection closed unexpectedly (%d)%s", code, suffix)) //nolint:staticcheck // TS error text
				} else {
					finish()
				}
				return
			}
			if s.finished.Load() {
				continue
			}
			done, err := s.handleEvent(res.Text, cfg, &duration, &reportedDuration, &language, &finalTexts, &segments)
			if err != nil {
				fail(err)
				return
			}
			if done {
				finish()
				return
			}
		}
	}
}

// handleEvent is the TS onMessageText handler; done reports that the stream
// should finish (a Metadata event after the input ended).
func (s *deepgramTranscriptionStream) handleEvent(text string, cfg deepgramStreamConfig, duration, reportedDuration **float64, language *string, finalTexts *[]deepgramFinalText, segments *[]provider.TranscriptSegment) (bool, error) {
	text = textdecode.UTF8([]byte(text))
	var raw interface{}
	if err := jsonexact.Unmarshal([]byte(text), &raw); err != nil {
		return false, &providererrors.JSONParseError{Text: text, Cause: err}
	}
	if err := deepgramValidate(deepgramEventTypeNode, text, raw); err != nil {
		return false, err
	}
	if cfg.includeRawChunks {
		if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeRaw, RawValue: raw}) {
			return false, context.Cause(s.Context())
		}
	}
	event, _ := raw.(map[string]interface{})
	switch event["type"] {
	case "Error":
		if err := deepgramValidate(deepgramErrorNode, text, raw); err != nil {
			return false, err
		}
		message := "Deepgram streaming transcription failed."
		if v, ok := event["description"].(string); ok {
			message = v
		} else if v, ok := event["message"].(string); ok {
			message = v
		}
		return false, errors.New(message)

	case "Metadata":
		if err := deepgramValidate(deepgramMetadataNode, text, raw); err != nil {
			return false, err
		}
		if d, ok := event["duration"].(float64); ok {
			*reportedDuration = &d
			dd := d
			*duration = &dd
		}
		if !s.Emit(provider.TranscriptionStreamPart{Type: provider.TranscriptionStreamPartTypeResponseMetadata, Timestamp: cfg.timestamp, ModelID: cfg.modelID, Body: raw}) {
			return false, context.Cause(s.Context())
		}
		return s.inputEnded.Load(), nil

	case "Results":
		if err := deepgramValidate(deepgramResultNode, text, raw); err != nil {
			return false, err
		}
		var result deepgramStreamingResult
		_ = jsonexact.Unmarshal([]byte(text), &result)
		if result.Channel == nil || len(result.Channel.Alternatives) == 0 {
			return false, errors.New("Deepgram Results event is missing a transcript alternative.") //nolint:staticcheck // TS error text
		}
		alternative := result.Channel.Alternatives[0]
		if len(alternative.Languages) > 0 {
			*language = alternative.Languages[0]
		}
		start := 0.0
		if result.Start != nil {
			start = *result.Start
		}
		end := start
		if result.Duration != nil {
			end += *result.Duration
		}
		// Math.max(duration ?? 0, end)
		maxDuration := 0.0
		if *duration != nil {
			maxDuration = **duration
		}
		maxDuration = math.Max(maxDuration, end)
		*duration = &maxDuration
		speechFinal := result.SpeechFinal != nil && *result.SpeechFinal
		metadata := map[string]interface{}{"deepgram": map[string]interface{}{"speechFinal": speechFinal}}
		var channelIndex *int
		channelForSort := 0.0
		if len(result.ChannelIndex) > 0 {
			ci := int(result.ChannelIndex[0])
			channelIndex = &ci
			channelForSort = result.ChannelIndex[0]
		}
		startSecond := start
		if result.IsFinal != nil && *result.IsFinal {
			if alternative.Transcript != "" {
				*finalTexts = append(*finalTexts, deepgramFinalText{text: alternative.Transcript, startSecond: start, channelIndex: channelForSort})
			}
			for _, w := range alternative.Words {
				wordText := w.Word
				if w.PunctuatedWord != nil {
					wordText = *w.PunctuatedWord
				}
				*segments = append(*segments, provider.TranscriptSegment{Text: wordText, StartSecond: w.Start, EndSecond: w.End})
			}
			endSecond := end
			if !s.Emit(provider.TranscriptionStreamPart{
				Type: provider.TranscriptionStreamPartTypeFinal, Text: alternative.Transcript,
				StartSecond: &startSecond, EndSecond: &endSecond, ChannelIndex: channelIndex, ProviderMetadata: metadata,
			}) {
				return false, context.Cause(s.Context())
			}
		} else {
			if !s.Emit(provider.TranscriptionStreamPart{
				Type: provider.TranscriptionStreamPartTypePartial, Text: alternative.Transcript,
				StartSecond: &startSecond, DurationInSeconds: result.Duration, ChannelIndex: channelIndex, ProviderMetadata: metadata,
			}) {
				return false, context.Cause(s.Context())
			}
		}
	}
	return false, nil
}

// keepAlive sends {"type":"KeepAlive"} every deepgramKeepAliveInterval until
// the input ends or the stream finishes.
func (s *deepgramTranscriptionStream) keepAlive(conn *websocket.Conn, errCh chan<- error, inputEnded <-chan struct{}) {
	ticker := time.NewTicker(deepgramKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.Context().Done():
			return
		case <-inputEnded:
			return
		case <-ticker.C:
			if s.finished.Load() {
				return
			}
			if err := wsutil.Send(s.Context(), conn, `{"type":"KeepAlive"}`); err != nil {
				if s.Context().Err() == nil {
					wsutil.ReportError(s.Context(), errCh, err)
				}
				return
			}
		}
	}
}

// pumpAudio is TS sendAudio: each non-empty chunk goes out as a binary
// frame; when the audio ends, the input is marked ended, the keepalive stops
// and {"type":"CloseStream"} is sent.
func (s *deepgramTranscriptionStream) pumpAudio(conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error, inputEnded chan<- struct{}) {
	for audio != nil {
		if s.finished.Load() {
			return
		}
		chunk, err := audio.Next(s.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			if s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, err)
			}
			return
		}
		if s.finished.Load() {
			return
		}
		if len(chunk) == 0 {
			continue
		}
		if err := wsutil.Send(s.Context(), conn, chunk); err != nil {
			if s.Context().Err() == nil {
				wsutil.ReportError(s.Context(), errCh, err)
			}
			return
		}
	}
	if s.finished.Load() {
		return
	}
	s.inputEnded.Store(true)
	close(inputEnded)
	if err := wsutil.Send(s.Context(), conn, `{"type":"CloseStream"}`); err != nil && s.Context().Err() == nil {
		wsutil.ReportError(s.Context(), errCh, err)
	}
}

type deepgramStreamingResult struct {
	IsFinal      *bool     `json:"is_final"`
	SpeechFinal  *bool     `json:"speech_final"`
	Start        *float64  `json:"start"`
	Duration     *float64  `json:"duration"`
	ChannelIndex []float64 `json:"channel_index"`
	Channel      *struct {
		Alternatives []struct {
			Transcript string   `json:"transcript"`
			Languages  []string `json:"languages"`
			Words      []struct {
				Word           string  `json:"word"`
				PunctuatedWord *string `json:"punctuated_word"`
				Start          float64 `json:"start"`
				End            float64 `json:"end"`
			} `json:"words"`
		} `json:"alternatives"`
	} `json:"channel"`
}

// The TS streaming event schemas.
var (
	deepgramEventTypeNode = zodlite.Object(zodlite.F("type", zodlite.String))
	deepgramMetadataNode  = zodlite.Object(zodlite.F("duration", zodlite.Nullish(zodlite.Number)))
	deepgramErrorNode     = zodlite.Object(
		zodlite.F("description", zodlite.Nullish(zodlite.String)),
		zodlite.F("message", zodlite.Nullish(zodlite.String)),
	)
	deepgramResultNode = zodlite.Object(
		zodlite.F("is_final", zodlite.Nullish(zodlite.Boolean)),
		zodlite.F("speech_final", zodlite.Nullish(zodlite.Boolean)),
		zodlite.F("start", zodlite.Nullish(zodlite.Number)),
		zodlite.F("duration", zodlite.Nullish(zodlite.Number)),
		zodlite.F("channel_index", zodlite.Nullish(zodlite.Array(zodlite.Number))),
		zodlite.F("channel", zodlite.Nullish(zodlite.Object(
			zodlite.F("alternatives", zodlite.Array(zodlite.Object(
				zodlite.F("transcript", zodlite.String),
				zodlite.F("languages", zodlite.Nullish(zodlite.Array(zodlite.String))),
				zodlite.F("words", zodlite.Nullish(zodlite.Array(zodlite.Object(
					zodlite.F("word", zodlite.String),
					zodlite.F("punctuated_word", zodlite.Nullish(zodlite.String)),
					zodlite.F("start", zodlite.Number),
					zodlite.F("end", zodlite.Number),
				)))),
			))),
		))),
	)
)

// deepgramValidate is TS safeParseJSON / validateTypes with a schema: a
// mismatch is a TypeValidationError.
func deepgramValidate(node zodlite.Node, text string, _ interface{}) error {
	if message := zodlite.ValidationMessage(node, text); message != "" {
		return providererrors.NewValidationErrorWithContext(zodlite.CompactJSON(text), zodlite.IssueText(message), nil, &providererrors.ValidationContext{})
	}
	return nil
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*deepgramTranscriptionStream)(nil)
)
