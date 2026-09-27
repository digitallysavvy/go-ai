package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

const mcpHTTPAcceptHeader = "application/json, text/event-stream"

// Inbound SSE reconnection backoff, matching TS HttpMCPTransport's
// `reconnectionOptions` (mcp-http-transport.ts): 1s initial delay, 1.5x
// growth, capped at 30s, giving up after 2 attempts.
const (
	inboundSSEInitialReconnectionDelay    = 1 * time.Second
	inboundSSEMaxReconnectionDelay        = 30 * time.Second
	inboundSSEReconnectionDelayGrowFactor = 1.5
	inboundSSEMaxRetries                  = 2
)

// HTTPTransport implements the Transport interface for HTTP-based communication
// This transport communicates with MCP servers over HTTP
type HTTPTransport struct {
	// URL of the MCP server
	url string

	// HTTP client
	client    *http.Client
	sseClient SSEClient

	// Message queue for receiving
	receiveMu    sync.Mutex
	receiveQueue []*MCPMessage

	// State
	connected       bool
	protocolVersion string
	mu              sync.Mutex

	// Configuration
	config TransportConfig

	// OAuth
	oauth      *OAuthConfig
	refreshMu  sync.Mutex
	refreshCh  chan struct{}
	refreshErr error

	// Streamable HTTP session (hash 241a8c5): sessionID is captured from an
	// `mcp-session-id` response header and echoed on subsequent requests.
	sessionID               string
	onSessionIDChange       func(sessionID string)
	onSessionExpired        func(sessionID string)
	terminateSessionOnClose bool

	// onError receives non-fatal diagnostics from the background inbound SSE
	// listener, the send()/POST path (non-ok responses, fetch failures,
	// session expiry), and the POST-response SSE reader, matching TS
	// HttpMCPTransport's `onerror` callback (mcp-http-transport.ts): every
	// site that calls `this.onerror?.(...)` there has a matching call here.
	onError func(error)

	// sseLifecycleCtx/sseLifecycleCancel is the transport-lifetime context,
	// matching TS HttpMCPTransport's `this.abortController` (created once in
	// start(), aborted in close()). It is not only for the standing inbound
	// SSE GET listener below: every outbound Send() POST (and, when a POST
	// response is itself text/event-stream, the reader draining it) derives
	// its own per-call context by combining this one with the caller's ctx
	// (see deriveRequestContext), matching TS send()'s
	// `AbortSignal.any([transportSignal, options.signal])`. sseConnCancel
	// cancels just the current inbound GET/read, used when the negotiated
	// protocol switches to modern mid-flight. sseClosing gates every place
	// that would otherwise race a WaitGroup.Add against Close()'s Wait:
	// startInboundSSE (sseWG) and the POST-response SSE reader (postSSEWG).
	sseMu                sync.Mutex
	sseLifecycleCtx      context.Context
	sseLifecycleCancel   context.CancelFunc
	sseClosing           bool
	sseConnCancel        context.CancelFunc
	sseReconnectAttempts int
	sseReconnectTimer    *time.Timer
	lastInboundEventID   string
	sseWG                sync.WaitGroup

	// postSSEWG tracks background goroutines draining a text/event-stream
	// POST response (readMCPHTTPSSEMessages), so Close() can wait for them to
	// exit rather than leaking them, matching this slice's "goroutines must
	// exit" requirement (TS has no equivalent: JS has no goroutines to leak).
	postSSEWG sync.WaitGroup
}

// HTTPTransportConfig contains configuration for HTTP transport
type HTTPTransportConfig struct {
	// URL is the URL of the MCP server
	URL string

	// Timeout is the HTTP request timeout
	TimeoutMS int

	// OAuth configuration (optional)
	OAuth *OAuthConfig

	// Config is the base transport configuration
	Config TransportConfig

	// HTTPClient is an optional custom HTTP client used for all MCP HTTP requests.
	// Useful for TLS customization, proxy settings, and custom dialers.
	HTTPClient *http.Client

	// SSEClient is an optional custom SSE-capable client used instead of
	// HTTPClient when supplied. It lets callers provide custom event-stream
	// transports, TLS settings, proxy behavior, or dialers.
	SSEClient SSEClient

	// InitialSessionID resumes a previously established streamable HTTP
	// session (hash 241a8c5), sent as `mcp-session-id` on every request until
	// the server issues a new one.
	InitialSessionID string

	// OnSessionIDChange is called whenever the server assigns or changes the
	// session id (from an `mcp-session-id` response header).
	OnSessionIDChange func(sessionID string) `json:"-"`

	// OnSessionExpired is called when the server responds 404 to a request
	// carrying a session id, indicating that session is no longer valid.
	OnSessionExpired func(sessionID string) `json:"-"`

	// TerminateSessionOnClose sends a best-effort DELETE with the session id
	// when Close is called. Default true.
	TerminateSessionOnClose *bool

	// OnError, when set, receives non-fatal diagnostics from the background
	// inbound SSE listener (GET reconnect failures, malformed messages,
	// etc.), the send()/POST path (non-2xx responses, fetch failures, OAuth
	// refresh failures, session expiry), and the POST-response SSE reader
	// (stream read failures, malformed messages), matching TS
	// HttpMCPTransport's `onerror` callback. Optional.
	OnError func(error) `json:"-"`
}

