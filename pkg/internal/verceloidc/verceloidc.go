// Package verceloidc resolves the Vercel OIDC token like TS @vercel/oidc
// 3.2.0 getVercelOidcToken, keeping a refreshed token in memory (owner
// decision b: the Vercel token cache, the CLI login and process
// environment are never written, removed or renamed). It is shared by
// gateway.VercelOIDCTokenFunc and the Vercel Sandbox OIDC refresh.
package verceloidc

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

// TokenError is the default error for a missing, malformed or unrefreshable
// token (TS VercelOidcTokenError). Callers that export their own error type
// set Options.NewError.
type TokenError struct {
	Message string
	Cause   error
}

func (e *TokenError) Error() string { return e.Message }

// Unwrap returns the cause.
func (e *TokenError) Unwrap() error { return e.Cause }

type contextKey struct{}

// ContextWithToken returns a context whose lookups use token as the Vercel
// OIDC token (TS: the request context's x-vercel-oidc-token header), read
// before the refreshed token and VERCEL_OIDC_TOKEN.
func ContextWithToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, contextKey{}, token)
}

// Options configures a Resolver.
type Options struct {
	// ProjectID and TeamID select the project whose OIDC token is refreshed
	// (TS options.project / options.team); when either is empty it is read
	// from .vercel/project.json (projectId / orgId).
	ProjectID string
	TeamID    string
	// ExpirationBuffer refreshes a token expiring within it (TS
	// expirationBufferMs).
	ExpirationBuffer time.Duration
	// AccessToken returns the Vercel access token for the refresh request;
	// default: the Vercel CLI login token (read only).
	AccessToken func(ctx context.Context) (string, error)
	// HTTPClient sends the refresh request (default http.DefaultClient).
	HTTPClient *http.Client
	// APIBaseURL is the Vercel API origin (default https://api.vercel.com).
	APIBaseURL string
	// NewError builds the TS VercelOidcTokenError; default *TokenError.
	NewError func(message string, cause error) error
}

// MissingMessage is the TS getVercelOidcTokenSync error text.
const MissingMessage = "The 'x-vercel-oidc-token' header is missing from the request. Do you have the OIDC option enabled in the Vercel project settings?"

// Resolver is TS getVercelOidcToken with an in-memory refreshed token. It is
// safe for concurrent use; concurrent refreshes are serialized.
type Resolver struct {
	opts Options
	mu   sync.Mutex
	// refreshed is the last refreshed token; it stands in for TS's
	// process.env.VERCEL_OIDC_TOKEN assignment.
	refreshed string
}

// New returns a Resolver.
func New(opts Options) *Resolver { return &Resolver{opts: opts} }

func (r *Resolver) newError(message string, cause error) error {
	if r.opts.NewError != nil {
		return r.opts.NewError(message, cause)
	}
	return &TokenError{Message: message, Cause: cause}
}

// Token resolves the token with the Resolver's options.
func (r *Resolver) Token(ctx context.Context) (string, error) {
	return r.TokenFor(ctx, r.opts.ProjectID, r.opts.TeamID, r.opts.ExpirationBuffer)
}

// TokenFor is TS getVercelOidcToken({ project, team, expirationBufferMs }):
// the context token, the refreshed token or VERCEL_OIDC_TOKEN, refreshed
// when missing or expiring within buffer.
func (r *Resolver) TokenFor(ctx context.Context, projectID, teamID string, buffer time.Duration) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	token := r.current(ctx)
	syncMessage := ""
	if token == "" {
		syncMessage = MissingMessage
	}
	err := func() error {
		if token != "" {
			payload, err := r.tokenPayload(token)
			if err != nil {
				return err
			}
			if !tokenExpired(payload, buffer) {
				return nil
			}
		}
		if err := r.refresh(ctx, projectID, teamID, buffer); err != nil {
			return err
		}
		token = r.current(ctx)
		if token == "" {
			return r.newError(MissingMessage, nil)
		}
		return nil
	}()
	if err != nil {
		// TS joins the first lookup's message and the refresh error's.
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return "", err
		}
		return "", r.newError(syncMessage+"\n"+err.Error(), err)
	}
	return token, nil
}

