package grokbuild

import (
	"context"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// r88toolsSandbox is TS grok-build-harness.test.ts `preparationSandbox`.
type r88toolsSandbox struct {
	mu       sync.Mutex
	grokHome string
	homeDir  string
	files    map[string]string
	commands []string
	writes   []string
	deletes  []string

	failRead, failWrite, failCreate, failReplace, failHome bool
}

const r88toolsWorkDir = "/workspace/grok"

func r88toolsNewSandbox(grokHome, homeDir string, content *string) (*r88toolsSandbox, string) {
	if homeDir == "" {
		homeDir = "/home/agent"
	}
	dir := grokHome
	if dir == "" {
		dir = homeDir + "/.grok"
	}
	configPath := posixResolve(r88toolsWorkDir, dir) + "/config.toml"
	s := &r88toolsSandbox{grokHome: grokHome, homeDir: homeDir, files: map[string]string{}}
	if content != nil {
		s.files[configPath] = *content
	}
	return s, configPath
}

var r88toolsMove = regexp.MustCompile(` && mv -f '([^']+)' '([^']+)'$`)

func (s *r88toolsSandbox) Description() string { return "r88 grok sandbox" }
func (s *r88toolsSandbox) Run(ctx context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	if err := ctx.Err(); err != nil {
		return providerutils.SandboxRunResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commands = append(s.commands, opts.Command)
	switch {
	case opts.Command == `printf "%s" "${GROK_HOME:-}"`:
		if s.failHome {
			return providerutils.SandboxRunResult{ExitCode: 1, Stderr: "Resolution failed."}, nil
		}
		return providerutils.SandboxRunResult{Stdout: s.grokHome}, nil
	case opts.Command == `printf "%s" "$HOME"`:
		return providerutils.SandboxRunResult{Stdout: s.homeDir}, nil
	case s.failCreate && strings.HasPrefix(opts.Command, "mkdir -p "),
		s.failReplace && strings.HasPrefix(opts.Command, "chmod 600 "):
		return providerutils.SandboxRunResult{ExitCode: 1, Stderr: "Filesystem failure."}, nil
	}
	if m := r88toolsMove.FindStringSubmatch(opts.Command); m != nil {
		s.files[m[2]] = s.files[m[1]]
		delete(s.files, m[1])
	}
	return providerutils.SandboxRunResult{}, nil
}
func (s *r88toolsSandbox) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("unused")
}
func (s *r88toolsSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (s *r88toolsSandbox) ReadBinaryFile(context.Context, string) ([]byte, error)  { return nil, nil }
func (s *r88toolsSandbox) ReadTextFile(_ context.Context, opts providerutils.SandboxReadTextFileOptions) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRead {
		return nil, errors.New("Sandbox filesystem failure.")
	}
	v, ok := s.files[opts.Path]
	if !ok {
		return nil, nil
	}
	return &v, nil
}
func (s *r88toolsSandbox) WriteFile(context.Context, string, io.Reader) error    { return nil }
func (s *r88toolsSandbox) WriteBinaryFile(context.Context, string, []byte) error { return nil }
func (s *r88toolsSandbox) WriteTextFile(_ context.Context, opts providerutils.SandboxWriteTextFileOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, opts.Path)
	if s.failWrite {
		return errors.New("Sandbox filesystem failure.")
	}
	s.files[opts.Path] = opts.Content
	return nil
}
func (s *r88toolsSandbox) DeleteFile(_ context.Context, p string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, p)
	_, ok := s.files[p]
	delete(s.files, p)
	return ok, nil
}

func r88toolsPrepare(ctx context.Context, s *r88toolsSandbox) error {
	return PrepareSandbox(ctx, acp.PrepareSandboxOptions{Sandbox: s, WorkDir: r88toolsWorkDir})
}

func r88toolsParse(t *testing.T, content string) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := toml.Unmarshal([]byte(content), &out); err != nil {
		t.Fatalf("written TOML does not parse: %v\n%s", err, content)
	}
	return out
}

func r88toolsStr(s string) *string { return &s }

func TestR88toolsGrokBuildConfigWiresPrepareSandbox(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PrepareSandbox == nil || cfg.PermissionModeMapping != nil {
		t.Fatalf("PrepareSandbox set %v, PermissionModeMapping %v", cfg.PrepareSandbox != nil, cfg.PermissionModeMapping)
	}
}