// SSEClient is the minimal interface needed by custom SSE-capable transports.
type SSEClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// OAuthConfig contains OAuth configuration
type OAuthConfig struct {
	// TokenURL is the OAuth token endpoint
	TokenURL string

	// ClientID is the OAuth client ID
	ClientID string

	// ClientSecret is the OAuth client secret
	ClientSecret string

	// Scopes are the OAuth scopes to request
	Scopes []string

	// AccessToken is the current access token
	AccessToken string

	// RefreshToken is the refresh token
	RefreshToken string

	// ExpiresAt is when the access token expires
	ExpiresAt time.Time

	// RefreshTokenFunc refreshes the OAuth token. When nil, refresh attempts
	// return an error and callers should provide a fresh access token manually.
	RefreshTokenFunc func(ctx context.Context, cfg *OAuthConfig) (accessToken string, expiresIn time.Duration, err error) `json:"-"`
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(config HTTPTransportConfig) *HTTPTransport {
	timeout := time.Duration(config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout == 0 {
		httpClient.Timeout = timeout
	}

	// Default redirect policy: error on redirect (security-first posture for MCP).
	// Callers can opt into following redirects by setting Redirect: MCPRedirectFollow.
	if config.Config.Redirect != MCPRedirectFollow {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("mcp: server returned redirect to %s; set Redirect: MCPRedirectFollow to allow redirects", req.URL)
		}
	}

	terminateSessionOnClose := true
	if config.TerminateSessionOnClose != nil {
		terminateSessionOnClose = *config.TerminateSessionOnClose
	}

	return &HTTPTransport{
		url:                     config.URL,
		client:                  httpClient,
		sseClient:               config.SSEClient,
		receiveQueue:            make([]*MCPMessage, 0),
		config:                  config.Config,
		oauth:                   config.OAuth,
		sessionID:               config.InitialSessionID,
		onSessionIDChange:       config.OnSessionIDChange,
		onSessionExpired:        config.OnSessionExpired,
		terminateSessionOnClose: terminateSessionOnClose,
		onError:                 config.OnError,
	}
}

// SessionID returns the current streamable HTTP session id, or "" if none
// has been established yet (hash 241a8c5).
func (t *HTTPTransport) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

func (t *HTTPTransport) setSessionID(sessionID string) {
	t.mu.Lock()
	changed := t.sessionID != sessionID
	t.sessionID = sessionID
	onChange := t.onSessionIDChange
	t.mu.Unlock()
	if changed && onChange != nil {
		onChange(sessionID)
	}
}

// expireSessionID clears the session id after the server responds 404 to a
// request that carried it, matching TS HTTPTransport.expireSessionId (hash
// 241a8c5).
func (t *HTTPTransport) expireSessionID(expired string) {
	t.mu.Lock()
	if t.sessionID != expired {
		t.mu.Unlock()
		return
	}
	t.sessionID = ""
	onExpired := t.onSessionExpired
	t.mu.Unlock()
	if onExpired != nil {
		onExpired(expired)
	}
}

// applyStandardHeaders sets the headers common to every streamable HTTP
// request, including the `mcp-session-id` header when a session is
// established, and returns the session id (if any) that was attached so the
// caller can detect a 404 session-expiry for this specific request.
//
// The session id is attached only when includeSessionID is true AND the
// negotiated protocol is not the modern (2026-07-28) one, matching TS
// commonHeaders' `!this.isModernProtocol() && includeSessionId &&
// this.sessionId`. Callers pass includeSessionID=false for an `initialize`
// request, matching TS send()'s `includeSessionId: !isInitializeRequest`
// (a fresh session has not been established yet, so nothing to echo).
func (t *HTTPTransport) applyStandardHeaders(req *http.Request, includeSessionID bool) (sentSessionID string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", mcpHTTPAcceptHeader)
	req.Header.Set("mcp-protocol-version", t.ProtocolVersion())
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", version.UserAgent())
	if includeSessionID && !t.isModernProtocol() {
		if sessionID := t.SessionID(); sessionID != "" {
			req.Header.Set("mcp-session-id", sessionID)
			sentSessionID = sessionID
		}
	}
	return sentSessionID
}

// isInitializeMessage reports whether message is a JSON-RPC `initialize`
// request, matching TS send()'s `isInitializeRequest` check.
func isInitializeMessage(message *MCPMessage) bool {
	return message != nil && message.Method == "initialize"
}

// Connect establishes a connection to the HTTP server
func (t *HTTPTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	if t.connected {
		t.mu.Unlock()
		return fmt.Errorf("already connected")
	}
	hasOAuth := t.oauth != nil
	t.mu.Unlock()

	// If OAuth is configured, get access token
	if hasOAuth {
		if err := t.refreshOAuthToken(ctx); err != nil {
			return NewTransportError("failed to get OAuth token", err)
		}
	}

	// Test connection with a ping
	// For now, just mark as connected
	t.mu.Lock()
	if t.connected {
		t.mu.Unlock()
		return fmt.Errorf("already connected")
	}
	t.connected = true
	t.mu.Unlock()

	// Open the standing inbound SSE listener for the legacy protocol era,
	// matching TS HttpMCPTransport.start(): the transport-wide lifecycle is
	// created once here, and the initial GET is attempted unless the
	// negotiated protocol is already the modern (2026-07-28) one.
	t.startInboundSSELifecycle()
	if !t.isModernProtocol() {
		t.startInboundSSE(false, "")
	}
	return nil
}

// Close closes the connection. When a streamable HTTP session is
// established and TerminateSessionOnClose is enabled (the default), it sends
// a best-effort DELETE with the session id before disconnecting (hash
// 241a8c5); a failure to terminate is ignored, matching TS's fire-and-forget
// session termination. Matching TS close()'s `!this.isModernProtocol() &&
// this.sessionId && ...`, no DELETE is sent for the modern (2026-07-28)
// protocol era.
func (t *HTTPTransport) Close() error {
	// Cancel the transport-lifetime context (in-flight GET/POST requests, any
	// POST-response SSE reader, and any scheduled inbound-SSE reconnect) and
	// wait for every goroutine it owns to exit before proceeding, matching
	// TS's `this.inboundSseConnection?.close(); this.abortController?.abort();`
	// and guaranteeing Close() never leaks a goroutine. stopInboundSSE cancels
	// sseLifecycleCtx, which every in-flight Send()'s derived request context
	// (see deriveRequestContext) is also watching, so this unblocks in-flight
	// POST requests and POST-response SSE reads too, not just inbound SSE.
	t.stopInboundSSE()
	t.postSSEWG.Wait()

	t.mu.Lock()
	sessionID := t.sessionID
	terminate := t.terminateSessionOnClose
	url := t.url
	httpClient := t.client
	sseClient := t.sseClient
	protocolVersion := t.protocolVersion
	t.connected = false
	t.mu.Unlock()

	if terminate && sessionID != "" && !t.isModernProtocol() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
		if err == nil {
			req.Header.Set("mcp-session-id", sessionID)
			if protocolVersion != "" {
				req.Header.Set("mcp-protocol-version", protocolVersion)
			}
			req.Header.Set("User-Agent", version.UserAgent())
			client := SSEClient(httpClient)
			if sseClient != nil {
				client = sseClient
			}
			if resp, doErr := client.Do(req); doErr == nil && resp != nil {
				if resp.Body != nil {
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close() //nolint:errcheck
				}
			}
		}
	}

	return nil
}

