package libfx

// Port of libfx subscription.js, chatgpt.js and grok.js (Apache-2.0, see
// NOTICE): subscription credentials for codex (ChatGPT plan) and grok (Grok
// plan). The core sends these providers' requests without credentials; the
// authorizer adds them at the fetch edge from the host's Credentials
// callback, refreshing near expiry and once after a 401. Credentials never
// enter the core or a saved record, and a failed response that quotes them
// is masked before the core sees it.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness/internal/jsstr"
	"github.com/digitallysavvy/go-ai/pkg/harness/internal/securejson"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
)

const (
	refreshSkew              = 5 * time.Minute
	maxRememberedCredentials = 256
	maxTokenBytes            = 64 << 10
	maxErrorBodyBytes        = 64 << 10
	// maxTokenResponseBytes bounds an OAuth token or userinfo body. libfx
	// reads them unbounded; this is a Go hardening.
	maxTokenResponseBytes = 1 << 20
)

// Endpoints and OAuth clients of the subscription providers (chatgpt.js,
// grok.js).
const (
	ChatGPTIssuer   = "https://auth.openai.com"
	ChatGPTClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	GrokIssuer      = "https://auth.x.ai"
	GrokClientID    = "b1a00492-073a-47ea-816f-4c329264a828"

	codexOrigin            = "https://chatgpt.com"
	grokProxyOrigin        = "https://cli-chat-proxy.grok.com"
	grokLanguageModelsURL  = "https://api.x.ai/v1/language-models"
	chatgptAuthClaim       = "https://api.openai.com/auth"
	grokResponsesPath      = "/v1/responses"
	grokModelsPath         = "/v1/models"
	codexResponsesEndpoint = codexOrigin + "/backend-api/codex/responses"
	codexModelsEndpoint    = codexOrigin + "/backend-api/codex/models"
)

// Credentials are one subscription account's OAuth credentials (libfx
// FxProviderCredentials). Empty strings and a zero ExpiresAt mean absent.
type Credentials struct {
	AccessToken  string
	RefreshToken string
	AccountID    string
	// ExpiresAt is milliseconds since the epoch.
	ExpiresAt int64
}

// String never prints the tokens.
func (c Credentials) String() string {
	return fmt.Sprintf("libfx.Credentials{AccountID:%q, ExpiresAt:%d, tokens redacted}", c.AccountID, c.ExpiresAt)
}

// GoString never prints the tokens.
func (c Credentials) GoString() string { return c.String() }

// CredentialRequest names the account a credentials callback is asked for.
// SessionID is "" when the request carried none.
type CredentialRequest struct {
	Provider  string
	SessionID string
}

// SubscriptionAuth supplies a codex or grok agent's credentials.
type SubscriptionAuth struct {
	// Credentials returns the current credentials. Required. It is called
	// for every authorized request, so a host that rotates credentials
	// elsewhere is picked up.
	Credentials func(ctx context.Context, req CredentialRequest) (Credentials, error)
	// OnCredentials receives refreshed credentials once for each session
	// that uses them, so the host can save them.
	OnCredentials func(ctx context.Context, next Credentials, req CredentialRequest) error
	// RefreshLock runs refresh while the host holds a lock on the account,
	// for a host with several processes. It must call refresh and return
	// its error.
	RefreshLock func(ctx context.Context, req CredentialRequest, refresh func(ctx context.Context) error) error
	// Memory is shared refresh state. Agents that share one refresh an
	// account once; nil gives the agent its own.
	Memory *CredentialMemory
	// Now is the clock (tests).
	Now func() time.Time
}

// SubscriptionError is a failed refresh or account check (libfx error
// codes such as LIBFX_CHATGPT_REFRESH_FAILED).
type SubscriptionError struct {
	Code    string
	Status  int
	Message string
}

func (e *SubscriptionError) Error() string { return e.Message }

