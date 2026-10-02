package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ValidateOAuthState compares the state parameter returned in an OAuth callback
// against the state value that was originally sent in the authorization request.
//
// The comparison is performed with crypto/subtle.ConstantTimeCompare to prevent
// timing-based side-channel attacks. A mismatch indicates a possible CSRF attack
// and an error is returned.
//
// Usage in an authorization-code callback handler:
//
//	if err := ValidateOAuthState(returnedState, sentState); err != nil {
//	    http.Error(w, err.Error(), http.StatusBadRequest)
//	    return
//	}
func ValidateOAuthState(returnedState, sentState string) error {
	if subtle.ConstantTimeCompare([]byte(returnedState), []byte(sentState)) != 1 {
		return fmt.Errorf("mcp oauth: state parameter mismatch — possible CSRF attack")
	}
	return nil
}

// OAuthAuthorizationServerInformation pins the authorization server and token
// endpoint (and, when known, issuer) that issued stored OAuth credentials.
// Matches TS OAuthAuthorizationServerInformation (oauth.ts).
type OAuthAuthorizationServerInformation struct {
	Issuer                 string `json:"issuer,omitempty"`
	AuthorizationServerURL string `json:"authorization_server"`
	TokenEndpoint          string `json:"token_endpoint"`
}

// OAuthProtectedResourceMetadata is OAuth 2.0 Protected Resource Metadata used
// by MCP servers to advertise their resource identity and authorization servers.
type OAuthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers,omitempty"`
	// ScopesSupported lists the OAuth scopes the protected resource supports.
	// Used for scope selection precedence (TS selectScope, hash 1011e33).
	ScopesSupported []string `json:"scopes_supported,omitempty"`
}