// Send sends a message to the MCP server
func (t *HTTPTransport) Send(ctx context.Context, message *MCPMessage) error {
	return t.send(ctx, message, nil)
}

// SendWithHeaders sends message with additional per-request HTTP headers
// merged in on top of the standard headers (hash 0c60a40), used to carry
// x-mcp-header-derived `Mcp-Param-*` headers on a `tools/call` request.
func (t *HTTPTransport) SendWithHeaders(ctx context.Context, message *MCPMessage, headers map[string]string) error {
	return t.send(ctx, message, headers)
}

func (t *HTTPTransport) send(ctx context.Context, message *MCPMessage, extraHeaders map[string]string) error {
	t.mu.Lock()
	connected := t.connected
	t.mu.Unlock()

	if !connected {
		return NewTransportError("not connected", nil)
	}

	// fail reports err via OnError before returning it, matching every
	// `this.onerror?.(error); throw error;` site in TS send()'s attempt()
	// (non-ok responses, fetch failures, OAuth refresh failures, session
	// expiry). Matching TS's `if (options?.signal?.aborted) throw error;`
	// guard, a caller-canceled context suppresses the report. This
	// deliberately checks the caller's own ctx, not reqCtx below: TS's guard
	// only looks at `options?.signal` (the caller's own per-call signal), so
	// an abort caused solely by Close() (the transport-wide signal) is still
	// reported here, exactly as TS's send() does.
	fail := func(err error) error {
		if ctx.Err() == nil {
			t.reportError(err)
		}
		return err
	}

	// Marshal message to JSON
	data, err := json.Marshal(message)
	if err != nil {
		return fail(NewTransportError("failed to marshal message", err))
	}

	if t.config.EnableLogging {
		fmt.Printf("MCP HTTP Send: %s\n", string(data))
	}

	// reqCtx combines the caller's ctx with the transport-lifetime context
	// that Close cancels, matching TS send()'s
	// `AbortSignal.any([transportSignal, options.signal])`: it drives the
	// POST request itself and, for a text/event-stream response, the reader
	// draining it. release must run exactly once the derived context is no
	// longer needed; handedOff tracks whether that responsibility moved to a
	// background goroutine (the SSE-reader case) instead of happening here.
	reqCtx, release := t.deriveRequestContext(ctx)
	handedOff := false
	defer func() {
		if !handedOff {
			release()
		}
	}()

	// Create HTTP request
	req, err := http.NewRequestWithContext(reqCtx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return fail(NewTransportError("failed to create request", err))
	}

	// Set headers, including mcp-session-id when a session is established
	// (hash 241a8c5). The session id is never attached to an `initialize`
	// request, matching TS commonHeaders' `includeSessionId:
	// !isInitializeRequest`.
	includeSessionID := !isInitializeMessage(message)
	sentSessionID := t.applyStandardHeaders(req, includeSessionID)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	// Set OAuth token if available
	if token, expired, ok := t.oauthTokenSnapshot(); ok {
		if expired {
			if err := t.refreshOAuthToken(ctx); err != nil {
				return fail(NewTransportError("failed to refresh OAuth token", err))
			}
			token, _, ok = t.oauthTokenSnapshot()
		}
		if ok && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		client := SSEClient(t.client)
		if t.sseClient != nil {
			client = t.sseClient
		}
		resp, err = client.Do(req)
		if err != nil {
			return fail(NewTransportError("failed to send request", err))
		}
		if resp == nil {
			return fail(NewTransportError("failed to send request", fmt.Errorf("nil HTTP response")))
		}
		if resp.Body == nil {
			resp.Body = io.NopCloser(bytes.NewReader(nil))
		}
		if resp.StatusCode != http.StatusUnauthorized || !t.oauthConfigured() || attempt == 1 {
			break
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close() //nolint:errcheck
		if err := t.refreshOAuthToken(ctx); err != nil {
			return fail(NewTransportError("failed to refresh OAuth token", err))
		}
		req, err = http.NewRequestWithContext(reqCtx, "POST", t.url, bytes.NewReader(data))
		if err != nil {
			return fail(NewTransportError("failed to create request", err))
		}
		sentSessionID = t.applyStandardHeaders(req, includeSessionID)
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		if token, _, ok := t.oauthTokenSnapshot(); ok && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	// Matching TS applySessionIdFromResponse/the 404 handling in send(),
	// both are no-ops entirely for the modern (2026-07-28) protocol era:
	// `if (this.isModernProtocol()) return;` and
	// `if (!this.isModernProtocol() && sessionIdForRequest) { ... }`.
	if !t.isModernProtocol() {
		if sessionID := resp.Header.Get("mcp-session-id"); sessionID != "" {
			t.setSessionID(sessionID)
		}
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close() //nolint:errcheck
		body, _ := io.ReadAll(resp.Body)
		errMessage := fmt.Sprintf("MCP HTTP Transport Error: POSTing to endpoint (HTTP %d): %s", resp.StatusCode, string(body))
		// Matches TS send()'s two distinct 404 suffixes: a request that
		// carried a (now stale) session id gets the session-expired message,
		// while a 404 with no session id in play means the server likely
		// doesn't support this transport at all. Neither suffix applies (and
		// no session is expired) for the modern protocol era.
		if resp.StatusCode == http.StatusNotFound && !t.isModernProtocol() {
			if sentSessionID != "" {
				t.expireSessionID(sentSessionID)
				errMessage += ". The MCP session expired. Create a new client without `initialSessionId` to start a fresh session"
			} else {
				errMessage += ". This server does not support HTTP transport. Try using `sse` transport instead"
			}
		}
		return fail(NewMCPClientError(0, errMessage, nil, WithMCPHTTPResponse(resp.StatusCode, t.url, string(body))))
	}
	if resp.StatusCode == http.StatusAccepted {
		// If the server accepted the message (e.g. the initialized
		// notification), optionally (re)start inbound SSE when it was not
		// available earlier (e.g. a 405 before init), matching TS's send()
		// 202 handling. Fire-and-forget: do not block Send() on it.
		if !t.isModernProtocol() {
			t.sseMu.Lock()
			hasConn := t.sseConnCancel != nil
			t.sseMu.Unlock()
			if !hasConn {
				t.startInboundSSE(false, "")
			}
		}
		return nil
	}
	if IsNotification(message) {
		return nil
	}

	// Content-type dispatch matches TS send()'s exact order: application/json
	// is parsed, text/event-stream is handed to a reader, and anything else
	// (including a missing content-type) is an "Unexpected content type"
	// error, reported via OnError and returned.
	contentType := resp.Header.Get("content-type")
	switch {
	case strings.Contains(contentType, "application/json"):
		defer resp.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fail(NewTransportError("failed to read response", err))
		}

		if t.config.EnableLogging {
			fmt.Printf("MCP HTTP Receive: %s\n", string(body))
		}

		messages, err := parseMCPHTTPJSONMessages(body)
		if err != nil {
			return fail(NewTransportError("failed to unmarshal response", err))
		}

		t.queueReceivedMessages(messages)
		return nil

	case strings.Contains(contentType, "text/event-stream"):
		// Guard the handoff with the same sseClosing flag (and sseMu lock)
		// startInboundSSE uses for sseWG: postSSEWG.Add must never race
		// Close()'s postSSEWG.Wait, so a transport that is already closing
		// drains and discards the stream here instead of starting a
		// goroutine Close() might already have stopped waiting for.
		t.sseMu.Lock()
		closing := t.sseClosing
		if !closing {
			t.postSSEWG.Add(1)
		}
		t.sseMu.Unlock()
		if closing {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close() //nolint:errcheck
			return nil
		}
		handedOff = true
		go func() {
			defer t.postSSEWG.Done()
			t.readMCPHTTPSSEMessages(reqCtx, release, resp.Body)
		}()
		return nil

	default:
		resp.Body.Close() //nolint:errcheck
		return fail(NewMCPClientError(0, fmt.Sprintf("MCP HTTP Transport Error: Unexpected content type: %s", contentType), nil, WithMCPHTTPStatus(resp.StatusCode), WithMCPHTTPURL(t.url)))
	}
}

func (t *HTTPTransport) queueReceivedMessages(messages []*MCPMessage) {
	if len(messages) == 0 {
		return
	}
	t.receiveMu.Lock()
	t.receiveQueue = append(t.receiveQueue, messages...)
	t.receiveMu.Unlock()
}

func parseMCPHTTPJSONMessages(body []byte) ([]*MCPMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var batch []MCPMessage
		if err := unmarshalSafeJSON(trimmed, &batch); err != nil {
			return nil, err
		}
		messages := make([]*MCPMessage, 0, len(batch))
		for i := range batch {
			msg := batch[i]
			messages = append(messages, &msg)
		}
		return messages, nil
	}
	var responseMsg MCPMessage
	if err := unmarshalSafeJSON(trimmed, &responseMsg); err != nil {
		return nil, err
	}
	return []*MCPMessage{&responseMsg}, nil
}

