package claudecode_test

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
	"github.com/digitallysavvy/go-ai/pkg/harness/claudecode"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// recSandbox records what the host runs, spawns and writes, mirroring the
// fakeNetworkSandboxSessionForStartupSuccess helper of TS
// claude-code-harness.test.ts.
type recSandbox struct {
	*testSandbox
	mu        sync.Mutex
	runs      []string
	spawns    []string
	spawnEnvs []map[string]string
	writes    []providerutils.SandboxWriteTextFileOptions
}

func newRecSandbox(srv *bridgetest.Server) *recSandbox {
	s := &recSandbox{testSandbox: newTestSandbox(srv)}
	wireSpawn(s.testSandbox)
	return s
}

func (s *recSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	s.mu.Lock()
	s.runs = append(s.runs, opts.Command)
	s.mu.Unlock()
	return s.testSandbox.Run(ctx, opts)
}

func (s *recSandbox) Spawn(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	s.mu.Lock()
	s.spawns = append(s.spawns, opts.Command)
	s.spawnEnvs = append(s.spawnEnvs, opts.Env)
	s.mu.Unlock()
	return s.testSandbox.Sandbox.Spawn(ctx, opts)
}

func (s *recSandbox) WriteTextFile(ctx context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	s.mu.Lock()
	s.writes = append(s.writes, opts)
	s.mu.Unlock()
	return s.testSandbox.WriteTextFile(ctx, opts)
}

func (s *recSandbox) Restricted() providerutils.SandboxSession { return s }

// recAdderSandbox additionally supports additive request transformations.
type recAdderSandbox struct {
	*recSandbox
	trans [][]harness.RequestTransformation
}

func (s *recAdderSandbox) Restricted() providerutils.SandboxSession { return s }
func (s *recAdderSandbox) AddRequestTransformations(_ context.Context, t []harness.RequestTransformation) error {
	s.mu.Lock()
	s.trans = append(s.trans, t)
	s.mu.Unlock()
	return nil
}

// replaySession starts a harness and runs one prompt turn, returning the first
// start frame the bridge received.
func replayPrompt(t *testing.T, h *claudecode.Harness, sandbox harness.NetworkSandboxSession, start harness.StartOptions, turn harness.PromptTurnOptions, srvStart <-chan map[string]any) (harness.Session, map[string]any) {
	t.Helper()
	start.SandboxSession = sandbox
	if start.SessionID == "" {
		start.SessionID = "s1"
	}
	if start.SessionWorkDir == "" {
		start.SessionWorkDir = "/vercel/sandbox/claude-code-s1"
	}
	sess, err := h.DoStart(context.Background(), start)
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if turn.Prompt.Message == nil && turn.Prompt.Text == "" {
		turn.Prompt = harness.TextPrompt("Hello")
	}
	if turn.Emit == nil {
		turn.Emit = func(harness.StreamPart) {}
	}
	if _, err := sess.DoPromptTurn(context.Background(), turn); err != nil {
		t.Fatalf("DoPromptTurn: %v", err)
	}
	select {
	case frame := <-srvStart:
		return sess, frame
	case <-time.After(3 * time.Second):
		t.Fatal("bridge never received a start frame")
		return nil, nil
	}
}

func newReplayServer() (*bridgetest.Server, chan map[string]any) {
	ch := make(chan map[string]any, 8)
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(_ *bridgetest.Turn, start map[string]any) { ch <- start }})
	return srv, ch
}

