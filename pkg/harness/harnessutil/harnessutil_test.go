package harnessutil

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// memSandbox mirrors the in-memory makeSandbox() helpers of the TS
// write-skills / write-instructions tests.
type memSandbox struct {
	mu          sync.Mutex
	files       map[string]string
	directories map[string]bool
	runs        []string
	writes      []providerutils.SandboxWriteTextFileOptions
}

var quoted = regexp.MustCompile(`'([^']*)'`)

func newMemSandbox() *memSandbox {
	return &memSandbox{files: map[string]string{}, directories: map[string]bool{}}
}

func quotedValues(cmd string) []string {
	var out []string
	for _, m := range quoted.FindAllStringSubmatch(cmd, -1) {
		out = append(out, m[1])
	}
	return out
}

var skillDirRE = regexp.MustCompile(`^(.+/skills/[^/]+)/`)

func (m *memSandbox) Description() string { return "mem" }
func (m *memSandbox) Run(_ context.Context, o providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs = append(m.runs, o.Command)
	switch {
	case strings.HasPrefix(o.Command, "test ! -e "):
		target := quotedValues(o.Command)[0]
		exists := m.directories[target]
		for p := range m.files {
			if strings.HasPrefix(p, target+"/") {
				exists = true
			}
		}
		if exists {
			return providerutils.SandboxRunResult{ExitCode: 1}, nil
		}
	case strings.HasPrefix(o.Command, "mv -f "):
		v := quotedValues(o.Command)
		m.files[v[1]] = m.files[v[0]]
		delete(m.files, v[0])
	case strings.HasPrefix(o.Command, "rm -rf -- "), strings.HasPrefix(o.Command, "rm -f -- "):
		for _, target := range quotedValues(o.Command) {
			delete(m.directories, target)
			for p := range m.files {
				if p == target || strings.HasPrefix(p, target+"/") {
					delete(m.files, p)
				}
			}
		}
	}
	return providerutils.SandboxRunResult{}, nil
}
func (m *memSandbox) Spawn(context.Context, providerutils.SandboxProcessOptions) (providerutils.SandboxProcess, error) {
	return nil, errors.New("unsupported")
}
func (m *memSandbox) ReadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("unsupported")
}
func (m *memSandbox) ReadBinaryFile(context.Context, string) ([]byte, error) {
	return nil, errors.New("unsupported")
}
func (m *memSandbox) ReadTextFile(_ context.Context, o providerutils.SandboxReadTextFileOptions) (*string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.files[o.Path]; ok {
		return &c, nil
	}
	return nil, nil
}
func (m *memSandbox) WriteFile(context.Context, string, io.Reader) error {
	return errors.New("unsupported")
}
func (m *memSandbox) WriteBinaryFile(context.Context, string, []byte) error {
	return errors.New("unsupported")
}
func (m *memSandbox) WriteTextFile(_ context.Context, o providerutils.SandboxWriteTextFileOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writes = append(m.writes, o)
	m.files[o.Path] = o.Content
	if sm := skillDirRE.FindStringSubmatch(o.Path); sm != nil {
		m.directories[sm[1]] = true
	}
	return nil
}

var demoSkill = harness.Skill{
	Name:        "demo",
	Description: "Demo skill.",
	Content:     "Use reference.md.",
	Files:       []harness.SkillFile{{Path: "reference.md", Content: "# Reference"}},
}

const manifestPath = "/home/user/.agents/skills/.ai-sdk-harness-skills.json"

func writeSkills(t *testing.T, sb *memSandbox, skills []harness.Skill, mutate ...func(*WriteSkillsOptions)) (*WriteSkillsResult, error) {
	t.Helper()
	opts := WriteSkillsOptions{Sandbox: sb, HomePath: "/home/user", SkillsDir: ".agents/skills", Skills: skills}
	for _, m := range mutate {
		m(&opts)
	}
	return WriteSkills(context.Background(), opts)
}