func parseMCPHTTPSSEMessages(body io.Reader) ([]*MCPMessage, error) {
	parser := streaming.NewSSEParser(body)
	var messages []*MCPMessage
	for {
		event, err := parser.Next()
		if err == io.EOF {
			return messages, nil
		}
		if err != nil {
			return nil, err
		}
		if event.Event != "" && event.Event != "message" {
			continue
		}
		var msg MCPMessage
		if err := unmarshalSafeJSON([]byte(event.Data), &msg); err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}
}

// readMCPHTTPSSEMessages parses a text/event-stream response returned from a
// POST send() (as opposed to the standing inbound SSE GET listener), matching
// TS send()'s `processEvents()`. A clean end-of-stream (EOF) returns
// silently, matching TS's `if (done) return;`; a read error is reported via
// OnError and stops the loop, matching TS's outer catch
// (`this.onerror?.(error)`), except that report is skipped when ctx (the
// request context deriveRequestContext produced, combining the caller's ctx
// with the transport-lifetime one Close cancels) is already done, matching
// TS's `options?.signal?.aborted || error.name === 'AbortError'` check: TS
// checks the caller signal OR a generic AbortError (which fires for either
// signal in the merged `AbortSignal.any`), which is exactly what checking
// this combined ctx captures in one condition. A malformed message is
// reported via OnError but does not stop the loop, matching TS's inner catch
// around `parseJSONRPCMessage`. release is deriveRequestContext's cleanup
// func for ctx, invoked once this goroutine is done with it.
func (t *HTTPTransport) readMCPHTTPSSEMessages(ctx context.Context, release context.CancelFunc, body io.ReadCloser) {
	defer release()
	defer body.Close() //nolint:errcheck
	parser := streaming.NewSSEParser(body)
	for {
		event, err := parser.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			if ctx.Err() == nil {
				t.reportError(NewTransportError("failed to read event-stream response", err))
			}
			return
		}
		if event.Event != "" && event.Event != "message" {
			continue
		}
		var msg MCPMessage
		if err := unmarshalSafeJSON([]byte(event.Data), &msg); err != nil {
			t.reportError(NewMCPClientError(0, "MCP HTTP Transport Error: Failed to parse message", err))
			continue
		}
		t.queueReceivedMessages([]*MCPMessage{&msg})
	}
}