func replayHarness(t *testing.T, s claudecode.Settings) *claudecode.Harness {
	t.Helper()
	if s.StartupTimeout == 0 {
		s.StartupTimeout = 2 * time.Second
	}
	if s.MintBridgeToken == nil {
		s.MintBridgeToken = func(string) string { return "tok" }
	}
	h, err := claudecode.New(s)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TS 'quotes dynamic startup paths in shell commands'.
func TestTSReplay_QuotesDynamicStartupPaths(t *testing.T) {
	srv, _ := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	h := replayHarness(t, claudecode.Settings{})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{
		SessionID: "s1; env > /tmp/leak #", SandboxSession: sandbox,
		SessionWorkDir: "/vercel/sandbox/claude-code-s1; env > /tmp/workdir-leak #",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	stateDir := "/home/agent/.ai-sdk-harness/.agent-runs/s1%3B%20env%20%3E%20%2Ftmp%2Fleak%20%23"
	wantMkdir := "mkdir -p '/vercel/sandbox/claude-code-s1; env > /tmp/workdir-leak #' '" + stateDir + "/bridge'"
	found := false
	for _, r := range sandbox.runs {
		if r == wantMkdir {
			found = true
		}
	}
	if !found {
		t.Fatalf("runs = %q, want to contain %q", sandbox.runs, wantMkdir)
	}
	wantSpawn := "node '/home/agent/.ai-sdk-harness/.harness-bootstrap/claude-code/bridge.mjs' --workdir '/vercel/sandbox/claude-code-s1; env > /tmp/workdir-leak #' --bridge-state-dir '" + stateDir + "/bridge'"
	if !reflect.DeepEqual(sandbox.spawns, []string{wantSpawn}) {
		t.Fatalf("spawns = %q, want [%q]", sandbox.spawns, wantSpawn)
	}
}

// TS 'sets custom headers for Gateway and direct auth'.
func TestTSReplay_CustomHeaders(t *testing.T) {
	for name, auth := range map[string]map[string]string{
		"gateway": {"AI_GATEWAY_API_KEY": "gateway-key"},
		"direct":  {"ANTHROPIC_API_KEY": "anthropic-key"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, ch := newReplayServer()
			t.Cleanup(srv.Close)
			h := replayHarness(t, claudecode.Settings{Auth: harness.AuthEnvironment(auth)})
			_, start := replayPrompt(t, h, newRecSandbox(srv), harness.StartOptions{Headers: map[string]string{"x-tenant": "acme"}}, harness.PromptTurnOptions{}, ch)
			env, _ := start["env"].(map[string]any)
			if env["ANTHROPIC_CUSTOM_HEADERS"] != "x-tenant: acme" {
				t.Fatalf("env = %v", env)
			}
		})
	}
}

// TS 'brokers credentials when the sandbox supports additive request transformations'.
func TestTSReplay_BrokersCredentials(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := &recAdderSandbox{recSandbox: newRecSandbox(srv)}
	var forwarded []harness.CredentialForwardingOptions
	h := replayHarness(t, claudecode.Settings{
		Auth: harness.AuthEnvironment(map[string]string{"ANTHROPIC_API_KEY": "anthropic-secret", "ANTHROPIC_BASE_URL": "https://anthropic.example/v1"}),
		CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return "ephemeral-" + o.EnvironmentVariableName, nil
		},
	})
	_, start := replayPrompt(t, h, sandbox, harness.StartOptions{}, harness.PromptTurnOptions{}, ch)
	wantTrans := `[[{"match":{"host":"anthropic.example","path":{"startsWith":"/v1"},"headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"ephemeral-ANTHROPIC_API_KEY"}}]},"transform":{"headers":{"x-api-key":"anthropic-secret"}}}]]`
	if b, _ := json.Marshal(sandbox.trans); string(b) != wantTrans {
		t.Fatalf("transformations:\n got %s\nwant %s", b, wantTrans)
	}
	if len(forwarded) != 1 || forwarded[0].EnvironmentVariableName != "ANTHROPIC_API_KEY" || !regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`).MatchString(forwarded[0].Credential) {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	env, _ := start["env"].(map[string]any)
	if env["ANTHROPIC_API_KEY"] != "ephemeral-ANTHROPIC_API_KEY" {
		t.Fatalf("start.env = %v", env)
	}
	if b, _ := json.Marshal(sandbox.spawnEnvs[0]); strings.Contains(string(b), "anthropic-secret") {
		t.Fatalf("spawn env leaked the secret: %s", b)
	}
}

// TS 'adds a Gateway placeholder when resuming with a new authentication mode'.
func TestTSReplay_GatewayPlaceholderOnResumeWithNewAuthMode(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := &recAdderSandbox{recSandbox: newRecSandbox(srv)}
	h := replayHarness(t, claudecode.Settings{Auth: harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "current-gateway-secret"})})
	resume, _ := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{
		"sandboxCredentialEnvironment": map[string]any{"ANTHROPIC_API_KEY": "saved-anthropic-placeholder"},
	})
	_, start := replayPrompt(t, h, sandbox, harness.StartOptions{ResumeFrom: resume}, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Continue the session.")}, ch)
	env, _ := start["env"].(map[string]any)
	if env["ANTHROPIC_API_KEY"] != "saved-anthropic-placeholder" {
		t.Fatalf("ANTHROPIC_API_KEY = %v", env["ANTHROPIC_API_KEY"])
	}
	if gw, _ := env["AI_GATEWAY_API_KEY"].(string); !regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`).MatchString(gw) {
		t.Fatalf("AI_GATEWAY_API_KEY = %v", env["AI_GATEWAY_API_KEY"])
	}
	if b, _ := json.Marshal(env); strings.Contains(string(b), "current-gateway-secret") {
		t.Fatalf("start env leaked: %s", b)
	}
	if b, _ := json.Marshal(sandbox.spawnEnvs[0]); strings.Contains(string(b), "current-gateway-secret") {
		t.Fatalf("spawn env leaked: %s", b)
	}
	want := `{"match":{"host":"ai-gateway.vercel.sh","headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"saved-anthropic-placeholder"}}]},"transform":{"headers":{"x-api-key":"current-gateway-secret"}}}`
	found := false
	for _, tr := range sandbox.trans[0] {
		if b, _ := json.Marshal(tr); string(b) == want {
			found = true
		}
	}
	if !found {
		b, _ := json.Marshal(sandbox.trans)
		t.Fatalf("transformation %s not found in %s", want, b)
	}
}

