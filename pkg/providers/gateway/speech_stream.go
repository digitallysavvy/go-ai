package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/speechstream"
	wsutil "github.com/digitallysavvy/go-ai/pkg/providerutils/websocket"
	"golang.org/x/net/websocket"
)

const (
	// maxSpeechTextDeltaCodeUnits keeps text-delta frames under 64 KiB
	// (envelope rule 7) even when JSON escapes every UTF-16 code unit as
	// \uXXXX (TS MAX_TEXT_DELTA_CODE_UNITS).
	maxSpeechTextDeltaCodeUnits = 8 * 1024

	// maxSpeechServerFrameBytes bounds a single server frame the client will
	// buffer. Audio chunks are small; this only limits a misbehaving server.
	maxSpeechServerFrameBytes = 8 << 20
)

// SpeechClientSecretOptions configures GetSpeechToken.
type SpeechClientSecretOptions struct {
	// ExpiresAfterSeconds is the token lifetime in seconds. Gateway default is 60s (max 300s).
	ExpiresAfterSeconds *int
}

// SpeechClientSecretResult is a minted speech-bound client secret, mirroring
// TS GatewaySpeechFactoryGetTokenResult.
type SpeechClientSecretResult struct {
	// Token is the minted "vcst_" client secret.
	Token string
	// URL is the WebSocket URL of the streaming speech surface for this model.
	URL string
	// ExpiresAt is the token expiry, epoch seconds.
	ExpiresAt *int64
}

// ExperimentalSpeech returns a speech model bound to modelID that supports
// experimental streaming speech (provider.SpeechStreamer). It mirrors the
// TypeScript SDK's callable gateway.experimental_speech(modelId); use
// GetSpeechToken for its .getToken() counterpart.
func (p *Provider) ExperimentalSpeech(modelID string) *SpeechModel {
	return NewSpeechModel(p, modelID)
}

// GetSpeechToken mints a short-lived, speech-bound client secret via the
// Gateway realtime client-secret route with routeKind "speech" (TS
// gateway.experimental_speech.getToken). The token is meant for a client that
// connects without holding the long-lived Gateway credential.
func (p *Provider) GetSpeechToken(ctx context.Context, modelID string, opts *SpeechClientSecretOptions) (*SpeechClientSecretResult, error) {
	params := MintRealtimeClientSecretParams{ModelID: modelID, RouteKind: "speech"}
	if opts != nil && opts.ExpiresAfterSeconds != nil {
		params.ExpiresAfterSeconds = opts.ExpiresAfterSeconds
	}
	secret, err := p.MintRealtimeClientSecret(ctx, params)
	if err != nil {
		return nil, err
	}
	return &SpeechClientSecretResult{
		Token:     secret.Token,
		URL:       ToGatewaySpeechURL(p.baseURL, modelID),
		ExpiresAt: secret.ExpiresAt,
	}, nil
}

// ToGatewaySpeechURL builds the Gateway streaming speech WebSocket URL for
// modelID (TS toGatewaySpeechUrl): the HTTP(S) base is upgraded to WS(S) and
// the model id is passed as the "ai-model-id" query parameter.
func ToGatewaySpeechURL(baseURL string, modelID string) string {
	u, err := url.Parse(strings.Replace(baseURL, "http", "ws", 1) + "/speech-model")
	if err != nil {
		return ""
	}
	query := u.Query()
	query.Set("ai-model-id", modelID)
	u.RawQuery = query.Encode()
	return u.String()
}

// splitSpeechTextDelta splits a long text delta into pieces of at most
// maxSpeechTextDeltaCodeUnits UTF-16 code units without separating a
// surrogate pair (TS splitTextDelta).
func splitSpeechTextDelta(delta string) []string {
	units := utf16.Encode([]rune(delta))
	var pieces []string
	for offset := 0; offset < len(units); {
		end := offset + maxSpeechTextDeltaCodeUnits
		if end > len(units) {
			end = len(units)
		}
		if end < len(units) && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff {
			end--
		}
		pieces = append(pieces, string(utf16.Decode(units[offset:end])))
		offset = end
	}
	return pieces
}

