package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

// Gateway streaming transcription WebSocket envelope (v1) frame types (TS
// transcription-stream-envelope.ts). The client sends exactly one "start"
// TEXT frame, then raw audio as BINARY frames, then "audio-done"; the server
// replies with one JSON-serialized TranscriptionModelV4StreamPart per TEXT
// frame and closes 1000 after "finish" (non-1000 after an "error" part).
const (
	transcriptionStreamStartFrameType     = "transcription-stream.start"
	transcriptionStreamAudioDoneFrameType = "transcription-stream.audio-done"

	// maxTranscriptionAudioFrameBytes keeps audio frames under server
	// frame-size limits (envelope rule 7).
	maxTranscriptionAudioFrameBytes = 64 * 1024
)

// DoStream streams a transcript for live audio over the Gateway's streaming
// transcription WebSocket, using the ai-gateway-transcription.v1 subprotocol
// and the transcription-stream envelope. Mirrors TS
// GatewayTranscriptionModel#doStream.
func (m *TranscriptionModel) DoStream(ctx context.Context, opts *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error) {
	if opts == nil {
		opts = &provider.TranscriptionStreamOptions{}
	}

	token, authMethod, err := m.provider.authResolver(ctx)
	if err != nil {
		return nil, err
	}

	headers := internalhttp.MergeHeaders(m.provider.headers, m.getModelConfigHeaders())
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	headers = internalhttp.MergeHeaders(headers, opts.Headers)
	// The handshake carries auth on both channels, matching TS
	// createAuthHeaders: the raw Authorization/ai-gateway-auth-method headers
	// alongside the ai-gateway-auth.<token> subprotocol below.
	headers["Authorization"] = "Bearer " + token
	headers["ai-gateway-auth-method"] = authMethod

	startFrame := map[string]interface{}{
		"type":             transcriptionStreamStartFrameType,
		"inputAudioFormat": transcriptionAudioFormatWire(opts.InputAudioFormat),
	}
	if opts.ProviderOptions != nil {
		startFrame["providerOptions"] = opts.ProviderOptions
	}
	if opts.IncludeRawChunks {
		startFrame["includeRawChunks"] = true
	}

	wsURL := ToGatewayTranscriptionURL(m.provider.baseURL, m.modelID)
	// The team scope rides the subprotocol from the fully merged per-call
	// header set (headers, above — config headers + opts.Headers), not the
	// static TeamIDOrSlug config field, so a caller overriding
	// x-vercel-ai-gateway-team per call (e.g. via opts.Headers) takes effect
	// here exactly like it does on every other Gateway request.
	protocols := GetGatewayTranscriptionProtocols(token, gatewayTeamFromHeaders(headers))

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGatewayTranscriptionStream(abortCtx, gatewayTranscriptionStreamConfig{
		url:        wsURL,
		protocols:  protocols,
		headers:    headers,
		startFrame: startFrame,
		audio:      opts.Audio,
		authMethod: authMethod,
	})

	return &provider.TranscriptionStreamResult{
		Stream:      stream,
		RequestBody: startFrame,
		Response:    &provider.TranscriptionStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

func transcriptionAudioFormatWire(f provider.AudioFormat) map[string]interface{} {
	out := map[string]interface{}{"type": f.Type}
	if f.Rate != nil {
		out["rate"] = *f.Rate
	}
	return out
}

type gatewayTranscriptionStreamConfig struct {
	url        string
	protocols  []string
	headers    map[string]string
	startFrame map[string]interface{}
	audio      provider.AudioStream
	authMethod string
}

// gatewayTranscriptionStream implements provider.TranscriptionStream over the
// AI Gateway's streaming transcription WebSocket. The Next/Err/Close/emit/
// setErr plumbing is the shared wsutil.Session core; pumpAudio's max-frame-
// size chunk splitting and its own server-error-triggered audioCtx (distinct
// from the session's ctx, so a server "error" part can stop audio while the
// receive loop keeps running) don't fit the shared wsutil.PumpAudio loop
// cleanly, so it stays local.
type gatewayTranscriptionStream struct {
	*wsutil.Session[provider.TranscriptionStreamPart]
}

func newGatewayTranscriptionStream(parentCtx context.Context, cfg gatewayTranscriptionStreamConfig) *gatewayTranscriptionStream {
	s := &gatewayTranscriptionStream{Session: wsutil.NewSession[provider.TranscriptionStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

func (s *gatewayTranscriptionStream) run(cfg gatewayTranscriptionStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason (finish, error, or cancellation) instead of only on an
	// explicit Close() call, which a consumer that only drains Next() to
	// io.EOF may never make.
	defer s.CancelContext()

	fail := func(err error) {
		s.SetErr(err)
		cfg.audio.Cancel(err)
	}

	conn, err := s.dial(cfg.url, cfg.protocols, cfg.headers)
	if err != nil {
		fail(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck

	startPayload, err := json.Marshal(cfg.startFrame)
	if err != nil {
		fail(err)
		return
	}
	if err := s.sendText(conn, string(startPayload)); err != nil {
		fail(err)
		return
	}

	// audioCtx (rather than the session's ctx directly) lets a server
	// "error" part stop audio pumping while the receive loop below keeps
	// running to observe the server's terminal close (TS stopAudio()).
	audioCtx, stopAudio := context.WithCancel(s.Context())
	defer stopAudio()
	audioErrCh := make(chan error, 1)
	go s.pumpAudio(audioCtx, conn, cfg.audio, audioErrCh)

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	var lastServerError interface{}
	hasServerError := false
	finished := false

	for {
		select {
		case <-s.Context().Done():
			if finished {
				return
			}
			fail(s.Context().Err())
			return

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
				// Priority mirrors TS's real event ordering, not just which
				// piece of state happens to be set: an abnormal disconnect
				// (TS onSocketError) always wins over a remembered server
				// error part, because in TS onSocketError fires (and
				// finishes the stream) before onClose's hasServerErrorPart
				// branch ever runs. Only a clean close (TS onClose, which is
				// the sole handler that fires for a graceful disconnect)
				// falls through to the remembered server error, then to the
				// generic "closed before a finish part" message.
				if !wsutil.IsCleanClose(res.Err) {
					fail(errors.New("Connection error on AI Gateway transcription stream")) //nolint:staticcheck // matches TS SDK's exact error text
				} else if hasServerError {
					fail(gatewayTranscriptionServerError(lastServerError, cfg.authMethod))
				} else {
					fail(errors.New("AI Gateway transcription stream closed before a finish part was received"))
				}
				return
			}

			part, ok := parseGatewayTranscriptionStreamPart(res.Text)
			if !ok {
				continue
			}

			if part.Type == provider.TranscriptionStreamPartTypeFinish {
				// Not setting finished here: both branches below return
				// immediately, so nothing reads the flag again.
				if !s.Emit(*part) {
					return
				}
				return
			}

			if part.Type == provider.TranscriptionStreamPartTypeError {
				hasServerError = true
				lastServerError = part.Err
				// envelope rule 5: error parts are terminal — stop sending audio
				// while the server holds the connection open (e.g. for its final
				// billing flush).
				stopAudio()
				cfg.audio.Cancel(nil)
			}

			if !s.Emit(*part) {
				return
			}
		}
	}
}

// gatewayServerErrorStatusCodes are the canonical status codes for server
// error-part types; there is no real HTTP status on the WebSocket itself (TS
// SERVER_ERROR_STATUS_CODES).
var gatewayServerErrorStatusCodes = map[string]int{
	"authentication_error":  401,
	"failed_dependency":     424,
	"forbidden":             403,
	"internal_server_error": 500,
	"invalid_request_error": 400,
	"model_not_found":       404,
	"rate_limit_exceeded":   429,
}

// gatewayTranscriptionServerError maps a server error-part payload
// ({message, type}) to the typed Gateway error class for its type, mirroring
// TS createErrorFromServerErrorPart; unknown shapes keep the generic message.
func gatewayTranscriptionServerError(payload interface{}, authMethod string) error {
	m, ok := payload.(map[string]interface{})
	if ok {
		msg, hasMsg := m["message"].(string)
		errType, hasType := m["type"].(string)
		if hasMsg && hasType {
			if statusCode, known := gatewayServerErrorStatusCodes[errType]; known {
				body, marshalErr := json.Marshal(map[string]interface{}{
					"error": map[string]interface{}{"message": msg, "type": errType},
				})
				if marshalErr == nil {
					return gatewayerrors.CreateGatewayErrorFromResponse(body, statusCode, "AI Gateway transcription stream failed", nil, authMethod)
				}
			}
		}
		if hasMsg && msg != "" {
			return fmt.Errorf("AI Gateway transcription stream failed: %s", msg)
		}
	}
	return fmt.Errorf("AI Gateway transcription stream failed: %v", payload)
}

// pumpAudio reads chunks from audio and forwards them as binary frames (split
// to stay under the server frame-size limit), sending the audio-done TEXT
// frame at EOF. Any other failure — reading from the AudioStream, or writing
// to the WebSocket — is reported on errCh, mirroring TS's
// `void sendAudio(socket).catch(finishWithError)` (a rejected `reader.read()`
// fails the stream exactly like a failed `socket.send`). A failure that stems
// from ctx already being cancelled (either the session's own ctx, or
// stopAudio() pausing the pump for a server error part) is not reported:
// run()'s own handling of that cancellation already covers it.
func (s *gatewayTranscriptionStream) pumpAudio(ctx context.Context, conn *websocket.Conn, audio provider.AudioStream, errCh chan<- error) {
	for {
		chunk, err := audio.Next(ctx)
		if err != nil {
			if err == io.EOF {
				if sendErr := s.sendText(conn, fmt.Sprintf(`{"type":%q}`, transcriptionStreamAudioDoneFrameType)); sendErr != nil && ctx.Err() == nil {
					wsutil.ReportError(ctx, errCh, sendErr)
				}
			} else if ctx.Err() == nil {
				wsutil.ReportError(ctx, errCh, err)
			}
			return
		}
		for offset := 0; offset < len(chunk); offset += maxTranscriptionAudioFrameBytes {
			end := offset + maxTranscriptionAudioFrameBytes
			if end > len(chunk) {
				end = len(chunk)
			}
			if err := s.sendBinary(conn, chunk[offset:end]); err != nil {
				if ctx.Err() == nil {
					wsutil.ReportError(ctx, errCh, err)
				}
				return
			}
		}
	}
}

func (s *gatewayTranscriptionStream) dial(wsURL string, protocols []string, headers map[string]string) (*websocket.Conn, error) {
	return wsutil.Dial(s.Context(), wsURL, wsutil.DialOptions{Headers: headers, Protocols: protocols})
}

func (s *gatewayTranscriptionStream) sendText(conn *websocket.Conn, message string) error {
	return wsutil.Send(s.Context(), conn, message)
}

func (s *gatewayTranscriptionStream) sendBinary(conn *websocket.Conn, message []byte) error {
	return wsutil.Send(s.Context(), conn, message)
}

// parseGatewayTranscriptionStreamPart parses one server TEXT frame (a
// flattened TranscriptionModelV4StreamPart) into a provider.TranscriptionStreamPart.
// Unknown types are ignored (envelope rule 6); malformed JSON returns ok=false.
func parseGatewayTranscriptionStreamPart(text string) (*provider.TranscriptionStreamPart, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, false
	}
	rawType, _ := raw["type"].(string)

	part := &provider.TranscriptionStreamPart{Type: rawType}

	// Mirrors TS parseTranscriptionStreamPart: required fields must be
	// present with the right type or the whole frame is rejected (ok=false),
	// not silently defaulted. Optional fields use isOptionalX (absent is
	// fine; present-but-wrong-type is not).
	switch rawType {
	case provider.TranscriptionStreamPartTypeStreamStart:
		warnings, ok := warningsFromInterfaceStrict(raw["warnings"])
		if !ok {
			return nil, false
		}
		part.Warnings = warnings

	case provider.TranscriptionStreamPartTypeDelta:
		delta, ok := raw["delta"].(string)
		if !ok {
			return nil, false
		}
		if !isOptionalString(raw["id"]) || !isOptionalRecord(raw["providerMetadata"]) {
			return nil, false
		}
		part.Delta = delta
		part.ID, _ = raw["id"].(string)
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypePartial:
		text, ok := raw["text"].(string)
		if !ok {
			return nil, false
		}
		if !isOptionalString(raw["id"]) || !isOptionalNumber(raw["startSecond"]) ||
			!isOptionalNumber(raw["durationInSeconds"]) || !isOptionalNumber(raw["channelIndex"]) ||
			!isOptionalRecord(raw["providerMetadata"]) {
			return nil, false
		}
		part.Text = text
		part.ID, _ = raw["id"].(string)
		part.StartSecond = float64PtrFromInterface(raw["startSecond"])
		part.DurationInSeconds = float64PtrFromInterface(raw["durationInSeconds"])
		part.ChannelIndex = intPtrFromInterface(raw["channelIndex"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypeFinal:
		text, ok := raw["text"].(string)
		if !ok {
			return nil, false
		}
		if !isOptionalString(raw["id"]) || !isOptionalNumber(raw["startSecond"]) ||
			!isOptionalNumber(raw["endSecond"]) || !isOptionalNumber(raw["channelIndex"]) ||
			!isOptionalRecord(raw["providerMetadata"]) {
			return nil, false
		}
		part.Text = text
		part.ID, _ = raw["id"].(string)
		part.StartSecond = float64PtrFromInterface(raw["startSecond"])
		part.EndSecond = float64PtrFromInterface(raw["endSecond"])
		part.ChannelIndex = intPtrFromInterface(raw["channelIndex"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])

	case provider.TranscriptionStreamPartTypeResponseMetadata:
		if !isOptionalString(raw["modelId"]) || !isOptionalRecord(raw["headers"]) {
			return nil, false
		}
		part.ModelID, _ = raw["modelId"].(string)
		part.Headers = stringMapFromInterface(raw["headers"])
		if ts := raw["timestamp"]; ts != nil {
			tsStr, ok := ts.(string)
			if !ok {
				return nil, false
			}
			parsed, err := time.Parse(time.RFC3339Nano, tsStr)
			if err != nil {
				return nil, false
			}
			part.Timestamp = parsed
		}

	case provider.TranscriptionStreamPartTypeFinish:
		text, ok := raw["text"].(string)
		if !ok {
			return nil, false
		}
		segments, ok := segmentsFromInterfaceStrict(raw["segments"])
		if !ok {
			return nil, false
		}
		if !isOptionalString(raw["language"]) || !isOptionalNumber(raw["durationInSeconds"]) ||
			!isOptionalRecord(raw["providerMetadata"]) {
			return nil, false
		}
		part.FinishText = text
		part.Language, _ = raw["language"].(string)
		part.DurationInSeconds = float64PtrFromInterface(raw["durationInSeconds"])
		part.ProviderMetadata = mapFromInterface(raw["providerMetadata"])
		part.Segments = segments

	case provider.TranscriptionStreamPartTypeRaw:
		if _, ok := raw["rawValue"]; !ok {
			return nil, false
		}
		part.RawValue = raw["rawValue"]

	case provider.TranscriptionStreamPartTypeError:
		if _, ok := raw["error"]; !ok {
			return nil, false
		}
		part.Err = raw["error"]

	default:
		return nil, false
	}

	return part, true
}

// isOptionalString mirrors TS isOptional(value, isString): absent (nil) is
// fine; present-but-non-string is not.
func isOptionalString(v interface{}) bool {
	if v == nil {
		return true
	}
	_, ok := v.(string)
	return ok
}

// isOptionalNumber mirrors TS isOptional(value, isNumber).
func isOptionalNumber(v interface{}) bool {
	if v == nil {
		return true
	}
	_, ok := v.(float64)
	return ok
}

// isOptionalRecord mirrors TS isOptional(value, isRecord).
func isOptionalRecord(v interface{}) bool {
	if v == nil {
		return true
	}
	_, ok := v.(map[string]interface{})
	return ok
}

// warningsFromInterfaceStrict mirrors TS's stream-start validation:
// Array.isArray(warnings) && warnings.every(isWarning), where isWarning
// requires a record with a string `type`.
func warningsFromInterfaceStrict(v interface{}) ([]types.Warning, bool) {
	list, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]types.Warning, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if _, ok := m["type"].(string); !ok {
			return nil, false
		}
		w := types.Warning{}
		w.Type, _ = m["type"].(string)
		w.Feature, _ = m["feature"].(string)
		w.Setting, _ = m["setting"].(string)
		w.Details, _ = m["details"].(string)
		w.Message, _ = m["message"].(string)
		out = append(out, w)
	}
	return out, true
}

// segmentsFromInterfaceStrict mirrors TS's finish validation:
// Array.isArray(segments) && segments.every(isSegment), where isSegment
// requires a record with string text and numeric startSecond/endSecond.
func segmentsFromInterfaceStrict(v interface{}) ([]provider.TranscriptSegment, bool) {
	list, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]provider.TranscriptSegment, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		text, ok := m["text"].(string)
		if !ok {
			return nil, false
		}
		startSecond, ok := float64FromInterface(m["startSecond"])
		if !ok {
			return nil, false
		}
		endSecond, ok := float64FromInterface(m["endSecond"])
		if !ok {
			return nil, false
		}
		out = append(out, provider.TranscriptSegment{Text: text, StartSecond: startSecond, EndSecond: endSecond})
	}
	return out, true
}

func mapFromInterface(v interface{}) map[string]interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

func stringMapFromInterface(v interface{}) map[string]string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	return out
}

func float64FromInterface(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func float64PtrFromInterface(v interface{}) *float64 {
	if f, ok := float64FromInterface(v); ok {
		return &f
	}
	return nil
}

func intPtrFromInterface(v interface{}) *int {
	if f, ok := float64FromInterface(v); ok {
		i := int(f)
		return &i
	}
	return nil
}

var (
	_ provider.TranscriptionStreamer = (*TranscriptionModel)(nil)
	_ provider.TranscriptionStream   = (*gatewayTranscriptionStream)(nil)
)
