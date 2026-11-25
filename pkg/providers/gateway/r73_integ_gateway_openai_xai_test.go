package gateway

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

// TestR73IntegGatewayOpenAIXAIProviderTools (provb CR-1, CR-3): openai and xai
// provider tools reach the AI Gateway as provider tools {type: "provider", id,
// name, args} with the TS camelCase args, as TS prepareTools sends them (v0.5
// sent function tools named "openai.*"/"xai.*").
func TestR73IntegGatewayOpenAIXAIProviderTools(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(p, "openai/gpt-5")
	body, err := m.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			openaitool.WebSearch(openaitool.WebSearchConfig{SearchContextSize: "high", UserLocation: &openaitool.WebSearchLocation{City: "Paris"}}),
			xai.MCPServer(xai.MCPServerConfig{ServerURL: "https://mcp.example.com", ServerLabel: "docs", Authorization: "Bearer s"}),
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(body["tools"])
	if err != nil {
		t.Fatal(err)
	}
	var gotGeneric, wantGeneric interface{}
	if err := json.Unmarshal(got, &gotGeneric); err != nil {
		t.Fatal(err)
	}
	want := `[{"type":"provider","name":"openai.web_search","id":"openai.web_search","args":{"searchContextSize":"high","userLocation":{"type":"approximate","city":"Paris"}}},` +
		`{"type":"provider","name":"xai.mcp","id":"xai.mcp","args":{"serverUrl":"https://mcp.example.com","serverLabel":"docs","authorization":"Bearer s"}}]`
	if err := json.Unmarshal([]byte(want), &wantGeneric); err != nil {
		t.Fatal(err)
	}
	g, _ := json.Marshal(gotGeneric)
	w, _ := json.Marshal(wantGeneric)
	if string(g) != string(w) {
		t.Fatalf("gateway tools =\n%s\nwant\n%s", g, w)
	}
}
