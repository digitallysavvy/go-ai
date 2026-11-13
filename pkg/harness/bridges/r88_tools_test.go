package bridges

import (
	"strings"
	"testing"
)

// The 2ebc78b654 resync (ai@7.0.170; first pinned at 0f3a85f92d /
// ai@7.0.165 in round 88): every vendored bridge is rebuilt from that
// checkout. The checks pin the bridge-only fixes of the round 88 TS
// delta: deepagents requires --home-dir and checks file locations
// (bb8aa09ec8, e4a6112776), opencode asks for reads outside the workdir
// (3af9d06557), claude-code loads only user settings (e4a6112776), and
// every runBridge bridge reads --host-disconnect-timeout-ms (a54f0fd8c6).
func TestR88toolsBridgesAt0f3a85f92d(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.TSCommit != "2ebc78b654" || m.TSTag != "ai@7.0.170" {
		t.Fatalf("manifest pinned to %q / %q", m.TSTag, m.TSCommit)
	}
	want := map[Adapter]string{
		ACP: "1.0.121", ClaudeCode: "1.0.185", Codex: "1.0.183", DeepAgents: "1.0.181",
		GitHubCopilot: "1.0.78", GrokBuild: "1.0.120", OpenCode: "1.0.184",
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
		// f779a62cc9: Google / Mistral through AI Gateway use openai-compatible.
		OpenCode: {`readableSkillRoots: start.readableSkillRoots,`, `providerID === "mistral" ? { npm: "@ai-sdk/openai-compatible" }`},
		// c7ebb5653a: dotted CLAUDE_CODE_SUBAGENT_MODEL ids are mapped.
		ClaudeCode: {`settingSources: ["user"],`, `...toGatewayModelOverride(env.CLAUDE_CODE_SUBAGENT_MODEL),`},
		// 95c1307de5: the caller's model_reasoning_summary wins.
		Codex: {"model_reasoning_summary: \"detailed\",\n\t\t...start.codexConfig,"},
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
