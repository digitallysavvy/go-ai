package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
	"github.com/digitallysavvy/go-ai/pkg/version"
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

	receiveMu    sync.Mutex
	receiveQueue []*MCPMessage

	mu              sync.Mutex
	connected       bool
	protocolVersion string
	streamBody      io.Closer
	cancel          context.CancelFunc
	streamErr       error

	config TransportConfig
	oauth  *OAuthConfig
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
}

// NewSSETransport creates a new legacy SSE transport.
func NewSSETransport(config SSETransportConfig) *SSETransport {
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

	if config.Config.Redirect != MCPRedirectFollow {
		httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("mcp: server returned redirect to %s; set Redirect: MCPRedirectFollow to allow redirects", req.URL)
		}
	}

	baseURL, _ := url.Parse(config.URL)
	return &SSETransport{
		url:          config.URL,
		baseURL:      baseURL,
		client:       httpClient,
		sseClient:    config.SSEClient,
		receiveQueue: make([]*MCPMessage, 0),
		config:       config.Config,
		oauth:        config.OAuth,
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

	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, t.url, nil)
	if err != nil {
		cancel()
		return NewTransportError("failed to create SSE request", err)
	}
	t.applyHeaders(ctx, req, map[string]string{"Accept": "text/event-stream"})

	client := SSEClient(t.client)
	if t.sseClient != nil {
		client = t.sseClient
	}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return NewTransportError("failed to establish SSE connection", err)
	}
	if resp == nil {
		cancel()
		return NewTransportError("failed to establish SSE connection", fmt.Errorf("nil HTTP response"))
	}
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close() //nolint:errcheck
		cancel()
		statusText := http.StatusText(resp.StatusCode)
		if statusText == "" {
			statusText = resp.Status
		}
		message := fmt.Sprintf("MCP SSE Transport Error: %d %s", resp.StatusCode, statusText)
		if resp.StatusCode == http.StatusMethodNotAllowed {
			message += ". This server does not support SSE transport. Try using `http` transport instead"
		}
		return NewMCPClientError(0, strings.TrimSpace(message), nil, WithMCPHTTPResponse(resp.StatusCode, t.url, ""))
	}

	ready := make(chan error, 1)
	t.mu.Lock()
	t.streamBody = resp.Body
	t.cancel = cancel
	t.streamErr = nil
	t.mu.Unlock()
	go t.readSSEStream(resp.Body, ready)

	select {
	case err := <-ready:
		if err != nil {
			t.closeWithoutCallback()
			return err
		}
		return nil
	case <-ctx.Done():
		t.closeWithoutCallback()
		return ctx.Err()
	}
}

// Close closes the SSE stream and clears the locked endpoint.
func (t *SSETransport) Close() error {
	t.closeWithoutCallback()
	return nil
}

func (t *SSETransport) closeWithoutCallback() {
	t.mu.Lock()
	body := t.streamBody
	cancel := t.cancel
	t.connected = false
	t.endpoint = nil
	t.streamBody = nil
	t.cancel = nil
	t.streamErr = io.EOF
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
	t.mu.Lock()
	connected := t.connected
	endpoint := t.endpoint
	t.mu.Unlock()

	if !connected || endpoint == nil {
		return NewMCPClientError(0, "MCP SSE Transport Error: Not connected", nil)
	}

	data, err := json.Marshal(message)
	if err != nil {
		return NewTransportError("failed to marshal message", err)
	}
	if t.config.EnableLogging {
		fmt.Printf("MCP SSE Send: %s\n", string(data))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return NewTransportError("failed to create request", err)
	}
	t.applyHeaders(ctx, req, map[string]string{"Content-Type": "application/json"})

	client := SSEClient(t.client)
	if t.sseClient != nil {
		client = t.sseClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return NewTransportError("failed to send request", err)
	}
	if resp == nil {
		return NewTransportError("failed to send request", fmt.Errorf("nil HTTP response"))
	}
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		message := fmt.Sprintf("MCP SSE Transport Error: POSTing to endpoint (HTTP %d): %s", resp.StatusCode, string(body))
		return NewMCPClientError(0, message, nil, WithMCPHTTPResponse(resp.StatusCode, endpoint.String(), string(body)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
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

func (t *SSETransport) applyHeaders(ctx context.Context, req *http.Request, base map[string]string) {
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
	req.Header.Set("User-Agent", version.UserAgent())
}

func (t *SSETransport) readSSEStream(body io.ReadCloser, ready chan<- error) {
	defer body.Close() //nolint:errcheck
	parser := streaming.NewSSEParser(body)
	readySent := false
	for {
		event, err := parser.Next()
		if err == io.EOF {
			t.mu.Lock()
			wasConnected := t.connected
			t.connected = false
			t.endpoint = nil
			if readySent && wasConnected {
				t.streamErr = NewMCPClientError(0, "MCP SSE Transport Error: Connection closed unexpectedly", nil)
			} else {
				t.streamErr = io.EOF
			}
			t.mu.Unlock()
			if !readySent {
				ready <- NewMCPClientError(0, "MCP SSE Transport Error: Connection closed unexpectedly", nil)
			}
			return
		}
		if err != nil {
			if !readySent {
				ready <- NewTransportError("failed to read SSE event", err)
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
			if endpoint.Scheme != t.baseURL.Scheme || endpoint.Host != t.baseURL.Host {
				t.mu.Lock()
				t.connected = false
				t.endpoint = nil
				t.mu.Unlock()
				if !readySent {
					ready <- NewMCPClientError(0, fmt.Sprintf("MCP SSE Transport Error: Endpoint origin does not match connection origin: %s://%s", endpoint.Scheme, endpoint.Host), nil)
				}
				return
			}
			t.mu.Lock()
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
		var msg MCPMessage
		if err := unmarshalSafeJSON([]byte(event.Data), &msg); err != nil {
			continue
		}
		t.queueReceivedMessages([]*MCPMessage{&msg})
	}
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

func (t *SSETransport) refreshOAuthToken(ctx context.Context) error {
	if t.oauth == nil || t.oauth.RefreshTokenFunc == nil {
		return fmt.Errorf("LOAuth refresh not yet implemented - please provide access token manually")
	}
	token, expiresIn, err := t.oauth.RefreshTokenFunc(ctx, t.oauth)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("LOAuth refresh returned empty access token")
	}
	if expiresIn <= 0 {
		expiresIn = time.Hour
	}
	t.mu.Lock()
	t.oauth.AccessToken = token
	t.oauth.ExpiresAt = time.Now().Add(expiresIn)
	t.mu.Unlock()
	return nil
}

func (t *SSETransport) oauthTokenSnapshot() (token string, expired bool, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.oauth == nil || t.oauth.AccessToken == "" {
		return "", false, false
	}
	return t.oauth.AccessToken, time.Now().After(t.oauth.ExpiresAt), true
}
