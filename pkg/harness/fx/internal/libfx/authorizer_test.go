package libfx

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var authNow = time.UnixMilli(1_900_000_000_000)

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "header." + base64.RawURLEncoding.EncodeToString(b) + ".signature"
}

type authCall struct {
	method, url, body string
	header            http.Header
}

// authNetwork is a fake host fetch: OAuth endpoints answer with rotated
// tokens, everything else with respond.
type authNetwork struct {
	mu       sync.Mutex
	calls    []authCall
	rotated  string
	refreshN atomic.Int32
	gate     chan struct{} // when set, refreshes wait on it
	respond  func(c authCall, n int) *http.Response
	modelN   int
}

func jsonResponse(status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(string(b)))}
}

func textResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Length": {fmt.Sprint(len(body))}}, Body: io.NopCloser(strings.NewReader(body))}
}

func (n *authNetwork) fetch(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	c := authCall{req.Method, req.URL.String(), string(body), req.Header.Clone()}
	n.mu.Lock()
	n.calls = append(n.calls, c)
	n.mu.Unlock()
	switch c.url {
	case ChatGPTIssuer + "/oauth/token", GrokIssuer + "/oauth2/token":
		n.refreshN.Add(1)
		if n.gate != nil {
			<-n.gate
		}
		return jsonResponse(200, map[string]any{"access_token": n.rotated, "refresh_token": "rotated-refresh", "expires_in": 3600}), nil
	case GrokIssuer + "/oauth2/userinfo":
		return jsonResponse(200, map[string]any{"sub": "account"}), nil
	}
	n.mu.Lock()
	i := n.modelN
	n.modelN++
	respond := n.respond
	n.mu.Unlock()
	if respond != nil {
		return respond(c, i), nil
	}
	return textResponse(200, "ok"), nil
}

func (n *authNetwork) modelCalls() []authCall {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []authCall
	for _, c := range n.calls {
		if !strings.Contains(c.url, "auth.") {
			out = append(out, c)
		}
	}
	return out
}

type hostStore struct {
	mu    sync.Mutex
	creds Credentials
	saved []Credentials
	reads int
}

func (h *hostStore) auth() SubscriptionAuth {
	return SubscriptionAuth{
		Credentials: func(context.Context, CredentialRequest) (Credentials, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.reads++
			return h.creds, nil
		},
		OnCredentials: func(_ context.Context, next Credentials, _ CredentialRequest) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.saved = append(h.saved, next)
			return nil
		},
		Now: func() time.Time { return authNow },
	}
}

func newTestAuthorizer(t *testing.T, auth SubscriptionAuth, n *authNetwork) *authorizer {
	t.Helper()
	z, err := newAuthorizer([]*subscriptionSpec{codexSubscription, grokSubscription}, auth, n.fetch)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func send(t *testing.T, z *authorizer, n *authNetwork, rawURL string, header http.Header) (*http.Response, error) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(`{"model":"m"}`))
	if header != nil {
		req.Header = header
	}
	endpoint := endpointOf(req.URL)
	spec := z.specFor(endpoint)
	if spec == nil {
		t.Fatalf("no spec for %s", endpoint)
	}
	return z.do(spec, endpoint, n.fetch, req, []byte(`{"model":"m"}`))
}

