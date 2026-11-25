package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS b0fccbda62 (prepare-google-interactions-tools.test.ts, google-
// interactions-language-model.test.ts): google.tools.mcpServer exists with the
// id google.mcp_server, and the Interactions request still sends the real
// header values (only the telemetry copy is redacted, see pkg/telemetry).
func TestPortprovgMCPServerToolConstructor(t *testing.T) {
	args := map[string]interface{}{
		"name":         "local-mcp",
		"url":          "https://example.com/mcp",
		"headers":      map[string]string{"Authorization": "Bearer test-secret", "X-Custom-Key": "test-secret"},
		"allowedTools": []string{"getWeather"},
	}
	tool := MCPServerTool(args)
	if tool.ProviderID != "google.mcp_server" || tool.Name != "mcp_server" || !tool.ProviderExecuted || tool.Type != "provider" {
		t.Fatalf("tool = %+v", tool)
	}
	mapped, warning := mapInteractionsProviderTool(tool)
	if warning != nil {
		t.Fatalf("warning: %+v", warning)
	}
	raw, _ := json.Marshal(mapped)
	want := `{"allowed_tools":["getWeather"],"headers":{"Authorization":"Bearer test-secret","X-Custom-Key":"test-secret"},"name":"local-mcp","type":"mcp_server","url":"https://example.com/mcp"}`
	if string(raw) != want {
		t.Fatalf("wire tool = %s", raw)
	}
	if args["headers"].(map[string]string)["Authorization"] != "Bearer test-secret" {
		t.Fatal("tool args were modified")
	}
}

func TestPortprovgInteractionsRequestKeepsMCPCredentials(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"v1","status":"completed","steps":[],"usage":{"total_tokens":1}}`)
	}))
	defer server.Close()
	p := New(Config{APIKey: "k", BaseURL: server.URL})
	model := NewInteractionsLanguageModel(p, ModelGemini25Flash)
	_, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools:  []types.Tool{MCPServerTool(map[string]interface{}{"url": "https://example.com/mcp", "headers": map[string]string{"Authorization": "Bearer test-secret"}})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"Authorization":"Bearer test-secret"`) {
		t.Fatalf("request body lost the credential: %s", body)
	}
}

const portprovgGenerateResponse = `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`
const portprovgInteractionResponse = `{"id":"v1","status":"completed","steps":[],"usage":{"total_tokens":1}}`

func portprovgServer(t *testing.T, resp string) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var got map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(server.Close)
	return server, &got
}

func portprovgLevel(r types.ReasoningLevel) *types.ReasoningLevel { return &r }

func portprovgWarningDetails(ws []types.Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Type+"/"+w.Feature+": "+w.Details)
	}
	return out
}

// TS google-language-model.test.ts "Gemini 4 Hydrogen" and the xhigh
// it.each rows (4e43721f73): Hydrogen and Neon accept xhigh and have no
// minimal level; other Gemini 4 models cap at high with a compatibility
// warning.
func TestPortprovgGemini4ThinkingLevels(t *testing.T) {
	cases := []struct {
		model     string
		reasoning types.ReasoningLevel
		want      string
		warns     bool
	}{
		{"gemini-4-hydrogen", types.ReasoningLow, "low", false},
		{"gemini-4-hydrogen", types.ReasoningMedium, "medium", false},
		{"gemini-4-hydrogen", types.ReasoningHigh, "high", false},
		{"gemini-4-hydrogen", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-hydrogen-eap", types.ReasoningXHigh, "xhigh", false},
		{"models/gemini-4-hydrogen-eap", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-hydrogen", types.ReasoningMax, "xhigh", true},
		{"gemini-4-hydrogen", types.ReasoningMinimal, "low", true},
		{"gemini-4-neon", types.ReasoningXHigh, "xhigh", false},
		{"gemini-4-argon", types.ReasoningXHigh, "high", true},
		{"gemini-4-carbon", types.ReasoningXHigh, "high", true},
		{"gemini-4-hydrogenous", types.ReasoningXHigh, "high", true},
	}
	for _, tc := range cases {
		t.Run(tc.model+"/"+string(tc.reasoning), func(t *testing.T) {
			server, body := portprovgServer(t, portprovgGenerateResponse)
			model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), tc.model)
			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: portprovgLevel(tc.reasoning)})
			if err != nil {
				t.Fatal(err)
			}
			gc, _ := (*body)["generationConfig"].(map[string]interface{})
			tcfg, _ := gc["thinkingConfig"].(map[string]interface{})
			if tcfg["thinkingLevel"] != tc.want {
				t.Fatalf("thinkingLevel = %v, want %s (body %v)", tcfg["thinkingLevel"], tc.want, *body)
			}
			if tc.warns != (len(result.Warnings) > 0) {
				t.Fatalf("warnings = %v", portprovgWarningDetails(result.Warnings))
			}
		})
	}
}