// OAuthAuthorizationServerMetadata is OAuth/OIDC authorization server metadata.
type OAuthAuthorizationServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint,omitempty"`
	// AuthorizationResponseIssParameterSupported and ClientIDMetadataDocumentSupported
	// mirror TS OAuthMetadataSchema additions (hash 1f29230).
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported,omitempty"`
	ClientIDMetadataDocumentSupported          bool     `json:"client_id_metadata_document_supported,omitempty"`
	ScopesSupported                            []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported                     []string `json:"response_types_supported,omitempty"`
	GrantTypesSupported                        []string `json:"grant_types_supported,omitempty"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethods                   []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	JWKSURI                                    string   `json:"jwks_uri,omitempty"`
	SubjectTypesSupported                      []string `json:"subject_types_supported,omitempty"`
	IDTokenSigningAlgValues                    []string `json:"id_token_signing_alg_values_supported,omitempty"`
}

// OAuthDiscoveryURL is one authorization-server metadata endpoint candidate.
type OAuthDiscoveryURL struct {
	URL            string
	Type           string
	ExpectedIssuer string
}

// OAuthDiscoveryOptions configures MCP OAuth metadata discovery.
type OAuthDiscoveryOptions struct {
	ProtocolVersion                string
	ResourceMetadataURL            string
	HTTPClient                     *http.Client
	ValidateAuthorizationServerURL func(serverURL string, authorizationServerURL string) error

	// TrustedOrigin is a developer-configured origin whose discovery hops skip
	// the SSRF guard in DiscoverAuthorizationServerMetadata (TS trustedOrigin).
	// It must never be derived from response data. See
	// TrustedOAuthAuthorizationServerOrigin.
	TrustedOrigin string
}

// CreateOAuthAuthorizationServerInformation creates the credential pin used by
// TS to ensure stored client/tokens are only reused with the authorization
// server metadata that originally issued them.
func CreateOAuthAuthorizationServerInformation(authorizationServerURL string, metadata *OAuthAuthorizationServerMetadata) (OAuthAuthorizationServerInformation, error) {
	authURL, err := url.Parse(authorizationServerURL)
	if err != nil {
		return OAuthAuthorizationServerInformation{}, err
	}
	tokenURL := authURL.ResolveReference(&url.URL{Path: "/token"})
	if metadata != nil && metadata.TokenEndpoint != "" {
		tokenURL, err = url.Parse(metadata.TokenEndpoint)
		if err != nil {
			return OAuthAuthorizationServerInformation{}, err
		}
	}
	issuer := authorizationServerURL
	if metadata != nil && metadata.Issuer != "" {
		issuer = metadata.Issuer
	}
	return OAuthAuthorizationServerInformation{
		Issuer:                 issuer,
		AuthorizationServerURL: oauthURLHref(authURL),
		TokenEndpoint:          oauthURLHref(tokenURL),
	}, nil
}

// AssertOAuthAuthorizationServerInformationMatches prevents rediscovered
// metadata from being used with credentials issued by a different AS/token
// endpoint. Issuer is compared only when both sides have one (matching TS
// assertAuthorizationServerInformationMatches, which tolerates older stored
// pins that predate the issuer field).
func AssertOAuthAuthorizationServerInformationMatches(stored, current OAuthAuthorizationServerInformation) error {
	if (stored.Issuer != "" && current.Issuer != "" && stored.Issuer != current.Issuer) ||
		normalizeOAuthURL(stored.AuthorizationServerURL) != normalizeOAuthURL(current.AuthorizationServerURL) ||
		normalizeOAuthURL(stored.TokenEndpoint) != normalizeOAuthURL(current.TokenEndpoint) {
		return &MCPClientOAuthError{
			Code:    OAuthErrorCodeAuthorizationServerMismatch,
			Message: "OAuth authorization server metadata does not match the metadata that issued the stored credentials",
		}
	}
	return nil
}

// SelectOAuthAuthorizationServerURL discovers protected resource metadata and
// returns its first authorization server, or the MCP server URL when PRM is not
// available. The optional validation hook runs before callers fetch AS metadata,
// matching TypeScript's OAuthClientProvider.validateAuthorizationServerURL.
func SelectOAuthAuthorizationServerURL(ctx context.Context, serverURL string, opts OAuthDiscoveryOptions) (string, *OAuthProtectedResourceMetadata, error) {
	metadata, err := DiscoverOAuthProtectedResourceMetadata(ctx, serverURL, opts)
	authServerURL := serverURL
	if err == nil {
		if len(metadata.AuthorizationServers) > 0 {
			authServerURL = metadata.AuthorizationServers[0]
		}
	} else if !strings.Contains(err.Error(), "Resource server does not implement OAuth 2.0 Protected Resource Metadata.") {
		return "", nil, err
	}
	// An authorization server selected by response metadata is untrusted until
	// its target has passed the SSRF guard. A same-origin (or loopback-to-
	// loopback) server is already the developer-configured request target.
	if TrustedOAuthAuthorizationServerOrigin(serverURL, authServerURL) == "" {
		if err := assertSafeOAuthEndpoint(authServerURL, false); err != nil {
			return "", nil, err
		}
	}
	if opts.ValidateAuthorizationServerURL != nil {
		if err := opts.ValidateAuthorizationServerURL(serverURL, authServerURL); err != nil {
			return "", nil, err
		}
	}
	if err != nil {
		return authServerURL, nil, nil
	}
	return authServerURL, &metadata, nil
}

// AssertOAuthResourceMetadataURLSameOrigin rejects WWW-Authenticate
// resource_metadata URLs that would send discovery traffic away from the MCP
// server origin.
func AssertOAuthResourceMetadataURLSameOrigin(serverURL string, resourceMetadataURL string) error {
	if resourceMetadataURL == "" {
		return nil
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return err
	}
	resource, err := url.Parse(resourceMetadataURL)
	if err != nil {
		return err
	}
	if origin(server) != origin(resource) {
		return NewMCPClientError(0, fmt.Sprintf("OAuth protected resource metadata URL %s must have the same origin as the MCP server URL %s", resource.String(), origin(server)), nil)
	}
	return nil
}

// ExtractResourceMetadataURL extracts RFC 9728 resource_metadata from a Bearer
// WWW-Authenticate header.
func ExtractResourceMetadataURL(resp *http.Response) (*url.URL, bool) {
	params := ExtractWWWAuthenticateParams(resp)
	return params.ResourceMetadataURL, params.ResourceMetadataURL != nil
}

// WWWAuthenticateParams is the subset of a Bearer WWW-Authenticate challenge
// this client understands, matching TS extractWWWAuthenticateParams
// (oauth.ts, hash 1011e33).
type WWWAuthenticateParams struct {
	ResourceMetadataURL *url.URL
	// Scope is the space-separated scope list requested by the challenge, if
	// present. Used with precedence challenge > PRM > client metadata by
	// SelectOAuthScope.
	Scope string
}

// ExtractWWWAuthenticateParams parses a Bearer WWW-Authenticate header for
// both the RFC 9728 resource_metadata URL and an RFC 6750 scope parameter,
// matching TS extractWWWAuthenticateParams (oauth.ts, hash 1011e33).
func ExtractWWWAuthenticateParams(resp *http.Response) WWWAuthenticateParams {
	if resp == nil {
		return WWWAuthenticateParams{}
	}
	header := resp.Header.Get("WWW-Authenticate")
	if header == "" {
		return WWWAuthenticateParams{}
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return WWWAuthenticateParams{}
	}

	var out WWWAuthenticateParams
	if resourceMetadataURL := extractQuotedChallengeParam(header, "resource_metadata"); resourceMetadataURL != "" {
		if parsed, err := url.Parse(resourceMetadataURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
			out.ResourceMetadataURL = parsed
		}
	}
	out.Scope = extractQuotedChallengeParam(header, "scope")
	return out
}

// extractQuotedChallengeParam extracts `key="value"` from an HTTP challenge
// header (e.g. WWW-Authenticate), matching TS's per-param regex extraction in
// extractWWWAuthenticateParams.
func extractQuotedChallengeParam(header, key string) string {
	needle := key + `="`
	start := strings.Index(header, needle)
	if start < 0 {
		return ""
	}
	start += len(needle)
	end := strings.Index(header[start:], `"`)
	if end < 0 {
		return ""
	}
	return header[start : start+end]
}