// "creates the native read ask rule when configuration is absent"
func TestR88toolsGrokBuildPrepareCreatesReadAsk(t *testing.T) {
	s, configPath := r88toolsNewSandbox("", "", nil)
	if err := r88toolsPrepare(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if got := r88toolsParse(t, s.files[configPath]); !reflect.DeepEqual(got, map[string]any{"permission": map[string]any{"ask": []any{"Read"}}}) {
		t.Fatalf("config = %#v", got)
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v", s.writes)
	}
	tmp := s.writes[0]
	if !regexp.MustCompile(`^/home/agent/\.grok/config\.toml\.[\da-f-]+\.tmp$`).MatchString(tmp) {
		t.Fatalf("temporary path = %q", tmp)
	}
	wantCreate := "mkdir -p '/home/agent/.grok' && (umask 077; set -C; : > '" + tmp + "')"
	wantReplace := "chmod 600 '" + tmp + "' && mv -f '" + tmp + "' '" + configPath + "'"
	joined := strings.Join(s.commands, "\n")
	if !strings.Contains(joined, wantCreate) || !strings.Contains(joined, wantReplace) {
		t.Fatalf("commands = %q", s.commands)
	}
	if len(s.deletes) != 1 || s.deletes[0] != tmp {
		t.Fatalf("deletes = %v", s.deletes)
	}
	if _, ok := s.files[tmp]; ok {
		t.Fatal("temporary file left behind")
	}
}

// "preserves native settings and permission rules"
func TestR88toolsGrokBuildPreparePreservesSettings(t *testing.T) {
	s, configPath := r88toolsNewSandbox("", "", r88toolsStr(strings.Join([]string{
		`model = "grok-build-0.1"`, `[permission]`, `ask = ["Bash"]`, `allow = ["Read(/workspace/**)"]`,
		`deny = ["Read(/private/**)"]`, `[mcp_servers.example]`, `command = "example-mcp"`,
	}, "\n")))
	if err := r88toolsPrepare(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"mcp_servers": map[string]any{"example": map[string]any{"command": "example-mcp"}},
		"model":       "grok-build-0.1",
		"permission": map[string]any{
			"allow": []any{"Read(/workspace/**)"}, "ask": []any{"Bash", "Read"}, "deny": []any{"Read(/private/**)"},
		},
	}
	if got := r88toolsParse(t, s.files[configPath]); !reflect.DeepEqual(got, want) {
		t.Fatalf("config = %#v", got)
	}
}

// "does not rewrite configuration that already asks for reads" and "is
// idempotent across repeated preparation calls"
func TestR88toolsGrokBuildPrepareIdempotent(t *testing.T) {
	content := "# native configuration\n[permission]\nask = [\"Read\", \"Bash\"]\n"
	s, configPath := r88toolsNewSandbox("", "", &content)
	if err := r88toolsPrepare(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if s.files[configPath] != content || len(s.writes) != 0 {
		t.Fatalf("rewrote: %q, writes %v", s.files[configPath], s.writes)
	}

	s, _ = r88toolsNewSandbox("", "", r88toolsStr(`model = "grok"`))
	for i := 0; i < 2; i++ {
		if err := r88toolsPrepare(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.writes) != 1 {
		t.Fatalf("writes = %v", s.writes)
	}
}

// "resolves native configuration at $expected"
func TestR88toolsGrokBuildPrepareConfigLocation(t *testing.T) {
	for _, tc := range []struct{ grokHome, homeDir, want string }{
		{"/custom/grok home", "/home/agent", "/custom/grok home/config.toml"},
		{".native-grok", "/home/agent", "/workspace/grok/.native-grok/config.toml"},
		{"", "/custom/home", "/custom/home/.grok/config.toml"},
	} {
		s, _ := r88toolsNewSandbox(tc.grokHome, tc.homeDir, nil)
		if err := r88toolsPrepare(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		if _, ok := s.files[tc.want]; !ok {
			t.Fatalf("%q: files = %v", tc.grokHome, s.files)
		}
		if tc.grokHome != "" {
			for _, c := range s.commands {
				if c == `printf "%s" "$HOME"` {
					t.Fatalf("%q: resolved $HOME although GROK_HOME is set", tc.grokHome)
				}
			}
		}
	}
}

// "rejects invalid configuration without overwriting it"
func TestR88toolsGrokBuildPrepareRejectsInvalidConfig(t *testing.T) {
	for _, content := range []string{
		"secret = \"sensitive-value\"\n[permission",
		`permission = "sensitive-value"`,
		`permission = []`,
		`permission = 2026-01-01T00:00:00Z`,
		`permission = 2026-01-01`,
		`permission = 07:32:00`,
		"[[permission]]\nask = [\"Bash\"]",
		"[permission]\nask = \"Read\"",
		"[permission]\nask = [123]",
	} {
		s, configPath := r88toolsNewSandbox("", "", &content)
		err := r88toolsPrepare(context.Background(), s)
		if err == nil || !strings.HasPrefix(err.Error(), "Grok Build configuration ") {
			t.Fatalf("%q: err = %v", content, err)
		}
		if strings.Contains(err.Error(), "sensitive-value") {
			t.Fatalf("%q: error leaks the configuration: %v", content, err)
		}
		if s.files[configPath] != content || len(s.writes) != 0 {
			t.Fatalf("%q: overwritten", content)
		}
	}
	for content, want := range map[string]string{
		"secret = \"x\"\n[permission":    "Grok Build configuration must contain valid TOML.",
		`permission = 1`:                 "Grok Build configuration permission must be a table.",
		"[permission]\nask = [\"a\", 1]": "Grok Build configuration permission.ask must be a string array.",
	} {
		s, _ := r88toolsNewSandbox("", "", &content)
		if err := r88toolsPrepare(context.Background(), s); err == nil || err.Error() != want {
			t.Fatalf("%q: err = %v, want %q", content, err, want)
		}
	}
}

// "preserves the original configuration when the %s operation fails"
func TestR88toolsGrokBuildPrepareFailuresPreserveConfig(t *testing.T) {
	for _, op := range []string{"read", "create", "write", "replace"} {
		content := `model = "grok"`
		s, configPath := r88toolsNewSandbox("", "", &content)
		switch op {
		case "read":
			s.failRead = true
		case "create":
			s.failCreate = true
		case "write":
			s.failWrite = true
		case "replace":
			s.failReplace = true
		}
		err := r88toolsPrepare(context.Background(), s)
		if err == nil {
			t.Fatalf("%s: expected an error", op)
		}
		if op == "create" && err.Error() != "Unable to create the temporary Grok Build configuration." {
			t.Fatalf("create: err = %v", err)
		}
		if op == "replace" && err.Error() != "Unable to replace the Grok Build configuration." {
			t.Fatalf("replace: err = %v", err)
		}
		if s.files[configPath] != content {
			t.Fatalf("%s: config changed to %q", op, s.files[configPath])
		}
		wantDeletes := 0
		if op == "write" || op == "replace" {
			wantDeletes = 1
		}
		if len(s.deletes) != wantDeletes {
			t.Fatalf("%s: deletes = %v", op, s.deletes)
		}
	}
}

// "propagates cancellation without writing configuration" and "rejects
// failed configuration-directory resolution without writing files"
func TestR88toolsGrokBuildPrepareCancellationAndResolution(t *testing.T) {
	s, _ := r88toolsNewSandbox("", "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r88toolsPrepare(ctx, s); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(s.commands) != 0 || len(s.writes) != 0 {
		t.Fatalf("commands %v writes %v", s.commands, s.writes)
	}

	s, _ = r88toolsNewSandbox("", "", nil)
	s.failHome = true
	if err := r88toolsPrepare(context.Background(), s); err == nil || err.Error() != "Unable to resolve the Grok Build configuration directory." {
		t.Fatalf("err = %v", err)
	}
	if len(s.writes) != 0 {
		t.Fatalf("writes = %v", s.writes)
	}
}

// TS e4a6112776 grok-build-harness.test.ts: repository-owned hooks and MCP
// servers are disabled through the agent environment, and the settings
// pass the acp ForwardEnv / CredentialEnv / Env overlap validation.
func TestR88toolsGrokBuildDisablesRepositoryHooksAndMCPs(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"GROK_CLAUDE_HOOKS_ENABLED": "false", "GROK_CURSOR_HOOKS_ENABLED": "false",
		"GROK_CLAUDE_MCPS_ENABLED": "false", "GROK_CURSOR_MCPS_ENABLED": "false",
	}
	if !reflect.DeepEqual(cfg.Env, want) {
		t.Fatalf("Env = %v", cfg.Env)
	}
	if _, err := CreateGrokBuild(); err != nil {
		t.Fatalf("CreateGrokBuild: %v", err)
	}
}

// TS a54f0fd8c6 grok-build-harness.test.ts: hostDisconnectTimeoutMs is
// forwarded to createACP.
func TestR88toolsGrokBuildHostDisconnectTimeout(t *testing.T) {
	ms := 60000
	cfg, err := BuildConfig(Settings{HostDisconnectTimeoutMS: &ms})
	if err != nil || cfg.HostDisconnectTimeout != 60*time.Second {
		t.Fatalf("HostDisconnectTimeout = %v, %v", cfg.HostDisconnectTimeout, err)
	}
	if cfg, _ := BuildConfig(Settings{}); cfg.HostDisconnectTimeout != 0 {
		t.Fatalf("unset HostDisconnectTimeout = %v", cfg.HostDisconnectTimeout)
	}
}
