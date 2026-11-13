package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/verceloidc"
)

// VercelOIDCTokenError is the TS @vercel/oidc VercelOidcTokenError: the
// Vercel OIDC token is missing, malformed or could not be refreshed.
type VercelOIDCTokenError struct {
	Message string
	Cause   error
}

func (e *VercelOIDCTokenError) Error() string { return e.Message }

// Unwrap returns the cause.
func (e *VercelOIDCTokenError) Unwrap() error { return e.Cause }

// ContextWithVercelOIDCToken returns a context whose requests use token as
// the Vercel OIDC token, the counterpart of the `x-vercel-oidc-token` header
// TS reads from the Vercel request context. It is read by the function that
// VercelOIDCTokenFunc returns, before VERCEL_OIDC_TOKEN, and by the Vercel
// Sandbox OIDC refresh (harness/sandbox/vercel OIDCRefresh), as both TS
// packages read the same request context through @vercel/oidc.
func ContextWithVercelOIDCToken(ctx context.Context, token string) context.Context {
	return verceloidc.ContextWithToken(ctx, token)
}

// VercelOIDCTokenOptions configures VercelOIDCTokenFunc.
type VercelOIDCTokenOptions struct {
	// ProjectID and TeamID select the project whose OIDC token is refreshed
	// (TS options.project / options.team). When either is empty it is read
	// from the linked project (.vercel/project.json in the working directory
	// or a parent: projectId and orgId).
	ProjectID string
	TeamID    string

	// ExpirationBuffer refreshes a token that expires within this duration
	// (TS expirationBufferMs).
	ExpirationBuffer time.Duration

	// AccessToken returns the Vercel access token for the refresh request.
	// Default: the token of the Vercel CLI login (auth.json in the Vercel CLI
	// data directory), read only; an expired login is an error asking for
	// `vercel login` (the Go SDK never refreshes, rewrites or clears the CLI
	// login).
	AccessToken func(ctx context.Context) (string, error)

	// HTTPClient sends the refresh request. Default: http.DefaultClient.
	HTTPClient *http.Client

	// APIBaseURL is the Vercel API origin. Default: https://api.vercel.com.
	APIBaseURL string
}

// VercelOIDCTokenFunc returns a Config.OIDCTokenFunc that resolves the Vercel
// OIDC token like TS getVercelOidcToken (@vercel/oidc 3.2.0, used by
// getGatewayAuthToken): the request context token (ContextWithVercelOIDCToken)
// or VERCEL_OIDC_TOKEN, refreshed when it is missing or its JWT `exp` falls
// within ExpirationBuffer. A refresh reads the cached project token (read
// only) or asks the Vercel API for a new one with the Vercel access token
// (POST /v1/projects/{project}/token?source=vercel-oidc-refresh).
//
// Unlike TS, the refreshed token is kept in memory (the returned function's
// cache) instead of being written to the Vercel token cache and
// process.env, and the Vercel CLI login is never refreshed, rewritten or
// cleared. The function is safe for concurrent use; concurrent refreshes are
// serialized.
func VercelOIDCTokenFunc(opts VercelOIDCTokenOptions) func(ctx context.Context) (string, error) {
	r := verceloidc.New(verceloidc.Options{
		ProjectID: opts.ProjectID, TeamID: opts.TeamID, ExpirationBuffer: opts.ExpirationBuffer,
		AccessToken: opts.AccessToken, HTTPClient: opts.HTTPClient, APIBaseURL: opts.APIBaseURL,
		NewError: func(message string, cause error) error {
			return &VercelOIDCTokenError{Message: message, Cause: cause}
		},
	})
	return r.Token
}

const vercelOIDCMissingMessage = verceloidc.MissingMessage
