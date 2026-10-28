package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// SSETransport implements the legacy MCP SSE transport.
//
// It mirrors the TypeScript SseMCPTransport wire behavior: connect with a GET
// SSE request, wait for the first same-origin endpoint event, POST JSON-RPC
// messages to that locked endpoint, and receive JSON-RPC messages from message
// events or events without an explicit event field.
type SSETransport struct {
	url       string
	baseURL   *url.URL
	endpoint  *url.URL
	client    *http.Client
	sseClient SSEClient
	// streamClient is client without http.Client.Timeout, which would cut the
	// long-lived event stream off after the request timeout.
	streamClient *http.Client

	receiveMu    sync.Mutex
	receiveQueue []*MCPMessage

	mu              sync.Mutex
	connected       bool
	protocolVersion string
	streamBody      io.Closer
	cancel          context.CancelFunc
	// streamCtx is the transport-lifetime context (TS abortController
	// signal), canceled by Close. Every Send POST also watches it.
	streamCtx context.Context
	streamErr error
	// streamGen identifies the current stream. Connect and Close each bump
	// it, so a reader goroutine left over from a closed or failed stream
	// (Close does not wait for it) cannot clear the state of a newer one.
	streamGen uint64

	config TransportConfig
	oauth  *OAuthConfig
	auth   *transportAuth
	// onError receives non-fatal diagnostics (TS onerror).
	onError func(error)

	// refreshMu/refreshCh/refreshErr single-flight concurrent OAuth token
	// refreshes triggered by a 401 response (hash 4b5cb49), mirroring
	// HTTPTransport.refreshOAuthToken.
	refreshMu  sync.Mutex
	refreshCh  chan struct{}
	refreshErr error
}

// SSETransportConfig contains configuration for legacy SSE transport.
type SSETransportConfig struct {
	// URL is the SSE endpoint URL of the MCP server.
	URL string

	// TimeoutMS is the HTTP request timeout.
	TimeoutMS int

	// OAuth configuration (optional).
	OAuth *OAuthConfig

	// Config is the base transport configuration.
	Config TransportConfig

	// HTTPClient is an optional custom HTTP client used for GET and POST requests.
	HTTPClient *http.Client

	// SSEClient is an optional custom SSE-capable client used instead of
	// HTTPClient when supplied.
	SSEClient SSEClient

	// AuthProvider enables the MCP OAuth flow: its tokens are sent as a
	// bearer token, and a 401 response runs Auth once (shared by concurrent
	// requests) before the request is retried. Matches TS SseMCPTransport's
	// authProvider. When Auth cannot complete without user interaction the
	// request fails with *UnauthorizedError.
	AuthProvider OAuthClientProvider `json:"-"`

	// AuthHTTPClient is the HTTP client used for the OAuth discovery,
	// registration and token requests Auth makes (TS passes the transport's
	// fetch). Nil uses Auth's default client.
	AuthHTTPClient *http.Client `json:"-"`

	// OnError, when set, receives non-fatal diagnostics (TS onerror).
	OnError func(error) `json:"-"`
}

// NewSSETransport creates a new legacy SSE transport.
func NewSSETransport(config SSETransportConfig) *SSETransport {
	timeout := time.Duration(config.TimeoutMS) * time.Millisecond
	// No default: TS fetch has no request timeout, the caller's ctx governs.

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout == 0 && timeout > 0 {
		httpClient.Timeout = timeout
	}

	if config.Config.Redirect != MCPRedirectFollow {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("mcp: server returned redirect to %s; set Redirect: MCPRedirectFollow to allow redirects", req.URL)
		}
	}

	baseURL, _ := url.Parse(config.URL)
	streamClient := *httpClient
	streamClient.Timeout = 0
	return &SSETransport{
		url:          config.URL,
		baseURL:      baseURL,
		client:       httpClient,
		streamClient: &streamClient,
		sseClient:    config.SSEClient,
		receiveQueue: make([]*MCPMessage, 0),
		config:       config.Config,
		oauth:        config.OAuth,
		auth:         newTransportAuth(config.AuthProvider, config.URL, config.AuthHTTPClient),
		onError:      config.OnError,
	}
}

// sendFailed reports a send failure through OnError unless the caller's own
// context was cancelled (TS: `if (options?.signal?.aborted) throw error`).
func (t *SSETransport) sendFailed(ctx context.Context, err error) error {
	if ctx.Err() == nil {
		t.reportError(err)
	}
	return err
}

func (t *SSETransport) reportError(err error) {
	if t.onError != nil && err != nil {
		t.onError(err)
	}
}

