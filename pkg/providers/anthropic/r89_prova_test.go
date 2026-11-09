package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
)

// TestR89provaThinkingStripsUndeclaredKeys: the anthropicLanguageModelOptions
// thinking union strips the keys a thinking type does not declare (TS
// anthropic-language-model-options.ts, zod object strip): blockBinding is
// kept only for adaptive and the typeless binding-only form, display only
// for adaptive, and neither is validated for the other types. Expected
// values recorded from TS createAnthropic at 382d0b8eaf.
func TestR89provaThinkingStripsUndeclaredKeys(t *testing.T) {
	cases := []struct {
		name         string
		thinking     map[string]interface{}
		wantThinking string
		wantBeta     bool
	}{
		{
			name:         "enabled drops blockBinding",
			thinking:     map[string]interface{}{"type": "enabled", "budgetTokens": 2000, "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "drop_block"}},
			wantThinking: `{"budget_tokens":2000,"type":"enabled"}`,
		},
		{
			name:         "enabled ignores an invalid display",
			thinking:     map[string]interface{}{"type": "enabled", "budgetTokens": 2000, "display": "bogus"},
			wantThinking: `{"budget_tokens":2000,"type":"enabled"}`,
		},
		{
			name:         "disabled ignores an invalid blockBinding",
			thinking:     map[string]interface{}{"type": "disabled", "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "bogus"}},
			wantThinking: `{"type":"disabled"}`,
		},
		{
			name:         "adaptive keeps blockBinding",
			thinking:     map[string]interface{}{"type": "adaptive", "blockBinding": map[string]interface{}{"prefixMismatchBehavior": "drop_block"}},
			wantThinking: `{"block_binding":{"prefix_mismatch_behavior":"drop_block"},"type":"adaptive"}`,
			wantBeta:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
			req, err := model.prepareRequest(&provider.GenerateOptions{
				Prompt:          types.Prompt{Text: "Hello"},
				ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": tc.thinking}},
			}, false)
			if err != nil {
				t.Fatalf("prepareRequest: %v", err)
			}
			got, _ := json.Marshal(req.body["thinking"])
			if string(got) != tc.wantThinking {
				t.Fatalf("thinking = %s, want %s", got, tc.wantThinking)
			}
			hasBeta := strings.Contains(strings.Join(req.betas, ","), BetaHeaderThinkingBindingControls)
			if hasBeta != tc.wantBeta {
				t.Fatalf("betas = %v, want binding beta %v", req.betas, tc.wantBeta)
			}
		})
	}
}

// TestR89provaEmptyRequiredStrings: z.string() accepts an empty string, so
// an empty mcpServers name / url, anthropic skillId or fallback model is
// sent as is; only a missing key fails (TS anthropic-language-model-
// options.ts; recorded with TS createAnthropic at 382d0b8eaf).
func TestR89provaEmptyRequiredStrings(t *testing.T) {
	model := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
	prepare := func(o map[string]interface{}) (*preparedRequest, error) {
		return model.prepareRequest(&provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "Hello"},
			ProviderOptions: map[string]interface{}{"anthropic": o},
		}, false)
	}
	req, err := prepare(map[string]interface{}{
		"mcpServers": []interface{}{map[string]interface{}{"type": "url", "name": "", "url": ""}},
		"container":  map[string]interface{}{"skills": []interface{}{map[string]interface{}{"type": "anthropic", "skillId": ""}}},
		"fallbacks":  []interface{}{map[string]interface{}{"model": ""}},
	})
	if err != nil {
		t.Fatalf("prepareRequest: %v", err)
	}
	got, _ := json.Marshal(map[string]interface{}{"m": req.body["mcp_servers"], "c": req.body["container"], "f": req.body["fallbacks"]})
	want := `{"c":{"skills":[{"skill_id":"","type":"anthropic"}]},"f":[{"model":""}],"m":[{"name":"","type":"url","url":""}]}`
	if string(got) != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
	if _, err := prepare(map[string]interface{}{"mcpServers": []interface{}{map[string]interface{}{"type": "url", "url": "u"}}}); err == nil {
		t.Fatal("a missing mcpServers name must fail")
	}
}

// TestR89provaFileSizeBytesNumber: anthropicUploadFileResponseSchema reads
// size_bytes as z.number() (TS files/anthropic-files-api.ts), so a
// fractional size does not fail the upload; providerMetadata keeps the
// number (an integral size stays int64 as in v0.5) and ByteSize truncates.
func TestR89provaFileSizeBytesNumber(t *testing.T) {
	for _, tc := range []struct {
		size     string
		wantMeta interface{}
		wantSize int64
	}{
		{"12.5", 12.5, 12},
		{"1024", int64(1024), 1024},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"file_1","size_bytes":` + tc.size + `}`))
		}))
		res, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Files().UploadFile(context.Background(), types.UploadFileOptions{
			Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain",
		})
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.size, err)
		}
		meta := res.ProviderMetadata["anthropic"].(map[string]interface{})
		if meta["sizeBytes"] != tc.wantMeta || res.ByteSize == nil || *res.ByteSize != tc.wantSize {
			t.Fatalf("%s: sizeBytes = %#v, ByteSize = %v", tc.size, meta["sizeBytes"], res.ByteSize)
		}
	}
}

// TestR89provaForcedFallbackEmptyTools: when forced tool use falls back to
// auto and no declared tool keeps the forced name (a toolset has no name),
// TS sends `tools: []` (declaredTools?.filter, anthropic-prepare-tools.ts);
// Go omitted tools. Recorded with TS createAnthropic at 382d0b8eaf.
func TestR89provaForcedFallbackEmptyTools(t *testing.T) {
	toolset := tools.ComputerToolset20260801(tools.ComputerToolset20260801Config{})
	toolset.Name = "computer"
	model := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
	req, err := model.prepareRequest(&provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "Hello"},
		Tools:           []types.Tool{toolset},
		ToolChoice:      types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "computer"},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": 2000}}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(map[string]interface{}{"tools": req.body["tools"], "tc": req.body["tool_choice"]})
	if string(got) != `{"tc":{"type":"auto"},"tools":[]}` {
		t.Fatalf("body = %s", got)
	}
}
