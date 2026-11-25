package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// r74provbToolsWant is the TS azure-openai-provider.test.ts "tool search
// tool" inline snapshot of the request tools.
const r74provbToolsWant = `[{"type":"tool_search"},{"defer_loading":true,"name":"get_weather","parameters":{"additionalProperties":false,"properties":{"location":{"type":"string"}},"required":["location"],"type":"object"},"strict":false,"type":"function"}]`

func r74provbToolSearchTools(searchTool types.Tool, optionsName string) []types.Tool {
	return []types.Tool{
		searchTool,
		{
			Name: "get_weather",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"properties":           map[string]interface{}{"location": map[string]interface{}{"type": "string"}},
				"required":             []interface{}{"location"},
				"additionalProperties": false,
			},
			ProviderOptions: map[string]interface{}{optionsName: map[string]interface{}{"deferLoading": true}},
		},
	}
}

func r74provbCanon(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var g interface{}
	if err := json.Unmarshal(b, &g); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(g)
	return string(b)
}

// TestR74ProvbAzureToolSearchDefersFunctionTools ports TS "tool search tool":
// the Azure toolSearch wrapper is the openai.tool_search provider tool, and
// function tools marked with providerOptions.azure or .openai deferLoading are
// sent with defer_loading, in generate and stream, for the wrapper and for a
// TS-shaped tool (type provider, id openai.tool_search, name toolSearch).
func TestR74ProvbAzureToolSearchDefersFunctionTools(t *testing.T) {
	ts := ToolSearch(openaitool.ToolSearchArgs{})
	if ts.Type != types.ToolTypeProviderDefined || ts.ProviderID != "openai.tool_search" {
		t.Fatalf("ToolSearch identity = %q/%q, want provider/openai.tool_search", ts.Type, ts.ProviderID)
	}
	tsShaped := types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: "openai.tool_search", Name: "toolSearch", ProviderArgs: map[string]interface{}{}}

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, `data: {"type":"response.completed","response":{"id":"r1","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"r1","object":"response","created_at":1,"status":"completed","model":"test-deployment","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()

	p := mustNewProvider(t, Config{ResourceName: "test-resource", APIKey: "k", BaseURL: server.URL})
	m, err := p.LanguageModel("test-deployment")
	if err != nil {
		t.Fatal(err)
	}
	for _, search := range []types.Tool{ts, tsShaped} {
		for _, name := range []string{"azure", "openai"} {
			opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: r74provbToolSearchTools(search, name)}
			if _, err := m.DoGenerate(context.Background(), opts); err != nil {
				t.Fatalf("DoGenerate(%s, %s): %v", search.Name, name, err)
			}
			if got := r74provbCanon(t, body["tools"]); got != r74provbToolsWant {
				t.Errorf("generate %s/%s tools =\n%s\nwant\n%s", search.Name, name, got, r74provbToolsWant)
			}
		}
		stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "Hello"}, Tools: r74provbToolSearchTools(search, "azure")})
		if err != nil {
			t.Fatalf("DoStream: %v", err)
		}
		for {
			if _, err := stream.Next(); err != nil {
				break
			}
		}
		_ = stream.Close()
		if got := r74provbCanon(t, body["tools"]); got != r74provbToolsWant {
			t.Errorf("stream %s tools =\n%s\nwant\n%s", search.Name, got, r74provbToolsWant)
		}
	}
}

// TestR74ProvbAzureWrappersCarryProviderIdentity: every Azure tool wrapper
// returns the openai provider tool (TS azureOpenaiTools re-exports the openai
// factories), so the AI Gateway gets {type: "provider", id: "openai.*", args}.
func TestR74ProvbAzureWrappersCarryProviderIdentity(t *testing.T) {
	for _, tc := range []struct {
		tool types.Tool
		id   string
	}{
		{CodeInterpreter(openaitool.CodeInterpreterConfig{}), "openai.code_interpreter"},
		{FileSearch(openaitool.FileSearchConfig{VectorStoreIDs: []string{"vs_1"}}), "openai.file_search"},
		{ImageGeneration(openaitool.ImageGenerationConfig{}), "openai.image_generation"},
		{WebSearch(openaitool.WebSearchConfig{}), "openai.web_search"},
		{WebSearchPreview(openaitool.WebSearchPreviewConfig{}), "openai.web_search_preview"},
		{ToolSearch(openaitool.ToolSearchArgs{}), "openai.tool_search"},
	} {
		if tc.tool.Type != types.ToolTypeProviderDefined || tc.tool.ProviderID != tc.id || tc.tool.ProviderArgs == nil {
			t.Errorf("%s: Type=%q ProviderID=%q ProviderArgs=%v", tc.id, tc.tool.Type, tc.tool.ProviderID, tc.tool.ProviderArgs)
		}
	}
}
