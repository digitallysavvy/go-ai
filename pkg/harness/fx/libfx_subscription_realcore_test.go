package fx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/agent"
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/fx/internal/libfx"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// The subscription cases of TS fx-session-native.test.ts, on the real
// fx-core.wasm.

// memSubscription is an in-memory subscription store (the TS tests'
// mocked resolveFxAuthentication).
type memSubscription struct {
	mu     sync.Mutex
	creds  libfx.Credentials
	writes int
}

func (m *memSubscription) bind(provider string) *fxSubscription {
	return &fxSubscription{
		provider: provider,
		read: func(context.Context) (libfx.Credentials, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.creds, nil
		},
		write: func(_ context.Context, c libfx.Credentials) error {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.creds = c
			m.writes++
			return nil
		},
	}
}

func (m *memSubscription) snapshot() (libfx.Credentials, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.creds, m.writes
}

// subscriptionNetwork answers the core's requests for a codex or grok
// session.
type subscriptionNetwork struct {
	t       *testing.T
	mu      sync.Mutex
	rotated string
	rejectN int // reject this many model requests with 401
	model   func(index int) *http.Response
	modelN  int
	refresh []string
	auth    []string
	posts   int
	unknown []string
}

func (n *subscriptionNetwork) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_, _ = io.Copy(io.Discard, req.Body)
	}
	url := req.URL.String()
	endpoint := libfx.EndpointOf(req.URL)
	n.mu.Lock()
	defer n.mu.Unlock()
	respond := func(status int, body string) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
	switch endpoint {
	case "https://auth.openai.com/oauth/token", "https://auth.x.ai/oauth2/token":
		n.refresh = append(n.refresh, endpoint)
		b, _ := json.Marshal(map[string]any{"access_token": n.rotated, "refresh_token": "rotated-refresh-secret", "expires_in": 3600})
		return respond(200, string(b))
	case "https://auth.x.ai/oauth2/userinfo":
		return respond(200, `{"sub":"account"}`)
	case "https://x.ai/cli/stable":
		// The version check is anonymous: it never carries credentials.
		for _, name := range []string{"Authorization", "Cookie", "Chatgpt-Account-Id", "X-Grok-User-Id", "X-Userid", "X-Api-Key"} {
			if req.Header.Get(name) != "" {
				n.t.Errorf("the Grok CLI version request carried %s", name)
			}
		}
		return respond(200, "1.0.13\n")
	}
	if !strings.HasSuffix(endpoint, "/responses") {
		n.unknown = append(n.unknown, req.Method+" "+url)
		return respond(404, "")
	}
	n.posts++
	n.auth = append(n.auth, req.Header.Get("authorization"))
	if n.rejectN > 0 {
		n.rejectN--
		return respond(401, "rejected test-access-secret")
	}
	i := n.modelN
	n.modelN++
	resp := n.model(i)
	resp.Request = req
	return resp, nil
}

func (n *subscriptionNetwork) postCount() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.posts
}

func sseBody(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(bytes.NewReader([]byte(body)))}
}

func newSubscriptionHarness(t *testing.T, provider string, sub *memSubscription, network http.RoundTripper) *libfxHarness {
	t.Helper()
	h, err := CreateFx(Settings{Runtime: RuntimeLibfx, Auth: harness.AuthMode(harness.AuthModeDirect), Wasm: requireLibfxWasm(t), HTTPClient: &http.Client{Transport: network}})
	if err != nil {
		t.Fatal(err)
	}
	lh := h.(*libfxHarness)
	lh.resolve = func(context.Context, AuthenticationMode, string) (fxAuthentication, error) {
		return fxAuthentication{Provider: provider, Subscription: sub.bind(provider)}, nil
	}
	return lh
}

func subscriptionModel(provider string) string {
	if provider == "codex" {
		return "gpt-5-mini"
	}
	return "grok-code-fast-1"
}

