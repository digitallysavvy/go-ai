package fal

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSWire replays the recorded TS fal tests (testdata/tswire/fal.jsonl.gz).
// Fal's speech, transcription and video models use fixed hosts, so this runs
// in-package to point them at the replay server.
func TestTSWire(t *testing.T) {
	tsfixture.RunWire(t, "../testdata/tswire/fal.jsonl.gz", tsfixture.WireSuite{
		Provider: func(base string, m tsfixture.WireModel) (provider.Provider, error) {
			headers := map[string]string{}
			if h, ok := m.Config["headers"].(map[string]interface{}); ok {
				for k, v := range h {
					if s, ok := v.(string); ok && !strings.EqualFold(k, "user-agent") {
						headers[k] = s
					}
				}
			}
			p := New(Config{APIKey: "test-api-key", BaseURL: base, Headers: headers})
			// The TS models post to the path as given (`url: ({path}) => path`);
			// the recorded host is rewritten to the replay server.
			p.speechHost = strings.TrimRight(base, "/")
			p.queueHost = strings.TrimRight(base, "/")
			return p, nil
		},
	})
}