// CredentialMemory is the refresh state an authorizer keeps (libfx
// createCredentialMemory). Each access token the host supplied maps to the
// newest credentials refreshed from it, so a host that has not saved a
// rotation yet never spends the old refresh token twice, and concurrent
// requests that need the same refresh share one.
type CredentialMemory struct {
	mu          sync.Mutex
	descendants map[string]*Credentials
	order       []string // descendants keys, oldest first
	refreshing  map[string]*refreshTask
	// saved is libfx's WeakMap of the sessions each rotation was handed
	// to. An entry outlives its descendants slot, since a request may
	// still hold those credentials; savedOrder bounds it instead.
	saved      map[*Credentials]map[string]bool
	savedOrder []*Credentials
}

// maxSavedCredentials bounds CredentialMemory.saved.
const maxSavedCredentials = 4 * maxRememberedCredentials

type refreshTask struct {
	done chan struct{}
	next *Credentials
	err  error
}

// NewCredentialMemory returns empty refresh state.
func NewCredentialMemory() *CredentialMemory {
	return &CredentialMemory{
		descendants: map[string]*Credentials{},
		refreshing:  map[string]*refreshTask{},
		saved:       map[*Credentials]map[string]bool{},
	}
}

// subscriptionSpec is what differs between providers (libfx provider spec).
type subscriptionSpec struct {
	provider  string
	matches   func(endpoint string) bool
	sessionID func(h http.Header) string
	normalize func(c Credentials) (Credentials, error)
	refresh   func(ctx context.Context, c Credentials, fetch FetchFunc, now func() time.Time) (Credentials, error)
	authorize func(h http.Header, endpoint string, c *Credentials)
}

// EndpointOf is `${url.origin}${url.pathname}`: the scheme and host
// lowercased, a default port dropped.
func EndpointOf(u *url.URL) string {
	return OriginOf(u) + u.EscapedPath()
}

func endpointOf(u *url.URL) string { return EndpointOf(u) }

// OriginOf is a URL's origin as WHATWG URL serializes it.
func OriginOf(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host
}

// IsCodexRequest is true only for the exact Codex endpoints the core calls.
func IsCodexRequest(endpoint string) bool {
	return endpoint == codexResponsesEndpoint || endpoint == codexModelsEndpoint
}

// IsGrokRequest is true only for the exact Grok endpoints the core calls.
func IsGrokRequest(endpoint string) bool {
	return endpoint == grokProxyOrigin+grokResponsesPath ||
		endpoint == grokProxyOrigin+grokModelsPath ||
		endpoint == grokLanguageModelsURL
}

var codexSubscription = &subscriptionSpec{
	provider:  "codex",
	matches:   IsCodexRequest,
	sessionID: func(h http.Header) string { return h.Get("session-id") },
	normalize: NormalizeChatGPTCredentials,
	refresh:   refreshChatGPTCredentials,
	authorize: func(h http.Header, _ string, c *Credentials) {
		h.Set("authorization", "Bearer "+c.AccessToken)
		h.Set("chatgpt-account-id", c.AccountID)
	},
}

var grokSubscription = &subscriptionSpec{
	provider:  "grok",
	matches:   IsGrokRequest,
	sessionID: func(h http.Header) string { return h.Get("x-grok-conv-id") },
	normalize: NormalizeGrokCredentials,
	refresh:   refreshGrokCredentials,
	authorize: func(h http.Header, endpoint string, c *Credentials) {
		h.Set("authorization", "Bearer "+c.AccessToken)
		if endpoint == grokProxyOrigin+grokResponsesPath {
			h.Set("x-grok-user-id", c.AccountID)
		}
		if endpoint == grokProxyOrigin+grokModelsPath {
			h.Set("x-userid", c.AccountID)
		}
	},
}

// tokenString checks a required, bounded credential field.
func tokenString(value, name, label string) error {
	if value == "" {
		return fmt.Errorf("%s credentials %s must be a non-empty string", label, name)
	}
	if len(value) > maxTokenBytes {
		return fmt.Errorf("%s credentials %s exceeds %d bytes", label, name, maxTokenBytes)
	}
	return nil
}

