package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
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

// vercelOIDCTokenContextKey carries a per-request OIDC token (the Vercel
// request context's x-vercel-oidc-token header in TS).
type vercelOIDCTokenContextKey struct{}

// ContextWithVercelOIDCToken returns a context whose requests use token as
// the Vercel OIDC token, the counterpart of the `x-vercel-oidc-token` header
// TS reads from the Vercel request context. It is read by the function that
// VercelOIDCTokenFunc returns, before VERCEL_OIDC_TOKEN.
func ContextWithVercelOIDCToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, vercelOIDCTokenContextKey{}, token)
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
	r := &vercelOIDCResolver{opts: opts}
	return r.token
}

type vercelOIDCResolver struct {
	opts VercelOIDCTokenOptions
	mu   sync.Mutex
	// refreshed is the last refreshed token; it stands in for TS's
	// process.env.VERCEL_OIDC_TOKEN assignment.
	refreshed string
}

const vercelOIDCMissingMessage = "The 'x-vercel-oidc-token' header is missing from the request. Do you have the OIDC option enabled in the Vercel project settings?"

func (r *vercelOIDCResolver) current(ctx context.Context) string {
	if v, ok := ctx.Value(vercelOIDCTokenContextKey{}).(string); ok && v != "" {
		return v
	}
	if r.refreshed != "" {
		return r.refreshed
	}
	return os.Getenv("VERCEL_OIDC_TOKEN")
}

func (r *vercelOIDCResolver) token(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	token := r.current(ctx)
	syncMessage := ""
	if token == "" {
		syncMessage = vercelOIDCMissingMessage
	}
	err := func() error {
		if token != "" {
			payload, err := vercelTokenPayload(token)
			if err != nil {
				return err
			}
			if !vercelTokenExpired(payload, r.opts.ExpirationBuffer) {
				return nil
			}
		}
		if err := r.refresh(ctx); err != nil {
			return err
		}
		token = r.current(ctx)
		if token == "" {
			return &VercelOIDCTokenError{Message: vercelOIDCMissingMessage}
		}
		return nil
	}()
	if err != nil {
		// TS joins the first lookup's message and the refresh error's.
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return "", err
		}
		return "", &VercelOIDCTokenError{Message: syncMessage + "\n" + err.Error(), Cause: err}
	}
	return token, nil
}

// refresh is TS refreshToken without the file and process.env writes.
func (r *vercelOIDCResolver) refresh(ctx context.Context) error {
	projectID, teamID := r.opts.ProjectID, r.opts.TeamID
	if projectID == "" || teamID == "" {
		info, err := vercelFindProjectInfo()
		if err != nil {
			return err
		}
		if projectID == "" {
			projectID = info.projectID
		}
		if teamID == "" {
			teamID = info.teamID
		}
	}
	if projectID == "" {
		return &VercelOIDCTokenError{Message: "Failed to refresh OIDC token: No project specified. Try re-linking your project with `vc link`"}
	}
	token, err := vercelLoadCachedToken(projectID)
	if err != nil {
		return err
	}
	expired := token == ""
	if !expired {
		payload, err := vercelTokenPayload(token)
		if err != nil {
			return err
		}
		expired = vercelTokenExpired(payload, r.opts.ExpirationBuffer)
	}
	if expired {
		accessToken := r.opts.AccessToken
		if accessToken == nil {
			accessToken = func(context.Context) (string, error) { return vercelCLIAccessToken(r.opts.ExpirationBuffer) }
		}
		authToken, err := accessToken(ctx)
		if err != nil {
			return err
		}
		token, err = r.requestOIDCToken(ctx, authToken, projectID, teamID)
		if err != nil {
			return err
		}
	}
	r.refreshed = token
	return nil
}

// requestOIDCToken is TS getVercelOidcToken(authToken, projectId, teamId) in
// token-util.js.
func (r *vercelOIDCResolver) requestOIDCToken(ctx context.Context, authToken, projectID, teamID string) (string, error) {
	base := r.opts.APIBaseURL
	if base == "" {
		base = "https://api.vercel.com"
	}
	url := base + "/v1/projects/" + projectID + "/token?source=vercel-oidc-refresh"
	if teamID != "" {
		url += "&teamId=" + teamID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+authToken)
	client := r.opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		statusText := strings.TrimPrefix(resp.Status, fmt.Sprintf("%d ", resp.StatusCode))
		return "", &VercelOIDCTokenError{Message: "Failed to refresh OIDC token: " + statusText}
	}
	body, err := fileutil.ReadResponseWithSizeLimit(resp, url, 1<<20)
	if err != nil {
		return "", err
	}
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}
	return vercelAssertTokenResponse(value)
}