// TestLibfxRealCoreSurfacesRejectedSubscriptionResponse mirrors TS
// "surfaces rejected native model response status".
func TestLibfxRealCoreSurfacesRejectedSubscriptionResponse(t *testing.T) {
	sub := &memSubscription{creds: libfx.Credentials{AccessToken: "test-access-secret", RefreshToken: "test-refresh-secret", AccountID: "account", ExpiresAt: 2_000_000_000_000}}
	network := &subscriptionNetwork{t: t, model: func(int) *http.Response {
		return &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"not-found","message":"Model not found."}}`))}
	}}
	h := newSubscriptionHarness(t, "grok", sub, network)
	a, err := harness.NewAgent(harness.AgentSettings{Harness: h, Model: "spacexai/grok-4.7"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sb := newFakeSandbox()
	session, err := a.CreateSession(ctx, harness.CreateSessionOptions{SandboxSession: sb})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy(ctx) //nolint:errcheck
	_, err = a.Generate(ctx, agent.AgentGenerateOptions{Prompt: "Reply.", HarnessSession: session})
	if err == nil || !strings.Contains(err.Error(), "fx grok model request spacexai/grok-4.7 failed with HTTP 404.") {
		t.Fatalf("err = %v (unknown requests %v)", err, network.unknown)
	}
	if strings.Contains(sb.allFiles(), "test-access-secret") || strings.Contains(sb.allFiles(), "Model not found") {
		t.Fatal("the rejected response or the credential reached the sandbox state")
	}
}

func rotatedTestToken() string {
	claims, _ := json.Marshal(map[string]any{"sub": "account", "exp": 2_000_000_000, "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account"}})
	return "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
}

// TestLibfxRealCoreSubscriptionRefresh mirrors TS "uses $provider
// subscriptions with libfx-owned $refresh refresh".
func TestLibfxRealCoreSubscriptionRefresh(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		for _, refresh := range []string{"expiry", "401"} {
			t.Run(provider+"/"+refresh, func(t *testing.T) {
				fixtures := responsesFixtures(t, provider, true)
				rotated := rotatedTestToken()
				expires := int64(2_000_000_000_000)
				if refresh == "expiry" {
					expires = time.Now().Add(time.Minute).UnixMilli()
				}
				sub := &memSubscription{creds: libfx.Credentials{AccessToken: "test-access-secret", RefreshToken: "test-refresh-secret", AccountID: "account", ExpiresAt: expires}}
				network := &subscriptionNetwork{t: t, rotated: rotated, model: func(i int) *http.Response {
					if i >= len(fixtures) {
						t.Errorf("unexpected subscription model request %d", i)
						return &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
					}
					return sseBody(fixtures[i])
				}}
				if refresh == "401" {
					network.rejectN = 1
				}
				h := newSubscriptionHarness(t, provider, sub, network)
				var mu sync.Mutex
				executed := 0
				calculator := types.Tool{
					Name: "calculator",
					Parameters: map[string]any{"type": "object", "properties": map[string]any{
						"a": map[string]any{"type": "number"}, "b": map[string]any{"type": "number"}, "op": map[string]any{"type": "string"},
					}, "required": []any{"a", "b", "op"}},
					Execute: func(_ context.Context, in map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
						mu.Lock()
						executed++
						mu.Unlock()
						a, _ := in["a"].(float64)
						b, _ := in["b"].(float64)
						if in["op"] == "add" {
							return a + b, nil
						}
						return a * b, nil
					},
				}
				a, err := harness.NewAgent(harness.AgentSettings{Harness: h, Model: subscriptionModel(provider), UserTools: map[string]types.Tool{"calculator": calculator}})
				if err != nil {
					t.Fatal(err)
				}
				ctx := context.Background()
				sb := newFakeSandbox()
				session, err := a.CreateSession(ctx, harness.CreateSessionOptions{SandboxSession: sb})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Destroy(ctx) //nolint:errcheck
				result, err := a.Generate(ctx, agent.AgentGenerateOptions{Prompt: "Calculate (12 + 7) * 3 * 10.", HarnessSession: session})
				if err != nil {
					t.Fatalf("generate: %v (unknown requests %v)", err, network.unknown)
				}
				wantText, wantCalls := 28, 3
				if provider == "grok" {
					wantText, wantCalls = 2786, 0
				}
				if got := len([]rune(result.Text)); got != wantText {
					t.Errorf("text length = %d, want %d", got, wantText)
				}
				mu.Lock()
				if executed != wantCalls {
					t.Errorf("calculator ran %d times, want %d", executed, wantCalls)
				}
				mu.Unlock()
				creds, writes := sub.snapshot()
				if writes != 1 || creds.AccessToken != rotated || creds.RefreshToken != "rotated-refresh-secret" {
					t.Errorf("writes = %d, credentials rotated = %v", writes, creds.AccessToken == rotated && creds.RefreshToken == "rotated-refresh-secret")
				}
				network.mu.Lock()
				if len(network.refresh) != 1 {
					t.Errorf("refresh requests = %v", network.refresh)
				}
				if network.modelN != len(fixtures) {
					t.Errorf("model responses served = %d, want %d", network.modelN, len(fixtures))
				}
				if last := network.auth[len(network.auth)-1]; last != "Bearer "+rotated {
					t.Errorf("last model request authorization was not the rotated token")
				}
				network.mu.Unlock()
				saved := sb.allFiles()
				for _, secret := range []string{"test-access-secret", "test-refresh-secret", rotated, "rotated-refresh-secret"} {
					if strings.Contains(saved, secret) {
						t.Errorf("the sandbox state holds a credential")
					}
				}
				if strings.Contains(saved, "rejected test-access-secret") {
					t.Error("the sandbox state holds the rejected response")
				}
			})
		}
	}
}

// TestLibfxRealCoreSubscriptionParsedOutputColdReplay mirrors TS "maps
// parsed codex/grok output and usage without repeating text on cold
// replay".
func TestLibfxRealCoreSubscriptionParsedOutputColdReplay(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		t.Run(provider, func(t *testing.T) {
			fixtures := responsesFixtures(t, provider, true)
			fixture := fixtures[len(fixtures)-1]
			sub := &memSubscription{creds: libfx.Credentials{AccessToken: "test-token", RefreshToken: "test-refresh", AccountID: "account", ExpiresAt: 2_000_000_000_000}}
			network := &subscriptionNetwork{t: t, model: func(int) *http.Response { return sseBody(fixture) }}
			h := newSubscriptionHarness(t, provider, sub, network)
			sb := newFakeSandbox()
			ctx := context.Background()
			start := startOpts(sb, "parsed-"+provider)
			opts := harness.TurnSettings{Model: subscriptionModel(provider)}
			session, err := h.DoStart(ctx, start)
			if err != nil {
				t.Fatal(err)
			}
			defer session.DoDestroy(ctx) //nolint:errcheck
			suspended := make(chan (<-chan *harness.ContinueTurnState), 1)
			var once sync.Once
			before := &partLog{}
			before.on = func(p harness.StreamPart) {
				// TS serves the response as one chunk, so its suspension never
				// lands before the record is complete. The pump here reads
				// 32 KiB at a time; suspend at the first text delta that
				// arrives once the record holds the whole response.
				if p.PartType() == harness.PartTypeTextDelta && modelRecordComplete(session) {
					once.Do(func() { suspended <- suspendFromEmit(t, session) })
				}
			}
			control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Reply."), Emit: before.emit})
			if err != nil {
				t.Fatal(err)
			}
			var continueFrom *harness.ContinueTurnState
			select {
			case ch := <-suspended:
				continueFrom = <-ch
			case <-time.After(60 * time.Second):
				t.Fatalf("no text arrived (unknown requests %v, parts %d)", network.unknown, len(before.snapshot()))
			}
			_ = waitDone(t, control)
			if continueFrom == nil {
				t.Fatal("no continuation")
			}
			requests := network.postCount()
			resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.DoDestroy(ctx) //nolint:errcheck
			after := &partLog{}
			continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
			if err != nil {
				t.Fatal(err)
			}
			if err := waitDone(t, continued); err != nil {
				t.Fatalf("continued: %v", err)
			}
			if n := network.postCount(); n != requests {
				t.Errorf("cold replay fetched: %d -> %d posts", requests, n)
			}
			all := &partLog{parts: append(before.snapshot(), after.snapshot()...)}
			want := 28
			if provider == "grok" {
				want = 2786
			}
			if got := len([]rune(all.text())); got != want {
				t.Errorf("text length = %d, want %d", got, want)
			}
			if n := len(all.ofType(harness.PartTypeTextStart)); n != 1 {
				t.Errorf("text-start parts = %d", n)
			}
			if n := len(all.ofType(harness.PartTypeTextEnd)); n != 1 {
				t.Errorf("text-end parts = %d", n)
			}
			if errs := after.ofType(harness.PartTypeError); len(errs) != 0 {
				t.Errorf("errors: %+v", errs)
			}
			finishes := after.ofType(harness.PartTypeFinish)
			if len(finishes) != 1 {
				t.Fatalf("finish parts = %d", len(finishes))
			}
			f := finishes[0].(*harness.FinishPart)
			if f.FinishReason != (harness.FinishReason{Unified: "stop", Raw: "stop"}) {
				t.Errorf("finish reason = %+v", f.FinishReason)
			}
			u := f.TotalUsage
			if u.InputTokens.Total == nil || *u.InputTokens.Total <= 0 {
				t.Errorf("total usage = %+v", u)
			}
			if provider == "grok" {
				got := []any{derefAny(u.InputTokens.CacheRead), u.InputTokens.CacheWrite == nil, derefAny(u.InputTokens.NoCache), derefAny(u.InputTokens.Total), derefAny(u.OutputTokens.Reasoning), derefAny(u.OutputTokens.Text), derefAny(u.OutputTokens.Total)}
				want := []any{int64(192), true, int64(24), int64(216), int64(253), int64(578), int64(831)}
				if mustJSON(got) != mustJSON(want) {
					t.Errorf("grok usage = %s, want %s", mustJSON(got), mustJSON(want))
				}
			}
		})
	}
}

func derefAny[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// modelRecordComplete reports whether the turn's first model record holds
// its whole response.
func modelRecordComplete(session harness.Session) bool {
	s := session.(*libfxSession)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Turn != nil && len(s.state.Turn.Models) > 0 && s.state.Turn.Models[0] != nil && s.state.Turn.Models[0].Complete
}

// TestLibfxRealCoreSubscriptionColdReplayFromPartialRecord suspends at the
// first text delta even when the model record is still incomplete, the
// timing the parsed-output test above avoids (TS serves its fixture as one
// chunk; a network body in chunks reaches it too). A truncated record is
// never replayed: as in TS, the continuation re-attempts the request, and
// the new attempt's text is a new block (TS part ids carry the attempt), so
// the abandoned partial block is not continued or mixed into it.
func TestLibfxRealCoreSubscriptionColdReplayFromPartialRecord(t *testing.T) {
	for _, provider := range []string{"codex", "grok"} {
		t.Run(provider, func(t *testing.T) {
			fixtures := responsesFixtures(t, provider, true)
			fixture := fixtures[len(fixtures)-1]
			sub := &memSubscription{creds: libfx.Credentials{AccessToken: "test-token", RefreshToken: "test-refresh", AccountID: "account", ExpiresAt: 2_000_000_000_000}}
			network := &subscriptionNetwork{t: t, model: func(int) *http.Response { return sseBody(fixture) }}
			h := newSubscriptionHarness(t, provider, sub, network)
			sb := newFakeSandbox()
			ctx := context.Background()
			start := startOpts(sb, "partial-"+provider)
			opts := harness.TurnSettings{Model: subscriptionModel(provider)}
			session, err := h.DoStart(ctx, start)
			if err != nil {
				t.Fatal(err)
			}
			defer session.DoDestroy(ctx) //nolint:errcheck
			suspended := make(chan (<-chan *harness.ContinueTurnState), 1)
			var once sync.Once
			var partial bool
			before := &partLog{}
			before.on = func(p harness.StreamPart) {
				if p.PartType() == harness.PartTypeTextDelta {
					once.Do(func() {
						partial = !modelRecordComplete(session)
						suspended <- suspendFromEmit(t, session)
					})
				}
			}
			control, err := session.DoPromptTurn(ctx, harness.PromptTurnOptions{TurnSettings: opts, Prompt: harness.TextPrompt("Reply."), Emit: before.emit})
			if err != nil {
				t.Fatal(err)
			}
			var continueFrom *harness.ContinueTurnState
			select {
			case ch := <-suspended:
				continueFrom = <-ch
			case <-time.After(60 * time.Second):
				t.Fatal("no text arrived")
			}
			_ = waitDone(t, control)
			if continueFrom == nil {
				t.Fatal("no continuation")
			}
			requests := network.postCount()
			resumed, err := h.DoStart(ctx, harness.StartOptions{SandboxSession: sb, SessionID: start.SessionID, SessionWorkDir: start.SessionWorkDir, ContinueFrom: continueFrom})
			if err != nil {
				t.Fatal(err)
			}
			defer resumed.DoDestroy(ctx) //nolint:errcheck
			after := &partLog{}
			continued, err := resumed.DoContinueTurn(ctx, harness.ContinueTurnOptions{TurnSettings: opts, Emit: after.emit})
			if err != nil {
				t.Fatal(err)
			}
			if err := waitDone(t, continued); err != nil {
				t.Fatalf("continued: %v", err)
			}
			wantPosts := requests
			if partial {
				wantPosts++
			}
			if n := network.postCount(); n != wantPosts {
				t.Errorf("posts after continuation = %d, want %d (record partial at suspend: %v)", n, wantPosts, partial)
			}
			all := &partLog{parts: append(before.snapshot(), after.snapshot()...)}
			want := 28
			if provider == "grok" {
				want = 2786
			}
			wantStarts := 1
			if partial {
				// The partial attempt's text stays as emitted; the new
				// attempt emits the whole response as a new block.
				wantStarts = 2
				if got := len([]rune(after.text())); got != want {
					t.Errorf("re-attempted text length = %d, want %d", got, want)
				}
				starts := all.ofType(harness.PartTypeTextStart)
				if len(starts) == 2 && starts[0].(*harness.TextStartPart).ID == starts[1].(*harness.TextStartPart).ID {
					t.Errorf("the re-attempt reused text block id %q", starts[0].(*harness.TextStartPart).ID)
				}
			} else if got := len([]rune(all.text())); got != want {
				t.Errorf("text length = %d, want %d", got, want)
			}
			if n := len(all.ofType(harness.PartTypeTextStart)); n != wantStarts {
				t.Errorf("text-start parts = %d, want %d", n, wantStarts)
			}
			if n := len(after.ofType(harness.PartTypeTextEnd)); n != 1 {
				t.Errorf("text-end parts = %d", n)
			}
			if errs := after.ofType(harness.PartTypeError); len(errs) != 0 {
				t.Errorf("errors: %+v", errs)
			}
			if n := len(after.ofType(harness.PartTypeFinish)); n != 1 {
				t.Errorf("finish parts = %d", n)
			}
		})
	}
}