// DiscoverOAuthProtectedResourceMetadata discovers MCP protected resource
// metadata using the same path-aware well-known and root fallback as TS.
func DiscoverOAuthProtectedResourceMetadata(ctx context.Context, serverURL string, opts OAuthDiscoveryOptions) (OAuthProtectedResourceMetadata, error) {
	client := opts.HTTPClient
	protocolVersion := opts.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = ProtocolVersion
	}
	metadataURL, err := protectedResourceMetadataURL(serverURL, opts.ResourceMetadataURL)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, err
	}
	// The configured MCP server is trusted as a request target. Redirects
	// crossing its origin are still validated before they are followed.
	trustedOrigin := ""
	if server, err := url.Parse(serverURL); err == nil && server.Scheme != "" && server.Host != "" {
		trustedOrigin = origin(server)
	}
	resp, err := oauthMetadataGET(ctx, client, metadataURL, protocolVersion, trustedOrigin)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, err
	}
	if opts.ResourceMetadataURL == "" && shouldAttemptOAuthFallback(resp, metadataURL, serverURL) {
		closeResponse(resp)
		rootURL, err := url.Parse(serverURL)
		if err != nil {
			return OAuthProtectedResourceMetadata{}, err
		}
		resp, err = oauthMetadataGET(ctx, client, rootURL.ResolveReference(&url.URL{Path: "/.well-known/oauth-protected-resource"}).String(), protocolVersion, trustedOrigin)
		if err != nil {
			return OAuthProtectedResourceMetadata{}, err
		}
	}
	if resp == nil || resp.StatusCode == http.StatusNotFound {
		closeResponse(resp)
		return OAuthProtectedResourceMetadata{}, fmt.Errorf("Resource server does not implement OAuth 2.0 Protected Resource Metadata.") //nolint:staticcheck // matches TS SDK's exact error text
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OAuthProtectedResourceMetadata{}, fmt.Errorf("HTTP %d trying to load well-known OAuth protected resource metadata.", resp.StatusCode) //nolint:staticcheck // matches TS SDK's exact error text
	}
	rawBody, err := readLimitedOAuthBody(resp)
	if err != nil {
		return OAuthProtectedResourceMetadata{}, err
	}
	var metadata OAuthProtectedResourceMetadata
	if err := json.Unmarshal(rawBody, &metadata); err != nil {
		return OAuthProtectedResourceMetadata{}, err
	}
	if err := validateOAuthProtectedResourceMetadata(metadata); err != nil {
		return OAuthProtectedResourceMetadata{}, err
	}
	return metadata, nil
}

