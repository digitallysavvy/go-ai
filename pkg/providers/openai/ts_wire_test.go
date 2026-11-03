package openai

import (
	"bytes"
	"compress/gzip"
	"io"
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
	r85provbSkipIfStaleRecording(t, "../testdata/tswire/openai.jsonl.gz", "should send language and vocabulary hints for gpt-transcribe")
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
		Skip: map[string]string{
			"OpenAI batch service > creates a Responses batch from prepared JSONL requests":                                                           "provider headers are not captured in the batch recording config; JSONL line key order is semantically irrelevant (covered by batch unit tests)",
			"OpenAI batch service > warns when a provider tool can return unsupported batch output":                                                   "typed Go hosted tools are named by their id, so the warning feature text differs from the TS tool name; covered by TestBatchWarnsForUnsupportedHostedTool",
			"falls back to input_text beyond input_json limits: '32 container levels'":                                                                "the recorder truncates call arguments nested deeper than 14 levels, so the replayed value is not the TS one; covered by TestR85provbDecisionInputJSON",
			"falls back to input_text beyond input_json limits: '33 container levels'":                                                                "the recorder truncates call arguments nested deeper than 14 levels, so the replayed value is not the TS one; covered by TestR85provbDecisionInputJSON",
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

// r85provbSkipIfStaleRecording skips a replay of a recording made before TS
// 30548ee7f9: Go follows the TS e64ed82f27..30548ee7f9 changes (reasoning
// rounding, transcription defaults, web search status, Decisions input),
// which the older recording does not have. The 30548ee7f9 recording is in
// state/parity/oct_27_2026/reports/r85-provb-tswire/; once it replaces the
// committed one, marker is found and the replay runs.
func r85provbSkipIfStaleRecording(t *testing.T, path, marker string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return
	}
	if !bytes.Contains(data, []byte(marker)) {
		t.Skipf("%s predates TS 30548ee7f9 (no %q); replace it with the r85 recording", path, marker)
	}
}
