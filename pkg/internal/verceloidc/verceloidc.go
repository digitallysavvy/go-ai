// Package verceloidc resolves the Vercel OIDC token like TS @vercel/oidc
// getVercelOidcToken, keeping a refreshed token in memory (owner decision b:
// the Vercel token cache, the CLI login and process environment are never
// written, removed or renamed). It is shared by gateway.VercelOIDCTokenFunc,
// which follows @vercel/oidc 3.2.0 (the version TS @ai-sdk/gateway pins), and
// the Vercel Sandbox OIDC refresh, which follows 4.0.0 (the version
// @vercel/sandbox 3.9.0 pins; Options.V4).
package verceloidc

import (
	"context"
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
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
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
	// V4 follows @vercel/oidc 4.0.0 instead of 3.2.0: the token file named
	// by VERCEL_OIDC_TOKEN_FILE is read on each lookup (after the request
	// context token, before VERCEL_OIDC_TOKEN and the refreshed token), the
	// error texts lose their "Please run `vc env pull`" / "Do you have the
	// OIDC option enabled" suffixes, and a CLI login with only a refresh
	// token is not reported as missing.
	V4 bool
}

// MissingMessage is the @vercel/oidc 3.2.0 getVercelOidcTokenSync error text.
const MissingMessage = "The 'x-vercel-oidc-token' header is missing from the request. Do you have the OIDC option enabled in the Vercel project settings?"

// MissingMessageV4 is the @vercel/oidc 4.0.0 getVercelOidcTokenSync error
// text.
const MissingMessageV4 = "The 'x-vercel-oidc-token' header is missing from the request."

// TokenFileEnv names the file @vercel/oidc 4.0.0 reads the token from.
const TokenFileEnv = "VERCEL_OIDC_TOKEN_FILE"

// Resolver is TS getVercelOidcToken with an in-memory refreshed token. It is
// safe for concurrent use; concurrent refreshes are serialized.
type Resolver struct {
	opts Options
	mu   sync.Mutex
	// refreshed is the last refreshed token; it stands in for TS's
	// process.env.VERCEL_OIDC_TOKEN assignment.
	refreshed string
	// saved holds the tokens refreshed per project; it stands in for TS
	// saveToken (com.vercel.token/<project>.json), read before that file.
	saved map[string]string
}

// New returns a Resolver.
func New(opts Options) *Resolver { return &Resolver{opts: opts} }

func (r *Resolver) missingMessage() string {
	if r.opts.V4 {
		return MissingMessageV4
	}
	return MissingMessage
}