// Connect establishes the SSE stream and waits for the first endpoint event.
func (t *SSETransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	if t.connected {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	if t.baseURL == nil || t.baseURL.Scheme == "" || t.baseURL.Host == "" {
		return NewMCPClientError(0, "MCP SSE Transport Error: invalid URL", nil)
	}

	// The stream outlives this call: only the handshake is bound to ctx
	// (TS start() has no caller signal), so cancelling a setup context after
	// Connect returns does not drop the connection.
	reqCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stopWatch := context.AfterFunc(ctx, cancel)
	// Install the stream's abort handle before the GET, as TS start() creates
	// its abortController first: a Close during the handshake aborts the
	// request instead of being lost. A Connect still in flight is superseded
	// and its request or stream is aborted, so its reader cannot leak.
	t.mu.Lock()
	prevCancel, prevBody := t.cancel, t.streamBody
	t.streamGen++
	gen := t.streamGen
	t.cancel = cancel
	t.streamCtx = reqCtx
	t.streamBody = nil
	t.mu.Unlock()
	if prevCancel != nil {
		prevCancel()
	}
	if prevBody != nil {
		prevBody.Close() //nolint:errcheck
	}
	// fail ends a handshake that never started a reader. Once Close (or a
	// later Connect) owns the transport, TS is silent: the aborted fetch
	// returns without onerror.
	fail := func(err error) error {
		stopWatch()
		cancel()
		if !t.releaseHandshake(gen) {
			return errSSEClosedDuringConnect()
		}
		if ctx.Err() == nil {
			t.reportError(err)
		}
		return err
	}
	var resp *http.Response
	// TS fails the connection when an OK response has no body (`!response.ok
	// || !response.body`); only a custom SSEClient can return one.
	missingBody := false
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, t.url, nil)
		if err != nil {
			return fail(NewTransportError("failed to create SSE request", err))
		}
		if err := t.applyHeaders(ctx, req, map[string]string{"Accept": "text/event-stream"}); err != nil {
			// TS: a failing tokens() throws into establishConnection's catch,
			// which reports it.
			return fail(NewTransportError("failed to load OAuth tokens", err))
		}

		client := SSEClient(t.streamClient)
		if t.sseClient != nil {
			client = t.sseClient
		}
		resp, err = client.Do(req)
		if err != nil {
			// TS reports a failed fetch through onerror unless it was aborted.
			return fail(NewTransportError("failed to establish SSE connection", err))
		}
		if resp == nil {
			return fail(NewTransportError("failed to establish SSE connection", fmt.Errorf("nil HTTP response")))
		}
		missingBody = resp.Body == nil
		if missingBody {
			resp.Body = io.NopCloser(bytes.NewReader(nil))
		}
		if resp.StatusCode == http.StatusUnauthorized && t.auth.enabled() && attempt == 0 {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, mcpMaxErrorBodyBytes))
			resp.Body.Close() //nolint:errcheck
			if err := t.auth.authorize(ctx, resp); err != nil {
				// TS authorizeOnce is not tied to the abort signal, so its
				// failure is reported even after close(); only the caller's
				// own cancellation (Go-only) is not.
				stopWatch()
				cancel()
				t.releaseHandshake(gen)
				if ctx.Err() == nil {
					t.reportError(err)
				}
				return err
			}
			continue
		}
		break
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || missingBody {
		defer resp.Body.Close() //nolint:errcheck
		statusText := responseStatusText(resp)
		// TS #19430: keep the (bounded) response body on the error.
		text := string(readErrorBody(resp.Body))
		message := fmt.Sprintf("MCP SSE Transport Error: %d %s", resp.StatusCode, statusText)
		if text != "" {
			message += ": " + text
		}
		if resp.StatusCode == http.StatusMethodNotAllowed {
			message += ". This server does not support SSE transport. Try using `http` transport instead"
		}
		return fail(NewMCPClientError(0, message, nil, WithMCPHTTPResponse(resp.StatusCode, t.url, text)))
	}

	ready := make(chan error, 1)
	t.mu.Lock()
	if t.streamGen != gen {
		// Close (or a later Connect) ran during the handshake.
		t.mu.Unlock()
		stopWatch()
		cancel()
		resp.Body.Close() //nolint:errcheck
		return errSSEClosedDuringConnect()
	}
	t.streamBody = resp.Body
	t.streamErr = nil
	t.mu.Unlock()
	go t.readSSEStream(resp.Body, gen, ready)

	select {
	case err := <-ready:
		stopWatch()
		if err != nil {
			// A stale stream means Close ran while Connect waited: it
			// already reset the transport (and a later Connect may own it
			// now), and TS reports nothing once aborted.
			if t.closeStream(gen) {
				t.reportError(err)
			}
			return err
		}
		return nil
	case <-ctx.Done():
		stopWatch()
		// Close only this Connect's stream: a Close and a later Connect may
		// have replaced it while ctx ended.
		t.closeStream(gen)
		return ctx.Err()
	}
}