// TS 'brokers an explicit Claude OAuth token at the host boundary'.
func TestTSReplay_BrokersExplicitOAuthToken(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := &recAdderSandbox{recSandbox: newRecSandbox(srv)}
	h := replayHarness(t, claudecode.Settings{
		Auth: harness.AuthEnvironment(map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "host-oauth-token"}),
		CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			return "ephemeral-" + o.EnvironmentVariableName, nil
		},
	})
	_, start := replayPrompt(t, h, sandbox, harness.StartOptions{}, harness.PromptTurnOptions{}, ch)
	want := `[[{"match":{"host":"api.anthropic.com","headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer ephemeral-CLAUDE_CODE_OAUTH_TOKEN"}}]},"transform":{"headers":{"Authorization":"Bearer host-oauth-token"}}}]]`
	if b, _ := json.Marshal(sandbox.trans); string(b) != want {
		t.Fatalf("transformations:\n got %s\nwant %s", b, want)
	}
	env, _ := start["env"].(map[string]any)
	if env["CLAUDE_CODE_OAUTH_TOKEN"] != "ephemeral-CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("start.env = %v", env)
	}
	if b, _ := json.Marshal(sandbox.spawnEnvs[0]); strings.Contains(string(b), "host-oauth-token") {
		t.Fatalf("spawn env leaked: %s", b)
	}
}

