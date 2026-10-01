package anthropic

import (
	"context"
	"net/http"

	"golang.org/x/oauth2"
)

// HeadersResolver resolves request headers dynamically.
type HeadersResolver func(ctx context.Context) (map[string]string, error)

// StaticHeaders returns a resolver for a fixed header map.
func StaticHeaders(headers map[string]string) HeadersResolver {
	return func(context.Context) (map[string]string, error) {
		if headers == nil {
			return nil, nil
		}
		cloned := make(map[string]string, len(headers))
		for k, v := range headers {
			cloned[k] = v
		}
		return cloned, nil
	}
}

// Options configures the Google Vertex Anthropic provider.
type Options struct {
	// Project is the Google Cloud project ID. Defaults to GOOGLE_VERTEX_PROJECT.
	Project string

	// Location is the Vertex AI region, for example "us-east5". Defaults to
	// GOOGLE_VERTEX_LOCATION.
	Location string

	// BaseURL overrides the Vertex Anthropic URL prefix. When empty, the URL is
	// built from Project and Location.
	BaseURL string

	// CredentialsFile is a service account JSON credentials file to use instead
	// of Application Default Credentials.
	CredentialsFile string

	// CredentialsJSON is service account JSON credentials content to use instead
	// of Application Default Credentials.
	CredentialsJSON []byte

	// TokenSource provides Google OAuth2 tokens directly. It is used when
	// CredentialsJSON and CredentialsFile are empty, before falling back to ADC.
	TokenSource oauth2.TokenSource `json:"-"`

	// AuthToken overrides Google ADC token resolution. It is called for each
	// request, matching the TypeScript generateAuthToken hook.
	AuthToken func(ctx context.Context) (string, error)

	// Headers are custom HTTP headers. They are applied after Authorization, so a
	// user-provided Authorization header overrides the generated token.
	Headers map[string]string

	// HeadersResolver resolves custom HTTP headers per request. Values are
	// applied after Headers and Authorization, matching the TypeScript SDK's
	// resolvable headers option.
	HeadersResolver HeadersResolver

	// HTTPClient customizes the underlying HTTP client.
	HTTPClient *http.Client
}