// ---- Inbound SSE (legacy protocol era) ----
//
// Ports TS HttpMCPTransport's openInboundSse/startInboundSse/
// scheduleInboundSseReconnection (packages/mcp/src/tool/mcp-http-transport.ts):
// a standing background GET (Accept: text/event-stream) that receives
// server-initiated messages outside of a POST response. Only the legacy
// protocol era opens it; isModernProtocol() (2026-07-28) short-circuits it
// entirely, matching TS.

// isModernProtocol reports whether the negotiated protocol version is the
// modern (2026-07-28) one, matching TS's `isModernProtocol()`.
func (t *HTTPTransport) isModernProtocol() bool {
	return t.ProtocolVersion() == LatestProtocolVersion
}

// reportError forwards a non-fatal inbound-SSE diagnostic to OnError, if
// configured, matching TS's `this.onerror?.(error)`.
func (t *HTTPTransport) reportError(err error) {
	if t.onError != nil {
		t.onError(err)
	}
}

// deriveRequestContext derives the context.Context for a single outbound
// Send() POST request (and, when its response is itself a text/event-stream,
// the reader draining it) by combining the caller-supplied ctx with the
// transport-lifetime context Close cancels, matching TS send()'s
// `AbortSignal.any([transportSignal, options.signal])`. Before Connect has
// run (sseLifecycleCtx is nil, e.g. tests that set connected directly) there
// is no transport-lifetime context yet, so ctx is returned unchanged.
//
// The returned release func must be called exactly once, when the derived
// context is no longer needed, to stop the background watcher
// context.AfterFunc registers against the transport-lifetime context;
// otherwise that watcher would sit blocked until Close() finally cancels it,
// leaking one per Send() call for the life of the transport.
func (t *HTTPTransport) deriveRequestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	t.sseMu.Lock()
	lifecycleCtx := t.sseLifecycleCtx
	t.sseMu.Unlock()
	if lifecycleCtx == nil {
		return ctx, func() {}
	}
	reqCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifecycleCtx, cancel)
	return reqCtx, func() {
		stop()
		cancel()
	}
}

