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
		// Language results (content, usage, finish reason, metadata) are
		// compared too, since review round 81.
		LanguageResults: true,
		SkipResults: func() map[string]string {
			why := "Go keeps the flat v0.5.2 compaction metadata keys (type, itemId, encryptedContent) beside the provider-keyed ones"
			out := map[string]string{}
			for _, n := range []string{
				"should append an explicit compaction trigger as the final input item",
				"should not append a compaction trigger when disabled",
				"should parse compaction output item from real fixture",
				"should send context_management in request body",
				"should include compaction item with encrypted content in content",
				"should extract usage from compaction response",
			} {
				out["OpenAIResponsesLanguageModel > doGenerate > compaction > "+n] = why
			}
			out["OpenAIResponsesLanguageModel > mixed citation types > compaction > should stream compaction output item from real fixture"] = why
			return out
		}(),
		Skip: r85provbWithStaleRecordingSkips(map[string]string{
			"OpenAI batch service > creates a Responses batch from prepared JSONL requests":                                                           "provider headers are not captured in the batch recording config; JSONL line key order is semantically irrelevant (covered by batch unit tests)",
			"OpenAI batch service > warns when a provider tool can return unsupported batch output":                                                   "typed Go hosted tools are named by their id, so the warning feature text differs from the TS tool name; covered by TestBatchWarnsForUnsupportedHostedTool",
			"OpenAIResponsesLanguageModel > doGenerate > basic text response > should replay a regular function named tool_search as a function call": "the TS tool-call input is an object whose key order Go maps cannot carry; Go sends the Input string it is given as is",
		}),
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

// r85provbStaleRecordingSkips lists tests of the e64ed82f27 recording whose
// TS behavior changed in e64ed82f27..30548ee7f9 and which the new TS no
// longer has (so the 30548ee7f9 recording, reports/r85-provb-tswire, does not
// contain them). Go follows the new TS. Delete this list once
// testdata/tswire/openai.jsonl.gz is the 30548ee7f9 recording.
var r85provbStaleRecordingSkips = map[string]string{
	"OpenAIResponsesLanguageModel > doGenerate > basic text response > should omit unsupported GPT-6 reasoning effort none":    "TS 61a315e333 rounds the effort instead (30548ee7f9 recording)",
	"OpenAIResponsesLanguageModel > doGenerate > basic text response > should omit unsupported GPT-6 reasoning effort minimal": "TS 61a315e333 rounds the effort instead (30548ee7f9 recording)",
	"doGenerate > reasoning models > should omit unsupported GPT-6 reasoning effort none":                                      "TS 61a315e333 rounds the effort instead (30548ee7f9 recording)",
	"doGenerate > reasoning models > should omit unsupported GPT-6 reasoning effort minimal":                                   "TS 61a315e333 rounds the effort instead (30548ee7f9 recording)",
}

func r85provbWithStaleRecordingSkips(skip map[string]string) map[string]string {
	for k, v := range r85provbStaleRecordingSkips {
		skip[k] = v
	}
	return skip
}