// TS 'customizes real credentials when request transformations are unavailable'.
func TestTSReplay_CustomizesRealCredentialsWithoutTransformations(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	var forwarded []harness.CredentialForwardingOptions
	h := replayHarness(t, claudecode.Settings{
		Auth: harness.AuthEnvironment(map[string]string{"ANTHROPIC_API_KEY": "anthropic-secret"}),
		CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return "caller-managed-credential", nil
		},
	})
	_, start := replayPrompt(t, h, sandbox, harness.StartOptions{}, harness.PromptTurnOptions{}, ch)
	if len(forwarded) != 1 || forwarded[0].Credential != "anthropic-secret" || forwarded[0].EnvironmentVariableName != "ANTHROPIC_API_KEY" {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	env, _ := start["env"].(map[string]any)
	if env["ANTHROPIC_API_KEY"] != "caller-managed-credential" {
		t.Fatalf("start.env = %v", env)
	}
	if b, _ := json.Marshal(sandbox.spawnEnvs[0]); strings.Contains(string(b), "anthropic-secret") {
		t.Fatalf("spawn env leaked: %s", b)
	}
}

// TS 'customizes credentials forwarded through the Claude process environment'.
func TestTSReplay_CustomizesProcessEnvironmentCredentials(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	var forwarded []harness.CredentialForwardingOptions
	h := replayHarness(t, claudecode.Settings{
		Auth: harness.AuthEnvironment(map[string]string{"ANTHROPIC_API_KEY": "bridge-secret"}),
		Env:  map[string]string{"ANTHROPIC_API_KEY": "turn-api-key", "ANTHROPIC_AUTH_TOKEN": "turn-auth-token", "NON_SECRET": "preserved"},
		CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			forwarded = append(forwarded, o)
			return "ephemeral-" + o.Credential, nil
		},
	})
	_, start := replayPrompt(t, h, newRecSandbox(srv), harness.StartOptions{}, harness.PromptTurnOptions{}, ch)
	want := []harness.CredentialForwardingOptions{
		{Credential: "turn-api-key", EnvironmentVariableName: "ANTHROPIC_API_KEY"},
		{Credential: "turn-auth-token", EnvironmentVariableName: "ANTHROPIC_AUTH_TOKEN"},
	}
	if !reflect.DeepEqual(forwarded, want) {
		t.Fatalf("forwarded = %+v, want %+v", forwarded, want)
	}
	env, _ := start["env"].(map[string]any)
	if env["ANTHROPIC_API_KEY"] != "ephemeral-turn-api-key" || env["ANTHROPIC_AUTH_TOKEN"] != "ephemeral-turn-auth-token" || env["NON_SECRET"] != "preserved" {
		t.Fatalf("start.env = %v", env)
	}
}

// TS 'uses a caller-minted bridge token and reuses it when attaching'.
func TestTSReplay_CallerMintedTokenReusedOnAttach(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "token-for-test-sandbox"})
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	var mints int32
	var minted []string
	h := replayHarness(t, claudecode.Settings{MintBridgeToken: func(id string) string {
		atomic.AddInt32(&mints, 1)
		minted = append(minted, id)
		return "token-for-" + id
	}})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(minted, []string{"test-sandbox"}) {
		t.Fatalf("minted = %v", minted)
	}
	if got := sandbox.spawnEnvs[0]["BRIDGE_CHANNEL_TOKEN"]; got != "token-for-test-sandbox" {
		t.Fatalf("BRIDGE_CHANNEL_TOKEN = %q", got)
	}
	resume, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var data struct{ Bridge struct{ Token string } }
	_ = json.Unmarshal(resume.Data, &data)
	if data.Bridge.Token != "token-for-test-sandbox" {
		t.Fatalf("resume.data = %s", resume.Data)
	}
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w", ResumeFrom: resume})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	if atomic.LoadInt32(&mints) != 1 {
		t.Fatalf("mintBridgeToken called %d times, want 1", mints)
	}
}