// startInboundSSELifecycle creates the long-lived context shared by every
// inbound SSE GET attempt (and its reconnects) for the life of the
// transport, matching TS's `this.abortController` (created once in
// start()). It is a no-op if already started.
func (t *HTTPTransport) startInboundSSELifecycle() {
	t.sseMu.Lock()
	defer t.sseMu.Unlock()
	if t.sseLifecycleCtx != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.sseLifecycleCtx = ctx
	t.sseLifecycleCancel = cancel
}

// stopInboundSSE tears down the inbound SSE lifecycle: it marks the
// transport as closing (so no new GET or reconnect can start), cancels any
// in-flight GET, stops a pending reconnect timer, and waits for every
// background goroutine to exit before returning. This guarantees Close()
// never leaks a goroutine, matching TS's
// `this.inboundSseConnection?.close(); this.abortController?.abort();`.
func (t *HTTPTransport) stopInboundSSE() {
	t.sseMu.Lock()
	t.sseClosing = true
	if t.sseLifecycleCancel != nil {
		t.sseLifecycleCancel()
	}
	timer := t.sseReconnectTimer
	t.sseReconnectTimer = nil
	t.sseMu.Unlock()

	if timer != nil && timer.Stop() {
		// The timer's callback (which would have called sseWG.Done() itself)
		// will now never run, so account for its earlier sseWG.Add(1) here.
		t.sseWG.Done()
	}

	t.sseWG.Wait()
}