// releaseHandshake clears the abort handle a failed handshake installed and
// reports whether gen was still the current stream (no Close or later
// Connect since).
func (t *SSETransport) releaseHandshake(gen uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.streamGen != gen {
		return false
	}
	t.cancel = nil
	t.streamCtx = nil
	t.streamBody = nil
	return true
}

// closeStream closes the transport if gen is still its current stream and
// reports whether it did.
func (t *SSETransport) closeStream(gen uint64) bool {
	t.mu.Lock()
	if t.streamGen != gen {
		t.mu.Unlock()
		return false
	}
	t.closeLocked()
	return true
}

// Close closes the SSE stream and clears the locked endpoint.
func (t *SSETransport) Close() error {
	t.closeWithoutCallback()
	return nil
}

func (t *SSETransport) closeWithoutCallback() {
	t.mu.Lock()
	t.closeLocked()
}

// closeLocked resets the transport and aborts its stream. It is called with
// t.mu held and releases it.
func (t *SSETransport) closeLocked() {
	body := t.streamBody
	cancel := t.cancel
	t.connected = false
	t.endpoint = nil
	t.streamBody = nil
	t.cancel = nil
	t.streamCtx = nil
	t.streamErr = io.EOF
	t.streamGen++
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if body != nil {
		body.Close() //nolint:errcheck
	}
}

// Send posts a JSON-RPC message to the endpoint received from the SSE stream.
func (t *SSETransport) Send(ctx context.Context, message *MCPMessage) error {
	// TS send() starts with options.signal.throwIfAborted().
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	connected := t.connected
	endpoint := t.endpoint
	streamCtx := t.streamCtx
	t.mu.Unlock()

	if !connected || endpoint == nil {
		return NewMCPClientError(0, "MCP SSE Transport Error: Not connected", nil)
	}

	// TS send() signals the POST with AbortSignal.any([transportSignal,
	// options.signal]): Close aborts an in-flight POST. sendFailed still
	// checks only the caller's ctx, so that abort is reported, as in TS.
	reqCtx := ctx
	if streamCtx != nil {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(streamCtx, cancel)
		defer stop()
	}

	data, err := json.Marshal(message)
	if err != nil {
		return t.sendFailed(ctx, NewTransportError("failed to marshal message", err))
	}
	if t.config.EnableLogging {
		fmt.Printf("MCP SSE Send: %s\n", string(data))
	}

	// Retry once on a 401, refreshing the OAuth token first (hash 4b5cb49).
	// Concurrent Send calls share a single in-flight refresh; a 401 that
	// arrives after another call already refreshed the token (a "stale" 401
	// for the old token) retries immediately with the now-current token
	// instead of triggering a second, redundant refresh.
	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint.String(), bytes.NewReader(data))
		if err != nil {
			return t.sendFailed(ctx, NewTransportError("failed to create request", err))
		}
		// TS builds the headers (authProvider.tokens()) inside attempt's try,
		// so a failure is reported through onerror like a failed fetch.
		if err := t.applyHeaders(ctx, req, map[string]string{"Content-Type": "application/json"}); err != nil {
			return t.sendFailed(ctx, NewTransportError("failed to load OAuth tokens", err))
		}
		sentToken, _, _ := t.oauthTokenSnapshot()

		client := SSEClient(t.client)
		if t.sseClient != nil {
			client = t.sseClient
		}
		resp, err = client.Do(req)
		if err != nil {
			return t.sendFailed(ctx, NewTransportError("failed to send request", err))
		}
		if resp == nil {
			return t.sendFailed(ctx, NewTransportError("failed to send request", fmt.Errorf("nil HTTP response")))
		}
		if resp.Body == nil {
			resp.Body = io.NopCloser(bytes.NewReader(nil))
		}

		if resp.StatusCode != http.StatusUnauthorized || attempt == 1 || (!t.oauthConfigured() && !t.auth.enabled()) {
			break
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, mcpMaxErrorBodyBytes))
		resp.Body.Close() //nolint:errcheck

		if t.auth.enabled() {
			// Another request may already have refreshed the token (TS
			// accessTokenChanged): retry with it instead of re-authorizing.
			if t.auth.tokenChanged(ctx, req.Header.Get("Authorization")) {
				continue
			}
			if err := t.auth.authorize(ctx, resp); err != nil {
				return t.sendFailed(ctx, err)
			}
			continue
		}
		if currentToken, _, _ := t.oauthTokenSnapshot(); currentToken == "" || currentToken == sentToken {
			if err := t.refreshOAuthToken(ctx); err != nil {
				return t.sendFailed(ctx, NewTransportError("failed to refresh OAuth token", err))
			}
		}
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body := readErrorBody(resp.Body)
		message := fmt.Sprintf("MCP SSE Transport Error: POSTing to endpoint (HTTP %d): %s", resp.StatusCode, string(body))
		return t.sendFailed(ctx, NewMCPClientError(0, message, nil, WithMCPHTTPResponse(resp.StatusCode, endpoint.String(), string(body))))
	}
	drainBody(resp.Body)
	return nil
}