// TS 'resumes the exact conversation after detaching and attaching'.
func TestTSReplay_ResumesExactConversationAfterAttach(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	h := replayHarness(t, claudecode.Settings{})
	first, err := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{"claudeSessionId": "claude-session-1"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w", ResumeFrom: first})
	if err != nil {
		t.Fatal(err)
	}
	resume, err := sess.DoDetach(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, start := replayPrompt(t, h, sandbox, harness.StartOptions{SessionWorkDir: "/w", ResumeFrom: resume}, harness.PromptTurnOptions{Prompt: harness.TextPrompt("Continue the work.")}, ch)
	if start["resumeSessionId"] != "claude-session-1" {
		t.Fatalf("start = %v", start)
	}
	if _, ok := start["continue"]; ok {
		t.Fatalf("start must not carry continue: %v", start)
	}
}

// TS 'sends the thinking configuration and effort', 'sends configured MCP
// servers', 'defaults to summarized adaptive thinking', 'sends environment
// configuration'.
func TestTSReplay_StartConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		settings claudecode.Settings
		check    func(t *testing.T, start map[string]any)
	}{
		{"thinking and effort", claudecode.Settings{Thinking: &claudecode.ThinkingConfig{Type: "enabled", Display: "omitted"}, Effort: "max"},
			func(t *testing.T, s map[string]any) {
				if !reflect.DeepEqual(s["thinking"], map[string]any{"type": "enabled", "display": "omitted"}) || s["effort"] != "max" {
					t.Fatalf("start = %v", s)
				}
			}},
		{"mcp servers", claudecode.Settings{MCPServers: map[string]any{"context7": map[string]any{"type": "http", "url": "https://mcp.context7.com/mcp"}}},
			func(t *testing.T, s map[string]any) {
				if !reflect.DeepEqual(s["mcpServers"], map[string]any{"context7": map[string]any{"type": "http", "url": "https://mcp.context7.com/mcp"}}) {
					t.Fatalf("mcpServers = %v", s["mcpServers"])
				}
			}},
		{"default thinking", claudecode.Settings{}, func(t *testing.T, s map[string]any) {
			if !reflect.DeepEqual(s["thinking"], map[string]any{"type": "adaptive", "display": "summarized"}) {
				t.Fatalf("thinking = %v", s["thinking"])
			}
		}},
		{"disabled thinking has no display", claudecode.Settings{Thinking: &claudecode.ThinkingConfig{Type: "disabled", Display: "summarized"}}, func(t *testing.T, s map[string]any) {
			if !reflect.DeepEqual(s["thinking"], map[string]any{"type": "disabled"}) {
				t.Fatalf("thinking = %v", s["thinking"])
			}
		}},
		{"env", claudecode.Settings{Env: map[string]string{"DEPLOYMENT_ENV": "staging"}}, func(t *testing.T, s map[string]any) {
			if env, _ := s["env"].(map[string]any); env["DEPLOYMENT_ENV"] != "staging" {
				t.Fatalf("env = %v", s["env"])
			}
		}},
		{"maxTurns", claudecode.Settings{MaxTurns: 7}, func(t *testing.T, s map[string]any) {
			if s["maxTurns"] != float64(7) {
				t.Fatalf("maxTurns = %v", s["maxTurns"])
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, ch := newReplayServer()
			t.Cleanup(srv.Close)
			h := replayHarness(t, tc.settings)
			_, start := replayPrompt(t, h, newRecSandbox(srv), harness.StartOptions{}, harness.PromptTurnOptions{}, ch)
			tc.check(t, start)
		})
	}
}

// TS 'sends environment configuration when rerunning a continued turn'.
func TestTSReplay_RerunContinuationCarriesEnvAndContinue(t *testing.T) {
	srv, ch := newReplayServer()
	t.Cleanup(srv.Close)
	h := replayHarness(t, claudecode.Settings{Env: map[string]string{"DEPLOYMENT_ENV": "staging"}})
	cont, _ := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{})
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: newRecSandbox(srv), SessionWorkDir: "/w", ContinueFrom: cont})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
	if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(harness.StreamPart) {}}); err != nil {
		t.Fatal(err)
	}
	start := <-ch
	if env, _ := start["env"].(map[string]any); env["DEPLOYMENT_ENV"] != "staging" || start["continue"] != true || start["prompt"] != "Continue." {
		t.Fatalf("start = %v", start)
	}
}