func (r *Resolver) current(ctx context.Context) string {
	if v, ok := ctx.Value(contextKey{}).(string); ok && v != "" {
		return v
	}
	if r.refreshed != "" {
		return r.refreshed
	}
	return os.Getenv("VERCEL_OIDC_TOKEN")
}

// refresh is TS refreshToken without the file and process.env writes.
func (r *Resolver) refresh(ctx context.Context, projectID, teamID string, buffer time.Duration) error {
	if projectID == "" || teamID == "" {
		info, err := r.findProjectInfo()
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
		return r.newError("Failed to refresh OIDC token: No project specified. Try re-linking your project with `vc link`", nil)
	}
	token, err := r.loadCachedToken(projectID)
	if err != nil {
		return err
	}
	expired := token == ""
	if !expired {
		payload, err := r.tokenPayload(token)
		if err != nil {
			return err
		}
		expired = tokenExpired(payload, buffer)
	}
	if expired {
		accessToken := r.opts.AccessToken
		if accessToken == nil {
			accessToken = func(context.Context) (string, error) { return cliAccessToken(buffer) }
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
func (r *Resolver) requestOIDCToken(ctx context.Context, authToken, projectID, teamID string) (string, error) {
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
		return "", r.newError("Failed to refresh OIDC token: "+statusText, nil)
	}
	body, err := fileutil.ReadResponseWithSizeLimit(resp, url, 1<<20)
	if err != nil {
		return "", err
	}
	var value interface{}
	if err := json.Unmarshal(body, &value); err != nil {
		return "", err
	}
	return assertTokenResponse(value)
}

// assertTokenResponse is TS assertVercelOidcTokenResponse.
func assertTokenResponse(value interface{}) (string, error) {
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

// tokenPayload is TS getTokenPayload: the JWT payload, base64url
// decoded and parsed as JSON.
func (r *Resolver) tokenPayload(token string) (map[string]interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, r.newError("Invalid token. Please run `vc env pull` and try again", nil)
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

// tokenExpired is TS isExpired: `token.exp * 1000 < Date.now() +
// bufferMs`; a missing or non-numeric exp compares as NaN (not expired).
func tokenExpired(payload map[string]interface{}, buffer time.Duration) bool {
	exp, ok := payload["exp"].(float64)
	if !ok || math.IsNaN(exp) {
		return false
	}
	return exp*1000 < float64(time.Now().UnixMilli())+float64(buffer.Milliseconds())
}

type projectInfo struct{ projectID, teamID string }

// findProjectInfo is TS findProjectInfo: .vercel/project.json in the
// working directory or its nearest parent that has a .vercel directory.
func (r *Resolver) findProjectInfo() (projectInfo, error) {
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
		return projectInfo{}, r.newError("Unable to find project root directory. Have you linked your project with `vc link?`", nil)
	}
	data, err := os.ReadFile(filepath.Join(root, ".vercel", "project.json"))
	if err != nil {
		return projectInfo{}, r.newError("project.json not found, have you linked your project with `vc link?`", nil)
	}
	var prj map[string]interface{}
	if err := json.Unmarshal(data, &prj); err != nil {
		return projectInfo{}, err
	}
	projectID, okP := prj["projectId"].(string)
	teamID, okT := prj["orgId"].(string)
	if !okP && !okT {
		return projectInfo{}, errors.New("Expected a string-valued projectId property. Try running `vc link` to re-link your project.") //nolint:staticcheck // TS TypeError text
	}
	return projectInfo{projectID: projectID, teamID: teamID}, nil
}

// userDataDir is TS getUserDataDir.
func userDataDir() string {
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

// loadCachedToken is TS loadToken (read only): the cached project
// token in the Vercel token directory, "" when there is none.
func (r *Resolver) loadCachedToken(projectID string) (string, error) {
	dir := userDataDir()
	if dir == "" {
		return "", r.newError("Unable to find user data directory. Please reach out to Vercel support.", nil)
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
	return assertTokenResponse(value)
}

// cliAccessToken is TS getVercelToken without the OAuth refresh: the
// Vercel CLI login token (auth.json, read only) when it is still valid.
func cliAccessToken(buffer time.Duration) (string, error) {
	dir := userDataDir()
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