func TestWriteSkills(t *testing.T) {
	t.Run("writes skills and a completed ownership manifest", func(t *testing.T) {
		sb := newMemSandbox()
		res, err := writeSkills(t, sb, []harness.Skill{demoSkill})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*res, WriteSkillsResult{Changed: true, Written: []string{"demo"}, Removed: []string{}, Unchanged: []string{}}) {
			t.Fatalf("%+v", res)
		}
		if sb.files["/home/user/.agents/skills/demo/SKILL.md"] != "---\nname: demo\ndescription: Demo skill.\n---\n\nUse reference.md." {
			t.Fatal(sb.files["/home/user/.agents/skills/demo/SKILL.md"])
		}
		if sb.files["/home/user/.agents/skills/demo/reference.md"] != "# Reference" {
			t.Fatal("reference.md")
		}
		// Hash from the TS inline snapshot.
		want := "{\n  \"version\": 1,\n  \"state\": \"complete\",\n  \"skills\": [\n    {\n      \"name\": \"demo\",\n      \"hash\": \"c14ffcc71e9d9c39fb21765d4ac61acd2fc428e973a224e6f20d76f23aa3954b\"\n    }\n  ]\n}\n"
		if sb.files[manifestPath] != want {
			t.Fatalf("manifest = %q", sb.files[manifestPath])
		}
	})

	t.Run("does not write or delete anything when all skill hashes match", func(t *testing.T) {
		sb := newMemSandbox()
		_, _ = writeSkills(t, sb, []harness.Skill{demoSkill})
		writes, runs := len(sb.writes), len(sb.runs)
		res, err := writeSkills(t, sb, []harness.Skill{demoSkill})
		if err != nil || !reflect.DeepEqual(*res, WriteSkillsResult{Changed: false, Written: []string{}, Removed: []string{}, Unchanged: []string{"demo"}}) {
			t.Fatalf("%+v %v", res, err)
		}
		if len(sb.writes) != writes || len(sb.runs) != runs {
			t.Fatal("no I/O expected")
		}
	})

	t.Run("replaces changed skills and removes only manifest-owned skills", func(t *testing.T) {
		sb := newMemSandbox()
		sb.directories["/home/user/.agents/skills/external"] = true
		sb.files["/home/user/.agents/skills/external/SKILL.md"] = "external"
		_, _ = writeSkills(t, sb, []harness.Skill{demoSkill, {Name: "old", Description: "Old.", Content: "Old."}})
		changed := demoSkill
		changed.Content = "Changed."
		res, err := writeSkills(t, sb, []harness.Skill{changed})
		if err != nil || !reflect.DeepEqual(*res, WriteSkillsResult{Changed: true, Written: []string{"demo"}, Removed: []string{"old"}, Unchanged: []string{}}) {
			t.Fatalf("%+v %v", res, err)
		}
		if !strings.Contains(sb.files["/home/user/.agents/skills/demo/SKILL.md"], "Changed.") {
			t.Fatal("demo not rewritten")
		}
		if _, ok := sb.files["/home/user/.agents/skills/old/SKILL.md"]; ok {
			t.Fatal("old not removed")
		}
		if sb.files["/home/user/.agents/skills/external/SKILL.md"] != "external" {
			t.Fatal("external skill must be preserved")
		}
	})

	t.Run("sorts result and manifest entries by skill name", func(t *testing.T) {
		sb := newMemSandbox()
		res, _ := writeSkills(t, sb, []harness.Skill{{Name: "zeta", Description: "Z.", Content: "Z."}, {Name: "alpha", Description: "A.", Content: "A."}})
		if !reflect.DeepEqual(res.Written, []string{"alpha", "zeta"}) {
			t.Fatal(res.Written)
		}
		var m skillsManifest
		_ = json.Unmarshal([]byte(sb.files[manifestPath]), &m)
		if m.Skills[0].Name != "alpha" || m.Skills[1].Name != "zeta" {
			t.Fatal(m.Skills)
		}
	})

	t.Run("rejects collisions with skill directories not owned by the manifest", func(t *testing.T) {
		sb := newMemSandbox()
		sb.directories["/home/user/.agents/skills/demo"] = true
		if _, err := writeSkills(t, sb, []harness.Skill{demoSkill}); err == nil || !strings.Contains(err.Error(), "already exists and is not owned") {
			t.Fatal(err)
		}
		if _, ok := sb.files[manifestPath]; ok {
			t.Fatal("manifest must not be written")
		}
	})

	t.Run("recovers a pending update by clearing every recorded directory", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[manifestPath] = `{"version":1,"state":"pending","skills":[{"name":"demo","hash":"` + strings.Repeat("a", 64) + `"},{"name":"old","hash":"` + strings.Repeat("b", 64) + `"}]}`
		sb.files["/home/user/.agents/skills/demo/partial.txt"] = "partial"
		sb.files["/home/user/.agents/skills/old/SKILL.md"] = "old"
		res, err := writeSkills(t, sb, []harness.Skill{demoSkill})
		if err != nil || !reflect.DeepEqual(*res, WriteSkillsResult{Changed: true, Written: []string{"demo"}, Removed: []string{"old"}, Unchanged: []string{}}) {
			t.Fatalf("%+v %v", res, err)
		}
		for path, want := range map[string]bool{
			"/home/user/.agents/skills/demo/partial.txt": false,
			"/home/user/.agents/skills/old/SKILL.md":     false,
			"/home/user/.agents/skills/demo/SKILL.md":    true,
		} {
			if _, ok := sb.files[path]; ok != want {
				t.Errorf("%s present=%v", path, ok)
			}
		}
	})

	t.Run("validates all skill paths before writing", func(t *testing.T) {
		sb := newMemSandbox()
		bad := demoSkill
		bad.Files = []harness.SkillFile{{Path: "../reference.md", Content: "# Reference"}}
		if _, err := writeSkills(t, sb, []harness.Skill{bad}); err == nil || !strings.Contains(err.Error(), "Invalid skill file path") {
			t.Fatal(err)
		}
		if len(sb.runs) != 0 || len(sb.writes) != 0 {
			t.Fatal("nothing may be written")
		}
	})

	t.Run("supports stricter skill-name patterns", func(t *testing.T) {
		_, err := writeSkills(t, newMemSandbox(), []harness.Skill{{Name: "Demo", Description: "Demo.", Content: "Demo."}}, func(o *WriteSkillsOptions) {
			o.SkillNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
			o.InvalidSkillNameMessage = func(name string) string {
				return "Invalid deepagents skill name '" + name + "': must be lowercase alphanumeric with hyphens, 1-64 chars."
			}
		})
		if err == nil || !strings.Contains(err.Error(), "Invalid deepagents skill name 'Demo'") {
			t.Fatal(err)
		}
	})

	t.Run("can strip leading slashes from attached file paths", func(t *testing.T) {
		sb := newMemSandbox()
		_, err := writeSkills(t, sb, []harness.Skill{{Name: "demo", Description: "Demo.", Content: "Demo.", Files: []harness.SkillFile{{Path: "/reference.md", Content: "# Reference"}}}}, func(o *WriteSkillsOptions) {
			o.FilePathMode = SkillFilePathStripLeadingSlashes
		})
		if err != nil || sb.files["/home/user/.agents/skills/demo/reference.md"] != "# Reference" {
			t.Fatal(err)
		}
	})

	t.Run("rejects non-absolute or empty homePath", func(t *testing.T) {
		_, err := writeSkills(t, newMemSandbox(), []harness.Skill{demoSkill}, func(o *WriteSkillsOptions) { o.HomePath = "relative/home" })
		if err == nil || err.Error() != `Invalid homePath "relative/home": expected an absolute POSIX path.` {
			t.Fatal(err)
		}
		_, err = writeSkills(t, newMemSandbox(), []harness.Skill{demoSkill}, func(o *WriteSkillsOptions) { o.HomePath = "   " })
		if err == nil || err.Error() != "Invalid homePath: expected a non-empty string." {
			t.Fatal(err)
		}
	})

	for _, dir := range []string{"/skills", `C:\skills`, `skills\sub`, "../skills", "foo/../../bar", "", "   ", ".", "./"} {
		_, err := writeSkills(t, newMemSandbox(), []harness.Skill{demoSkill}, func(o *WriteSkillsOptions) { o.SkillsDir = dir })
		if err == nil || !strings.Contains(err.Error(), "expected a relative POSIX path without traversal") {
			t.Errorf("skillsDir %q: %v", dir, err)
		}
	}
}

