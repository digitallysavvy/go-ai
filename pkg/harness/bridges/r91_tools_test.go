package bridges

import (
	"strings"
	"testing"
)

// The 2ebc78b654 resync (ai@7.0.170) carries the round 91 TS bridge changes:
// public-key bridge authentication in every runBridge bundle (c849d37dac:
// the bridge reads BRIDGE_AUTH_PUBLIC_KEY and sends a challenge before
// bridge-hello), Claude Code's abortController option (fcd763a9e4) and
// OpenCode ignoring compacted tool parts (aa4e711233).
func TestR91toolsBridgesAt2ebc78b654(t *testing.T) {
	for _, adapter := range []Adapter{ClaudeCode, Codex, OpenCode, DeepAgents, ACP} {
		data, _, err := BridgeEntry(adapter)
		if err != nil {
			t.Fatal(err)
		}
		for _, needle := range []string{`env["BRIDGE_AUTH_PUBLIC_KEY"]`, `bridge-auth-challenge`, `ai-sdk-harness-bridge-auth-v1\0`} {
			if !strings.Contains(string(data), needle) {
				t.Errorf("%s bridge lacks %q", adapter, needle)
			}
		}
		if strings.Contains(string(data), `env["BRIDGE_CHANNEL_TOKEN"]`) {
			t.Errorf("%s bridge still reads the shared channel token", adapter)
		}
	}
	checks := map[Adapter][]string{
		ClaudeCode: {`abortController: abortCtl`},
		OpenCode:   {`if (asOpenCodeObject(stateObject?.time)?.compacted != null) return void 0;`},
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