// BuildAuthorizationServerDiscoveryURLs returns TS-equivalent OAuth/OIDC
// metadata candidates in priority order.
func BuildAuthorizationServerDiscoveryURLs(authorizationServerURL string) ([]OAuthDiscoveryURL, error) {
	parsed, err := url.Parse(authorizationServerURL)
	if err != nil {
		return nil, err
	}
	hasPath := parsed.Path != "" && parsed.Path != "/"
	rootIssuer := origin(parsed)
	if !hasPath {
		return []OAuthDiscoveryURL{
			{URL: rootIssuer + "/.well-known/oauth-authorization-server", Type: "oauth", ExpectedIssuer: rootIssuer},
			{URL: rootIssuer + "/.well-known/openid-configuration", Type: "oidc", ExpectedIssuer: rootIssuer},
		}, nil
	}
	pathname := strings.TrimSuffix(parsed.Path, "/")
	pathIssuer := rootIssuer + pathname
	return []OAuthDiscoveryURL{
		{URL: rootIssuer + "/.well-known/oauth-authorization-server" + pathname, Type: "oauth", ExpectedIssuer: pathIssuer},
		{URL: rootIssuer + "/.well-known/oauth-authorization-server", Type: "oauth", ExpectedIssuer: rootIssuer},
		{URL: rootIssuer + "/.well-known/openid-configuration" + pathname, Type: "oidc", ExpectedIssuer: pathIssuer},
		{URL: rootIssuer + pathname + "/.well-known/openid-configuration", Type: "oidc", ExpectedIssuer: pathIssuer},
	}, nil
}

// DiscoverAuthorizationServerMetadata discovers OAuth/OIDC authorization server
// metadata and validates the issuer against the discovery URL, matching TS.
func DiscoverAuthorizationServerMetadata(ctx context.Context, authorizationServerURL string, opts OAuthDiscoveryOptions) (*OAuthAuthorizationServerMetadata, error) {
	client := opts.HTTPClient
	protocolVersion := opts.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = ProtocolVersion
	}
	urls, err := BuildAuthorizationServerDiscoveryURLs(authorizationServerURL)
	if err != nil {
		return nil, err
	}
	for _, candidate := range urls {
		resp, err := oauthMetadataGET(ctx, client, candidate.URL, protocolVersion, opts.TrustedOrigin)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			continue
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			closeResponse(resp)
			if resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError {
				continue
			}
			label := "OAuth"
			if candidate.Type == "oidc" {
				label = "OpenID provider"
			}
			return nil, fmt.Errorf("HTTP %d trying to load %s metadata from %s", resp.StatusCode, label, candidate.URL)
		}
		rawBody, bodyErr := readLimitedOAuthBody(resp)
		resp.Body.Close() //nolint:errcheck
		if bodyErr != nil {
			return nil, bodyErr
		}
		var metadata OAuthAuthorizationServerMetadata
		if err := json.Unmarshal(rawBody, &metadata); err != nil {
			return nil, err
		}
		if err := validateOAuthAuthorizationServerMetadata(metadata, candidate.Type); err != nil {
			return nil, err
		}
		if !oauthIssuerMatches(metadata.Issuer, candidate.ExpectedIssuer) {
			return nil, NewMCPClientError(0, fmt.Sprintf("OAuth authorization server metadata issuer %s does not match expected issuer %s", metadata.Issuer, candidate.ExpectedIssuer), nil)
		}
		if candidate.Type == "oidc" && !containsOAuthString(metadata.CodeChallengeMethodsSupported, "S256") {
			return nil, fmt.Errorf("Incompatible OIDC provider at %s: does not support S256 code challenge method required by MCP specification", candidate.URL) //nolint:staticcheck // matches TS SDK's exact error text
		}
		return &metadata, nil
	}
	return nil, nil
}