// withVCEnvPull is a 3.2.0 error text; 4.0.0 drops the suffix.
func (r *Resolver) withVCEnvPull(message string) string {
	if r.opts.V4 {
		return message + "."
	}
	return message + ". Please run `vc env pull` and try again"
}

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

	// 4.0.0: `getContext().headers?.["x-vercel-oidc-token"] ? null : await
	// tryOidcTokenFromFile()`, outside the try that joins messages.
	token := ""
	if r.opts.V4 && contextToken(ctx) == "" {
		fileToken, err := r.TokenFromFile()
		if err != nil {
			return "", err
		}
		token = fileToken
	}
	if token == "" {
		token = r.current(ctx)
	}
	syncMessage := ""
	if token == "" {
		syncMessage = r.missingMessage()
	}
	err := func() error {
		if token != "" {
			payload, err := r.TokenPayload(token)
			if err != nil {
				return err
			}
			expired, err := TokenExpired(payload, buffer)
			if err != nil {
				return err
			}
			if !expired {
				return nil
			}
		}
		if err := r.refresh(ctx, projectID, teamID, buffer); err != nil {
			return err
		}
		token = r.current(ctx)
		if token == "" {
			return r.newError(r.missingMessage(), nil)
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

// TokenFromFile is 4.0.0 tryOidcTokenFromFile: "" when
// VERCEL_OIDC_TOKEN_FILE is unset or empty, else the file's trimmed text; a
// read error is returned as is and an empty file is a TokenError naming the
// path.
func (r *Resolver) TokenFromFile() (string, error) {
	path := os.Getenv(TokenFileEnv)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	// readFile(…, "utf8"): invalid UTF-8 becomes U+FFFD; then String.trim.
	token := providerutils.JSTrim(string(textdecode.ReplaceInvalid(data)))
	if token == "" {
		return "", r.newError("The Vercel OIDC token file is empty: "+path, nil)
	}
	return token, nil
}

func contextToken(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

func (r *Resolver) current(ctx context.Context) string {
	if v := contextToken(ctx); v != "" {
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
	token, ok := r.saved[projectID]
	if !ok {
		var err error
		if token, err = r.loadCachedToken(projectID); err != nil {
			return err
		}
	}
	expired := token == ""
	if !expired {
		payload, err := r.TokenPayload(token)
		if err != nil {
			return err
		}
		if expired, err = TokenExpired(payload, buffer); err != nil {
			return err
		}
	}
	if expired {
		accessToken := r.opts.AccessToken
		if accessToken == nil {
			accessToken = func(context.Context) (string, error) { return cliAccessToken(buffer, r.opts.V4) }
		}
		authToken, err := accessToken(ctx)
		if err != nil {
			return err
		}
		token, err = r.requestOIDCToken(ctx, authToken, projectID, teamID)
		if err != nil {
			return err
		}
		if r.saved == nil {
			r.saved = map[string]string{}
		}
		r.saved[projectID] = token
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
	return r.assertTokenResponse(value)
}

// assertTokenResponse is TS assertVercelOidcTokenResponse.
func (r *Resolver) assertTokenResponse(value interface{}) (string, error) {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return "", errors.New(r.withVCEnvPull("Vercel OIDC token is malformed. Expected an object"))
	}
	token, ok := obj["token"].(string)
	if !ok {
		return "", errors.New(r.withVCEnvPull("Vercel OIDC token is malformed. Expected a string-valued token property"))
	}
	return token, nil
}

// TokenPayload is TS getTokenPayload: the JWT payload of a three-part token,
// its second part decoded like Buffer.from(…, "base64") after the -/_ swap
// (characters outside the alphabet skipped, invalid UTF-8 as U+FFFD) and
// parsed as JSON (any JSON value).
func (r *Resolver) TokenPayload(token string) (interface{}, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, r.newError(r.withVCEnvPull("Invalid token"), nil)
	}
	b64 := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	b64 += strings.Repeat("=", (4-len(b64)%4)%4)
	return DecodeBase64JSON(b64)
}

// DecodeBase64JSON is JSON.parse(Buffer.from(text, "base64").toString("utf8")).
func DecodeBase64JSON(text string) (interface{}, error) {
	decoded, err := providerutils.EncodeSandboxText(text, "base64")
	if err != nil {
		return nil, err
	}
	var payload interface{}
	if err := json.Unmarshal(textdecode.ReplaceInvalid(decoded), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// TokenExpired is TS isExpired: `token.exp * 1000 < Date.now() + bufferMs`,
// exp converted with JS ToNumber (a missing exp, an object or a non-numeric
// string is NaN: not expired); a null payload throws like `null.exp`.
func TokenExpired(payload interface{}, buffer time.Duration) (bool, error) {
	if payload == nil {
		return false, errors.New("Cannot read properties of null (reading 'exp')") //nolint:staticcheck // JS TypeError text
	}
	obj, ok := payload.(map[string]interface{})
	if !ok {
		// A primitive or an array has no exp property: undefined -> NaN.
		return false, nil
	}
	raw, present := obj["exp"]
	if !present {
		return false, nil
	}
	exp := jsToNumber(raw)
	if math.IsNaN(exp) {
		return false, nil
	}
	return exp*1000 < float64(time.Now().UnixMilli())+float64(buffer.Milliseconds()), nil
}

// jsToNumber is JS ToNumber for a value decoded from JSON.
func jsToNumber(v interface{}) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		if f, ok := providerutils.JSNumber(x); ok {
			return f
		}
		return math.NaN()
	case []interface{}:
		// ToPrimitive of an array is its String() join.
		if f, ok := providerutils.JSNumber(providerutils.JSString(x)); ok {
			return f
		}
		return math.NaN()
	}
	return math.NaN()
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
	return r.assertTokenResponse(value)
}

// cliAccessToken is TS getVercelToken without the OAuth refresh: the
// Vercel CLI login token (auth.json, read only) when it is still valid. 3.2.0
// reports a login without a token as missing; 4.0.0 only one with neither a
// token nor a refresh token (the refresh is then not ported).
func cliAccessToken(buffer time.Duration, v4 bool) (string, error) {
	dir := userDataDir()
	if dir == "" {
		return "", fmt.Errorf("Unable to find Vercel CLI data directory. Your platform: %s. Supported: darwin, linux, win32.", runtime.GOOS) //nolint:staticcheck // TS error text
	}
	var config struct {
		Token        string      `json:"token"`
		RefreshToken string      `json:"refreshToken"`
		ExpiresAt    interface{} `json:"expiresAt"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "com.vercel.cli", "auth.json"))
	if err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &config)
	}
	if config.Token == "" && (!v4 || config.RefreshToken == "") {
		return "", errors.New("No authentication found. Please log in with the Vercel CLI (vercel login).") //nolint:staticcheck // TS AccessTokenMissingError text
	}
	expiresAt, ok := config.ExpiresAt.(float64)
	if config.Token != "" && (!ok || expiresAt >= float64(time.Now().Unix())+buffer.Seconds()) {
		return config.Token, nil
	}
	reason := "the Vercel CLI login has expired; run `vercel login` (the Go SDK does not refresh the CLI login)"
	if config.Token == "" {
		reason = "the Vercel CLI login has only a refresh token; run `vercel login` (the Go SDK does not refresh the CLI login)"
	}
	return "", errors.New("Failed to refresh authentication token.: " + reason) //nolint:staticcheck // TS RefreshAccessTokenFailedError text plus the Go reason
}