// TS 'writes standard Claude skill files and enables their names on start' and
// the two unsafe-path rejections.
func TestTSReplay_Skills(t *testing.T) {
	t.Run("writes files and enables names", func(t *testing.T) {
		srv, ch := newReplayServer()
		t.Cleanup(srv.Close)
		sandbox := newRecSandbox(srv)
		h := replayHarness(t, claudecode.Settings{})
		skills := []harness.Skill{
			{Name: "weather-forecast", Description: "Use the weather forecast tool.", Content: "Call `get_weather` before answering forecasts.",
				Files: []harness.SkillFile{{Path: "reference.md", Content: "# Forecast reference"}}},
			{Name: "weather-codes", Description: "Read weather code reference.", Content: "Read `weather-codes.md` for code descriptions."},
		}
		_, start := replayPrompt(t, h, sandbox, harness.StartOptions{}, harness.PromptTurnOptions{
			TurnSettings: harness.TurnSettings{Skills: skills}, Prompt: harness.TextPrompt("which skills do you have available?"),
		}, ch)
		if !reflect.DeepEqual(start["skills"], []any{"weather-forecast", "weather-codes"}) {
			t.Fatalf("skills = %v", start["skills"])
		}
		sandbox.mu.Lock()
		defer sandbox.mu.Unlock()
		hasMkdir := false
		for _, r := range sandbox.runs {
			if r == "mkdir -p '/home/agent/.claude/skills'" {
				hasMkdir = true
			}
		}
		if !hasMkdir {
			t.Fatalf("runs = %q", sandbox.runs)
		}
		var skillWrites []string
		var meta string
		for _, w := range sandbox.writes {
			if strings.Contains(w.Path, "/weather-") {
				skillWrites = append(skillWrites, w.Path)
			}
			if strings.HasSuffix(w.Path, "/bridge-meta.json") {
				if w.Path != "/home/agent/.ai-sdk-harness/.agent-runs/s1/bridge/bridge-meta.json" {
					t.Errorf("bridge-meta path = %s", w.Path)
				}
				meta = w.Content
			}
		}
		if meta != `{"type":"claude-code","state":"starting"}` {
			t.Errorf("bridge-meta = %q", meta)
		}
		for _, p := range []string{
			"/home/agent/.claude/skills/weather-forecast/SKILL.md",
			"/home/agent/.claude/skills/weather-forecast/reference.md",
			"/home/agent/.claude/skills/weather-codes/SKILL.md",
		} {
			ok := false
			for _, w := range skillWrites {
				if w == p {
					ok = true
				}
			}
			if !ok {
				t.Errorf("missing write %s in %v", p, skillWrites)
			}
		}
		if len(skillWrites) != 3 {
			t.Errorf("skill writes = %v, want exactly 3", skillWrites)
		}
	})
	for _, tc := range []struct {
		name    string
		skill   harness.Skill
		wantErr string
	}{
		{"unsafe skill name", harness.Skill{Name: "../weather", Description: "unsafe", Content: "unsafe"}, "Invalid Claude Code skill name"},
		{"unsafe skill file path", harness.Skill{Name: "weather", Description: "unsafe", Content: "unsafe", Files: []harness.SkillFile{{Path: "../weather-codes.md", Content: "unsafe"}}}, "Invalid Claude Code skill file path"},
	} {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			srv, _ := newReplayServer()
			t.Cleanup(srv.Close)
			sandbox := newRecSandbox(srv)
			h := replayHarness(t, claudecode.Settings{})
			sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w"})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
			_, err = sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
				Prompt: harness.TextPrompt("Use the skill."), TurnSettings: harness.TurnSettings{Skills: []harness.Skill{tc.skill}}, Emit: func(harness.StreamPart) {},
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			sandbox.mu.Lock()
			defer sandbox.mu.Unlock()
			for _, w := range sandbox.writes {
				if strings.Contains(w.Path, "../") || strings.Contains(w.Path, "weather-codes.md") {
					t.Errorf("unsafe write %s", w.Path)
				}
			}
			for _, r := range sandbox.runs {
				if r == "mkdir -p '/home/agent/.claude/skills'" {
					t.Errorf("skills dir created before validation: %q", sandbox.runs)
				}
			}
		})
	}
}

