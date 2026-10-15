package azure

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSWire replays the recorded TS azure tests (testdata/tswire/azure.jsonl.gz).
// It runs in-package so a provider built from a resource name (which selects
// Azure's URL layout: /v1 segment and api-version query) can still be pointed
// at the replay server. Language models are covered by TestTSCases.
func TestTSWire(t *testing.T) {
	tsfixture.RunWire(t, "../testdata/tswire/azure.jsonl.gz", tsfixture.WireSuite{
		Skip: map[string]string{
			"speech > doGenerate > should use correct URL format": "the TS test serves a Uint8Array-shaped JSON body as audio; the recording cannot carry it",
		},
		Provider: func(base string, m tsfixture.WireModel) (provider.Provider, error) {
			srv := base
			if u, err := url.Parse(base); err == nil {
				srv = u.Scheme + "://" + u.Host
			}
			headers := map[string]string{}
			if h, ok := m.Config["headers"].(map[string]interface{}); ok {
				for k, v := range h {
					if s, ok := v.(string); ok && !strings.EqualFold(k, "user-agent") && !strings.EqualFold(k, "api-key") {
						headers[k] = s
					}
				}
			}
			guard := &http.Client{Transport: tsfixture.GuardTransport{Next: http.DefaultTransport}}
			p, err := New(Config{
				APIKey:                 "test-api-key",
				UseDeploymentBasedURLs: usesDeployments(m),
				ResourceName:           "test-resource",
				Headers:                headers,
				HTTPClient:             guard,
				SpeechBaseURL:          srv,
				MaiBaseURL:             srv,
			})
			if err != nil {
				return nil, err
			}
			p.client.SetBaseURL(srv + "/openai")
			p.config.BaseURL = srv + "/openai" // openai-backed models derive their URL from it
			return p, nil
		},
	})
}

func usesDeployments(m tsfixture.WireModel) bool {
	for _, u := range m.RequestURLs {
		if strings.Contains(u, "/deployments/") {
			return true
		}
	}
	return false
}