// skill hashes generated by ../testdata/gen_identity_fixtures.mjs running the
// TS algorithm (includes non-ASCII names and localeCompare ordering).
func TestSkillHashParityWithTS(t *testing.T) {
	data, err := os.ReadFile("../testdata/identity_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Skills []struct {
			Skill harness.Skill `json:"skill"`
			Hash  string        `json:"hash"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	permissive := regexp.MustCompile(`^.+$`)
	for _, tc := range f.Skills {
		p, err := projectSkill(tc.Skill, permissive, func(string) string { return "bad" }, SkillFilePathRelative, func(string, string) string { return "bad" }, false)
		if err != nil || p.hash != tc.Hash {
			t.Errorf("skill %s hash = %s, want TS %s (%v)", tc.Skill.Name, p.hash, tc.Hash, err)
		}
	}
}

func writeInstr(t *testing.T, sb *memSandbox, file, instructions string) *WriteInstructionsResult {
	t.Helper()
	res, err := WriteInstructions(context.Background(), WriteInstructionsOptions{Sandbox: sb, HomePath: "/home/user", InstructionsFile: file, Instructions: instructions})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestWriteInstructions(t *testing.T) {
	const agents = "/home/user/AGENTS.md"
	const meta = "/home/user/.AGENTS.md.ai-sdk-harness-instructions.json"

	t.Run("writes to a non-existent file and creates companion metadata", func(t *testing.T) {
		sb := newMemSandbox()
		res := writeInstr(t, sb, "AGENTS.md", "Always run tests.")
		if *res != (WriteInstructionsResult{Changed: true, FilePath: agents}) || sb.files[agents] != "Always run tests.\n" {
			t.Fatal(res, sb.files[agents])
		}
		want := "{\n  \"version\": 1,\n  \"originalContent\": null,\n  \"instructions\": \"Always run tests.\",\n  \"appliedContent\": \"Always run tests.\\n\"\n}\n"
		if sb.files[meta] != want {
			t.Fatalf("%q", sb.files[meta])
		}
	})

	t.Run("appends to an existing file without overriding content", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[agents] = "# Project Guidelines\n\nBe thorough."
		writeInstr(t, sb, "AGENTS.md", "Always run tests.")
		if sb.files[agents] != "# Project Guidelines\n\nBe thorough.\n\nAlways run tests.\n" {
			t.Fatal(sb.files[agents])
		}
		var m instructionsMetadata
		_ = json.Unmarshal([]byte(sb.files[meta]), &m)
		if m.OriginalContent == nil || *m.OriginalContent != "# Project Guidelines\n\nBe thorough." {
			t.Fatal(m)
		}
	})

	t.Run("replaces previous instructions", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[agents] = "# Base"
		writeInstr(t, sb, "AGENTS.md", "First instructions.")
		writeInstr(t, sb, "AGENTS.md", "Updated instructions.")
		if sb.files[agents] != "# Base\n\nUpdated instructions.\n" {
			t.Fatal(sb.files[agents])
		}
	})

	t.Run("idempotent call returns changed false with zero writes", func(t *testing.T) {
		sb := newMemSandbox()
		writeInstr(t, sb, "AGENTS.md", "Always run tests.")
		writes := len(sb.writes)
		if res := writeInstr(t, sb, "AGENTS.md", "Always run tests."); res.Changed || len(sb.writes) != writes {
			t.Fatal("expected no-op")
		}
	})

	t.Run("clearing restores pre-existing content and deletes metadata", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[agents] = "# Base\n"
		writeInstr(t, sb, "AGENTS.md", "Always run tests.")
		res := writeInstr(t, sb, "AGENTS.md", "")
		if !res.Changed || sb.files[agents] != "# Base\n" {
			t.Fatal(sb.files[agents])
		}
		if _, ok := sb.files[meta]; ok {
			t.Fatal("metadata must be removed")
		}
	})

	t.Run("clearing on a harness-created file removes file and metadata", func(t *testing.T) {
		sb := newMemSandbox()
		writeInstr(t, sb, "AGENTS.md", "Temporary instructions.")
		writeInstr(t, sb, "AGENTS.md", "  ")
		if _, ok := sb.files[agents]; ok {
			t.Fatal("file must be removed")
		}
		if _, ok := sb.files[meta]; ok {
			t.Fatal("metadata must be removed")
		}
	})

	t.Run("clearing without metadata is a no-op", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[agents] = "# Unmanaged"
		if res := writeInstr(t, sb, "AGENTS.md", ""); res.Changed || sb.files[agents] != "# Unmanaged" {
			t.Fatal(res)
		}
	})

	t.Run("preserves external edits made while instructions were active", func(t *testing.T) {
		sb := newMemSandbox()
		sb.files[agents] = "# Base\n"
		writeInstr(t, sb, "AGENTS.md", "First instructions.")
		sb.files[agents] = "# Base\n\n# User Section\n\nFirst instructions.\n"
		writeInstr(t, sb, "AGENTS.md", "Second instructions.")
		if sb.files[agents] != "# Base\n\n# User Section\n\nSecond instructions.\n" {
			t.Fatal(sb.files[agents])
		}
		writeInstr(t, sb, "AGENTS.md", "")
		if sb.files[agents] != "# Base\n\n# User Section\n" {
			t.Fatal(sb.files[agents])
		}
	})

	t.Run("manages multiple and nested targets independently", func(t *testing.T) {
		sb := newMemSandbox()
		writeInstr(t, sb, "AGENTS.md", "Codex instructions.")
		writeInstr(t, sb, "CLAUDE.md", "Claude instructions.")
		writeInstr(t, sb, "CLAUDE.md", "")
		if _, ok := sb.files["/home/user/CLAUDE.md"]; ok {
			t.Fatal("CLAUDE.md must be removed")
		}
		if sb.files[agents] != "Codex instructions.\n" {
			t.Fatal(sb.files[agents])
		}
		res := writeInstr(t, sb, ".claude/CLAUDE.md", "Nested instructions.")
		if res.FilePath != "/home/user/.claude/CLAUDE.md" {
			t.Fatal(res.FilePath)
		}
		if _, ok := sb.files["/home/user/.claude/.CLAUDE.md.ai-sdk-harness-instructions.json"]; !ok {
			t.Fatal("nested metadata missing")
		}
	})

	t.Run("rejects invalid paths", func(t *testing.T) {
		_, err := WriteInstructions(context.Background(), WriteInstructionsOptions{Sandbox: newMemSandbox(), HomePath: "", InstructionsFile: "AGENTS.md", Instructions: "Test"})
		if err == nil || err.Error() != "Invalid homePath: expected a non-empty string." {
			t.Fatal(err)
		}
		for _, file := range []string{"/AGENTS.md", `C:\AGENTS.md`, `sub\AGENTS.md`, "../AGENTS.md", "foo/../../bar.md", "", "   ", ".", "./", "sub/", "sub/.", "sub/.."} {
			_, err := WriteInstructions(context.Background(), WriteInstructionsOptions{Sandbox: newMemSandbox(), HomePath: "/home/user", InstructionsFile: file, Instructions: "Test"})
			if err == nil || !strings.Contains(err.Error(), "expected a relative POSIX path without traversal") {
				t.Errorf("%q: %v", file, err)
			}
		}
	})
}

func TestGetAIGatewayAuthFromEnv(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want AIGatewayAuth
	}{
		{map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "VERCEL_OIDC_TOKEN": "oidc-token"}, AIGatewayAuth{"gateway-key", DefaultAIGatewayBaseURL}},
		{map[string]string{"AI_GATEWAY_API_KEY": "gateway-key", "AI_GATEWAY_BASE_URL": "https://gateway.example.com"}, AIGatewayAuth{"gateway-key", "https://gateway.example.com"}},
		{map[string]string{"VERCEL_OIDC_TOKEN": "oidc-token"}, AIGatewayAuth{"oidc-token", DefaultAIGatewayBaseURL}},
		{map[string]string{}, AIGatewayAuth{"", DefaultAIGatewayBaseURL}},
	}
	for _, tc := range cases {
		if got := GetAIGatewayAuthFromEnv(tc.env); got != tc.want {
			t.Errorf("%v => %+v", tc.env, got)
		}
	}
}

func TestShouldResolveNativeSubscription(t *testing.T) {
	cases := []struct {
		auth   string
		env    map[string]string
		direct bool
		want   bool
	}{
		{"", nil, false, true},
		{"auto", nil, false, true},
		{"auto", map[string]string{"AI_GATEWAY_API_KEY": "k"}, false, false},
		{"", map[string]string{"VERCEL_OIDC_TOKEN": "t"}, false, false},
		{"direct", map[string]string{"AI_GATEWAY_API_KEY": "k"}, false, true},
		{"ai-gateway", nil, false, false},
		{"direct", nil, true, false},
	}
	for _, tc := range cases {
		if got := ShouldResolveNativeSubscription(tc.auth, tc.env, tc.direct); got != tc.want {
			t.Errorf("%+v => %v", tc, got)
		}
	}
}

func TestAuthenticationEnvironment(t *testing.T) {
	isolated := harness.AuthEnvironment(map[string]string{"AI_GATEWAY_API_KEY": "k"})
	if !IsAuthenticationEnvironment(isolated) || IsAuthenticationEnvironment(harness.AuthMode("auto")) || IsAuthenticationEnvironment(harness.Authentication{}) {
		t.Fatal("IsAuthenticationEnvironment")
	}
	proc := map[string]string{"X": "1"}
	if AuthenticationEnvironment(isolated, proc)["AI_GATEWAY_API_KEY"] != "k" || AuthenticationEnvironment(harness.AuthMode("auto"), proc)["X"] != "1" {
		t.Fatal("isolated env must replace the process env")
	}
}

func TestHeadersAndClientApp(t *testing.T) {
	if ClientApp("claude-code", "1.2.3") != "ai-sdk/harness-claude-code/1.2.3" {
		t.Fatal(ClientApp("claude-code", "1.2.3"))
	}
	h, err := NormalizeHeaders(map[string]string{"X-Trace": "1"})
	if err != nil || h["x-trace"] != "1" {
		t.Fatal(h, err)
	}
	if _, err := NormalizeHeaders(map[string]string{"User-Agent": "x"}); err == nil || err.Error() != "HarnessAgent: `headers` must not include the managed header `user-agent`." {
		t.Fatal(err)
	}
	if h, _ := NormalizeHeaders(nil); h != nil {
		t.Fatal("empty headers normalize to nil")
	}
}

func TestShellQuoteAndOS(t *testing.T) {
	for in, want := range map[string]string{"hello": `'hello'`, "it's": `'it'\''s'`, "": `''`} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %s", in, got)
		}
	}
	if !IsMacOS("darwin") || !IsLinux("linux") || !IsWindows("win32") || !IsWindows("windows") || IsMacOS("linux") {
		t.Fatal("os predicates")
	}
}

