package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type authTransport struct {
	base http.RoundTripper
	// baseURL is the Vertex Anthropic endpoint: credentials and the custom
	// headers go only to requests of its origin (a redirect hop to another
	// origin gets neither).
	baseURL     string
	tokenFunc   func(ctx context.Context) (string, error)
	headers     map[string]string
	headersFunc HeadersResolver
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.roundTrip(req)
	// A RoundTripper closes the request body, even on errors (a
	// credential or signing failure returns before the base transport).
	if err != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return resp, err
}

func (t *authTransport) roundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	_, generatedLabels := clone.Header[http.CanonicalHeaderKey(generatedLabelsHeader)]
	clone.Header.Del(generatedLabelsHeader)
	if !providerutils.IsSameOrigin(req.URL.String(), t.baseURL) {
		return t.base.RoundTrip(clone)
	}
	// TS config headers: {Authorization: Bearer <token>, ...options.headers},
	// combined below the call headers (combineHeaders(configHeaders,
	// headers, betas)), so a header the request already carries wins.
	config := http.Header{}
	// A hop back to the API origin after a foreign hop (A -> B -> A) gets
	// no token: fetch dropped Authorization on the foreign hop and never
	// adds it back (the other headers it keeps).
	if !providerutils.IsCrossOriginRedirectHop(req) {
		token, err := t.tokenFunc(req.Context())
		if err != nil {
			return nil, internalhttp.RejectFetch(err)
		}
		config.Set("Authorization", "Bearer "+token)
	}
	for k, v := range t.headers {
		config.Set(k, v)
	}
	if resolved, ok := req.Context().Value(resolvedConfigHeadersKey{}).(map[string]string); ok {
		// Resolved once for this call by the model (ResolveConfigHeaders).
		for k, v := range resolved {
			config.Set(k, v)
		}
	} else if t.headersFunc != nil {
		headers, err := t.headersFunc(req.Context())
		if err != nil {
			return nil, internalhttp.RejectFetch(err)
		}
		for k, v := range headers {
			config.Set(k, v)
		}
	}
	for k, vs := range config {
		if len(vs) == 0 {
			continue
		}
		if k == "Anthropic-Beta" {
			clone.Header.Set(k, mergeConfigBetas(clone.Header.Get(k), vs[0]))
			continue
		}
		// A configured labels header beats the one generated from
		// providerOptions.vertex.labels (TS transformRequestHeaders sees the
		// provider headers), but not a call header.
		if _, present := clone.Header[k]; !present || (generatedLabels && strings.EqualFold(k, LabelsHeader)) {
			clone.Header.Set(k, vs[0])
		}
	}
	return t.base.RoundTrip(clone)
}

// mergeConfigBetas adds the betas of a configured anthropic-beta header to
// the request's: TS reads them as user-supplied betas (getBetasFromHeaders:
// lower-cased, trimmed, empty entries dropped) and sends the union.
func mergeConfigBetas(requestValue, configValue string) string {
	var out []string
	seen := map[string]bool{}
	add := func(b string) {
		if b != "" && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	if requestValue != "" {
		for _, b := range strings.Split(requestValue, ",") {
			add(b)
		}
	}
	for _, b := range strings.Split(providerutils.JSToLower(configValue), ",") {
		add(providerutils.JSTrim(b))
	}
	return strings.Join(out, ",")
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
		// CredentialsFromJSON is deprecated upstream in favor of a
		// credential-type-restricted loader (CredentialsFromJSONWithType),
		// since it does not validate the credential configuration's type.
		// Restricting the accepted type(s) here is a behavior change (it
		// would reject credential JSON shapes developers may legitimately
		// be passing today, e.g. authorized_user) that needs a deliberate
		// product decision rather than a blind lint fix, so this is
		// intentionally left as-is for now.
		creds, err := google.CredentialsFromJSON(ctx, p.options.CredentialsJSON, defaultCloudPlatformAuth) //nolint:staticcheck
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
		// See the CredentialsJSON branch above for why this deprecated call
		// is intentionally not narrowed to a specific credential type.
		creds, err := google.CredentialsFromJSON(ctx, data, defaultCloudPlatformAuth) //nolint:staticcheck
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

// resolvedConfigHeadersKey carries the HeadersResolver values a call
// resolved once (resolveConfigHeadersWith) to the auth transport.
type resolvedConfigHeadersKey struct{}

// resolveConfigHeadersWith returns the anthropic Config.ResolveConfigHeaders
// hook for a HeadersResolver: TS resolves the config headers once at the
// start of each call, reads their anthropic-beta as user-supplied betas and
// sends the same values. nil when no resolver is configured.
func resolveConfigHeadersWith(resolver HeadersResolver) func(context.Context) (context.Context, map[string]string, error) {
	if resolver == nil {
		return nil
	}
	return func(ctx context.Context) (context.Context, map[string]string, error) {
		headers, err := resolver(ctx)
		if err != nil {
			return ctx, nil, err
		}
		if headers == nil {
			headers = map[string]string{}
		}
		return context.WithValue(ctx, resolvedConfigHeadersKey{}, headers), headers, nil
	}
}