func protectedResourceMetadataURL(serverURL, explicit string) (string, error) {
	if explicit != "" {
		parsed, err := url.Parse(explicit)
		if err != nil {
			return "", err
		}
		return parsed.String(), nil
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return "", err
	}
	pathname := strings.TrimSuffix(server.Path, "/")
	server.Path = "/.well-known/oauth-protected-resource" + pathname
	return server.String(), nil
}

// oauthIssuerMatches accepts an exact issuer match, or a trailing slash on an
// origin-only expected issuer (TS assertMetadataIssuerMatches, 809e922).
func oauthIssuerMatches(issuer, expected string) bool {
	if issuer == expected {
		return true
	}
	parsed, err := url.Parse(expected)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return expected == origin(parsed) && issuer == expected+"/"
}

func shouldAttemptOAuthFallback(resp *http.Response, _ string, serverURL string) bool {
	server, err := url.Parse(serverURL)
	if err != nil {
		return false
	}
	if server.Path == "" || server.Path == "/" {
		return false
	}
	return resp == nil || (resp.StatusCode >= http.StatusBadRequest && resp.StatusCode < http.StatusInternalServerError)
}

func closeResponse(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck
		resp.Body.Close()              //nolint:errcheck
	}
}

func normalizeOAuthURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return oauthURLHref(parsed)
}

func origin(u *url.URL) string {
	return u.Scheme + "://" + u.Host
}

func oauthURLHref(u *url.URL) string {
	if u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.Host != "" {
		copy := *u
		copy.Path = "/"
		return copy.String()
	}
	return u.String()
}

func containsOAuthString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validateOAuthProtectedResourceMetadata(metadata OAuthProtectedResourceMetadata) error {
	if metadata.Resource == "" {
		return fmt.Errorf("OAuth protected resource metadata missing required resource")
	}
	if !isAbsoluteOAuthURL(metadata.Resource) {
		return fmt.Errorf("OAuth protected resource metadata resource must be a valid URL")
	}
	for _, server := range metadata.AuthorizationServers {
		if !isAbsoluteOAuthURL(server) {
			return fmt.Errorf("OAuth protected resource metadata authorization server must be a valid URL")
		}
	}
	return nil
}

func validateOAuthAuthorizationServerMetadata(metadata OAuthAuthorizationServerMetadata, metadataType string) error {
	if metadata.Issuer == "" {
		return fmt.Errorf("OAuth authorization server metadata missing required issuer")
	}
	if metadata.AuthorizationEndpoint == "" {
		return fmt.Errorf("OAuth authorization server metadata missing required authorization_endpoint")
	}
	if !isAbsoluteOAuthURL(metadata.AuthorizationEndpoint) {
		return fmt.Errorf("OAuth authorization server metadata authorization_endpoint must be a valid URL")
	}
	if metadata.TokenEndpoint == "" {
		return fmt.Errorf("OAuth authorization server metadata missing required token_endpoint")
	}
	if !isAbsoluteOAuthURL(metadata.TokenEndpoint) {
		return fmt.Errorf("OAuth authorization server metadata token_endpoint must be a valid URL")
	}
	if len(metadata.ResponseTypesSupported) == 0 {
		return fmt.Errorf("OAuth authorization server metadata missing required response_types_supported")
	}
	if metadata.RegistrationEndpoint != "" {
		if !isAbsoluteOAuthURL(metadata.RegistrationEndpoint) {
			return fmt.Errorf("OAuth authorization server metadata registration_endpoint must be a valid URL")
		}
	}
	if metadataType == "oidc" {
		if metadata.JWKSURI == "" {
			return fmt.Errorf("OpenID provider metadata missing required jwks_uri")
		}
		if !isAbsoluteOAuthURL(metadata.JWKSURI) {
			return fmt.Errorf("OpenID provider metadata jwks_uri must be a valid URL")
		}
		if len(metadata.SubjectTypesSupported) == 0 {
			return fmt.Errorf("OpenID provider metadata missing required subject_types_supported")
		}
		if len(metadata.IDTokenSigningAlgValues) == 0 {
			return fmt.Errorf("OpenID provider metadata missing required id_token_signing_alg_values_supported")
		}
	}
	return nil
}

func isAbsoluteOAuthURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}