func TestCredentialForwarding(t *testing.T) {
	ctx := context.Background()
	env := map[string]string{"API_KEY": "real-secret", "BASE_URL": "https://api.example.com"}
	out, _ := ApplyCredentialForwarding(ctx, CredentialForwardingOptions{Environment: env, CredentialEnvironmentVariables: []string{"API_KEY"}})
	if !reflect.DeepEqual(out, env) {
		t.Fatal(out)
	}

	var calls []harness.CredentialForwardingOptions
	forward := func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
		calls = append(calls, o)
		return "ephemeral-" + o.EnvironmentVariableName, nil
	}
	out, err := ApplyCredentialForwarding(ctx, CredentialForwardingOptions{
		Environment:                    map[string]string{"API_KEY": "real-secret", "SECOND_API_KEY": "SECOND_API_KEY", "BASE_URL": "https://api.example.com"},
		CredentialEnvironmentVariables: []string{"API_KEY", "SECOND_API_KEY", "API_KEY", "MISSING_API_KEY"},
		CredentialForwarding:           forward,
	})
	if err != nil || !reflect.DeepEqual(out, map[string]string{"API_KEY": "ephemeral-API_KEY", "SECOND_API_KEY": "ephemeral-SECOND_API_KEY", "BASE_URL": "https://api.example.com"}) {
		t.Fatal(out, err)
	}
	if len(calls) != 2 || calls[0] != (harness.CredentialForwardingOptions{Credential: "real-secret", EnvironmentVariableName: "API_KEY"}) || calls[1].Credential != "SECOND_API_KEY" {
		t.Fatal(calls)
	}
	_, err = ApplyCredentialForwarding(ctx, CredentialForwardingOptions{
		Environment: map[string]string{"API_KEY": "real-secret"}, CredentialEnvironmentVariables: []string{"API_KEY"},
		CredentialForwarding: func(context.Context, harness.CredentialForwardingOptions) (string, error) {
			return "", errors.New("credential forwarding failed")
		},
	})
	if err == nil || err.Error() != "credential forwarding failed" {
		t.Fatal(err)
	}

	calls = nil
	sandboxEnv, err := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{
		Environment:                    map[string]string{"API_KEY": "real-secret", "SECOND_API_KEY": "second", "BASE_URL": "x"},
		CredentialEnvironmentVariables: []string{"API_KEY", "SECOND_API_KEY"},
		CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
			calls = append(calls, o)
			return "wrapped-" + o.Credential, nil
		},
	})
	if err != nil || len(calls) != 2 {
		t.Fatal(err, calls)
	}
	for _, c := range calls {
		if !IsSandboxCredentialPlaceholder(c.Credential) {
			t.Fatal(c.Credential)
		}
	}
	if !strings.HasPrefix(sandboxEnv["API_KEY"], "wrapped-aisdkhc_") || !strings.HasPrefix(sandboxEnv["SECOND_API_KEY"], "wrapped-aisdkhc_") {
		t.Fatal(sandboxEnv)
	}
	empty, _ := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{Environment: map[string]string{"BASE_URL": "x"}, CredentialEnvironmentVariables: []string{"API_KEY"}})
	if len(empty) != 0 {
		t.Fatal(empty)
	}
}

