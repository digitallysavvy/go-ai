package githubcopilot

import (
	"testing"
	"time"
)

// TS a54f0fd8c6 github-copilot-harness.test.ts: hostDisconnectTimeoutMs is
// forwarded to createACP.
func TestR88toolsCopilotHostDisconnectTimeout(t *testing.T) {
	ms := 60000
	cfg, err := BuildConfig(Settings{HostDisconnectTimeoutMS: &ms})
	if err != nil || cfg.HostDisconnectTimeout != 60*time.Second {
		t.Fatalf("HostDisconnectTimeout = %v, %v", cfg.HostDisconnectTimeout, err)
	}
}
