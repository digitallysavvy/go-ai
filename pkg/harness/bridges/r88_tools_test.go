package bridges

import (
	"strings"
	"testing"
)

// The 0f3a85f92d resync (ai@7.0.165): every vendored bridge is rebuilt from
// that checkout. The checks pin the bridge-only fixes of the round 88 TS
// delta: deepagents requires --home-dir and checks file locations
// (bb8aa09ec8, e4a6112776), opencode asks for reads outside the workdir
// (3af9d06557), claude-code loads only user settings (e4a6112776), and
// every runBridge bridge reads --host-disconnect-timeout-ms (a54f0fd8c6).
func TestR88toolsBridgesAt0f3a85f92d(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.TSCommit != "0f3a85f92d" || m.TSTag != "ai@7.0.165" {
		t.Fatalf("manifest pinned to %q / %q", m.TSTag, m.TSCommit)
	}
	want := map[Adapter]string{
		ACP: "1.0.116", ClaudeCode: "1.0.180", Codex: "1.0.178", DeepAgents: "1.0.176",
		GitHubCopilot: "1.0.73", GrokBuild: "1.0.115", OpenCode: "1.0.179",
	}
	for adapter, version := range want {
		if got := m.Adapters[string(adapter)].SourceVersion; got != version {
			t.Errorf("%s sourceVersion = %q, want %q", adapter, got, version)
		}
	}
	for _, adapter := range []Adapter{ClaudeCode, Codex, OpenCode, DeepAgents, ACP} {
		data, _, _ := BridgeEntry(adapter)
		if !strings.Contains(string(data), `reason.toString() !== "suspended"`) {
			t.Errorf("%s bridge lacks the host disconnect timeout", adapter)
		}
	}
	checks := map[Adapter][]string{
		DeepAgents: {`missing --workdir / --home-dir / --bridge-state-dir`, `readableSkillRoots: start.readableSkillRoots ?? []`},
		OpenCode:   {`readableSkillRoots: start.readableSkillRoots,`},
		ClaudeCode: {`settingSources: ["user"],`},
	}
	for adapter, needles := range checks {
		data, _, _ := BridgeEntry(adapter)
		for _, needle := range needles {
			if !strings.Contains(string(data), needle) {
				t.Errorf("%s bridge lacks %q", adapter, needle)
			}
		}
	}
}