func readAll(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func farFuture() int64 { return authNow.Add(24 * time.Hour).UnixMilli() }

func TestAuthorizerSpecsMatchOnlyExactEndpoints(t *testing.T) {
	z := newTestAuthorizer(t, SubscriptionAuth{Credentials: func(context.Context, CredentialRequest) (Credentials, error) { return Credentials{}, nil }}, &authNetwork{})
	for endpoint, want := range map[string]string{
		"https://chatgpt.com/backend-api/codex/responses":   "codex",
		"https://chatgpt.com/backend-api/codex/models":      "codex",
		"https://chatgpt.com/backend-api/codex/other":       "",
		"https://evil.example/backend-api/codex/models":     "",
		"https://cli-chat-proxy.grok.com/v1/responses":      "grok",
		"https://cli-chat-proxy.grok.com/v1/models":         "grok",
		"https://api.x.ai/v1/language-models":               "grok",
		"https://api.x.ai/v1/models":                        "",
		"https://auth.openai.com/oauth/token":               "",
		"https://ai-gateway.vercel.sh/v3/ai/language-model": "",
	} {
		got := ""
		if s := z.specFor(endpoint); s != nil {
			got = s.provider
		}
		if got != want {
			t.Errorf("%s: got %q, want %q", endpoint, got, want)
		}
	}
	for raw, want := range map[string]string{
		"HTTPS://ChatGPT.com:443/backend-api/codex/responses?x=1": "https://chatgpt.com/backend-api/codex/responses",
		"http://localhost:80/a":                                   "http://localhost/a",
		"https://h:8443/p":                                        "https://h:8443/p",
	} {
		req, _ := http.NewRequest("GET", raw, nil)
		if got := endpointOf(req.URL); got != want {
			t.Errorf("endpointOf(%s) = %s, want %s", raw, got, want)
		}
	}
}

func TestAuthorizerAddsProviderHeaders(t *testing.T) {
	n := &authNetwork{}
	store := &hostStore{creds: Credentials{AccessToken: "access", RefreshToken: "refresh", AccountID: "account", ExpiresAt: farFuture()}}
	z := newTestAuthorizer(t, store.auth(), n)
	for _, u := range []string{
		"https://chatgpt.com/backend-api/codex/responses",
		"https://cli-chat-proxy.grok.com/v1/responses",
		"https://cli-chat-proxy.grok.com/v1/models",
		"https://api.x.ai/v1/language-models",
	} {
		resp, err := send(t, z, n, u, http.Header{"Session-Id": {"s1"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
	}
	calls := n.modelCalls()
	type hv struct{ auth, account, grokUser, userID string }
	got := []hv{}
	for _, c := range calls {
		got = append(got, hv{c.header.Get("authorization"), c.header.Get("chatgpt-account-id"), c.header.Get("x-grok-user-id"), c.header.Get("x-userid")})
	}
	want := []hv{
		{"Bearer access", "account", "", ""},
		{"Bearer access", "", "account", ""},
		{"Bearer access", "", "", "account"},
		{"Bearer access", "", "", ""},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("headers = %v, want %v", got, want)
	}
	if len(store.saved) != 0 {
		t.Fatalf("saved %d credentials without a refresh", len(store.saved))
	}
}

func TestAuthorizerRefreshesNearExpiryAndRemembersRotation(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		t.Run(provider, func(t *testing.T) {
			rotated := jwt(map[string]any{"sub": "account", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
			n := &authNetwork{rotated: rotated}
			store := &hostStore{creds: Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: authNow.Add(time.Minute).UnixMilli()}}
			z := newTestAuthorizer(t, store.auth(), n)
			u := "https://chatgpt.com/backend-api/codex/responses"
			session := http.Header{"Session-Id": {"s1"}}
			if provider == "grok" {
				u = "https://cli-chat-proxy.grok.com/v1/responses"
				session = http.Header{"X-Grok-Conv-Id": {"s1"}}
			}
			for i := 0; i < 3; i++ {
				resp, err := send(t, z, n, u, session.Clone())
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close() //nolint:errcheck
			}
			if got := n.refreshN.Load(); got != 1 {
				t.Fatalf("refreshes = %d, want 1 (the host never saved the rotation, so the old refresh token must not be spent again)", got)
			}
			for _, c := range n.modelCalls() {
				if c.header.Get("authorization") != "Bearer "+rotated {
					t.Fatalf("model request used %q", c.header.Get("authorization"))
				}
			}
			if len(store.saved) != 1 || store.saved[0].AccessToken != rotated || store.saved[0].RefreshToken != "rotated-refresh" || store.saved[0].AccountID != "account" {
				t.Fatalf("saved = %v", store.saved)
			}
			if want := authNow.UnixMilli() + 3_600_000; store.saved[0].ExpiresAt != want {
				t.Fatalf("expiresAt = %d, want %d", store.saved[0].ExpiresAt, want)
			}
			var refresh authCall
			for _, c := range n.calls {
				if strings.HasSuffix(c.url, "/token") {
					refresh = c
				}
			}
			if provider == "codex" {
				if refresh.body != `{"client_id":"app_EMoamEEZ73f0CkXaXp7hrann","grant_type":"refresh_token","refresh_token":"old-refresh"}` || refresh.header.Get("content-type") != "application/json" {
					t.Fatalf("codex refresh = %+v", refresh)
				}
			} else if refresh.body != "grant_type=refresh_token&client_id=b1a00492-073a-47ea-816f-4c329264a828&refresh_token=old-refresh" || refresh.header.Get("content-type") != "application/x-www-form-urlencoded" {
				t.Fatalf("grok refresh = %+v", refresh)
			}
		})
	}
}

func TestAuthorizerRetriesOnceAfter401AndRedactsTheRejection(t *testing.T) {
	rotated := jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	n := &authNetwork{rotated: rotated, respond: func(c authCall, i int) *http.Response {
		if i == 0 {
			return textResponse(401, "rejected "+strings.TrimPrefix(c.header.Get("authorization"), "Bearer "))
		}
		if i == 1 {
			// The retry is rejected too, and the server quotes both the new
			// access token and the refresh token, raw and JSON-escaped.
			return jsonResponse(401, map[string]any{"error": "bad " + rotated, "refresh": "rotated-refresh", "escaped": "a\"b"})
		}
		return textResponse(200, "ok")
	}}
	store := &hostStore{creds: Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: farFuture()}}
	z := newTestAuthorizer(t, store.auth(), n)
	resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", http.Header{"Session-Id": {"s1"}})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, secret := range []string{rotated, "rotated-refresh", "old-access"} {
		if strings.Contains(body, secret) {
			t.Fatalf("body %q leaks %q", body, secret)
		}
	}
	if !strings.Contains(body, `"escaped":"a\"b"`) || !strings.Contains(body, "bad ****") {
		t.Fatalf("body = %s", body)
	}
	if resp.Header.Get("Content-Length") != "" || resp.ContentLength != int64(len(body)) {
		t.Fatalf("content length %q / %d", resp.Header.Get("Content-Length"), resp.ContentLength)
	}
	if n.refreshN.Load() != 1 {
		t.Fatalf("refreshes = %d", n.refreshN.Load())
	}
	calls := n.modelCalls()
	if len(calls) != 2 || calls[0].header.Get("authorization") != "Bearer old-access" || calls[1].header.Get("authorization") != "Bearer "+rotated {
		t.Fatalf("model calls = %+v", calls)
	}
	if calls[1].body != `{"model":"m"}` {
		t.Fatalf("retry body = %q", calls[1].body)
	}
}

func TestAuthorizerDoesNotRefreshWithoutRefreshToken(t *testing.T) {
	n := &authNetwork{respond: func(authCall, int) *http.Response { return textResponse(401, "no access-only") }}
	store := &hostStore{creds: Credentials{AccessToken: "access-only", AccountID: "account"}}
	z := newTestAuthorizer(t, store.auth(), n)
	resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	if body := readAll(t, resp); body != "no ***********" || n.refreshN.Load() != 0 || len(n.modelCalls()) != 1 {
		t.Fatalf("body %q, refreshes %d, calls %d", body, n.refreshN.Load(), len(n.modelCalls()))
	}
}

func TestWithoutCredentialsBodies(t *testing.T) {
	c := &Credentials{AccessToken: "tok\"en", RefreshToken: "ref"}
	for _, tc := range []struct{ name, in, want string }{
		{"text", "echo tok\"en and ref", "echo ****** and ***"},
		{"json escaped", `{"a": "tok\"en", "b": "ref"}`, `{"a":"*******","b":"***"}`},
		{"invalid json", `{"a": tok`, "Provider error details could not be decoded"},
		{"too large", strings.Repeat("x", maxErrorBodyBytes+1), "Provider error details exceeded the local limit"},
		{"empty", "", ""},
	} {
		resp, err := withoutCredentials(textResponse(500, tc.in), c)
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, resp); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	ok := textResponse(200, "tok\"en")
	if resp, _ := withoutCredentials(ok, c); resp != ok {
		t.Fatal("a successful response must pass through unchanged")
	}
}

func TestAuthorizerConcurrentRequestsShareOneRefresh(t *testing.T) {
	rotated := jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	n := &authNetwork{rotated: rotated, gate: make(chan struct{})}
	store := &hostStore{creds: Credentials{AccessToken: "old", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
	z := newTestAuthorizer(t, store.auth(), n)
	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", http.Header{"Session-Id": {fmt.Sprintf("s%d", i%2)}})
			if err == nil {
				resp.Body.Close() //nolint:errcheck
			}
			errs <- err
		}(i)
	}
	// Let every worker reach the refresh before it completes.
	deadline := time.Now().Add(5 * time.Second)
	for n.refreshN.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(n.gate)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := n.refreshN.Load(); got != 1 {
		t.Fatalf("refreshes = %d, want 1", got)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 2 {
		t.Fatalf("saved %d times, want once per session (2)", len(store.saved))
	}
}

func TestAuthorizerRefreshLock(t *testing.T) {
	rotated := jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	t.Run("runs the refresh under the host lock", func(t *testing.T) {
		n := &authNetwork{rotated: rotated}
		store := &hostStore{creds: Credentials{AccessToken: "old", RefreshToken: "r", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
		auth := store.auth()
		var held atomic.Bool
		var locks atomic.Int32
		auth.RefreshLock = func(ctx context.Context, req CredentialRequest, refresh func(context.Context) error) error {
			if req.Provider != "codex" || req.SessionID != "s" {
				t.Errorf("lock request %+v", req)
			}
			locks.Add(1)
			held.Store(true)
			defer held.Store(false)
			return refresh(ctx)
		}
		inner := n.fetch
		z, err := newAuthorizer([]*subscriptionSpec{codexSubscription}, auth, func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/oauth/token") && !held.Load() {
				t.Error("refresh ran outside the host lock")
			}
			return inner(r)
		})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", http.Header{"Session-Id": {"s"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		if locks.Load() != 1 || n.refreshN.Load() != 1 {
			t.Fatalf("locks %d refreshes %d", locks.Load(), n.refreshN.Load())
		}
	})
	t.Run("uses credentials another process saved while waiting", func(t *testing.T) {
		n := &authNetwork{rotated: rotated}
		store := &hostStore{creds: Credentials{AccessToken: "old", RefreshToken: "r", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
		auth := store.auth()
		auth.RefreshLock = func(ctx context.Context, _ CredentialRequest, refresh func(context.Context) error) error {
			store.mu.Lock()
			store.creds = Credentials{AccessToken: "other-process", RefreshToken: "r2", AccountID: "account", ExpiresAt: farFuture()}
			store.mu.Unlock()
			return refresh(ctx)
		}
		z := newTestAuthorizer(t, auth, n)
		resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", http.Header{"Session-Id": {"s"}})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		if n.refreshN.Load() != 0 {
			t.Fatal("refreshed although another process already had")
		}
		if got := n.modelCalls()[0].header.Get("authorization"); got != "Bearer other-process" {
			t.Fatalf("authorization %q", got)
		}
		if len(store.saved) != 0 {
			t.Fatal("credentials another process saved were saved again")
		}
	})
	t.Run("a lock that skips the refresh fails", func(t *testing.T) {
		n := &authNetwork{rotated: rotated}
		store := &hostStore{creds: Credentials{AccessToken: "old", RefreshToken: "r", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
		auth := store.auth()
		auth.RefreshLock = func(context.Context, CredentialRequest, func(context.Context) error) error { return nil }
		z := newTestAuthorizer(t, auth, n)
		if _, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", nil); err == nil || !strings.Contains(err.Error(), "refreshLock must run") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAuthorizerRefreshFailures(t *testing.T) {
	expiring := Credentials{AccessToken: "old", RefreshToken: "r", AccountID: "account", ExpiresAt: authNow.UnixMilli()}
	for _, tc := range []struct {
		name, url, code string
		fetch           func(r *http.Request) (*http.Response, error)
	}{
		{"chatgpt rejected", "https://chatgpt.com/backend-api/codex/responses", "LIBFX_CHATGPT_REFRESH_FAILED", func(*http.Request) (*http.Response, error) {
			return textResponse(400, "old"), nil
		}},
		{"grok rejected", "https://cli-chat-proxy.grok.com/v1/responses", "LIBFX_GROK_REFRESH_FAILED", func(*http.Request) (*http.Response, error) {
			return textResponse(400, ""), nil
		}},
		{"grok account changed", "https://cli-chat-proxy.grok.com/v1/responses", "LIBFX_GROK_ACCOUNT_CHANGED", func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/userinfo") {
				return jsonResponse(200, map[string]any{"sub": "someone-else"}), nil
			}
			return jsonResponse(200, map[string]any{"access_token": "new", "expires_in": 60}), nil
		}},
		{"grok account unknown", "https://cli-chat-proxy.grok.com/v1/responses", "LIBFX_GROK_ACCOUNT_UNAVAILABLE", func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, "/userinfo") {
				return jsonResponse(200, map[string]any{}), nil
			}
			return jsonResponse(200, map[string]any{"access_token": "new"}), nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &hostStore{creds: expiring}
			z, err := newAuthorizer([]*subscriptionSpec{codexSubscription, grokSubscription}, store.auth(), tc.fetch)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := http.NewRequest(http.MethodPost, tc.url, nil)
			ep := endpointOf(req.URL)
			_, err = z.do(z.specFor(ep), ep, tc.fetch, req, nil)
			var se *SubscriptionError
			if !errors.As(err, &se) || se.Code != tc.code {
				t.Fatalf("err = %v, want code %s", err, tc.code)
			}
			if strings.Contains(err.Error(), "old") || strings.Contains(err.Error(), "\"r\"") {
				t.Fatalf("error leaks a credential: %v", err)
			}
			if len(store.saved) != 0 {
				t.Fatal("saved after a failed refresh")
			}
		})
	}
}

func TestNormalizeCredentials(t *testing.T) {
	codexToken := jwt(map[string]any{"exp": 2_000_000_000, "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "claimed"}})
	got, err := NormalizeChatGPTCredentials(Credentials{AccessToken: codexToken})
	if err != nil || got.AccountID != "claimed" || got.ExpiresAt != 2_000_000_000_000 {
		t.Fatalf("got %v, %v", got, err)
	}
	got, err = NormalizeGrokCredentials(Credentials{AccessToken: jwt(map[string]any{"sub": "grok-sub"}), ExpiresAt: 5})
	if err != nil || got.AccountID != "grok-sub" || got.ExpiresAt != 5 {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, tc := range []struct {
		c    Credentials
		want string
	}{
		{Credentials{}, "ChatGPT credentials accessToken must be a non-empty string"},
		{Credentials{AccessToken: "opaque"}, "ChatGPT credentials need an accountId; it was not in the access token"},
		{Credentials{AccessToken: strings.Repeat("a", maxTokenBytes+1), AccountID: "a"}, "ChatGPT credentials accessToken exceeds 65536 bytes"},
		{Credentials{AccessToken: "a", RefreshToken: strings.Repeat("r", maxTokenBytes+1), AccountID: "a"}, "ChatGPT credentials refreshToken exceeds 65536 bytes"},
	} {
		if _, err := NormalizeChatGPTCredentials(tc.c); err == nil || err.Error() != tc.want {
			t.Errorf("err = %v, want %q", err, tc.want)
		}
	}
}

func TestCredentialsNeverFormatTokens(t *testing.T) {
	c := Credentials{AccessToken: "secret-access", RefreshToken: "secret-refresh", AccountID: "acct"}
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprint(&c)} {
		if strings.Contains(s, "secret") {
			t.Fatalf("formatted credentials leak a token: %s", s)
		}
	}
}

func TestStartAgentValidatesSubscriptionOptions(t *testing.T) {
	ctx := context.Background()
	if _, err := StartAgent(ctx, AgentOptions{Provider: "codex"}); err == nil || err.Error() != `provider "codex" needs a credentials() function` {
		t.Fatalf("err = %v", err)
	}
	if _, err := StartAgent(ctx, AgentOptions{Subscription: &SubscriptionAuth{}}); err == nil || !strings.Contains(err.Error(), "credentials needs provider") {
		t.Fatalf("err = %v", err)
	}
	if _, err := StartAgent(ctx, AgentOptions{Provider: "other"}); err == nil || !strings.Contains(err.Error(), "provider must be") {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthorizerMemoryIsBounded(t *testing.T) {
	store := &hostStore{}
	z := newTestAuthorizer(t, store.auth(), &authNetwork{})
	for i := 0; i < maxSavedCredentials+10; i++ {
		supplied := &Credentials{AccessToken: fmt.Sprintf("a%d", i)}
		next := &Credentials{AccessToken: fmt.Sprintf("n%d", i)}
		z.remember(codexSubscription, supplied, next)
		z.markSaved(next, "s")
	}
	z.mem.mu.Lock()
	defer z.mem.mu.Unlock()
	if len(z.mem.descendants) != maxRememberedCredentials || len(z.mem.order) != maxRememberedCredentials || len(z.mem.saved) != maxSavedCredentials || len(z.mem.savedOrder) != maxSavedCredentials {
		t.Fatalf("descendants %d order %d saved %d/%d", len(z.mem.descendants), len(z.mem.order), len(z.mem.saved), len(z.mem.savedOrder))
	}
	if _, ok := z.mem.descendants[keyOf(codexSubscription, "a0")]; ok {
		t.Fatal("the oldest rotation was not evicted")
	}
}

func TestAuthorizerRedactsCredentialsFromResponseHeaders(t *testing.T) {
	for _, status := range []int{200, 401, 500} {
		n := &authNetwork{respond: func(c authCall, _ int) *http.Response {
			r := textResponse(status, "")
			r.Header.Set("X-Echo", c.header.Get("authorization"))
			r.Header.Set("X-Refresh", `{"r":"old-refresh"}`)
			return r
		}}
		// No refresh token on the 401 case would stop the retry; keep one
		// and let the retry hit the same response.
		store := &hostStore{creds: Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: farFuture()}}
		n.rotated = jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
		z := newTestAuthorizer(t, store.auth(), n)
		resp, err := send(t, z, n, "https://chatgpt.com/backend-api/codex/responses", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		for name, values := range resp.Header {
			for _, v := range values {
				if strings.Contains(v, "old-access") || strings.Contains(v, "old-refresh") || strings.Contains(v, n.rotated) {
					t.Fatalf("status %d: header %s leaks a credential: %s", status, name, v)
				}
			}
		}
		if got := resp.Header.Get("X-Echo"); !strings.HasPrefix(got, "Bearer ***") {
			t.Fatalf("status %d: X-Echo = %q", status, got)
		}
	}
}

func TestAuthorizerKeepsStrictOrigins(t *testing.T) {
	z := newTestAuthorizer(t, SubscriptionAuth{Credentials: func(context.Context, CredentialRequest) (Credentials, error) { return Credentials{}, nil }}, &authNetwork{})
	for _, raw := range []string{
		"https://chatgpt.com//evil.example/backend-api/codex/responses",
		"https://chatgpt.com/https://evil.example/backend-api/codex/responses",
		"https://chatgpt.com.evil.example/backend-api/codex/responses",
		"https://chatgpt.com:8443/backend-api/codex/responses",
		"http://chatgpt.com/backend-api/codex/responses",
		"https://user@evil.example/backend-api/codex/responses",
		"https://cli-chat-proxy.grok.com/v1/responses/../../evil",
		"https://cli-chat-proxy.grok.com.evil.example/v1/responses",
	} {
		req, err := http.NewRequest("POST", raw, nil)
		if err != nil {
			continue
		}
		if s := z.specFor(EndpointOf(req.URL)); s != nil {
			t.Errorf("%s would be authorized as %s", raw, s.provider)
		}
	}
}

func TestAllowlistFetchRefusesCredentialedCrossOriginRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "reached "+r.Header.Get("Authorization"))
	}))
	defer target.Close()
	sameOrigin := http.NewServeMux()
	var origin *httptest.Server
	sameOrigin.HandleFunc("/off", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/x", http.StatusFound) })
	sameOrigin.HandleFunc("/on", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/final", http.StatusFound) })
	sameOrigin.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "final") })
	origin = httptest.NewServer(sameOrigin)
	defer origin.Close()
	fetch := AllowlistFetch(&http.Client{}, []string{origin.URL, target.URL})
	get := func(path string, auth bool) (*http.Response, error) {
		req, _ := http.NewRequest("GET", origin.URL+path, nil)
		if auth {
			req.Header.Set("Authorization", "Bearer secret")
		}
		return fetch(req)
	}
	if _, err := get("/off", true); err == nil || !strings.Contains(err.Error(), "credentialed redirect") {
		t.Fatalf("credentialed cross-origin redirect: %v", err)
	}
	resp, err := get("/off", false)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close() //nolint:errcheck
	resp, err = get("/on", true)
	if err != nil {
		t.Fatalf("same-origin redirect: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck
	if string(body) != "final" {
		t.Fatalf("body %q", body)
	}
}

// A request canceled once its refresh has spent the old refresh token must
// still let the host save the rotated credentials: a keyring write runs
// under the context it is given.
func TestAuthorizerSavesRotationAfterRequestCancel(t *testing.T) {
	store := &hostStore{creds: Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := &authNetwork{rotated: jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})}
	auth := store.auth()
	var saveErr error
	auth.OnCredentials = func(ctx context.Context, next Credentials, _ CredentialRequest) error {
		saveErr = ctx.Err()
		store.mu.Lock()
		store.saved = append(store.saved, next)
		store.mu.Unlock()
		return ctx.Err()
	}
	z := newTestAuthorizer(t, auth, n)
	fetch := func(req *http.Request) (*http.Response, error) {
		resp, err := n.fetch(req)
		if strings.HasSuffix(req.URL.Path, "/oauth/token") {
			cancel() // the turn is aborted while the token response arrives
		}
		return resp, err
	}
	z.fetch = fetch
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, codexResponsesEndpoint, strings.NewReader("{}"))
	req.Header.Set("session-id", "s")
	_, _ = z.do(codexSubscription, codexResponsesEndpoint, fetch, req, []byte("{}"))
	if saveErr != nil {
		t.Fatalf("OnCredentials ran with a canceled context: %v", saveErr)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 || store.saved[0].AccessToken != n.rotated {
		t.Fatalf("saved = %d rotations", len(store.saved))
	}
}

type pauseKey struct{}

// A request that chose expiring credentials just before another request's
// refresh of them finished must use that refresh's result, not spend the
// same refresh token a second time (libfx reads the remembered rotation
// and joins a running refresh atomically).
func TestAuthorizerLateRequestReusesFinishedRefresh(t *testing.T) {
	rotated := jwt(map[string]any{"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	n := &authNetwork{rotated: rotated}
	store := &hostStore{creds: Credentials{AccessToken: "old-access", RefreshToken: "old-refresh", AccountID: "account", ExpiresAt: authNow.UnixMilli()}}
	z := newTestAuthorizer(t, store.auth(), n)
	paused, release := make(chan struct{}), make(chan struct{})
	testHookBeforeRefresh = func(ctx context.Context) {
		if ctx.Value(pauseKey{}) != nil {
			close(paused)
			<-release
		}
	}
	defer func() { testHookBeforeRefresh = nil }()
	sendCtx := func(ctx context.Context) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, codexResponsesEndpoint, strings.NewReader("{}"))
		return z.do(codexSubscription, codexResponsesEndpoint, n.fetch, req, []byte("{}"))
	}
	late := make(chan error, 1)
	go func() {
		resp, err := sendCtx(context.WithValue(context.Background(), pauseKey{}, true))
		if err == nil {
			resp.Body.Close() //nolint:errcheck
		}
		late <- err
	}()
	<-paused // the late request chose the expiring credentials
	resp, err := sendCtx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close() //nolint:errcheck
	close(release)
	if err := <-late; err != nil {
		t.Fatal(err)
	}
	if got := n.refreshN.Load(); got != 1 {
		t.Fatalf("refresh token spent %d times, want 1", got)
	}
	calls := n.modelCalls()
	if len(calls) != 2 || calls[0].header.Get("authorization") != "Bearer "+rotated || calls[1].header.Get("authorization") != "Bearer "+rotated {
		t.Fatalf("model calls = %+v", calls)
	}
}

// A rotation replaced by a newer one keeps the sessions it was saved for
// (libfx's WeakMap entry lives while a request holds it), so a late waiter
// on its refresh never hands those older credentials to the host again.
func TestAuthorizerReplacedRotationStaysSaved(t *testing.T) {
	store := &hostStore{}
	z := newTestAuthorizer(t, store.auth(), &authNetwork{})
	supplied := &Credentials{AccessToken: "supplied"}
	first, second := &Credentials{AccessToken: "first"}, &Credentials{AccessToken: "second"}
	z.remember(codexSubscription, supplied, first)
	if err := z.save(context.Background(), codexSubscription, first, "s"); err != nil {
		t.Fatal(err)
	}
	z.remember(codexSubscription, supplied, second)
	if err := z.save(context.Background(), codexSubscription, first, "s"); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("the replaced rotation was saved %d times", len(store.saved))
	}
}