// "passes an explicit xhigh thinking level instead of portable reasoning" and
// "leaves the thinking level unset for the API default".
func TestPortprovgHydrogenExplicitAndDefaultThinking(t *testing.T) {
	server, body := portprovgServer(t, portprovgGenerateResponse)
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGemini4Hydrogen)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:          types.Prompt{Text: "hi"},
		Reasoning:       portprovgLevel(types.ReasoningLow),
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"thinkingConfig": map[string]interface{}{"thinkingLevel": "xhigh"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gc, _ := (*body)["generationConfig"].(map[string]interface{})
	if tcfg, _ := gc["thinkingConfig"].(map[string]interface{}); tcfg["thinkingLevel"] != "xhigh" || len(result.Warnings) != 0 {
		t.Fatalf("explicit: body %v warnings %v", *body, result.Warnings)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}); err != nil {
		t.Fatal(err)
	}
	gc, _ = (*body)["generationConfig"].(map[string]interface{})
	if _, has := gc["thinkingConfig"]; has {
		t.Fatalf("default call sent thinkingConfig: %v", gc)
	}
}

// TS google-interactions-language-model.test.ts (4e43721f73, 8e60cb4ba3):
// xhigh thinkingLevel passes for Hydrogen, top-level reasoning maps onto the
// model's levels, other models warn and fall back, agent calls drop it.
func TestPortprovgInteractionsReasoning(t *testing.T) {
	type tc struct {
		name      string
		model     string
		reasoning *types.ReasoningLevel
		option    string
		want      interface{}
		warnings  []string
	}
	cases := []tc{
		{"option xhigh hydrogen", "gemini-4-hydrogen", nil, "xhigh", "xhigh", nil},
		{"option xhigh hydrogen-eap", "gemini-4-hydrogen-eap", nil, "xhigh", "xhigh", nil},
		{"option wins over reasoning", "gemini-2.5-flash", portprovgLevel(types.ReasoningXHigh), "high", "high", nil},
		{"reasoning hydrogen", "gemini-4-hydrogen", portprovgLevel(types.ReasoningXHigh), "", "xhigh", nil},
		{"reasoning argon falls back", "gemini-4-argon", portprovgLevel(types.ReasoningXHigh), "", "high",
			[]string{`compatibility/reasoning: reasoning "xhigh" is not directly supported by this model. mapped to effort "high".`}},
		{"reasoning on a budget model", "gemini-2.5-flash", portprovgLevel(types.ReasoningHigh), "", nil,
			[]string{`unsupported/reasoning: reasoning "high" is not supported by this model.`}},
		{"provider-default reasoning", "gemini-4-hydrogen", portprovgLevel(types.ReasoningDefault), "", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, stream := range []bool{false, true} {
				server, body := portprovgServer(t, portprovgInteractionResponse)
				model := NewInteractionsLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), c.model)
				opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: c.reasoning}
				if c.option != "" {
					opts.ProviderOptions = map[string]interface{}{"google": map[string]interface{}{"thinkingLevel": c.option}}
				}
				var warnings []types.Warning
				if stream {
					// Only the request body matters on the stream path.
					server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						raw, _ := io.ReadAll(r.Body)
						_ = json.Unmarshal(raw, body)
						w.Header().Set("Content-Type", "text/event-stream")
						writeSSE(w, `{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","usage":{"total_tokens":1}}}`)
					})
					s, err := model.DoStream(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					_ = s.Close()
				} else {
					result, err := model.DoGenerate(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					warnings = result.Warnings
				}
				gc, _ := (*body)["generation_config"].(map[string]interface{})
				if gc["thinking_level"] != c.want {
					t.Fatalf("stream=%v thinking_level = %v, want %v (body %v)", stream, gc["thinking_level"], c.want, *body)
				}
				if !stream && strings.Join(portprovgWarningDetails(warnings), "|") != strings.Join(c.warnings, "|") {
					t.Fatalf("warnings = %v, want %v", portprovgWarningDetails(warnings), c.warnings)
				}
			}
		})
	}
}

func TestPortprovgInteractionsAgentDropsReasoning(t *testing.T) {
	server, _ := portprovgServer(t, portprovgInteractionResponse)
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).InteractionsAgent(InteractionsAgentDeepResearch)
	if err != nil {
		t.Fatal(err)
	}
	im := model.(*InteractionsLanguageModel)
	_, warnings, _, err := im.buildArgs(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, Reasoning: portprovgLevel(types.ReasoningHigh)}, false)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range warnings {
		if strings.Contains(w.Message, "reasoning") && strings.Contains(w.Message, "not supported when an agent is set") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no agent warning naming reasoning: %v", warnings)
	}
}

// TS 1145cd58eb row: gemini-nano-banana-2.1 takes no function tools, so a
// function tool next to a provider tool is not combined.
func TestPortprovgNanoBananaHasNoMixedTools(t *testing.T) {
	server, _ := portprovgServer(t, portprovgGenerateResponse)
	model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: server.URL}), ModelGeminiNanoBanana21)
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		Tools: []types.Tool{
			GoogleSearchTool(),
			{Type: types.ToolTypeFunction, Name: "f", Description: "d", Parameters: map[string]interface{}{"type": "object"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, w := range result.Warnings {
		if w.Feature == "combination of function and provider-defined tools" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v", portprovgWarningDetails(result.Warnings))
	}
}
