package vercel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/internal/verceloidc"
	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
// credentials nor a Vercel OIDC token (VERCEL_OIDC_TOKEN_FILE or
// VERCEL_OIDC_TOKEN) are available.
var ErrNoCredentials = errors.New("vercel sandbox: no credentials configured (set VERCEL_OIDC_TOKEN, or pass Token, TeamID, and ProjectID)")

// ErrPartialCredentials is returned by ResolveCredentials when only some of
// Token, TeamID and ProjectID are set. Mirrors @vercel/sandbox's "Missing
// credentials parameters" error, which TS reports as an authentication
// failure.
var ErrPartialCredentials = errors.New("Missing credentials parameters to access the Vercel API") //nolint:staticcheck // matches @vercel/sandbox error text

// ResolveCredentials mirrors TS `getCredentials`: explicit credentials win
// when all three fields are set; otherwise the Vercel OIDC token is looked
// up like @vercel/oidc 4.0.0 `getVercelOidcToken` (the file named by
// VERCEL_OIDC_TOKEN_FILE, read on each call, else VERCEL_OIDC_TOKEN), its
// payload checked like `getTokenPayload`, and its owner_id/project_id claims
// read like 3.9.0 `getCredentialsFromOIDCToken`. A token that cannot be read
// or decoded is a LocalOidcContextError (VercelOidcContextError when
// VERCEL_URL is set) whose cause carries the TS reason, as in TS.
//
// The token is used as read. The TS refresh through `@vercel/oidc`
// `getVercelOidcToken` (linked project, cached project token, Vercel CLI
// login, and a 5 minute refresh before each request) is opt-in: set
// OIDCRefresh on CreateSessionOptions, ResumeSessionOptions or
// LegacySettings. The interactive local dev credential prompt is not
// ported.
func ResolveCredentials(explicit Credentials) (Credentials, error) {
	if explicit.Token != "" && explicit.TeamID != "" && explicit.ProjectID != "" {
		return explicit, nil
	}
	if explicit.Token != "" || explicit.TeamID != "" || explicit.ProjectID != "" {
		var missing []string
		if explicit.Token == "" {
			missing = append(missing, "token")
		}
		if explicit.TeamID == "" {
			missing = append(missing, "teamId")
		}
		if explicit.ProjectID == "" {
			missing = append(missing, "projectId")
		}
		// Text mirrors @vercel/sandbox, which TS classifies as an
		// authentication failure by message.
		return Credentials{}, fmt.Errorf("%w: %s", ErrPartialCredentials, strings.Join(missing, ", "))
	}
	resolver := verceloidc.New(verceloidc.Options{V4: true})
	token, err := resolver.TokenFromFile()
	if err != nil {
		return Credentials{}, newVercelOIDCContextError(err)
	}
	if token == "" {
		token = os.Getenv("VERCEL_OIDC_TOKEN")
	}
	if token == "" {
		return Credentials{}, ErrNoCredentials
	}
	// getVercelOidcToken reads the payload for its expiry check before
	// getCredentialsFromOIDCToken; a failure there is joined into a
	// VercelOidcTokenError ("\n" + reason). The expiry itself only drives
	// the opt-in refresh.
	payload, err := resolver.TokenPayload(token)
	if err == nil {
		_, err = verceloidc.TokenExpired(payload, 0)
	}
	if err != nil {
		return Credentials{}, newVercelOIDCContextError(&verceloidc.TokenError{Message: "\n" + err.Error(), Cause: err})
	}
	ownerID, projectID, err := decodeOIDCClaims(token)
	if err != nil {
		return Credentials{}, newVercelOIDCContextError(err)
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

// oidcCredentialsSchema is the 3.9.0 get-credentials.js token payload schema.
var oidcCredentialsSchema = zodlite.Object(
	zodlite.F("exp", zodlite.Optional(zodlite.Number)),
	zodlite.F("iat", zodlite.Optional(zodlite.Number)),
	zodlite.F("owner_id", zodlite.String),
	zodlite.F("project_id", zodlite.String),
)

// decodeOIDCClaims is 3.9.0 `getCredentialsFromOIDCToken`: the second
// dot-separated part (any token that has one), decoded like
// Buffer.from(…, "base64") after the -/_ swap (other characters skipped,
// invalid UTF-8 as U+FFFD), parsed as JSON and checked with the zod schema
// (string owner_id and project_id, possibly empty; optional numeric exp and
// iat). Failures read "Invalid Vercel OIDC token: <reason>", the reason being
// the zod issue list (JSON.stringify(issues, null, 2)) or the parse error.
func decodeOIDCClaims(token string) (ownerID, projectID string, err error) {
	invalid := func(reason string) error {
		return errors.New("Invalid Vercel OIDC token: " + reason) //nolint:staticcheck // TS error text
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		// `token.split(".")[1]` is undefined.
		return "", "", invalid("Cannot read properties of undefined (reading 'replace')")
	}
	b64 := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	decoded, err := providerutils.EncodeSandboxText(b64, "base64")
	if err != nil {
		return "", "", invalid(err.Error())
	}
	text := textdecode.ReplaceInvalid(decoded)
	var payload interface{}
	if err := json.Unmarshal(text, &payload); err != nil {
		// Go's parse error text, not V8's SyntaxError text.
		return "", "", invalid(err.Error())
	}
	if message := zodlite.ValidationMessage(oidcCredentialsSchema, string(text)); message != "" {
		// ZodError.message: the issue list after the frame value.
		_, issues, _ := strings.Cut(message, ".\nError message: ")
		return "", "", invalid(issues)
	}
	claims := payload.(map[string]interface{})
	return claims["owner_id"].(string), claims["project_id"].(string), nil
}

// isAuthErrorStatus reports whether an HTTP status code indicates an
// authentication/authorization failure (mirrors the 401/403 branch of TS
// `isVercelSandboxAuthenticationFailure`).
func isAuthErrorStatus(status int) bool {
	return status == 401 || status == 403
}