// TS 'includes bridge startup stdout, stderr, and exit code when ready never arrives'.
func TestTSReplay_StartupFailureMessage(t *testing.T) {
	srv, _ := newReplayServer()
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	sandbox.SetSpawn(func(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
		proc := bridgetest.NewProcess()
		proc.WriteStdout(`{"type":"bridge-fatal","message":"Missing --workdir argument."}` + "\n")
		proc.WriteStderr("Cannot find module @anthropic-ai/claude-agent-sdk")
		proc.CloseStdout()
		proc.CloseStderr()
		proc.Exit(1)
		return proc, nil
	})
	h := replayHarness(t, claudecode.Settings{})
	_, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"claude-code bridge exited before becoming ready.", "Exit code: 1.", "bridge-fatal", "Missing --workdir argument.", "Cannot find module @anthropic-ai/claude-agent-sdk"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
}

// TS getBootstrap cases.
func TestTSReplay_GetBootstrap(t *testing.T) {
	h := replayHarness(t, claudecode.Settings{})
	recipe, err := h.GetBootstrap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if recipe.HarnessID != "claude-code" || recipe.BootstrapDir != ".harness-bootstrap/claude-code" {
		t.Fatalf("recipe = %+v", recipe)
	}
	var paths []string
	for _, f := range recipe.Files {
		paths = append(paths, f.Path)
		if len(f.Content) == 0 {
			t.Errorf("%s is empty", f.Path)
		}
	}
	want := []string{
		".harness-bootstrap/claude-code/package.json", ".harness-bootstrap/claude-code/pnpm-lock.yaml",
		".harness-bootstrap/claude-code/pnpm-workspace.yaml", ".harness-bootstrap/claude-code/bridge.mjs",
	}
	if len(paths) != 4 {
		t.Fatalf("paths = %v", paths)
	}
	for _, w := range want {
		ok := false
		for _, p := range paths {
			ok = ok || p == w
		}
		if !ok {
			t.Errorf("missing %s in %v", w, paths)
		}
	}
	if len(recipe.Commands) != 2 || recipe.Commands[0].Command != "pnpm install --frozen-lockfile --store-dir .pnpm-store" || recipe.Commands[1].Command != "./node_modules/.bin/claude --version" {
		t.Fatalf("commands = %+v", recipe.Commands)
	}
	for _, f := range recipe.Files {
		if strings.HasSuffix(f.Path, "/pnpm-workspace.yaml") && !strings.Contains(f.Content, "'@anthropic-ai/claude-code': true") {
			t.Errorf("workspace lacks the allowBuilds entry: %s", f.Content)
		}
	}
}

