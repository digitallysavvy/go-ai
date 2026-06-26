package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
)

type MintRealtimeClientSecretParams struct {
	ModelID             string
	ExpiresAfterSeconds *int
}

type RealtimeClientSecret struct {
	Token     string
	ExpiresAt *int64
}

func (p *Provider) MintRealtimeClientSecret(ctx context.Context, params MintRealtimeClientSecretParams) (*RealtimeClientSecret, error) {
	token, authMethod, err := p.authResolver(ctx)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{"model": params.ModelID}
	if params.ExpiresAfterSeconds != nil {
		body["expiresIn"] = *params.ExpiresAfterSeconds
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := gatewayOriginURL(p.baseURL, "/v1/realtime/client-secrets")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	for k, v := range p.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("ai-gateway-auth-method", authMethod)

	httpClient := internalhttp.DefaultHTTPClient
	if p.config.HTTPClient != nil {
		httpClient = p.config.HTTPClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, p.gatewayUnknownError(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, p.gatewayUnknownError(err)
	}
	response := &internalhttp.Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       responseBody,
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, p.gatewayAPIErrorWithAuthMethod(response, authMethod)
	}
	var parsed struct {
		Token     string `json:"token"`
		ExpiresAt *int64 `json:"expiresAt"`
	}
	if err := json.Unmarshal(response.Body, &parsed); err != nil || parsed.Token == "" {
		if err == nil {
			err = fmt.Errorf("missing token")
		}
		return nil, p.gatewayUnknownError(fmt.Errorf("invalid realtime client secret response: %w", err))
	}
	return &RealtimeClientSecret{Token: parsed.Token, ExpiresAt: parsed.ExpiresAt}, nil
}

func gatewayOriginURL(baseURL string, path string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return path
	}
	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