// vercelAssertTokenResponse is TS assertVercelOidcTokenResponse.
func vercelAssertTokenResponse(value interface{}) (string, error) {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return "", errors.New("Vercel OIDC token is malformed. Expected an object. Please run `vc env pull` and try again") //nolint:staticcheck // TS TypeError text
	}
	token, ok := obj["token"].(string)
	if !ok {
		return "", errors.New("Vercel OIDC token is malformed. Expected a string-valued token property. Please run `vc env pull` and try again") //nolint:staticcheck // TS TypeError text
	}
	return token, nil
}

// vercelTokenPayload is TS getTokenPayload: the JWT payload, base64url
// decoded and parsed as JSON.
func vercelTokenPayload(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, &VercelOIDCTokenError{Message: "Invalid token. Please run `vc env pull` and try again"}
	}
	b64 := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	b64 += strings.Repeat("=", (4-len(b64)%4)%4)
	// Buffer.from(…, 'base64') is lenient: decode what is valid.
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		decoded, _ = base64.RawStdEncoding.DecodeString(strings.TrimRight(b64, "="))
	}
	var payload interface{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, err
	}
	m, _ := payload.(map[string]interface{})
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}

// vercelTokenExpired is TS isExpired: `token.exp * 1000 < Date.now() +
// bufferMs`; a missing or non-numeric exp compares as NaN (not expired).
func vercelTokenExpired(payload map[string]interface{}, buffer time.Duration) bool {
	exp, ok := payload["exp"].(float64)
	if !ok || math.IsNaN(exp) {
		return false
	}
	return exp*1000 < float64(time.Now().UnixMilli())+float64(buffer.Milliseconds())
}

type vercelProjectInfo struct{ projectID, teamID string }

// vercelFindProjectInfo is TS findProjectInfo: .vercel/project.json in the
// working directory or its nearest parent that has a .vercel directory.
func vercelFindProjectInfo() (vercelProjectInfo, error) {
	dir, err := os.Getwd()
	root := ""
	if err == nil {
		for dir != filepath.Dir(dir) {
			if _, statErr := os.Stat(filepath.Join(dir, ".vercel")); statErr == nil {
				root = dir
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	if root == "" {
		return vercelProjectInfo{}, &VercelOIDCTokenError{Message: "Unable to find project root directory. Have you linked your project with `vc link?`"}
	}
	data, err := os.ReadFile(filepath.Join(root, ".vercel", "project.json"))
	if err != nil {
		return vercelProjectInfo{}, &VercelOIDCTokenError{Message: "project.json not found, have you linked your project with `vc link?`"}
	}
	var prj map[string]interface{}
	if err := json.Unmarshal(data, &prj); err != nil {
		return vercelProjectInfo{}, err
	}
	projectID, okP := prj["projectId"].(string)
	teamID, okT := prj["orgId"].(string)
	if !okP && !okT {
		return vercelProjectInfo{}, errors.New("Expected a string-valued projectId property. Try running `vc link` to re-link your project.") //nolint:staticcheck // TS TypeError text
	}
	return vercelProjectInfo{projectID: projectID, teamID: teamID}, nil
}

// vercelUserDataDir is TS getUserDataDir.
func vercelUserDataDir() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library/Application Support")
	case "linux":
		return filepath.Join(home, ".local/share")
	case "windows":
		return os.Getenv("LOCALAPPDATA")
	}
	return ""
}

// vercelLoadCachedToken is TS loadToken (read only): the cached project
// token in the Vercel token directory, "" when there is none.
func vercelLoadCachedToken(projectID string) (string, error) {
	dir := vercelUserDataDir()
	if dir == "" {
		return "", &VercelOIDCTokenError{Message: "Unable to find user data directory. Please reach out to Vercel support."}
	}
	data, err := os.ReadFile(filepath.Join(dir, "com.vercel.token", projectID+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	return vercelAssertTokenResponse(value)
}

// vercelCLIAccessToken is TS getVercelToken without the OAuth refresh: the
// Vercel CLI login token (auth.json, read only) when it is still valid.
func vercelCLIAccessToken(buffer time.Duration) (string, error) {
	dir := vercelUserDataDir()
	if dir == "" {
		return "", fmt.Errorf("Unable to find Vercel CLI data directory. Your platform: %s. Supported: darwin, linux, win32.", runtime.GOOS) //nolint:staticcheck // TS error text
	}
	var config struct {
		Token     string      `json:"token"`
		ExpiresAt interface{} `json:"expiresAt"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "com.vercel.cli", "auth.json"))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &config)
	}
	if config.Token == "" {
		return "", errors.New("No authentication found. Please log in with the Vercel CLI (vercel login).") //nolint:staticcheck // TS AccessTokenMissingError text
	}
	expiresAt, ok := config.ExpiresAt.(float64)
	if !ok || expiresAt >= float64(time.Now().Unix())+buffer.Seconds() {
		return config.Token, nil
	}
	return "", errors.New("Failed to refresh authentication token.: the Vercel CLI login has expired; run `vercel login` (the Go SDK does not refresh the CLI login)") //nolint:staticcheck // TS RefreshAccessTokenFailedError text plus the Go reason
}