// closeInboundSSEConnection cancels only the current inbound SSE connection,
// not the whole transport lifecycle, matching TS setProtocolVersion's
// `this.inboundSseConnection?.close(); this.inboundSseConnection = undefined;`
// used when the negotiated protocol switches to modern mid-flight.
func (t *HTTPTransport) closeInboundSSEConnection() {
	t.sseMu.Lock()
	cancel := t.sseConnCancel
	t.sseConnCancel = nil
	t.sseMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// startInboundSSE fires off a best-effort, resumable background GET for
// server-initiated messages, matching TS's `startInboundSse` (a
// fire-and-forget wrapper around `openInboundSse`). It is a no-op for the
// modern protocol, once the transport is closing, or before Connect has run.
func (t *HTTPTransport) startInboundSSE(triedAuth bool, resumeToken string) {
	if t.isModernProtocol() {
		return
	}
	t.sseMu.Lock()
	if t.sseClosing || t.sseLifecycleCtx == nil {
		t.sseMu.Unlock()
		return
	}
	lifecycleCtx := t.sseLifecycleCtx
	t.sseWG.Add(1)
	t.sseMu.Unlock()

	go func() {
		defer t.sseWG.Done()
		t.openInboundSSE(lifecycleCtx, triedAuth, resumeToken)
	}()
}

// nextInboundReconnectDelay computes the exponential backoff delay for the
// given (zero-based) reconnect attempt, matching TS's
// `getNextReconnectionDelay`.
func nextInboundReconnectDelay(attempt int) time.Duration {
	delay := float64(inboundSSEInitialReconnectionDelay) * math.Pow(inboundSSEReconnectionDelayGrowFactor, float64(attempt))
	if delay > float64(inboundSSEMaxReconnectionDelay) {
		return inboundSSEMaxReconnectionDelay
	}
	return time.Duration(delay)
}

// scheduleInboundSSEReconnection schedules a reconnect attempt after an
// exponential backoff delay, resuming from the last received event id via
// Last-Event-Id, matching TS's `scheduleInboundSseReconnection`. It gives up
// (reporting an error, matching TS) after inboundSSEMaxRetries attempts.
func (t *HTTPTransport) scheduleInboundSSEReconnection() {
	t.sseMu.Lock()
	if t.sseClosing {
		t.sseMu.Unlock()
		return
	}
	attempts := t.sseReconnectAttempts
	if inboundSSEMaxRetries > 0 && attempts >= inboundSSEMaxRetries {
		t.sseMu.Unlock()
		t.reportError(NewMCPClientError(0, fmt.Sprintf("MCP HTTP Transport Error: Maximum reconnection attempts (%d) exceeded.", inboundSSEMaxRetries), nil))
		return
	}
	delay := nextInboundReconnectDelay(attempts)
	t.sseReconnectAttempts = attempts + 1
	resumeToken := t.lastInboundEventID
	lifecycleCtx := t.sseLifecycleCtx
	t.sseWG.Add(1)
	t.sseReconnectTimer = time.AfterFunc(delay, func() {
		defer t.sseWG.Done()
		t.sseMu.Lock()
		closing := t.sseClosing
		t.sseMu.Unlock()
		if closing || lifecycleCtx == nil || lifecycleCtx.Err() != nil {
			return
		}
		t.openInboundSSE(lifecycleCtx, false, resumeToken)
	})
	t.sseMu.Unlock()
}

// maybeScheduleInboundSSEReconnect schedules a reconnect unless the
// lifecycle context is already canceled (Close), matching TS's
// `if (!this.abortController?.signal.aborted) { this.scheduleInboundSseReconnection(); }`.
func (t *HTTPTransport) maybeScheduleInboundSSEReconnect(lifecycleCtx context.Context) {
	if lifecycleCtx.Err() != nil {
		return
	}
	t.scheduleInboundSSEReconnection()
}

// openInboundSSE performs a single inbound SSE GET attempt: it builds the
// request (Accept: text/event-stream, mcp-session-id when legacy, and
// Last-Event-Id when resuming), retries once on a 401 by refreshing the
// OAuth token (reusing the same refreshOAuthToken single-flight path as
// send()), returns silently on a 405 (server does not support GET, matching
// TS), expires the session id on a 404 (matching TS), and on success hands
// the response body to readInboundSSEStream. lifecycleCtx is the
// transport-wide inbound SSE context (canceled by Close); a per-connection
// child context is derived from it so a protocol-version switch to modern
// can close just this connection without aborting the whole transport.
func (t *HTTPTransport) openInboundSSE(lifecycleCtx context.Context, triedAuth bool, resumeToken string) {
	if t.isModernProtocol() || lifecycleCtx.Err() != nil {
		return
	}

	sessionIDForRequest := t.SessionID()
	connCtx, connCancel := context.WithCancel(lifecycleCtx)

	req, err := http.NewRequestWithContext(connCtx, http.MethodGet, t.url, nil)
	if err != nil {
		connCancel()
		t.reportError(NewTransportError("failed to create inbound SSE request", err))
		// Matches TS: a thrown error while building the request/headers falls
		// into openInboundSse's outer catch, which schedules a reconnect.
		t.maybeScheduleInboundSSEReconnect(lifecycleCtx)
		return
	}
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("mcp-protocol-version", t.ProtocolVersion())
	req.Header.Set("User-Agent", version.UserAgent())
	if sessionIDForRequest != "" {
		req.Header.Set("mcp-session-id", sessionIDForRequest)
	}
	if resumeToken != "" {
		req.Header.Set("Last-Event-Id", resumeToken)
	}
	if token, expired, ok := t.oauthTokenSnapshot(); ok {
		if expired {
			if err := t.refreshOAuthToken(connCtx); err != nil {
				connCancel()
				t.reportError(NewTransportError("failed to refresh OAuth token", err))
				return
			}
			token, _, ok = t.oauthTokenSnapshot()
		}
		if ok && token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	client := SSEClient(t.client)
	if t.sseClient != nil {
		client = t.sseClient
	}

	resp, err := client.Do(req)
	if err != nil {
		aborted := connCtx.Err() != nil
		connCancel()
		if aborted {
			return
		}
		t.reportError(NewTransportError("failed to establish inbound SSE connection", err))
		t.maybeScheduleInboundSSEReconnect(lifecycleCtx)
		return
	}
	if resp == nil {
		connCancel()
		t.reportError(NewTransportError("failed to establish inbound SSE connection", fmt.Errorf("nil HTTP response")))
		t.maybeScheduleInboundSSEReconnect(lifecycleCtx)
		return
	}
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}

	if sessionID := resp.Header.Get("mcp-session-id"); sessionID != "" {
		t.setSessionID(sessionID)
	}

	// 401: run the OAuth refresh once (reusing the single-flight path used by
	// send()), then retry, matching TS's authorizeOnce()-guarded retry.
	if resp.StatusCode == http.StatusUnauthorized && t.oauthConfigured() && !triedAuth {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close() //nolint:errcheck
		if err := t.refreshOAuthToken(connCtx); err != nil {
			connCancel()
			t.reportError(NewTransportError("failed to refresh OAuth token", err))
			return
		}
		connCancel()
		t.openInboundSSE(lifecycleCtx, true, resumeToken)
		return
	}

	// 405: the server does not support GET on this endpoint. Matching TS,
	// this is silent (no error reported, no reconnection scheduled).
	if resp.StatusCode == http.StatusMethodNotAllowed {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close() //nolint:errcheck
		connCancel()
		return
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		if resp.StatusCode == http.StatusNotFound && sessionIDForRequest != "" {
			t.expireSessionID(sessionIDForRequest)
		}
		statusText := http.StatusText(resp.StatusCode)
		message := fmt.Sprintf("MCP HTTP Transport Error: GET SSE failed: %d %s", resp.StatusCode, statusText)
		t.reportError(NewMCPClientError(0, message, nil, WithMCPHTTPResponse(resp.StatusCode, t.url, string(body))))
		connCancel()
		return
	}

	t.sseMu.Lock()
	t.sseConnCancel = connCancel
	t.sseReconnectAttempts = 0
	t.sseMu.Unlock()

	t.readInboundSSEStream(connCtx, connCancel, resp.Body)
}

// readInboundSSEStream parses the inbound SSE stream, tracking the last
// event id for resumption (Last-Event-Id) and queueing JSON-RPC messages for
// Receive, matching TS's processEvents(). A clean end-of-stream (EOF)
// returns without scheduling a reconnect (matching TS: only a read error
// does); a read error schedules a reconnect unless the connection was
// intentionally canceled (Close, or a switch to the modern protocol).
func (t *HTTPTransport) readInboundSSEStream(connCtx context.Context, connCancel context.CancelFunc, body io.ReadCloser) {
	defer connCancel()
	defer body.Close() //nolint:errcheck
	parser := streaming.NewSSEParser(body)
	for {
		event, err := parser.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			if connCtx.Err() != nil {
				return
			}
			t.reportError(NewTransportError("failed to read inbound SSE event", err))
			t.scheduleInboundSSEReconnection()
			return
		}

		if event.ID != "" {
			t.sseMu.Lock()
			t.lastInboundEventID = event.ID
			t.sseMu.Unlock()
		}

		if event.Event != "" && event.Event != "message" {
			continue
		}
		var msg MCPMessage
		if err := unmarshalSafeJSON([]byte(event.Data), &msg); err != nil {
			t.reportError(NewMCPClientError(0, "MCP HTTP Transport Error: Failed to parse message", err))
			continue
		}
		t.queueReceivedMessages([]*MCPMessage{&msg})
	}
}