// DoStreamSpeech streams speech for streamed text over the Gateway's
// streaming speech WebSocket, using the ai-gateway-speech.v1 subprotocol and
// the speech-stream envelope. Mirrors TS GatewaySpeechModel#doStream.
func (m *SpeechModel) DoStreamSpeech(ctx context.Context, opts *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
	if opts == nil || opts.Text == nil {
		return nil, errors.New("speech stream requires a text stream")
	}
	token, authMethod, err := m.provider.authResolver(ctx)
	if err != nil {
		opts.Text.Cancel(err)
		return nil, err
	}

	// TS: combineHeaders(config.headers (auth included), options.headers,
	// model headers, o11y) so a per-call Authorization / auth-method / team
	// header wins, and the subprotocols derive from the merged set.
	headers := mergeHeadersFold(
		map[string]string{"Authorization": "Bearer " + token, "ai-gateway-auth-method": authMethod},
		m.provider.headers, opts.Headers, m.getModelConfigHeaders(),
	)
	AddO11yHeaders(headers, GetO11yHeaders(ctx))
	authMethod = gatewayHeaderValue(headers, "ai-gateway-auth-method")

	start := speechstream.StartFrame{
		Type:         speechstream.StartFrameType,
		Voice:        opts.Voice,
		Instructions: opts.Instructions,
		Speed:        opts.Speed,
		Language:     opts.Language,
	}
	if opts.OutputFormat != nil {
		format := &speechstream.AudioFormat{Type: opts.OutputFormat.Type}
		if opts.OutputFormat.Rate != nil {
			rate := float64(*opts.OutputFormat.Rate)
			format.Rate = &rate
		}
		start.OutputFormat = format
	}
	if opts.ProviderOptions != nil {
		start.ProviderOptions = opts.ProviderOptions
	}
	if opts.IncludeRawChunks {
		raw := true
		start.IncludeRawChunks = &raw
	}

	// The WebSocket URL is derived from the configured base URL only, so the
	// credentials in headers and subprotocols never leave the configured host.
	wsURL := ToGatewaySpeechURL(m.provider.baseURL, m.modelID)
	protocols := []string{GatewaySpeechSubprotocol}
	if bearer := gatewayHeaderValue(headers, "authorization"); strings.HasPrefix(bearer, "Bearer ") {
		protocols = GetGatewaySpeechProtocols(strings.TrimPrefix(bearer, "Bearer "), gatewayTeamFromHeaders(headers))
	}

	abortCtx := opts.AbortSignal
	if abortCtx == nil {
		abortCtx = ctx
	}

	stream := newGatewaySpeechStream(abortCtx, gatewaySpeechStreamConfig{
		url:        wsURL,
		protocols:  protocols,
		headers:    headers,
		startFrame: start,
		text:       opts.Text,
		authMethod: authMethod,
		wrapError:  m.provider.gatewayUnknownError,
	})

	return &provider.SpeechStreamResult{
		Stream:      stream,
		RequestBody: start,
		Response:    &provider.SpeechStreamResponseMetadata{Timestamp: time.Now(), ModelID: m.modelID},
	}, nil
}

type gatewaySpeechStreamConfig struct {
	url        string
	protocols  []string
	headers    map[string]string
	startFrame speechstream.StartFrame
	text       provider.SpeechTextStream
	authMethod string
	// wrapError mirrors TS asGatewayError (nil keeps errors unwrapped).
	wrapError func(error) error
}

// gatewaySpeechStream implements provider.SpeechStream over the AI Gateway's
// streaming speech WebSocket.
type gatewaySpeechStream struct {
	*wsutil.Session[provider.SpeechStreamPart]
	textExhausted atomic.Bool
}

func newGatewaySpeechStream(parentCtx context.Context, cfg gatewaySpeechStreamConfig) *gatewaySpeechStream {
	s := &gatewaySpeechStream{Session: wsutil.NewSession[provider.SpeechStreamPart](parentCtx)}
	go s.run(cfg)
	return s
}

