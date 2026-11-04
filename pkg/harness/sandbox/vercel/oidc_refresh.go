package vercel

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	httpinternal "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/verceloidc"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// OIDCRefresh opts in to the @vercel/sandbox 3.9.0 Vercel OIDC token
// refresh (`APIClient.ensureValidToken` and `getCredentials` through
// @vercel/oidc `getVercelOidcToken`):
//   - With no explicit credentials, the token is resolved like @vercel/oidc
//     4.0.0 getVercelOidcToken: the request context token
//     (gateway.ContextWithVercelOIDCToken), the file named by
//     VERCEL_OIDC_TOKEN_FILE (read on each lookup), a token refreshed earlier
//     by this OIDCRefresh, or VERCEL_OIDC_TOKEN, refreshed from the linked project
//     (.vercel/project.json, the cached project token, or the Vercel API with
//     the Vercel CLI login) when it is missing or expired.
//   - Before every Vercel Sandbox API request made with a JWT token (one
//     whose payload has an owner_id claim), a token expiring within 5 minutes
//     is refreshed the same way; a failed refresh is ignored and the current
//     token is sent, as in TS.
//
// The refreshed token is kept in memory in this value (reuse one OIDCRefresh
// to share it across sessions); unlike TS it is never written to the Vercel
// token cache or the process environment, and the Vercel CLI login is never
// refreshed, rewritten or cleared (the same rule as
// gateway.VercelOIDCTokenFunc). Without OIDCRefresh, the token
// (VERCEL_OIDC_TOKEN_FILE, else VERCEL_OIDC_TOKEN) is used as read.
type OIDCRefresh struct {
	// AccessToken returns the Vercel access token for the refresh request.
	// Default: the Vercel CLI login token (read only).
	AccessToken func(ctx context.Context) (string, error)
	// HTTPClient sends the refresh request. Default: http.DefaultClient.
	HTTPClient *http.Client
	// APIBaseURL is the Vercel API origin. Default: https://api.vercel.com.
	APIBaseURL string

	once     sync.Once
	resolver *verceloidc.Resolver
}

// sandboxOIDCExpirationBuffer is ensureValidToken's `expirationBufferMs: 300 * 1e3`.
const sandboxOIDCExpirationBuffer = 5 * time.Minute

func (o *OIDCRefresh) tokenResolver() *verceloidc.Resolver {
	o.once.Do(func() {
		o.resolver = verceloidc.New(verceloidc.Options{
			AccessToken: o.AccessToken, HTTPClient: o.HTTPClient, APIBaseURL: o.APIBaseURL,
			// @vercel/sandbox 3.9.0 pins @vercel/oidc 4.0.0.
			V4: true,
		})
	})
	return o.resolver
}

// vercelOIDCContextError is 3.9.0 get-credentials.js LocalOidcContextError
// (or VercelOidcContextError when VERCEL_URL is set): no OIDC token could be
// resolved. It is an authentication failure (TS
// VERCEL_AUTHENTICATION_ERROR_NAMES).
type vercelOIDCContextError struct {
	message string
	cause   error
}

func (e *vercelOIDCContextError) Error() string { return e.message }
func (e *vercelOIDCContextError) Unwrap() error { return e.cause }

func newVercelOIDCContextError(cause error) error {
	message := strings.Join([]string{
		"Could not get credentials from OIDC context.",
		"Please link your Vercel project using `npx vercel link`.",
		"Then, pull an initial OIDC token with `npx vercel env pull`",
		"and retry.",
		"╰▶ Make sure you are loading `.env.local` correctly, or passing $VERCEL_OIDC_TOKEN directly.",
	}, "\n")
	if os.Getenv("VERCEL_URL") != "" {
		message = strings.Join([]string{
			"Could not get credentials from OIDC context.",
			"Please make sure OIDC is set up for your project",
			"╰▶ Docs: https://vercel.com/docs/oidc",
		}, "\n")
	}
	return &vercelOIDCContextError{message: message, cause: cause}
}

// resolveCredentialsWithRefresh is 3.9.0 `getCredentials` with an
// OIDCRefresh: explicit credentials as ResolveCredentials, otherwise
// `getVercelToken` (getVercelOidcToken, then getCredentialsFromOIDCToken),
// whose failures are a LocalOidcContextError / VercelOidcContextError. The
// interactive dev-credential prompt (`shouldPromptForCredentials`) is not
// ported.
func resolveCredentialsWithRefresh(ctx context.Context, explicit Credentials, refresh *OIDCRefresh) (Credentials, error) {
	if refresh == nil {
		return ResolveCredentials(explicit)
	}
	if explicit.Token != "" || explicit.TeamID != "" || explicit.ProjectID != "" {
		return ResolveCredentials(explicit)
	}
	token, err := refresh.tokenResolver().TokenFor(ctx, "", "", 0)
	if err != nil {
		if ctx.Err() != nil {
			return Credentials{}, ctx.Err()
		}
		return Credentials{}, newVercelOIDCContextError(err)
	}
	ownerID, projectID, err := decodeOIDCClaims(token)
	if err != nil {
		return Credentials{}, newVercelOIDCContextError(err)
	}
	return Credentials{Token: token, TeamID: ownerID, ProjectID: projectID}, nil
}

// unverifiedTokenClaims is 3.9.0 api-client.js `decodeUnverifiedToken`: a
// three-part token whose base64url JSON payload has a truthy owner_id.
func unverifiedTokenClaims(token string) (ownerID, projectID string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	// Buffer.from(…, "base64url").toString("utf8"): both alphabets, other
	// characters skipped, decoding stops at "="; invalid UTF-8 is U+FFFD.
	raw, err := providerutils.EncodeSandboxText(parts[1], "base64url")
	if err != nil {
		return "", "", false
	}
	var payload map[string]any
	if json.Unmarshal(textdecode.ReplaceInvalid(raw), &payload) != nil {
		// Invalid JSON, or a payload that is not an object (null throws,
		// a primitive has no owner_id).
		return "", "", false
	}
	owner := payload["owner_id"]
	if !providerutils.JSTruthy(owner) {
		return "", "", false
	}
	// The team is sent as the `teamId` query (URLSearchParams String()).
	ownerID = providerutils.JSString(owner)
	if project, present := payload["project_id"]; present && project != nil {
		projectID = providerutils.JSString(project)
	}
	return ownerID, projectID, true
}

// oidcTokenTransport is 3.9.0 `APIClient.ensureValidToken`, run before each
// request: it refreshes a JWT token expiring within 5 minutes and sends the
// current token and team.
type oidcTokenTransport struct {
	base    http.RoundTripper
	refresh *OIDCRefresh

	mu        sync.Mutex
	token     string
	teamID    string
	projectID string
}

func (t *oidcTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	token, teamID, projectID := t.token, t.teamID, t.projectID
	t.mu.Unlock()
	// TS swallows every refresh error and keeps the current token.
	if fresh, err := t.refresh.tokenResolver().TokenFor(req.Context(), projectID, teamID, sandboxOIDCExpirationBuffer); err == nil && fresh != token {
		token = fresh
		if owner, _, ok := unverifiedTokenClaims(fresh); ok {
			teamID = owner
		}
		t.mu.Lock()
		t.token, t.teamID = token, teamID
		t.mu.Unlock()
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	// TS sends `teamId: this.teamId` first on every request (APIClient.path
	// put the client's team there); the refreshed team takes its place.
	clone.URL.RawQuery = httpinternal.SetSearchParam(clone.URL.RawQuery, "teamId", teamID)
	return t.base.RoundTrip(clone)
}
