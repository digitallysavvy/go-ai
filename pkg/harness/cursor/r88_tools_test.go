package cursor

import (
	"reflect"
	"testing"
	"time"
)

// TS e4a6112776 cursor-harness.test.ts (args) and a54f0fd8c6
// (hostDisconnectTimeoutMs forwarded to createACP).
func TestR88toolsCursorConfig(t *testing.T) {
	ms := 60000
	cfg := BuildConfig(Settings{HostDisconnectTimeoutMS: &ms})
	if cfg.HostDisconnectTimeout != 60*time.Second {
		t.Fatalf("HostDisconnectTimeout = %v", cfg.HostDisconnectTimeout)
	}
	if !reflect.DeepEqual(cfg.Args, []string{"--disable-auto-update", "--disable-project-configs", "acp"}) {
		t.Fatalf("Args = %v", cfg.Args)
	}
}
