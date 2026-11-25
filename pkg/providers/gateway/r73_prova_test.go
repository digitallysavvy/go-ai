package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	anthropicTools "github.com/digitallysavvy/go-ai/pkg/providers/anthropic/tools"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
)

func r73provaIntPtr(v int) *int { return &v }

// CR-3: TS prepareTools turns a provider tool into
// {type: 'provider', name, id, args} and the gateway sends the call options
// unchanged, so anthropic provider tools reach the gateway as provider tools
// with the camelCase args the caller passed (not as function tools named
// anthropic.*).
func TestR73prova_GatewaySendsAnthropicToolsAsProviderTools(t *testing.T) {
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"finish\",\"finishReason\":\"stop\"}\n\n"))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.LanguageModel("anthropic/claude-sonnet-4-6")
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			anthropicTools.WebSearch20250305(anthropicTools.WebSearch20260209Config{
				MaxUses:        r73provaIntPtr(3),
				AllowedDomains: []string{"example.com"},
				UserLocation:   &anthropicTools.WebSearchUserLocation{Type: "approximate", City: "Paris"},
			}),
			anthropicTools.Bash20250124(),
			anthropicTools.Computer20251124(anthropicTools.Computer20251124Args{DisplayWidthPx: 1024, DisplayHeightPx: 768, EnableZoom: true}),
			anthropicTools.WebFetch20260318(anthropicTools.WebFetch20260318Config{
				Citations: &anthropicTools.WebFetchCitations{Enabled: true}, UseCache: &disabled, ResponseInclusion: "excluded",
			}),
			anthropicTools.ComputerToolset20260801(anthropicTools.ComputerToolset20260801Config{
				Configs: map[anthropicTools.ComputerToolset20260801Member]anthropicTools.ComputerToolset20260801MemberConfig{
					anthropicTools.ComputerToolsetZoom: {Enabled: &disabled},
				},
			}),
			anthropicTools.Advisor20260301(anthropicTools.Advisor20260301Args{MaxTokens: r73provaIntPtr(2048)}),
			{Name: "weather", Description: "Get weather", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	var body struct {
		Tools []interface{} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	var want []interface{}
	if err := json.Unmarshal([]byte(`[
		{"type":"provider","id":"anthropic.web_search_20250305","name":"anthropic.web_search_20250305",
		 "args":{"maxUses":3,"allowedDomains":["example.com"],"userLocation":{"type":"approximate","city":"Paris"}}},
		{"type":"provider","id":"anthropic.bash_20250124","name":"anthropic.bash_20250124","args":{}},
		{"type":"provider","id":"anthropic.computer_20251124","name":"anthropic.computer_20251124",
		 "args":{"displayWidthPx":1024,"displayHeightPx":768,"enableZoom":true}},
		{"type":"provider","id":"anthropic.web_fetch_20260318","name":"anthropic.web_fetch_20260318",
		 "args":{"citations":{"enabled":true},"useCache":false,"responseInclusion":"excluded"}},
		{"type":"provider","id":"anthropic.computer_toolset_20260801","name":"anthropic.computer_toolset_20260801",
		 "args":{"configs":{"zoom":{"enabled":false}}}},
		{"type":"provider","id":"anthropic.advisor_20260301","name":"anthropic.advisor_20260301",
		 "args":{"model":"claude-opus-4-7","maxTokens":2048}},
		{"type":"function","name":"weather","description":"Get weather","inputSchema":{"type":"object"}}
	]`), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body.Tools, want) {
		got, _ := json.Marshal(body.Tools)
		t.Fatalf("tools =\n%s", got)
	}
}

// Google tools reach the gateway with a tool name (TS: the tools record key);
// before the gemini constructors set Name the gateway got name "".
func TestR73prova_GatewaySendsGoogleToolsWithName(t *testing.T) {
	model := newTestGatewayLanguageModel("google/gemini-2.5-pro")
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{gemini.GoogleSearchTool(), gemini.CodeExecutionTool()},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body["tools"])
	want := `[{"args":{},"id":"google.google_search","name":"google_search","type":"provider"},` +
		`{"args":{},"id":"google.code_execution","name":"code_execution","type":"provider"}]`
	if string(got) != want {
		t.Fatalf("tools = %s", got)
	}
}
