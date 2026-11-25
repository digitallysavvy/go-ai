package githubcopilot

import (
	"reflect"
	"testing"
)

// TS 6f9d73fd19 + 7acc5a3f92 github-copilot-harness.test.ts.
func TestPortToolsBuildConfigWriteProtectedPaths(t *testing.T) {
	cfg, err := BuildConfig(Settings{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".github/hooks", ".github/mcp.json", ".github/copilot", ".github/skills", ".github/agents",
		".github/extensions", ".claude", ".mcp.json", ".vscode/mcp.json",
	}
	if !reflect.DeepEqual(cfg.WriteProtectedPaths, want) {
		t.Fatalf("WriteProtectedPaths = %v", cfg.WriteProtectedPaths)
	}
}
