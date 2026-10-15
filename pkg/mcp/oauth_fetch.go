package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const maxOAuthDiscoveryRedirects = 10

// isOAuthLoopbackHost allows loopback HTTP(S) for local MCP OAuth (RFC 8252
// §7.3, RFC 6761 §6.3), matching TS isOAuthLoopbackHost.
func isOAuthLoopbackHost(hostname string) bool {
	normalized := strings.TrimRight(strings.ToLower(hostname), ".")
	return normalized == "localhost" ||
		strings.HasSuffix(normalized, ".localhost") ||
		normalized == "127.0.0.1" ||
		normalized == "[::1]" ||
		normalized == "::1"
}

// TrustedOAuthAuthorizationServerOrigin returns the authorization server origin
// when it may skip the SSRF guard: it is the configured MCP server's own
// origin, or both the MCP server and the authorization server are loopback
// (TS auth() trustedAuthorizationServerOrigin). Otherwise it returns "".
func TrustedOAuthAuthorizationServerOrigin(serverURL, authorizationServerURL string) string {
	server, err := url.Parse(serverURL)
	if err != nil {
		return ""
	}
	as, err := url.Parse(authorizationServerURL)
	if err != nil || as.Scheme == "" || as.Host == "" {
		return ""
	}
	if origin(as) == origin(server) || (isOAuthLoopbackHost(server.Hostname()) && isOAuthLoopbackHost(as.Hostname())) {
		return origin(as)
	}
	return ""
}

// assertSafeOAuthEndpoint guards metadata-derived OAuth URLs before they are
// requested (TS assertSafeOAuthEndpoint).
func assertSafeOAuthEndpoint(endpoint string, allowLoopback bool) error {
	if allowLoopback {
		if u, err := url.Parse(endpoint); err == nil && (u.Scheme == "http" || u.Scheme == "https") && isOAuthLoopbackHost(u.Hostname()) {
			return nil
		}
	}
	if err := fileutil.ValidateDownloadURL(endpoint); err != nil {
		return fmt.Errorf("OAuth endpoint URL is not allowed: %s: %w", endpoint, err)
	}
	return nil
}

// oauthMetadataGET fetches OAuth discovery metadata while enforcing the SSRF
// guard on every hop (TS fetchWithCorsRetry → fetchWithValidatedRedirects,
// c43e4b7):
//
//   - Redirects are followed manually; each hop that is not same-origin with
//     trustedOrigin (developer-configured, never response data) is validated
//     with fileutil.ValidateDownloadURL before it is requested.
//   - The MCP-Protocol-Version header is dropped once a redirect crosses
//     origin.
//   - When no HTTP client is configured, untrusted hops are dialed through the
//     DNS-pinning fileutil.SafeTransport. A caller-supplied client is
//     responsible for equivalent connect-time validation (TS: injected fetch).
//
// A transport failure (a fetch TypeError in TS) is retried once without the
// MCP-Protocol-Version header (the CORS-preflight workaround of TS
// fetchWithCorsRetry); a second failure returns a nil response and nil error,
// which the callers treat as "not found". Validation errors and context
// cancellation are returned as they are.
func oauthMetadataGET(ctx context.Context, client *http.Client, endpoint, protocolVersion, trustedOrigin string) (*http.Response, error) {
	resp, err := oauthMetadataGETOnce(ctx, client, endpoint, protocolVersion, trustedOrigin)
	var failure *oauthFetchFailure
	if !errors.As(err, &failure) {
		return resp, err
	}
	if protocolVersion != "" {
		return oauthMetadataGET(ctx, client, endpoint, "", trustedOrigin)
	}
	return nil, nil
}

// oauthFetchFailure marks a transport-level failure of an OAuth metadata
// request (TS: fetch rejecting with a TypeError).
type oauthFetchFailure struct{ err error }

func (e *oauthFetchFailure) Error() string { return e.err.Error() }
func (e *oauthFetchFailure) Unwrap() error { return e.err }

func oauthMetadataGETOnce(ctx context.Context, client *http.Client, endpoint, protocolVersion, trustedOrigin string) (*http.Response, error) {
	var headers http.Header
	if protocolVersion != "" {
		headers = http.Header{}
		headers.Set("MCP-Protocol-Version", protocolVersion)
	}
	current := endpoint
	for redirects := 0; redirects <= maxOAuthDiscoveryRedirects; redirects++ {
		trusted := trustedOrigin != "" && fileutil.IsSameOrigin(current, trustedOrigin)
		if !trusted {
			if err := fileutil.ValidateDownloadURL(current); err != nil {
				return nil, err
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current, nil)
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			req.Header[k] = append([]string(nil), v...)
		}
		resp, err := noRedirectClient(client, trusted).Do(req)
		if err != nil {
			var downloadErr *providererrors.DownloadError
			if ctx.Err() != nil || errors.As(err, &downloadErr) {
				return nil, err
			}
			return nil, &oauthFetchFailure{err: err}
		}

		location := resp.Header.Get("Location")
		if !isFetchRedirectStatus(resp.StatusCode) || location == "" {
			return resp, nil
		}
		closeResponse(resp)

		base, err := url.Parse(current)
		if err != nil {
			return nil, err
		}
		next, err := base.Parse(location)
		if err != nil {
			return nil, err
		}
		nextURL := next.String()
		if !fileutil.IsSameOrigin(nextURL, current) {
			headers = nil
		}
		current = nextURL
	}
	return nil, providererrors.NewDownloadError(endpoint, 0, "", fmt.Sprintf("Too many redirects (max %d)", maxOAuthDiscoveryRedirects), nil)
}

// isFetchRedirectStatus reports redirect statuses per the fetch spec; 300 and
// 304 are not redirects even with a Location header.
func isFetchRedirectStatus(status int) bool {
	switch status {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

func noRedirectClient(client *http.Client, trusted bool) *http.Client {
	var c http.Client
	if client != nil {
		c = *client
	} else if !trusted {
		c.Transport = fileutil.SafeTransport()
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &c
}
