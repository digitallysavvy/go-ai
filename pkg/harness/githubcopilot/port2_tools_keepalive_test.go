package githubcopilot

import (
	"testing"
	"time"
)

// TS 04a8f3837e: keepAliveMs is forwarded to the ACP settings; unset keeps
// the default and 0 disables the heartbeat.
func TestPort2toolsGitHubCopilotKeepAliveForwarded(t *testing.T) {
	ms := func(v int) *int { return &v }
	for _, tc := range []struct {
		name string
		set  *int
		want time.Duration
	}{
		{"unset", nil, 0},
		{"set", ms(100), 100 * time.Millisecond},
		{"zero disables", ms(0), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := BuildConfig(Settings{KeepAliveMS: tc.set})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.KeepAlive != tc.want {
				t.Fatalf("KeepAlive = %v, want %v", cfg.KeepAlive, tc.want)
			}
		})
	}
}
