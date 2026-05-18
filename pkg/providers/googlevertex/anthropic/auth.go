package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type authTransport struct {
	base        http.RoundTripper
	tokenFunc   func(ctx context.Context) (string, error)
	headers     map[string]string
	headersFunc HeadersResolver
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	token, err := t.tokenFunc(req.Context())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", "Bearer "+token)
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	if t.headersFunc != nil {
		headers, err := t.headersFunc(req.Context())
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			clone.Header.Set(k, v)
		}
	}
	return t.base.RoundTrip(clone)
}

func (p *GoogleVertexAnthropicProvider) lazyAuthToken() func(ctx context.Context) (string, error) {
	var mu sync.Mutex
	var source oauth2.TokenSource
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		if source == nil {
			loaded, err := p.tokenSource(ctx)
			if err != nil {
				mu.Unlock()
				return "", err
			}
			source = loaded
		}
		current := source
		mu.Unlock()

		token, err := current.Token()
		if err != nil {
			return "", fmt.Errorf("failed to fetch Google access token: %w", err)
		}
		if token == nil || token.AccessToken == "" {
			return "", fmt.Errorf("google token source returned an empty access token")
		}
		return token.AccessToken, nil
	}
}

func (p *GoogleVertexAnthropicProvider) tokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if len(p.options.CredentialsJSON) > 0 {
		creds, err := google.CredentialsFromJSON(ctx, p.options.CredentialsJSON, defaultCloudPlatformAuth)
		if err != nil {
			return nil, fmt.Errorf("failed to load Google credentials JSON: %w", err)
		}
		return creds.TokenSource, nil
	}
	if p.options.CredentialsFile != "" {
		data, err := os.ReadFile(p.options.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read Google credentials file: %w", err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, defaultCloudPlatformAuth)
		if err != nil {
			return nil, fmt.Errorf("failed to load Google credentials file: %w", err)
		}
		return creds.TokenSource, nil
	}
	if p.options.TokenSource != nil {
		return p.options.TokenSource, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, defaultCloudPlatformAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to load Google application default credentials: %w", err)
	}
	return creds.TokenSource, nil
}
