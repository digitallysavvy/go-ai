package mcp

import (
	"context"
	"net/http"
	"net/url"
)

// OAuthTokens is an OAuth 2.1 token response, matching TS OAuthTokensSchema
// (oauth-types.ts). Issuer/AuthorizationServer/TokenEndpoint pin the
// authorization server that issued the tokens (hash f0c6770) so a later
// refresh can detect stale rediscovered metadata before reusing them.
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    *int   `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`

	Issuer              string `json:"issuer,omitempty"`
	AuthorizationServer string `json:"authorization_server,omitempty"`
	TokenEndpoint       string `json:"token_endpoint,omitempty"`
}

// OAuthClientMetadata is OAuth Dynamic Client Registration (RFC 7591) request
// metadata, matching TS OAuthClientMetadataSchema (oauth-types.ts).
type OAuthClientMetadata struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ApplicationType         string   `json:"application_type,omitempty"` // "native" | "web"
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	LogoURI                 string   `json:"logo_uri,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	Contacts                []string `json:"contacts,omitempty"`
	TosURI                  string   `json:"tos_uri,omitempty"`
	PolicyURI               string   `json:"policy_uri,omitempty"`
	JWKSURI                 string   `json:"jwks_uri,omitempty"`
	SoftwareID              string   `json:"software_id,omitempty"`
	SoftwareVersion         string   `json:"software_version,omitempty"`
	SoftwareStatement       string   `json:"software_statement,omitempty"`
}

// OAuthClientInformation is the client credential half of a registered OAuth
// client, matching TS OAuthClientInformationSchema (oauth-types.ts).
// Issuer/AuthorizationServer/TokenEndpoint pin the authorization server that
// issued this client (hash f0c6770).
type OAuthClientInformation struct {
	ClientID              string `json:"client_id"`
	ClientSecret          string `json:"client_secret,omitempty"`
	ClientIDIssuedAt      *int64 `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt *int64 `json:"client_secret_expires_at,omitempty"`
	Issuer                string `json:"issuer,omitempty"`
	AuthorizationServer   string `json:"authorization_server,omitempty"`
	TokenEndpoint         string `json:"token_endpoint,omitempty"`
}

// OAuthClientInformationFull is the full result of Dynamic Client
// Registration: the client's own registered metadata plus the credentials
// the server issued for it, matching TS OAuthClientInformationFullSchema
// (OAuthClientMetadataSchema merged with OAuthClientInformationSchema).
type OAuthClientInformationFull struct {
	OAuthClientMetadata
	OAuthClientInformation
}

// AsClientInformation drops the metadata half, matching TS's structural
// subtyping where an OAuthClientInformationFull is usable wherever an
// OAuthClientInformation is expected.
func (f OAuthClientInformationFull) AsClientInformation() OAuthClientInformation {
	return f.OAuthClientInformation
}

// OAuthClientProvider is the Go analogue of TS OAuthClientProvider
// (oauth.ts): the storage and policy hooks that Auth needs to drive an OAuth
// 2.1 authorization_code + PKCE flow for an MCP server. Implementations
// typically back these with a keychain, database row, or in-memory struct.
//
// Optional TS methods (addClientAuthentication, invalidateCredentials,
// isClientInformationDynamicallyRegistered, saveClientInformation,
// authorizationServerInformation/saveAuthorizationServerInformation,
// validateAuthorizationServerURL, state/saveState/storedState,
// validateResourceURL) are modeled as separate capability interfaces below
// that a provider may additionally implement; Auth checks for them with a
// type assertion, mirroring an optional-method check in TS.
type OAuthClientProvider interface {
	// Tokens returns the current stored tokens, or nil if there are none.
	Tokens(ctx context.Context) (*OAuthTokens, error)
	// SaveTokens persists tokens obtained from an authorization code exchange
	// or a refresh.
	SaveTokens(ctx context.Context, tokens OAuthTokens) error
	// RedirectToAuthorization directs the end user to authorizationURL (e.g.
	// by opening a browser, or returning it to an HTTP caller as a redirect).
	RedirectToAuthorization(ctx context.Context, authorizationURL string) error
	// SaveCodeVerifier persists the PKCE code_verifier generated for this
	// authorization attempt.
	SaveCodeVerifier(ctx context.Context, codeVerifier string) error
	// CodeVerifier returns the PKCE code_verifier saved by SaveCodeVerifier.
	CodeVerifier(ctx context.Context) (string, error)
	// RedirectURL is the OAuth redirect_uri registered for this client.
	RedirectURL() string
	// ClientMetadata is this client's Dynamic Client Registration metadata,
	// used both to register a new client and to select scope.
	ClientMetadata() OAuthClientMetadata
	// ClientInformation returns previously stored (pre-registered or
	// dynamically registered) client credentials, or nil if none exist yet.
	ClientInformation(ctx context.Context) (*OAuthClientInformation, error)
}

