package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// HTTPClientOptions configures HTTPPolicyClient.
type HTTPClientOptions struct {
	Headers map[string]string
	Timeout time.Duration
	Client  *http.Client
}

type httpPolicyClient struct {
	baseURL string
	headers map[string]string
	client  *http.Client
}

// HTTPPolicyClient constructs a client for the OPA REST API.
func HTTPPolicyClient(baseURL string, opts ...HTTPClientOptions) PolicyClient {
	cfg := HTTPClientOptions{}
	if len(opts) > 0 {
		cfg = opts[0]
	}
	client := cfg.Client
	if client == nil {
		if cfg.Timeout > 0 {
			client = &http.Client{Timeout: cfg.Timeout}
		} else {
			client = http.DefaultClient
		}
	}
	return &httpPolicyClient{baseURL: baseURL, headers: cfg.Headers, client: client}
}

func (c *httpPolicyClient) Evaluate(ctx context.Context, policyPath string, input any) (any, error) {
	payload, err := json.Marshal(map[string]any{"input": input})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.evaluateURL(policyPath), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OPA HTTP status %d: %s", resp.StatusCode, string(body))
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if record, ok := decoded.(map[string]any); ok {
		if result, ok := record["result"]; ok {
			return result, nil
		}
	}
	return decoded, nil
}

func (c *httpPolicyClient) evaluateURL(policyPath string) string {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return c.baseURL
	}
	if strings.Contains(base.Path, "/v1/data") {
		if policyPath != "" {
			base.Path = path.Join(base.Path, policyPath)
		}
		return base.String()
	}
	base.Path = path.Join(base.Path, "/v1/data", policyPath)
	return base.String()
}
