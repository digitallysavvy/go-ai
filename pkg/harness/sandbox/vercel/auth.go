package vercel

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// vercelProviderID mirrors TS VERCEL_PROVIDER_ID ('vercel-sandbox'), the
// harness sandbox provider identifier used in error payloads.
const vercelProviderID = "vercel-sandbox"

// Credentials authenticate against the Vercel Sandbox API.
type Credentials struct {
	Token     string
	TeamID    string
	ProjectID string
}

// ErrNoCredentials is returned by ResolveCredentials when neither explicit
// credentials nor VERCEL_OIDC_TOKEN are available.
var ErrNoCredentials = errors.New("vercel sandbox: no credentials configured (set VERCEL_OIDC_TOKEN, or pass Token, TeamID, and ProjectID)")

// ResolveCredentials mirrors TS `getCredentials`: explicit credentials win
// when all three fields are set; otherwise the VERCEL_OIDC_TOKEN env var is
// decoded as a JWT for its owner_id/project_id claims.
//
// Capability gap: the TS SDK additionally refreshes a near-expiring OIDC
// token via `@vercel/oidc`'s `getVercelOidcToken` (walking linked-project
// files, local dev credential prompts, etc.). This port only reads
// VERCEL_OIDC_TOKEN once per call; it does not implement local-dev credential
// generation or proactive token refresh. In deployed Vercel environments the
// platform keeps VERCEL_OIDC_TOKEN current, so re-reading the env var on each
// call covers the common case.
func ResolveCredentials(explicit Credentials) (Credentials, error) {
	if explicit.Token != "" && explicit.TeamID != "" && explicit.ProjectID != "" {
		return explicit, nil
	}
	if explicit.Token != "" || explicit.TeamID != "" || explicit.ProjectID != "" {
		return Credentials{}, errors.New("vercel sandbox: Token, TeamID, and ProjectID must all be set together, or all omitted")
	}
	token := os.Getenv("VERCEL_OIDC_TOKEN")
	if token == "" {
		return Credentials{}, ErrNoCredentials
	}
	ownerID, projectID, err := decodeOIDCClaims(token)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{Token: token, TeamID: ownerID, ProjectID: projectID}, nil
}

// HasConfiguredCredentials mirrors TS `hasConfiguredCredentials`.
func HasConfiguredCredentials(explicit Credentials) bool {
	if os.Getenv("VERCEL_OIDC_TOKEN") != "" {
		return true
	}
	return explicit.Token != "" && explicit.TeamID != "" && explicit.ProjectID != ""
}

func decodeOIDCClaims(token string) (ownerID, projectID string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", errors.New("vercel sandbox: invalid VERCEL_OIDC_TOKEN (not a JWT)")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("vercel sandbox: invalid VERCEL_OIDC_TOKEN: " + err.Error())
	}
	var claims struct {
		OwnerID   string `json:"owner_id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", "", errors.New("vercel sandbox: invalid VERCEL_OIDC_TOKEN: " + err.Error())
	}
	if claims.OwnerID == "" || claims.ProjectID == "" {
		return "", "", errors.New("vercel sandbox: VERCEL_OIDC_TOKEN is missing owner_id/project_id claims")
	}
	return claims.OwnerID, claims.ProjectID, nil
}

// isAuthErrorStatus reports whether an HTTP status code indicates an
// authentication/authorization failure (mirrors the 401/403 branch of TS
// `isVercelSandboxAuthenticationFailure`).
func isAuthErrorStatus(status int) bool {
	return status == 401 || status == 403
}