// OAuthClientAuthenticator lets a provider fully customize how client
// credentials are added to token requests (TS
// OAuthClientProvider.addClientAuthentication, hash 78e0023).
type OAuthClientAuthenticator interface {
	AddClientAuthentication(ctx context.Context, headers http.Header, params url.Values, tokenURL string, metadata *OAuthAuthorizationServerMetadata) error
}

// OAuthCredentialInvalidationScope selects which stored credentials
// OAuthCredentialInvalidator.InvalidateCredentials should discard.
type OAuthCredentialInvalidationScope string

const (
	OAuthInvalidateAll      OAuthCredentialInvalidationScope = "all"
	OAuthInvalidateClient   OAuthCredentialInvalidationScope = "client"
	OAuthInvalidateTokens   OAuthCredentialInvalidationScope = "tokens"
	OAuthInvalidateVerifier OAuthCredentialInvalidationScope = "verifier"
)

// OAuthCredentialInvalidator lets Auth clear stored credentials after the
// authorization server reports they are no longer valid (TS
// OAuthClientProvider.invalidateCredentials).
type OAuthCredentialInvalidator interface {
	InvalidateCredentials(ctx context.Context, scope OAuthCredentialInvalidationScope) error
}

// OAuthTokenInvalidator extends OAuthCredentialInvalidator so a provider can
// receive the specific token generation being invalidated for
// OAuthInvalidateTokens, matching TS OAuthClientProvider.invalidateCredentials'
// optional `context: { tokens: OAuthTokens }` parameter (hash 4d0500e). If two
// clients share storage and one refresh succeeds while a concurrent one
// fails, the failing client identifies exactly which (now stale) tokens it
// tried so the provider can atomically compare-and-delete only that
// generation, keeping the winner's newer tokens intact. Auth calls
// InvalidateCredentialsForTokens instead of InvalidateCredentials when both
// the provider implements this interface and the token generation being
// invalidated is known; otherwise it falls back to the unconditional form.
type OAuthTokenInvalidator interface {
	InvalidateCredentialsForTokens(ctx context.Context, tokens OAuthTokens) error
}

// OAuthDynamicRegistrationReporter reports whether the current client
// information came from Dynamic Client Registration rather than being
// pre-registered by the developer (TS
// OAuthClientProvider.isClientInformationDynamicallyRegistered, hash
// 7fc2bc1). Auth only auto-invalidates and re-registers a client on
// invalid_client/unauthorized_client when this returns true; a pre-registered
// client is never silently replaced.
type OAuthDynamicRegistrationReporter interface {
	IsClientInformationDynamicallyRegistered(ctx context.Context) (bool, error)
}

// OAuthClientInformationSaver lets Auth persist newly registered (or
// AS-pin-updated) client information (TS
// OAuthClientProvider.saveClientInformation). Required for Dynamic Client
// Registration to be usable.
type OAuthClientInformationSaver interface {
	SaveClientInformation(ctx context.Context, info OAuthClientInformation) error
}

// OAuthAuthorizationServerInformationStore lets a provider persist the
// authorization-server pin (issuer/authorization_server/token_endpoint)
// separately from client information (TS
// OAuthClientProvider.authorizationServerInformation /
// saveAuthorizationServerInformation).
type OAuthAuthorizationServerInformationStore interface {
	AuthorizationServerInformation(ctx context.Context) (*OAuthAuthorizationServerInformation, error)
	SaveAuthorizationServerInformation(ctx context.Context, info OAuthAuthorizationServerInformation) error
}

// OAuthAuthorizationServerURLValidator lets an application constrain which
// authorization server URLs Auth will fetch metadata from, before it does so
// (TS OAuthClientProvider.validateAuthorizationServerURL).
type OAuthAuthorizationServerURLValidator interface {
	ValidateAuthorizationServerURL(ctx context.Context, serverURL, authorizationServerURL string) error
}

// OAuthStateProvider lets Auth generate, persist, and later verify a CSRF
// state parameter across the authorization redirect (TS
// OAuthClientProvider.state/saveState/storedState).
type OAuthStateProvider interface {
	State(ctx context.Context) (string, error)
	SaveState(ctx context.Context, state string) error
	// StoredState returns the previously saved state, and false if none was
	// stored (as opposed to an empty string being the stored value).
	StoredState(ctx context.Context) (state string, ok bool, err error)
}

// OAuthResourceURLValidator lets an application override RFC 8707 resource
// parameter selection (TS OAuthClientProvider.validateResourceURL). Returning
// ("", nil) omits the resource parameter.
type OAuthResourceURLValidator interface {
	ValidateResourceURL(ctx context.Context, defaultResource string, resourceMetadataResource string) (string, error)
}