// jwtClaims returns the claims of a JWT without verifying it, or nil.
func jwtClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 || parts[1] == "" {
		return nil
	}
	part := strings.NewReplacer("-", "+", "_", "/").Replace(parts[1])
	if pad := len(part) % 4; pad != 0 {
		part += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.StdEncoding.DecodeString(part)
	if err != nil {
		return nil
	}
	var claims map[string]any
	if securejson.Unmarshal(raw, &claims) != nil {
		return nil
	}
	return claims
}

// normalizeCredentials validates host credentials. The account ID and
// expiry default to the access token's claims.
func normalizeCredentials(c Credentials, label string, accountClaim func(map[string]any) string) (Credentials, error) {
	if err := tokenString(c.AccessToken, "accessToken", label); err != nil {
		return Credentials{}, err
	}
	if c.RefreshToken != "" {
		if err := tokenString(c.RefreshToken, "refreshToken", label); err != nil {
			return Credentials{}, err
		}
	}
	claims := jwtClaims(c.AccessToken)
	out := c
	if out.AccountID == "" {
		out.AccountID = accountClaim(claims)
	} else if err := tokenString(out.AccountID, "accountId", label); err != nil {
		return Credentials{}, err
	}
	if out.AccountID == "" {
		return Credentials{}, fmt.Errorf("%s credentials need an accountId; it was not in the access token", label)
	}
	if out.ExpiresAt == 0 {
		if exp, ok := claims["exp"].(float64); ok && !math.IsInf(exp, 0) && !math.IsNaN(exp) {
			out.ExpiresAt = int64(exp * 1000)
		}
	}
	return out, nil
}

// NormalizeChatGPTCredentials validates ChatGPT credentials; the account
// defaults to the token's chatgpt_account_id claim.
func NormalizeChatGPTCredentials(c Credentials) (Credentials, error) {
	return normalizeCredentials(c, "ChatGPT", func(claims map[string]any) string {
		auth, _ := claims[chatgptAuthClaim].(map[string]any)
		id, _ := auth["chatgpt_account_id"].(string)
		return id
	})
}

// NormalizeGrokCredentials validates Grok credentials; the account defaults
// to the token's sub claim.
func NormalizeGrokCredentials(c Credentials) (Credentials, error) {
	return normalizeCredentials(c, "Grok", func(claims map[string]any) string {
		sub, _ := claims["sub"].(string)
		return sub
	})
}

// needsRefresh is true when the credentials can and should be refreshed
// before use.
func needsRefresh(c *Credentials, now time.Time) bool {
	return c.RefreshToken != "" && c.ExpiresAt != 0 && c.ExpiresAt-refreshSkew.Milliseconds() <= now.UnixMilli()
}

type tokenResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresIn    *float64 `json:"expires_in"`
}

func readTokenJSON(resp *http.Response, into any) error {
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxTokenResponseBytes {
		return errors.New("token response exceeds the local limit")
	}
	return securejson.Unmarshal(data, into)
}

func closeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes))
		_ = resp.Body.Close()
	}
}

func doFetch(fetch FetchFunc, req *http.Request) (*http.Response, error) {
	resp, err := fetch(req)
	if err == nil && resp == nil {
		err = errors.New("fx fetch returned no response")
	}
	return resp, err
}

func expiresAtFrom(tokens tokenResponse, now func() time.Time) int64 {
	if tokens.ExpiresIn == nil || math.IsInf(*tokens.ExpiresIn, 0) || math.IsNaN(*tokens.ExpiresIn) {
		return 0
	}
	return now().UnixMilli() + int64(*tokens.ExpiresIn*1000)
}