// TS "resolveSandboxCredentialEnvironment" describe block (credential-forwarding.test.ts).
func TestResolveSandboxCredentialEnvironment(t *testing.T) {
	ctx := context.Background()

	t.Run("reuses only current credentials and creates placeholders for new names", func(t *testing.T) {
		previous := map[string]string{"API_KEY": "saved-placeholder", "REMOVED_API_KEY": "removed-placeholder"}
		var calls []harness.CredentialForwardingOptions
		result, err := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{
			Environment: map[string]string{
				"API_KEY": "rotated-secret", "NEW_API_KEY": "new-secret", "BASE_URL": "https://api.example.com",
			},
			CredentialEnvironmentVariables:       []string{"API_KEY", "NEW_API_KEY", "NEW_API_KEY", "REMOVED_API_KEY"},
			PreviousSandboxCredentialEnvironment: previous,
			CredentialForwarding: func(_ context.Context, o harness.CredentialForwardingOptions) (string, error) {
				calls = append(calls, o)
				return "wrapped-" + o.Credential, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result["API_KEY"] != "saved-placeholder" {
			t.Errorf("API_KEY = %q, want reused saved-placeholder", result["API_KEY"])
		}
		if !strings.HasPrefix(result["NEW_API_KEY"], "wrapped-aisdkhc_") {
			t.Errorf("NEW_API_KEY = %q, want fresh wrapped placeholder", result["NEW_API_KEY"])
		}
		if _, ok := result["REMOVED_API_KEY"]; ok {
			t.Errorf("REMOVED_API_KEY should be absent (not in Environment): %v", result)
		}
		if len(calls) != 1 || calls[0].EnvironmentVariableName != "NEW_API_KEY" {
			t.Fatalf("CredentialForwarding calls = %+v, want exactly one for NEW_API_KEY", calls)
		}
		if !reflect.DeepEqual(previous, map[string]string{"API_KEY": "saved-placeholder", "REMOVED_API_KEY": "removed-placeholder"}) {
			t.Errorf("previous map mutated: %v", previous)
		}
	})

	t.Run("keeps saved credentials with custom formats without forwarding them again", func(t *testing.T) {
		called := false
		result, err := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{
			Environment:                          map[string]string{"GITHUB_TOKEN": "rotated-secret"},
			CredentialEnvironmentVariables:       []string{"GITHUB_TOKEN"},
			PreviousSandboxCredentialEnvironment: map[string]string{"GITHUB_TOKEN": "gho_saved-placeholder"},
			CredentialForwarding: func(context.Context, harness.CredentialForwardingOptions) (string, error) {
				called = true
				return "another-placeholder", nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if result["GITHUB_TOKEN"] != "gho_saved-placeholder" {
			t.Errorf("GITHUB_TOKEN = %q, want reused gho_saved-placeholder", result["GITHUB_TOKEN"])
		}
		if called {
			t.Error("CredentialForwarding should not have been called")
		}
	})

	t.Run("generates placeholders when older lifecycle state has no saved map", func(t *testing.T) {
		result, err := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{
			Environment:                    map[string]string{"API_KEY": "host-secret"},
			CredentialEnvironmentVariables: []string{"API_KEY"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !IsSandboxCredentialPlaceholder(result["API_KEY"]) {
			t.Errorf("API_KEY = %q, want a generated placeholder", result["API_KEY"])
		}
		if strings.Contains(result["API_KEY"], "host-secret") {
			t.Errorf("result leaks the real secret: %v", result)
		}
	})

	t.Run("propagates forwarding failures before returning a partial environment", func(t *testing.T) {
		_, err := ResolveSandboxCredentialEnvironment(ctx, CredentialForwardingOptions{
			Environment:                    map[string]string{"API_KEY": "host-secret"},
			CredentialEnvironmentVariables: []string{"API_KEY"},
			CredentialForwarding: func(context.Context, harness.CredentialForwardingOptions) (string, error) {
				return "", errors.New("forwarding failed")
			},
		})
		if err == nil || err.Error() != "forwarding failed" {
			t.Fatalf("err = %v, want forwarding failed", err)
		}
	})
}

func TestCredentialBrokering(t *testing.T) {
	var warnings []string
	orig := Warn
	Warn = func(m string) { warnings = append(warnings, m) }
	defer func() { Warn = orig }()

	names := []string{"API_KEY", "SECOND_API_KEY"}
	env := map[string]string{"API_KEY": "real-secret", "SECOND_API_KEY": "second-real-secret"}
	if WarnCredentialBrokeringUnavailable(WarnCredentialBrokeringUnavailableOptions{Environment: env, ForwardedEnvironment: map[string]string{"API_KEY": "caller-managed-credential", "SECOND_API_KEY": "second-caller-managed-credential"}, CredentialEnvironmentVariables: names}) {
		t.Fatal("no warning when forwarding replaces all credentials")
	}
	if !WarnCredentialBrokeringUnavailable(WarnCredentialBrokeringUnavailableOptions{Environment: env, ForwardedEnvironment: map[string]string{"API_KEY": "caller-managed-credential", "SECOND_API_KEY": "second-real-secret"}, CredentialEnvironmentVariables: names}) {
		t.Fatal("warn when a credential is preserved")
	}
	if !WarnCredentialBrokeringUnavailable(WarnCredentialBrokeringUnavailableOptions{Environment: map[string]string{"API_KEY": "real-secret"}, ForwardedEnvironment: map[string]string{"API_KEY": "wrapped-real-secret"}, CredentialEnvironmentVariables: []string{"API_KEY"}}) {
		t.Fatal("warn when a forwarded value contains a credential")
	}
	if WarnCredentialBrokeringUnavailable(WarnCredentialBrokeringUnavailableOptions{Environment: map[string]string{}, ForwardedEnvironment: map[string]string{}, CredentialEnvironmentVariables: []string{"API_KEY"}}) {
		t.Fatal("no credentials, no warning")
	}
	if len(warnings) != 2 || warnings[0] != CredentialBrokeringUnavailableWarning {
		t.Fatal(warnings)
	}

	env2 := map[string]string{"API_KEY": "real-secret", "SECOND_API_KEY": "second-secret", "BASE_URL": "https://api.example.com/v1"}
	masked := MaskSandboxCredentials(env2, names)
	if !reflect.DeepEqual(masked, map[string]string{"API_KEY": "API_KEY", "SECOND_API_KEY": "SECOND_API_KEY", "BASE_URL": "https://api.example.com/v1"}) || env2["API_KEY"] != "real-secret" {
		t.Fatal(masked)
	}
	if m := MaskSandboxCredentials(map[string]string{"OTHER": "value"}, []string{"API_KEY"}); !reflect.DeepEqual(m, map[string]string{"OTHER": "value"}) {
		t.Fatal(m)
	}

	first, second := GenerateSandboxCredentialPlaceholder(), GenerateSandboxCredentialPlaceholder()
	if !regexp.MustCompile(`^aisdkhc_[A-Za-z0-9_-]{43}$`).MatchString(first) || first == second || !IsSandboxCredentialPlaceholder(first) {
		t.Fatal(first, second)
	}
	for _, v := range []string{"real-secret", "aisdkhc_", "prefix-" + first} {
		if IsSandboxCredentialPlaceholder(v) {
			t.Fatal(v)
		}
	}
}

func TestCreateCredentialRequestTransformation(t *testing.T) {
	mk := func(u string, match, transform map[string]string) string {
		rt, err := CreateCredentialRequestTransformation(CreateCredentialRequestTransformationOptions{MatchURL: u, MatchHeaders: match, TransformHeaders: transform})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(rt)
		return string(b)
	}
	auth := map[string]string{"Authorization": "Bearer sandbox-secret"}
	real := map[string]string{"Authorization": "Bearer real-secret"}
	want := `{"match":{"host":"api.example.com","path":{"startsWith":"/v1"},"headers":[{"key":{"exact":"Authorization"},"value":{"exact":"Bearer sandbox-secret"}}]},"transform":{"headers":{"Authorization":"Bearer real-secret"}}}`
	for _, u := range []string{"https://api.example.com/v1", "http://api.example.com/v1", "https://api.example.com:8443/v1"} {
		if got := mk(u, auth, real); got != want {
			t.Errorf("%s => %s", u, got)
		}
	}
	if got := mk("https://api.example.com/", map[string]string{"x-api-key": "sandbox-secret"}, map[string]string{"x-api-key": "real-secret"}); got != `{"match":{"host":"api.example.com","headers":[{"key":{"exact":"x-api-key"},"value":{"exact":"sandbox-secret"}}]},"transform":{"headers":{"x-api-key":"real-secret"}}}` {
		t.Fatal(got)
	}
	if _, err := CreateCredentialRequestTransformation(CreateCredentialRequestTransformationOptions{MatchURL: "not a url"}); err == nil {
		t.Fatal("invalid URL must error")
	}
}

func TestOAuth(t *testing.T) {
	if !IsAccessTokenExpiringSoon(400_000, 100_000, 0) || IsAccessTokenExpiringSoon(400_001, 100_000, 0) || !IsAccessTokenExpiringSoon(102_000, 100_000, 2_000) {
		t.Fatal("IsAccessTokenExpiringSoon")
	}

	var gotBody, gotType, gotClient string
	respond := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			gotBody, gotType, gotClient = string(b), r.Header.Get("content-type"), r.Header.Get("x-client")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}
	ctx := context.Background()

	srv := respond(200, `{"access_token":"access","expires_in":60}`)
	res, err := RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "client", RefreshToken: "refresh", Now: func() time.Time { return time.UnixMilli(1_000) }})
	srv.Close()
	if err != nil || *res != (RefreshOAuthAccessTokenResult{AccessToken: "access", ExpiresAt: 61_000}) {
		t.Fatal(res, err)
	}
	if gotBody != "grant_type=refresh_token&client_id=client&refresh_token=refresh" || gotType != "application/x-www-form-urlencoded" {
		t.Fatal(gotBody, gotType)
	}

	srv = respond(200, `{"access_token":"access","refresh_token":"rotated","expires_in":60}`)
	res, err = RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "client", RefreshToken: "refresh", RequestFormat: "json", Headers: map[string]string{"x-client": "test"}})
	srv.Close()
	if err != nil || res.RefreshToken != "rotated" || gotType != "application/json" || gotClient != "test" ||
		gotBody != `{"grant_type":"refresh_token","client_id":"client","refresh_token":"refresh"}` {
		t.Fatal(res, err, gotBody)
	}

	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1234}`))
	srv = respond(200, `{"access_token":"header.`+payload+`.signature"}`)
	res, err = RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "c", RefreshToken: "r"})
	srv.Close()
	if err != nil || res.ExpiresAt != 1_234_000 {
		t.Fatal(res, err)
	}

	srv = respond(401, "sensitive response")
	_, err = RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "c", RefreshToken: "sensitive-refresh-token"})
	srv.Close()
	if err == nil || err.Error() != "OAuth access token refresh failed with status 401." {
		t.Fatal(err)
	}
	srv = respond(200, `{"access_token":"not-a-jwt"}`)
	_, err = RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "c", RefreshToken: "r"})
	srv.Close()
	if err == nil || err.Error() != "OAuth access token refresh response does not include a usable expiry." {
		t.Fatal(err)
	}
	srv = respond(200, `{"access_token":"a","expires_in":1,"refresh_token":""}`)
	_, err = RefreshOAuthAccessToken(ctx, RefreshOAuthAccessTokenOptions{TokenURL: srv.URL, ClientID: "c", RefreshToken: "r"})
	srv.Close()
	if err == nil || !strings.Contains(err.Error(), "invalid refresh_token") {
		t.Fatal(err)
	}
}
