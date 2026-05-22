package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
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

	return &HTTPTransport{
		url:          config.URL,
		client:       httpClient,
		sseClient:    config.SSEClient,
		receiveQueue: make([]*MCPMessage, 0),
		config:       config.Config,
		oauth:        config.OAuth,
	}
}

// Connect establishes a connection to the HTTP server
func (t *HTTPTransport) Connect(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.connected {
		return fmt.Errorf("already connected")
	}

	// If OAuth is configured, get access token
	if t.oauth != nil {
		if err := t.refreshOAuthToken(ctx); err != nil {
			return NewTransportError("failed to get OAuth token", err)
		}
	}

	// Test connection with a ping
	// For now, just mark as connected
	t.connected = true
	return nil
}

// Close closes the connection
func (t *HTTPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.connected = false
	return nil
}

// Send sends a message to the MCP server
func (t *HTTPTransport) Send(ctx context.Context, message *MCPMessage) error {
	t.mu.Lock()
	connected := t.connected
	t.mu.Unlock()

	if !connected {
		return NewTransportError("not connected", nil)
	}

	// Marshal message to JSON
	data, err := json.Marshal(message)
	if err != nil {
		return NewTransportError("failed to marshal message", err)
	}

	if t.config.EnableLogging {
		fmt.Printf("MCP HTTP Send: %s\n", string(data))
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return NewTransportError("failed to create request", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("mcp-protocol-version", t.ProtocolVersion())
	for k, v := range t.config.Headers {
		req.Header.Set(k, v)
	}

	// Set OAuth token if available
	if t.oauth != nil && t.oauth.AccessToken != "" {
		// Check if token is expired
		if time.Now().After(t.oauth.ExpiresAt) {
			if err := t.refreshOAuthToken(ctx); err != nil {
				return NewTransportError("failed to refresh OAuth token", err)
			}
		}
		req.Header.Set("Authorization", "Bearer "+t.oauth.AccessToken)
	}

	var resp *http.Response
	for attempt := 0; attempt < 2; attempt++ {
		client := SSEClient(t.client)
		if t.sseClient != nil {
			client = t.sseClient
		}
		resp, err = client.Do(req)
		if err != nil {
			return NewTransportError("failed to send request", err)
		}
		if resp.StatusCode != http.StatusUnauthorized || t.oauth == nil || attempt == 1 {
			break
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close() //nolint:errcheck
		if err := t.refreshOAuthToken(ctx); err != nil {
			return NewTransportError("failed to refresh OAuth token", err)
		}
		req, err = http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
		if err != nil {
			return NewTransportError("failed to create request", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("mcp-protocol-version", t.ProtocolVersion())
		for k, v := range t.config.Headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("Authorization", "Bearer "+t.oauth.AccessToken)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return NewTransportError(fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(body)), nil)
	}

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return NewTransportError("failed to read response", err)
	}

	if t.config.EnableLogging {
		fmt.Printf("MCP HTTP Receive: %s\n", string(body))
	}

	// Parse response
	var responseMsg MCPMessage
	if err := unmarshalSafeJSON(body, &responseMsg); err != nil {
		return NewTransportError("failed to unmarshal response", err)
	}

	// Queue response for receiving
	t.receiveMu.Lock()
	t.receiveQueue = append(t.receiveQueue, &responseMsg)
	t.receiveMu.Unlock()

	return nil
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
// transport request headers.
func (t *HTTPTransport) SetProtocolVersion(version string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.protocolVersion = version
}

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
		return fmt.Errorf("LOAuth not configured")
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
	if t.oauth.RefreshTokenFunc == nil {
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
	t.oauth.AccessToken = token
	t.oauth.ExpiresAt = time.Now().Add(expiresIn)
	return nil
}

// SetAccessToken sets the OAuth access token manually
func (t *HTTPTransport) SetAccessToken(token string, expiresIn time.Duration) {
	if t.oauth != nil {
		t.oauth.AccessToken = token
		t.oauth.ExpiresAt = time.Now().Add(expiresIn)
	}
}