func (s *gatewaySpeechStream) run(cfg gatewaySpeechStreamConfig) {
	defer s.CloseParts()
	// Release the session's resources as soon as run() returns for any
	// reason, not only on an explicit Close().
	defer s.CancelContext()
	var cancelTextOnce sync.Once
	cancelText := func(reason error) {
		cancelTextOnce.Do(func() { cfg.text.Cancel(reason) })
	}
	// The provider owns the text stream and releases it on every terminal
	// path: a normal finish before the text ended, a failure, a server error
	// part, and a cancellation.
	defer func() {
		if !s.textExhausted.Load() {
			cancelText(nil)
		}
	}()

	fail := func(err error) {
		// TS finishWithError runs every terminal error except an abort reason
		// through asGatewayError.
		if cfg.wrapError != nil && !gatewayerrors.IsGatewayError(err) &&
			!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			err = cfg.wrapError(err)
		}
		s.SetErr(err)
		cancelText(err)
	}

	conn, err := wsutil.Dial(s.Context(), cfg.url, wsutil.DialOptions{Headers: cfg.headers, Protocols: cfg.protocols})
	if err != nil {
		fail(err)
		return
	}
	s.SetConn(conn)
	defer conn.Close() //nolint:errcheck
	conn.MaxPayloadBytes = maxSpeechServerFrameBytes

	startPayload, err := json.Marshal(cfg.startFrame)
	if err != nil {
		fail(err)
		return
	}
	if err := wsutil.Send(s.Context(), conn, string(startPayload)); err != nil {
		fail(err)
		return
	}

	// textCtx (rather than the session's ctx directly) lets a server "error"
	// part stop text pumping while the receive loop below keeps running to
	// observe the server's terminal close (TS textStopped).
	textCtx, stopText := context.WithCancel(s.Context())
	defer stopText()
	textErrCh := make(chan error, 1)
	go s.pumpText(textCtx, conn, cfg.text, textErrCh)

	msgCh := make(chan wsutil.Message)
	go wsutil.ReceiveLoop(s.Context(), conn, msgCh)

	var lastServerError interface{}
	hasServerError := false

	for {
		select {
		case <-s.Context().Done():
			fail(context.Cause(s.Context()))
			return

		case textErr := <-textErrCh:
			fail(textErr)
			return

		case res := <-msgCh:
			if res.Err != nil {
				// An abnormal disconnect (TS onSocketError) wins over a
				// remembered server error part; only a clean close falls
				// through to the remembered error, then to the generic
				// "closed before a finish part" message.
				switch {
				case !wsutil.IsCleanClose(res.Err):
					fail(errors.New("Connection error on AI Gateway speech stream")) //nolint:staticcheck // matches TS SDK's exact error text
				case hasServerError:
					fail(gatewayStreamServerError(lastServerError, cfg.authMethod, "speech"))
				default:
					fail(errors.New("AI Gateway speech stream closed before a finish part was received.")) //nolint:staticcheck // matches TS SDK's exact error text
				}
				return
			}

			part, ok := speechstream.ParsePart(res.Text)
			if !ok {
				continue // malformed or unknown frames are ignored (envelope rule 6)
			}

			if part.Type == provider.SpeechStreamPartTypeFinish {
				s.Emit(*part)
				return
			}

			if part.Type == provider.SpeechStreamPartTypeError {
				// Error parts are terminal (envelope rule 5): stop sending
				// text and surface the server's message when the socket
				// closes.
				hasServerError = true
				lastServerError = part.Err
				stopText()
				cancelText(nil)
				part.Err = gatewayStreamServerError(part.Err, cfg.authMethod, "speech")
			}

			if !s.Emit(*part) {
				fail(context.Cause(s.Context()))
				return
			}
		}
	}
}

// pumpText forwards text parts as text-delta frames (long parts split to stay
// under the frame-size limit), then sends text-done at the end of the text.
// Failures are reported on errCh unless they stem from ctx being cancelled,
// which run()'s own handling covers.
func (s *gatewaySpeechStream) pumpText(ctx context.Context, conn *websocket.Conn, text provider.SpeechTextStream, errCh chan<- error) {
	send := func(frame interface{}) error {
		data, err := json.Marshal(frame)
		if err != nil {
			return err
		}
		return wsutil.Send(s.Context(), conn, string(data))
	}
	for {
		part, err := text.Next(ctx)
		if err != nil {
			if err == io.EOF {
				s.textExhausted.Store(true)
				if sendErr := send(map[string]string{"type": speechstream.TextDoneFrameType}); sendErr != nil && ctx.Err() == nil {
					wsutil.ReportError(ctx, errCh, sendErr)
				}
			} else if ctx.Err() == nil {
				wsutil.ReportError(ctx, errCh, err)
			}
			return
		}

		pieces := splitSpeechTextDelta(part.Text)
		// An empty part still carries its voice or provider options.
		if len(pieces) == 0 && (part.Voice != "" || part.ProviderOptions != nil) {
			pieces = []string{""}
		}
		for i, delta := range pieces {
			if ctx.Err() != nil {
				return
			}
			// The voice applies to every frame of the part; provider options
			// (e.g. a turn break) only to its first frame.
			frame := speechstream.TextDeltaFrame{Type: speechstream.TextDeltaFrameType, Delta: delta, Voice: part.Voice}
			if i == 0 {
				frame.ProviderOptions = part.ProviderOptions
			}
			if err := send(frame); err != nil {
				if ctx.Err() == nil {
					wsutil.ReportError(ctx, errCh, err)
				}
				return
			}
		}
	}
}

var (
	_ provider.SpeechStreamer = (*SpeechModel)(nil)
	_ provider.SpeechStream   = (*gatewaySpeechStream)(nil)
)