// TS claude-code-instructions.test.ts 'keeps instructions separate...' /
// 'forwards instructions when rerunning a suspended turn'.
func TestTSReplay_InstructionsTransport(t *testing.T) {
	const instructions = "Use turbo build --concurrency=4."
	t.Run("prompt turns keep instructions separate from the prompt", func(t *testing.T) {
		srv, ch := newReplayServer()
		t.Cleanup(srv.Close)
		h := replayHarness(t, claudecode.Settings{})
		sess, start := replayPrompt(t, h, newRecSandbox(srv), harness.StartOptions{}, harness.PromptTurnOptions{
			Prompt: harness.TextPrompt("first turn"), TurnSettings: harness.TurnSettings{Instructions: instructions},
		}, ch)
		if start["prompt"] != "first turn" || start["instructions"] != instructions {
			t.Fatalf("first start = %v", start)
		}
		_ = sess
	})
	t.Run("resumed session", func(t *testing.T) {
		srv, ch := newReplayServer()
		t.Cleanup(srv.Close)
		h := replayHarness(t, claudecode.Settings{})
		resume, _ := harness.NewResumeSessionState(claudecode.HarnessID, map[string]any{})
		_, start := replayPrompt(t, h, newRecSandbox(srv), harness.StartOptions{ResumeFrom: resume}, harness.PromptTurnOptions{
			Prompt: harness.TextPrompt("resumed turn"), TurnSettings: harness.TurnSettings{Instructions: instructions},
		}, ch)
		if start["prompt"] != "resumed turn" || start["instructions"] != instructions {
			t.Fatalf("start = %v", start)
		}
	})
	t.Run("rerun of a suspended turn forwards instructions", func(t *testing.T) {
		srv, ch := newReplayServer()
		t.Cleanup(srv.Close)
		h := replayHarness(t, claudecode.Settings{})
		cont, _ := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{})
		sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: newRecSandbox(srv), SessionWorkDir: "/w", ContinueFrom: cont})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sess.DoDestroy(context.Background()) })
		if _, err := sess.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{TurnSettings: harness.TurnSettings{Instructions: instructions}, Emit: func(harness.StreamPart) {}}); err != nil {
			t.Fatal(err)
		}
		start := <-ch
		if start["prompt"] != "Continue." || start["instructions"] != instructions || start["continue"] != true {
			t.Fatalf("start = %v", start)
		}
	})
}

// TS 'distinguishes parked session resume from suspended turn continuation':
// replayed events of an attached continuation reach the first turn's listeners
// in arrival order (the listener attachment is released only once they are
// registered).
func TestTSReplay_AttachedContinuationDeliversReplayInOrder(t *testing.T) {
	srv := bridgetest.NewServer(bridgetest.Options{Token: "tok", OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
		turn.Emit(map[string]any{"type": "text-start", "id": "m"})
		turn.Emit(map[string]any{"type": "text-delta", "id": "m", "delta": "hello"})
		turn.Emit(map[string]any{"type": "text-end", "id": "m"})
		turn.Emit(map[string]any{
			"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "stop"},
			"totalUsage": map[string]any{"inputTokens": map[string]any{"total": 1}, "outputTokens": map[string]any{"total": 1}},
		})
	}})
	t.Cleanup(srv.Close)
	sandbox := newRecSandbox(srv)
	h := replayHarness(t, claudecode.Settings{})
	first, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := first.DoPromptTurn(context.Background(), harness.PromptTurnOptions{Prompt: harness.TextPrompt("go"), Emit: func(harness.StreamPart) {}})
	if err != nil {
		t.Fatal(err)
	}
	<-c.Done()
	// A fresh process attaches to the still-running bridge at cursor 0.
	cont, _ := harness.NewContinueTurnState(claudecode.HarnessID, map[string]any{
		"bridge": map[string]any{"port": 4319, "token": "tok", "lastSeenEventId": 0},
	})
	attached, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "s1", SandboxSession: sandbox, SessionWorkDir: "/w", ContinueFrom: cont})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = attached.DoDestroy(context.Background()) })
	var mu sync.Mutex
	var types []string
	control, err := attached.DoContinueTurn(context.Background(), harness.ContinueTurnOptions{Emit: func(p harness.StreamPart) {
		mu.Lock()
		types = append(types, p.PartType())
		mu.Unlock()
	}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-control.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("replayed turn never finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(types, []string{"text-start", "text-delta", "text-end", "finish"}) {
		t.Fatalf("replayed parts = %v", types)
	}
}
