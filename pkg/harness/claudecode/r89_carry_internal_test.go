package claudecode

import (
	"context"
	"os/exec"
	"reflect"
	"runtime"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TS claude-code-subscription.ts resolveClaudeCodeAuthentication (lines
// 67-84): with a direct credential in the environment the environment
// resolved WITHOUT the apiKeyHelper is returned, so the helper never runs
// and never fills the other credential variable. Expected values recorded
// from TS 382d0b8eaf (bun, HOME with apiKeyHelper "echo helper-key"):
// {ANTHROPIC_API_KEY: k} and {ANTHROPIC_AUTH_TOKEN: t}.
func TestR89carryHelperSkippedWithDirectCredential(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell paths")
	}
	r88carryHelperHome(t, "echo helper-key")
	runs := r88carryRecordHelperRuns(t, func(*exec.Cmd) ([]byte, error) { return []byte("helper-key\n"), nil })
	for _, tc := range []struct {
		env  map[string]string
		want map[string]string
	}{
		{map[string]string{"ANTHROPIC_API_KEY": "k"}, map[string]string{"ANTHROPIC_API_KEY": "k"}},
		{map[string]string{"ANTHROPIC_AUTH_TOKEN": "t"}, map[string]string{"ANTHROPIC_AUTH_TOKEN": "t"}},
	} {
		got, err := resolveAuthenticationEnvironment(context.Background(), harness.Authentication{}, tc.env)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("env %v: got %v, want %v", tc.env, got, tc.want)
		}
	}
	if len(*runs) != 0 {
		t.Errorf("apiKeyHelper ran: %q", *runs)
	}
}

// TS readApiKeyHelper reads `settings.apiKeyHelper` from JSON.parse: the
// key is case-sensitive, the last duplicate wins, a non-string is ignored.
func TestR89carryAPIKeyHelperKeyExact(t *testing.T) {
	for raw, want := range map[string]string{
		`{"apiKeyHelper":"echo a"}`:                         "echo a",
		`{"ApiKeyHelper":"echo a"}`:                         "",
		`{"APIKEYHELPER":"echo a","apiKeyHelper":"echo b"}`: "echo b",
		`{"apiKeyHelper":"echo a","ApiKeyHelper":"echo b"}`: "echo a",
		`{"apiKeyHelper":"echo a","apiKeyHelper":"echo c"}`: "echo c",
		`{"apiKeyHelper":1}`:                                "",
		`{"apiKeyHelper":null}`:                             "",
		`[]`:                                                "",
	} {
		if got := extractAPIKeyHelperCommand([]byte(raw)); got != want {
			t.Errorf("%s: got %q, want %q", raw, got, want)
		}
	}
}
