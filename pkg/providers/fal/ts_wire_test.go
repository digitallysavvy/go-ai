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
	speechHeaders := "the TS test server appends the JSON content-type a second time (response header `application/json, application/json`); the replay serves the recorded single header, everything else matches"
	tsfixture.RunWire(t, "../testdata/tswire/fal.jsonl.gz", tsfixture.WireSuite{
		Skip: map[string]string{
			"FalSpeechModel.doGenerate > should pass text and default output_format":                       speechHeaders,
			"FalSpeechModel.doGenerate > should pass headers":                                              speechHeaders,
			"FalSpeechModel.doGenerate > should return audio data":                                         speechHeaders,
			"FalSpeechModel.doGenerate > should include response data with timestamp, modelId and headers": speechHeaders,
			"FalSpeechModel.doGenerate > should include warnings for unsupported settings":                 speechHeaders,
			"~FalVideoModel > doStatus >":                                                                  "the operation carries only a responseUrl (no submitUrl), so Go validates it as an untrusted URL and rejects the replay server's 127.0.0.1 (TS validates the recorded queue.fal.run host without DNS); doStatus is covered by the fal video unit tests",
		},
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