// Receive receives a message from the MCP server
// In HTTP transport, messages are queued from Send operations
func (t *HTTPTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	// Poll the receive queue
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

		// Sleep briefly before checking again
		time.Sleep(10 * time.Millisecond)
	}
}

// IsConnected returns true if the transport is connected
func (t *HTTPTransport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.connected
}

// SetProtocolVersion stores the negotiated MCP protocol version for outbound
// transport request headers. It also mirrors TS HttpMCPTransport's
// setProtocolVersion: once the inbound SSE lifecycle has started (Connect
// has run), switching to the modern protocol closes any standing inbound SSE
// connection, and switching (back) to a legacy protocol (re)opens one if
// none is active.
func (t *HTTPTransport) SetProtocolVersion(version string) {
	t.mu.Lock()
	t.protocolVersion = version
	t.mu.Unlock()

	t.sseMu.Lock()
	lifecycleStarted := t.sseLifecycleCtx != nil
	t.sseMu.Unlock()
	if !lifecycleStarted {
		return
	}

	if t.isModernProtocol() {
		t.closeInboundSSEConnection()
		return
	}

	t.sseMu.Lock()
	hasConn := t.sseConnCancel != nil
	t.sseMu.Unlock()
	if !hasConn {
		t.startInboundSSE(false, "")
	}
}

// SupportsProtocolVersionDiscovery reports that HTTPTransport (streamable
// HTTP) supports probing `server/discover` (hash e6a9927), matching TS
// StreamableHTTPClientTransport.supportsProtocolVersionDiscovery.
func (t *HTTPTransport) SupportsProtocolVersionDiscovery() bool { return true }

// SupportsMCPToolParameterHeaders reports that HTTPTransport supports
// binding tool call arguments to HTTP headers via `x-mcp-header` (hash
// 0c60a40), matching TS StreamableHTTPClientTransport.supportsMcpToolParameterHeaders.
func (t *HTTPTransport) SupportsMCPToolParameterHeaders() bool { return true }

// ProtocolVersion returns the negotiated protocol version, or the latest
// supported version before initialization completes.
func (t *HTTPTransport) ProtocolVersion() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.protocolVersion != "" {
		return t.protocolVersion
	}
	return ProtocolVersion
}

// refreshOAuthToken refreshes the OAuth access token
func (t *HTTPTransport) refreshOAuthToken(ctx context.Context) error {
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

func (t *HTTPTransport) doRefreshOAuthToken(ctx context.Context) error {
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

// SetAccessToken sets the OAuth access token manually
func (t *HTTPTransport) SetAccessToken(token string, expiresIn time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.oauth != nil {
		t.oauth.AccessToken = token
		t.oauth.ExpiresAt = time.Now().Add(expiresIn)
	}
}

func (t *HTTPTransport) oauthConfigured() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.oauth != nil
}

func (t *HTTPTransport) oauthTokenSnapshot() (token string, expired bool, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.oauth == nil || t.oauth.AccessToken == "" {
		return "", false, false
	}
	return t.oauth.AccessToken, time.Now().After(t.oauth.ExpiresAt), true
}
