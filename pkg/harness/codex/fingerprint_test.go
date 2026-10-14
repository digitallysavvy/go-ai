package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

func rawTool(schema string) harness.ToolSpec {
	return harness.ToolSpec{Name: "weather", Description: "d", InputSchema: json.RawMessage(schema)}
}

// TS (#21959) "preserves thread history when persisted tool keys are
// reordered": value-equivalent tool definitions hash identically.
func TestFingerprintTurnConfiguration_KeyOrderInsensitive(t *testing.T) {
	a := fingerprintTurnConfiguration("ins", []harness.ToolSpec{rawTool(`{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"number"}}}`)})
	b := fingerprintTurnConfiguration("ins", []harness.ToolSpec{rawTool(`{"properties":{"b":{"type":"number"},"a":{"type":"string"}},"type":"object"}`)})
	if a != b {
		t.Fatalf("reordered keys changed the fingerprint: %s vs %s", a, b)
	}
	if !strings.HasPrefix(a, "v2:") {
		t.Fatalf("fingerprint %q lacks the v2: prefix", a)
	}
	c := fingerprintTurnConfiguration("ins", []harness.ToolSpec{rawTool(`{"type":"object","properties":{"a":{"type":"number"}}}`)})
	if a == c {
		t.Fatal("a genuine schema change must change the fingerprint")
	}
}

func syncSession(t *testing.T, persisted string) *session {
	t.Helper()
	return &session{opts: sessionOptions{sandboxHomeDir: "/home/agent", sandbox: bridgetest.NewSandbox()}, latestThreadID: "thread-1", latestTurnConfigurationFingerprint: persisted}
}

func TestSynchronizeTurnConfiguration_Restart(t *testing.T) {
	tools := []harness.ToolSpec{rawTool(`{"type":"object"}`)}
	current := fingerprintTurnConfiguration("ins", tools)

	cases := []struct {
		name      string
		persisted string
		tools     []harness.ToolSpec
		want      bool
	}{
		{"same v2 fingerprint keeps the thread", current, tools, false},
		{"changed v2 fingerprint restarts", current, []harness.ToolSpec{rawTool(`{"type":"string"}`)}, true},
		{"legacy fingerprint migrates without restart", strings.Repeat("a", 64), tools, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := syncSession(t, tc.persisted)
			got, err := s.synchronizeTurnConfiguration(context.Background(), nil, "ins", tc.tools)
			if err != nil {
				t.Fatalf("synchronizeTurnConfiguration: %v", err)
			}
			if got != tc.want {
				t.Fatalf("restartThread = %v, want %v", got, tc.want)
			}
			if !strings.HasPrefix(s.latestTurnConfigurationFingerprint, "v2:") {
				t.Fatalf("fingerprint not upgraded to v2: %q", s.latestTurnConfigurationFingerprint)
			}
		})
	}
}