// refreshChatGPTCredentials exchanges a refresh token at OpenAI, keeping the
// old refresh token when the response does not rotate it.
func refreshChatGPTCredentials(ctx context.Context, c Credentials, fetch FetchFunc, now func() time.Time) (Credentials, error) {
	if c.RefreshToken == "" {
		return Credentials{}, errors.New("ChatGPT credentials have no refreshToken")
	}
	body, _ := jsstr.Marshal(struct {
		ClientID     string `json:"client_id"`
		GrantType    string `json:"grant_type"`
		RefreshToken string `json:"refresh_token"`
	}{ChatGPTClientID, "refresh_token", c.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ChatGPTIssuer+"/oauth/token", bytes.NewReader(body))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("content-type", "application/json")
	resp, err := doFetch(fetch, req)
	if err != nil {
		return Credentials{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		closeBody(resp)
		return Credentials{}, &SubscriptionError{Code: "LIBFX_CHATGPT_REFRESH_FAILED", Status: resp.StatusCode, Message: fmt.Sprintf("ChatGPT token refresh failed with HTTP %d", resp.StatusCode)}
	}
	var tokens tokenResponse
	if err := readTokenJSON(resp, &tokens); err != nil {
		return Credentials{}, err
	}
	refresh := tokens.RefreshToken
	if refresh == "" {
		refresh = c.RefreshToken
	}
	next, err := NormalizeChatGPTCredentials(Credentials{AccessToken: tokens.AccessToken, RefreshToken: refresh, ExpiresAt: expiresAtFrom(tokens, now)})
	if err != nil {
		return Credentials{}, err
	}
	next.AccountID = c.AccountID
	return next, nil
}

// GrokAccountID returns the xAI account an access token belongs to: the
// sub of its userinfo.
func GrokAccountID(ctx context.Context, accessToken string, fetch FetchFunc) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GrokIssuer+"/oauth2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("authorization", "Bearer "+accessToken)
	req.Header.Set("accept", "application/json")
	resp, err := doFetch(fetch, req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		closeBody(resp)
		return "", &SubscriptionError{Code: "LIBFX_GROK_ACCOUNT_UNAVAILABLE", Status: resp.StatusCode, Message: fmt.Sprintf("xAI did not identify the account (HTTP %d)", resp.StatusCode)}
	}
	var info struct {
		Sub any `json:"sub"`
	}
	if readTokenJSON(resp, &info) != nil {
		info.Sub = nil
	}
	sub, _ := info.Sub.(string)
	if sub == "" {
		return "", &SubscriptionError{Code: "LIBFX_GROK_ACCOUNT_UNAVAILABLE", Message: "xAI's account response had no account id"}
	}
	return sub, nil
}

// refreshGrokCredentials exchanges a refresh token at xAI and confirms the
// new token is for the same account.
func refreshGrokCredentials(ctx context.Context, c Credentials, fetch FetchFunc, now func() time.Time) (Credentials, error) {
	if c.RefreshToken == "" {
		return Credentials{}, errors.New("Grok credentials have no refreshToken") //nolint:staticcheck // matches libfx error text
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", GrokClientID)
	form.Set("refresh_token", c.RefreshToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokIssuer+"/oauth2/token", strings.NewReader(encodeForm(form)))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")
	resp, err := doFetch(fetch, req)
	if err != nil {
		return Credentials{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		closeBody(resp)
		return Credentials{}, &SubscriptionError{Code: "LIBFX_GROK_REFRESH_FAILED", Status: resp.StatusCode, Message: fmt.Sprintf("Grok token refresh failed with HTTP %d", resp.StatusCode)}
	}
	var tokens tokenResponse
	if err := readTokenJSON(resp, &tokens); err != nil {
		return Credentials{}, err
	}
	refresh := tokens.RefreshToken
	if refresh == "" {
		refresh = c.RefreshToken
	}
	next, err := NormalizeGrokCredentials(Credentials{AccessToken: tokens.AccessToken, RefreshToken: refresh, AccountID: c.AccountID, ExpiresAt: expiresAtFrom(tokens, now)})
	if err != nil {
		return Credentials{}, err
	}
	account, err := GrokAccountID(ctx, next.AccessToken, fetch)
	if err != nil {
		return Credentials{}, err
	}
	if account != c.AccountID {
		return Credentials{}, &SubscriptionError{Code: "LIBFX_GROK_ACCOUNT_CHANGED", Message: "The refreshed Grok token belongs to a different account"}
	}
	return next, nil
}

// encodeForm is URLSearchParams.toString() for the fixed key order libfx
// uses (url.Values.Encode sorts the keys).
func encodeForm(v url.Values) string {
	var b strings.Builder
	for i, k := range []string{"grant_type", "client_id", "refresh_token"} {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(v.Get(k)))
	}
	return b.String()
}

// authorizer is libfx createSubscriptionAuthorizer.
type authorizer struct {
	specs []*subscriptionSpec
	auth  SubscriptionAuth
	fetch FetchFunc
	mem   *CredentialMemory
	now   func() time.Time
}

func newAuthorizer(specs []*subscriptionSpec, auth SubscriptionAuth, fetch FetchFunc) (*authorizer, error) {
	if auth.Credentials == nil {
		return nil, errors.New("a subscription provider needs a credentials() function")
	}
	mem := auth.Memory
	if mem == nil {
		mem = NewCredentialMemory()
	}
	now := auth.Now
	if now == nil {
		now = time.Now
	}
	return &authorizer{specs: specs, auth: auth, fetch: fetch, mem: mem, now: now}, nil
}

// specFor returns the spec that authorizes requests to endpoint, or nil.
func (z *authorizer) specFor(endpoint string) *subscriptionSpec {
	if z == nil {
		return nil
	}
	for _, s := range z.specs {
		if s.matches(endpoint) {
			return s
		}
	}
	return nil
}

func keyOf(spec *subscriptionSpec, token string) string { return spec.provider + "\x00" + token }

func (z *authorizer) remember(spec *subscriptionSpec, supplied, next *Credentials) {
	m := z.mem
	key := keyOf(spec, supplied.AccessToken)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.descendants[key]; ok {
		for i, k := range m.order {
			if k == key {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	}
	m.descendants[key] = next
	m.order = append(m.order, key)
	if len(m.order) > maxRememberedCredentials {
		oldest := m.order[0]
		m.order = m.order[1:]
		delete(m.descendants, oldest)
	}
}

func (z *authorizer) markSaved(next *Credentials, sessionID string) bool {
	m := z.mem
	m.mu.Lock()
	defer m.mu.Unlock()
	sessions := m.saved[next]
	if sessions == nil {
		sessions = map[string]bool{}
		m.saved[next] = sessions
		m.savedOrder = append(m.savedOrder, next)
		if len(m.savedOrder) > maxSavedCredentials {
			delete(m.saved, m.savedOrder[0])
			m.savedOrder = m.savedOrder[1:]
		}
	}
	if sessions[sessionID] {
		return false
	}
	sessions[sessionID] = true
	return true
}

// save hands next to OnCredentials once per session.
func (z *authorizer) save(ctx context.Context, spec *subscriptionSpec, next *Credentials, sessionID string) error {
	if z.auth.OnCredentials == nil {
		return nil
	}
	if !z.markSaved(next, sessionID) {
		return nil
	}
	// The refresh token is already spent: saving its replacement must not
	// stop because the request that triggered it was canceled, or a
	// keyring write is killed and the host is left holding a dead refresh
	// token. libfx's onCredentials takes no signal either.
	return z.auth.OnCredentials(context.WithoutCancel(ctx), *next, CredentialRequest{Provider: spec.provider, SessionID: sessionID})
}

func (z *authorizer) supplied(ctx context.Context, spec *subscriptionSpec, sessionID string) (*Credentials, error) {
	c, err := z.auth.Credentials(ctx, CredentialRequest{Provider: spec.provider, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	n, err := spec.normalize(c)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (z *authorizer) redeem(ctx context.Context, spec *subscriptionSpec, supplied, current *Credentials, sessionID string) (*Credentials, error) {
	if z.auth.RefreshLock != nil {
		// Another process may have refreshed and saved them while this
		// one waited for the lock.
		latest, err := z.supplied(ctx, spec, sessionID)
		if err != nil {
			return nil, err
		}
		if latest.AccessToken != current.AccessToken && latest.AccessToken != supplied.AccessToken && !needsRefresh(latest, z.now()) {
			z.remember(spec, supplied, latest)
			z.markSaved(latest, sessionID)
			return latest, nil
		}
	}
	n, err := spec.refresh(ctx, *current, z.fetch, z.now)
	if err != nil {
		return nil, err
	}
	next := &n
	z.remember(spec, supplied, next)
	if err := z.save(ctx, spec, next, sessionID); err != nil {
		return nil, err
	}
	return next, nil
}

func (z *authorizer) locked(ctx context.Context, spec *subscriptionSpec, supplied, current *Credentials, sessionID string) (*Credentials, error) {
	var next *Credentials
	err := z.auth.RefreshLock(ctx, CredentialRequest{Provider: spec.provider, SessionID: sessionID}, func(ctx context.Context) error {
		n, err := z.redeem(ctx, spec, supplied, current, sessionID)
		next = n
		return err
	})
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, errors.New("refreshLock must run and await the refresh it is given")
	}
	return next, nil
}

// testHookBeforeRefresh runs between a request's choice of credentials and
// its refresh (tests).
var testHookBeforeRefresh func(ctx context.Context)

// refresh spends current's refresh token once, however many requests need
// it at the same time.
func (z *authorizer) refresh(ctx context.Context, spec *subscriptionSpec, supplied, current *Credentials, sessionID string) (*Credentials, error) {
	if testHookBeforeRefresh != nil {
		testHookBeforeRefresh(ctx)
	}
	m := z.mem
	key := keyOf(spec, current.RefreshToken)
	m.mu.Lock()
	// libfx reads the remembered rotation and joins a running refresh in
	// one synchronous step. Here they are separate lock sections, so a
	// refresh that finished in between has already spent current's refresh
	// token: use its result rather than spending the token again.
	if d := m.descendants[keyOf(spec, supplied.AccessToken)]; d != nil && d != current && d.AccessToken != current.AccessToken && !needsRefresh(d, z.now()) {
		m.mu.Unlock()
		if sessionID != "" {
			if err := z.save(ctx, spec, d, sessionID); err != nil {
				return nil, err
			}
		}
		return d, nil
	}
	task, running := m.refreshing[key]
	if !running {
		task = &refreshTask{done: make(chan struct{})}
		m.refreshing[key] = task
	}
	m.mu.Unlock()
	if !running {
		func() {
			defer func() {
				m.mu.Lock()
				delete(m.refreshing, key)
				m.mu.Unlock()
				close(task.done)
			}()
			if z.auth.RefreshLock != nil {
				task.next, task.err = z.locked(ctx, spec, supplied, current, sessionID)
			} else {
				task.next, task.err = z.redeem(ctx, spec, supplied, current, sessionID)
			}
		}()
	} else {
		select {
		case <-task.done:
		case <-ctx.Done():
			return nil, context.Cause(ctx)
		}
	}
	if task.err != nil {
		return nil, task.err
	}
	if sessionID != "" {
		if err := z.save(ctx, spec, task.next, sessionID); err != nil {
			return nil, err
		}
	}
	return task.next, nil
}

func (z *authorizer) current(ctx context.Context, spec *subscriptionSpec, sessionID string) (supplied, used *Credentials, err error) {
	supplied, err = z.supplied(ctx, spec, sessionID)
	if err != nil {
		return nil, nil, err
	}
	z.mem.mu.Lock()
	best := z.mem.descendants[keyOf(spec, supplied.AccessToken)]
	z.mem.mu.Unlock()
	if best == nil {
		best = supplied
	}
	used = best
	if needsRefresh(best, z.now()) {
		if used, err = z.refresh(ctx, spec, supplied, best, sessionID); err != nil {
			return nil, nil, err
		}
	}
	if used != supplied && sessionID != "" {
		if err := z.save(ctx, spec, used, sessionID); err != nil {
			return nil, nil, err
		}
	}
	return supplied, used, nil
}

func withCredentials(spec *subscriptionSpec, endpoint string, req *http.Request, body []byte, c *Credentials) *http.Request {
	out := req.Clone(req.Context())
	out.Header = req.Header.Clone()
	if out.Header == nil {
		out.Header = http.Header{}
	}
	if body != nil {
		out.Body = io.NopCloser(bytes.NewReader(body))
		out.ContentLength = int64(len(body))
		out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	spec.authorize(out.Header, endpoint, c)
	return out
}

// do sends one request with credentials, refreshing once after a 401.
func (z *authorizer) do(spec *subscriptionSpec, endpoint string, hostFetch FetchFunc, req *http.Request, body []byte) (*http.Response, error) {
	ctx := req.Context()
	sessionID := spec.sessionID(req.Header)
	_, used, err := z.current(ctx, spec, sessionID)
	if err != nil {
		return nil, err
	}
	resp, err := doFetch(hostFetch, withCredentials(spec, endpoint, req, body, used))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized || used.RefreshToken == "" {
		return withoutCredentials(resp, used)
	}
	closeBody(resp)
	// Another request may have replaced these credentials while this one
	// was out. Only a rejection of the newest ones spends a refresh token.
	latestSupplied, latestUsed, err := z.current(ctx, spec, sessionID)
	if err != nil {
		return nil, err
	}
	next := latestUsed
	if latestUsed.AccessToken == used.AccessToken {
		if next, err = z.refresh(ctx, spec, latestSupplied, used, sessionID); err != nil {
			return nil, err
		}
	}
	resp, err = doFetch(hostFetch, withCredentials(spec, endpoint, req, body, next))
	if err != nil {
		return nil, err
	}
	// The retry's response may still quote the rejected credentials.
	return withoutCredentials(resp, next, used)
}

var jsonLeading = regexp.MustCompile(`^\s*[\[{"]`)

// withoutCredentials masks the credentials a failed response repeats. JSON
// is normalized first, so an escaped copy cannot survive.
func withoutCredentials(resp *http.Response, creds ...*Credentials) (*http.Response, error) {
	var secrets []string
	for _, c := range creds {
		if c != nil {
			secrets = append(secrets, c.AccessToken, c.RefreshToken)
		}
	}
	// Response headers never repeat the credentials either, whatever the
	// status (a Go hardening: libfx masks error bodies only).
	if headerHolds(resp.Header, secrets) {
		resp.Header = resp.Header.Clone()
		for name, values := range resp.Header {
			for i, v := range values {
				values[i] = RedactSecrets(v, secrets...)
			}
			resp.Header[name] = values
		}
	}
	if resp.StatusCode < 400 {
		return resp, nil
	}
	var body string
	if resp.Body == nil {
		body = ""
	} else {
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes+1))
		_ = resp.Body.Close()
		switch {
		case err != nil:
			return nil, err
		case len(data) > maxErrorBodyBytes:
			body = "Provider error details exceeded the local limit"
		default:
			body = strings.ToValidUTF8(string(data), "�")
			if jsonLeading.MatchString(body) {
				body = "Provider error details could not be decoded"
				if v, ok := jsonorder.Parse(data); ok {
					body = jsonorder.Stringify(v)
				}
			}
		}
	}
	body = RedactSecrets(body, secrets...)
	h := resp.Header.Clone()
	if h == nil {
		h = http.Header{}
	}
	h.Del("Content-Length")
	h.Del("Content-Encoding")
	return &http.Response{
		Status: resp.Status, StatusCode: resp.StatusCode, Proto: resp.Proto, ProtoMajor: resp.ProtoMajor, ProtoMinor: resp.ProtoMinor,
		Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: resp.Request,
	}, nil
}

func headerHolds(h http.Header, secrets []string) bool {
	for _, values := range h {
		for _, v := range values {
			for _, secret := range secrets {
				if secret != "" && strings.Contains(v, secret) {
					return true
				}
			}
		}
	}
	return false
}

// RedactSecrets replaces each secret, and its JSON-escaped form, with
// asterisks of the same JS length.
func RedactSecrets(body string, secrets ...string) string {
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		forms := []string{secret}
		if quoted, err := jsstr.Marshal(secret); err == nil {
			if escaped := string(quoted[1 : len(quoted)-1]); escaped != secret {
				forms = append(forms, escaped)
			}
		}
		for _, form := range forms {
			body = strings.ReplaceAll(body, form, strings.Repeat("*", len(utf16.Encode([]rune(form)))))
		}
	}
	return body
}
