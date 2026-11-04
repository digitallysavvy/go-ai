package bridges

import (
	"strings"
	"testing"
)

// The 3fab50aa9b resync: every bridge that bundles @ai-sdk/harness runBridge
// drops a repeated start (TS 3bf70f83bc, harness/src/bridge/index.ts 726-731)
// and advertises idempotentStart on bridge-hello (1013-1018); the
// claude-code bridge maps the ANTHROPIC_DEFAULT_*_MODEL aliases (TS
// a9a6fe7200).
func TestR87toolsBridgesAt3fab50aa9b(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.TSCommit != "3fab50aa9b" {
		t.Fatalf("tsCommit = %q", m.TSCommit)
	}
	for _, adapter := range []Adapter{ClaudeCode, Codex, OpenCode, DeepAgents, ACP} {
		data, ok, err := BridgeEntry(adapter)
		if err != nil || !ok {
			t.Fatalf("BridgeEntry(%q): %v %v", adapter, ok, err)
		}
		src := string(data)
		for _, want := range []string{"if (startId === lastStartId) return;", "idempotentStart: true,"} {
			if !strings.Contains(src, want) {
				t.Errorf("%s bridge lacks %q", adapter, want)
			}
		}
	}
	data, _, _ := BridgeEntry(ClaudeCode)
	if !strings.Contains(string(data), "toGatewayModelOverride(env.ANTHROPIC_DEFAULT_OPUS_MODEL)") {
		t.Error("claude-code bridge lacks the ANTHROPIC_DEFAULT_*_MODEL overrides")
	}
}

func TestR87toolsContentHashIsOrderIndependent(t *testing.T) {
	a := ContentHash(map[string]string{"a": "1", "b": "2"})
	if a != ContentHash(map[string]string{"b": "2", "a": "1"}) || a == ContentHash(map[string]string{"a": "1", "b": "3"}) {
		t.Fatal("ContentHash must depend on names and hashes only")
	}
}
