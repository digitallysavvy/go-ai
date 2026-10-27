package openai

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestTSWire replays the recorded TS openai tests (testdata/tswire/openai*.jsonl.gz)
// through the Go OpenAI provider: requests are diffed against what the TS
// models sent. Language models are compared on the request only (their
// results are covered by TestTSCases*).
func TestTSWire(t *testing.T) {
	files, _ := filepath.Glob("../testdata/tswire/openai.jsonl.gz")
	more, _ := filepath.Glob("../testdata/tswire/openai__*.jsonl.gz")
	files = append(files, more...)
	if len(files) == 0 {
		t.Skip("no recordings")
	}
	suite := tsfixture.WireSuite{
		ProviderTool: tsProviderTool,
		BatchPrefix:  "/v1", // batch objects carry no URL config in the recording
		Skip: map[string]string{
			"OpenAI batch service > creates a Responses batch from prepared JSONL requests":                                                           "provider headers are not captured in the batch recording config; JSONL line key order is semantically irrelevant (covered by batch unit tests)",
			"OpenAI batch service > warns when a provider tool can return unsupported batch output":                                                   "typed Go hosted tools are named by their id, so the warning feature text differs from the TS tool name; covered by TestBatchWarnsForUnsupportedHostedTool",
			"OpenAIResponsesLanguageModel > doGenerate > basic text response > should replay a regular function named tool_search as a function call": "the TS tool-call input is an object whose key order Go maps cannot carry; Go sends the Input string it is given as is",
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
			cfg := Config{APIKey: "test-api-key", BaseURL: base, Headers: headers,
				HTTPClient: &http.Client{Transport: tsfixture.GuardTransport{Next: http.DefaultTransport}}}
			if v, ok := m.Config["supportsWebSearchSourcesInclude"].(bool); ok {
				cfg.SupportsWebSearchSourcesInclude = &v
			}
			// Tests that build a model under another provider name (for example
			// "azure.responses") select name-dependent behavior.
			switch {
			case strings.HasSuffix(m.Provider, ".responses"):
				cfg.ResponsesProviderName = m.Provider
			case strings.HasSuffix(m.Provider, ".chat"):
				cfg.ChatProviderName = m.Provider
			case strings.HasSuffix(m.Provider, ".completion"):
				cfg.CompletionProviderName = m.Provider
			}
			return New(cfg), nil
		},
		Language: func(p provider.Provider, cls, id string) (provider.LanguageModel, error) {
			op := p.(*Provider)
			switch {
			case strings.Contains(cls, "Chat"):
				return op.ChatModel(id)
			case strings.Contains(cls, "Completion"):
				return op.CompletionModel(id)
			default:
				return op.ResponsesModel(id)
			}
		},
	}
	only := os.Getenv("TSWIRE_FILE")
	for _, f := range files {
		if only != "" && !strings.Contains(f, only) {
			continue
		}
		f := f
		t.Run(strings.TrimSuffix(filepath.Base(f), ".jsonl.gz"), func(t *testing.T) { tsfixture.RunWire(t, f, suite) })
	}
}