// Receive receives messages queued from the SSE stream.
func (t *SSETransport) Receive(ctx context.Context) (*MCPMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		t.receiveMu.Lock()
		if len(t.receiveQueue) > 0 {
			msg := t.receiveQueue[0]
			t.receiveQueue = t.receiveQueue[1:]
			t.receiveMu.Unlock()
			return msg, nil
		}
		t.receiveMu.Unlock()
		t.mu.Lock()
		connected := t.connected
		streamErr := t.streamErr
		t.mu.Unlock()
		if !connected && streamErr != nil {
			return nil, streamErr
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// IsConnected returns true when the transport has a locked SSE endpoint.
func (t *SSETransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// SetProtocolVersion stores the negotiated MCP protocol version for outbound
// transport request headers.
func (t *SSETransport) SetProtocolVersion(protocolVersion string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.protocolVersion = protocolVersion
}

// ProtocolVersion returns the negotiated protocol version, or the latest
// supported version before initialization completes.
func (t *SSETransport) ProtocolVersion() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.protocolVersion != "" {
		return t.protocolVersion
	}
	return ProtocolVersion
}

func (t *SSETransport) applyHeaders(ctx context.Context, req *http.Request, base map[string]string) error {
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	for k, v := range base {
		req.Header.Set(k, v)
	}
	req.Header.Set("mcp-protocol-version", t.ProtocolVersion())
	if token, expired, ok := t.oauthTokenSnapshot(); ok {
		if expired {
			_ = t.refreshOAuthToken(ctx)
			token, _, ok = t.oauthTokenSnapshot()
		}
		if ok && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	setMCPUserAgent(req.Header)
	return t.auth.setBearer(ctx, req.Header)
}

func (t *SSETransport) readSSEStream(body io.ReadCloser, gen uint64, ready chan<- error) {
	defer body.Close() //nolint:errcheck
	parser := streaming.NewSSEParser(body)
	readySent := false
	for {
		event, err := parser.Next()
		if err == io.EOF {
			t.mu.Lock()
			// A Close (or a later Connect) since this stream started owns
			// the transport state now: leave it alone (TS returns silently
			// once its abortController is aborted).
			aborted := t.streamGen != gen
			var unexpected error
			if !aborted {
				wasConnected := t.connected
				t.connected = false
				t.endpoint = nil
				if readySent && wasConnected {
					unexpected = NewMCPClientError(0, "MCP SSE Transport Error: Connection closed unexpectedly", nil)
					t.streamErr = unexpected
				} else {
					t.streamErr = io.EOF
				}
			}
			t.mu.Unlock()
			if !readySent {
				if aborted {
					ready <- errSSEClosedDuringConnect()
				} else {
					ready <- NewMCPClientError(0, "MCP SSE Transport Error: Connection closed before endpoint event was received", nil)
				}
			}
			// TS processEvents: a stream that ends after the endpoint event
			// throws into its catch, which reports through onerror (an
			// intentional close returns silently first).
			if unexpected != nil && !aborted {
				t.reportError(unexpected)
			}
			return
		}
		if err != nil {
			t.mu.Lock()
			aborted := t.streamGen != gen // closed via Close, or replaced
			if !aborted {
				t.connected = false
				t.endpoint = nil
				t.streamErr = NewMCPClientError(0, "MCP SSE Transport Error: Connection closed unexpectedly", nil)
			}
			t.mu.Unlock()
			if !readySent {
				if aborted {
					ready <- errSSEClosedDuringConnect()
				} else {
					ready <- NewTransportError("failed to read SSE event", err)
				}
			} else if !aborted {
				t.reportError(NewTransportError("failed to read SSE event", err))
			}
			return
		}

		if t.isStaleStream(gen) {
			if !readySent {
				ready <- errSSEClosedDuringConnect()
			}
			return
		}
		if event.Event == "endpoint" {
			if t.hasEndpoint() {
				continue
			}
			endpoint, endpointErr := url.Parse(event.Data)
			if endpointErr != nil {
				if !readySent {
					ready <- endpointErr
				}
				return
			}
			endpoint = t.baseURL.ResolveReference(endpoint)
			if origin(endpoint) != origin(t.baseURL) {
				t.mu.Lock()
				if t.streamGen == gen {
					t.connected = false
					t.endpoint = nil
				}
				t.mu.Unlock()
				if !readySent {
					ready <- NewMCPClientError(0, fmt.Sprintf("MCP SSE Transport Error: Endpoint origin does not match connection origin: %s", origin(endpoint)), nil)
				}
				return
			}
			t.mu.Lock()
			if t.streamGen != gen {
				t.mu.Unlock()
				if !readySent {
					ready <- errSSEClosedDuringConnect()
				}
				return
			}
			t.endpoint = endpoint
			t.connected = true
			t.mu.Unlock()
			if !readySent {
				readySent = true
				ready <- nil
			}
			continue
		}

		if event.Event != "" && event.Event != "message" {
			continue
		}
		msg, err := parseMCPMessage([]byte(event.Data))
		if err != nil {
			// TS reports the parse failure through onerror and keeps reading.
			t.reportError(&MCPClientError{Message: "MCP SSE Transport Error: Failed to parse message", Cause: err})
			continue
		}
		// Queue only while this stream is current: a Close and reconnect
		// between the stale check above and here must not deliver it.
		t.mu.Lock()
		if t.streamGen != gen {
			t.mu.Unlock()
			return
		}
		t.queueReceivedMessages([]*MCPMessage{msg})
		t.mu.Unlock()
	}
}

// errSSEClosedDuringConnect is what Connect returns when Close ends the
// stream before its endpoint event. TS start() never settles in that case
// (the aborted reader returns silently); a blocking Connect has to return.
func errSSEClosedDuringConnect() error {
	return NewMCPClientError(0, "MCP SSE Transport Error: Transport closed before endpoint event was received", nil)
}

// isStaleStream reports whether the stream gen belongs to has been closed or
// replaced by a later Connect.
func (t *SSETransport) isStaleStream(gen uint64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.streamGen != gen
}

func (t *SSETransport) hasEndpoint() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.endpoint != nil
}

func (t *SSETransport) queueReceivedMessages(messages []*MCPMessage) {
	if len(messages) == 0 {
		return
	}
	t.receiveMu.Lock()
	t.receiveQueue = append(t.receiveQueue, messages...)
	t.receiveMu.Unlock()
}

// refreshOAuthToken refreshes the OAuth access token. Concurrent callers
// (e.g. multiple in-flight Send calls that both hit a 401) are deduplicated
// into a single underlying refresh (hash 4b5cb49), mirroring
// HTTPTransport.refreshOAuthToken.
func (t *SSETransport) refreshOAuthToken(ctx context.Context) error {
	if t.oauth == nil {
		return fmt.Errorf("OAuth not configured")
	}

	t.refreshMu.Lock()
	if t.refreshCh != nil {
		ch := t.refreshCh
		t.refreshMu.Unlock()
		select {
		case <-ch:
			t.refreshMu.Lock()
			err := t.refreshErr
			t.refreshMu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.refreshCh = make(chan struct{})
	t.refreshMu.Unlock()

	err := t.doRefreshOAuthToken(ctx)

	t.refreshMu.Lock()
	t.refreshErr = err
	close(t.refreshCh)
	t.refreshCh = nil
	t.refreshMu.Unlock()
	return err
}

func (t *SSETransport) doRefreshOAuthToken(ctx context.Context) error {
	t.mu.Lock()
	oauth := t.oauth
	t.mu.Unlock()
	if oauth == nil || oauth.RefreshTokenFunc == nil {
		return fmt.Errorf("OAuth refresh not yet implemented - please provide access token manually")
	}
	token, expiresIn, err := oauth.RefreshTokenFunc(ctx, oauth)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("OAuth refresh returned empty access token")
	}
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	t.mu.Lock()
	if t.oauth != nil {
		t.oauth.AccessToken = token
		t.oauth.ExpiresAt = time.Now().Add(expiresIn)
	}
	t.mu.Unlock()
	return nil
}

func (t *SSETransport) oauthConfigured() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.oauth != nil
}

func (t *SSETransport) oauthTokenSnapshot() (token string, expired bool, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.oauth == nil || t.oauth.AccessToken == "" {
		return "", false, false
	}
	return t.oauth.AccessToken, time.Now().After(t.oauth.ExpiresAt), true
}
