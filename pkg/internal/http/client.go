package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultHTTPClient is a shared HTTP client with sensible defaults
var DefaultHTTPClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  false,
	},
}

// Client wraps an HTTP client with additional utilities
type Client struct {
	client  *http.Client
	baseURL string
	headers map[string]string
}

// Config contains configuration for an HTTP client
type Config struct {
	// BaseURL is the base URL for all requests
	BaseURL string

	// Headers are default headers to send with all requests
	Headers map[string]string

	// Timeout for requests (default: 60 seconds)
	Timeout time.Duration

	// HTTPClient is the underlying HTTP client to use
	// If nil, DefaultHTTPClient will be used
	HTTPClient *http.Client
}

// NewClient creates a new HTTP client with the given config
func NewClient(cfg Config) *Client {
	client := cfg.HTTPClient
	if client == nil {
		// Create a new client with custom timeout if specified
		if cfg.Timeout > 0 {
			client = &http.Client{
				Timeout: cfg.Timeout,
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			}
		} else {
			client = DefaultHTTPClient
		}
	}

	return &Client{
		client:  client,
		baseURL: cfg.BaseURL,
		headers: cfg.Headers,
	}
}

// Request represents an HTTP request
type Request struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    interface{}
	Query   map[string]string
}

// Response represents an HTTP response
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

// Do performs an HTTP request
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	// Build full URL
	url := c.baseURL + req.Path
	if len(req.Query) > 0 {
		url += "?"
		first := true
		for k, v := range req.Query {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add default headers
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Set content type for JSON body
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Perform request
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Headers:    httpResp.Header,
		Body:       respBody,
	}, nil
}

// DoJSON performs an HTTP request and decodes the JSON response
func (c *Client) DoJSON(ctx context.Context, req Request, result interface{}) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}

	// Check for error status codes
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(resp.Body))
	}

	// Decode JSON response
	if err := json.Unmarshal(resp.Body, result); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}

	return nil
}

// DoStream performs an HTTP request that returns a streaming response
func (c *Client) DoStream(ctx context.Context, req Request) (*http.Response, error) {
	// Build full URL
	url := c.baseURL + req.Path
	if len(req.Query) > 0 {
		url += "?"
		first := true
		for k, v := range req.Query {
			if !first {
				url += "&"
			}
			url += fmt.Sprintf("%s=%s", k, v)
			first = false
		}
	}

	// Serialize body if present
	var bodyReader io.Reader
	if req.Body != nil {
		bodyBytes, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Add default headers
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	// Add request-specific headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Set content type for JSON body
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Perform request
	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	// Check for error status codes
	if httpResp.StatusCode >= 400 {
		defer httpResp.Body.Close()
		errBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(errBody))
	}

	// Return the response for streaming (caller must close Body)
	return httpResp, nil
}

// Post performs a POST request
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	})
}

// PostJSON performs a POST request and decodes the JSON response
func (c *Client) PostJSON(ctx context.Context, path string, body, result interface{}) error {
	return c.DoJSON(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	}, result)
}

// Get performs a GET request
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
	})
}

// GetJSON performs a GET request and decodes the JSON response
func (c *Client) GetJSON(ctx context.Context, path string, result interface{}) error {
	return c.DoJSON(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
	}, result)
}

// SetHeader sets a default header for all requests
func (c *Client) SetHeader(key, value string) {
	if c.headers == nil {
		c.headers = make(map[string]string)
	}
	c.headers[key] = value
}

// SetBaseURL updates the base URL
func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = baseURL
}
