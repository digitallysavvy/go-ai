package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS #22486: unknown future Gemini model IDs get the current tool
// surface and thinkingLevel config through Vertex.
func TestFutureGeminiModelsUseCurrentToolsAndThinking(t *testing.T) {
	for _, modelID := range []string{"gemini-4.0-flash", "gemini-future-latest", "models/gemini-4.0-flash"} {
		t.Run(modelID, func(t *testing.T) {
			var body map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Done"}],"role":"model"},"finishReason":"STOP"}]}`))
			}))
			defer server.Close()

			prov, err := New(Config{Project: "test-project", Location: "us-central1", AccessToken: "test-token", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			model, err := prov.LanguageModel(modelID)
			if err != nil {
				t.Fatal(err)
			}
			level := types.ReasoningHigh
			var tools []types.Tool
			for _, id := range []string{"google.google_search", "google.enterprise_web_search", "google.url_context", "google.code_execution", "google.file_search", "google.vertex_rag_store"} {
				tools = append(tools, types.Tool{Type: "provider", ProviderID: id, Name: id})
			}
			tools = append(tools, types.Tool{Type: types.ToolTypeFunction, Name: "lookup", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}})

			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:    types.Prompt{Text: "Hello"},
				Reasoning: &level,
				Tools:     tools,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("warnings = %#v", result.Warnings)
			}
			gc, _ := body["generationConfig"].(map[string]interface{})
			tc, _ := gc["thinkingConfig"].(map[string]interface{})
			if tc["thinkingLevel"] != "high" {
				t.Fatalf("thinkingConfig = %#v", tc)
			}
			rawTools, _ := body["tools"].([]interface{})
			wantKeys := []string{"googleSearch", "enterpriseWebSearch", "urlContext", "codeExecution", "fileSearch", "retrieval", "functionDeclarations"}
			if len(rawTools) != len(wantKeys) {
				t.Fatalf("tools = %#v", rawTools)
			}
			for i, k := range wantKeys {
				entry, _ := rawTools[i].(map[string]interface{})
				if _, ok := entry[k]; !ok {
					t.Errorf("tools[%d] = %#v, want key %q", i, entry, k)
				}
			}
			cfg, _ := body["toolConfig"].(map[string]interface{})
			fc, _ := cfg["functionCallingConfig"].(map[string]interface{})
			if fc["mode"] != "VALIDATED" {
				t.Errorf("toolConfig = %#v, want VALIDATED", cfg)
			}
		})
	}
}

// Ports google-vertex-language-model.test.ts "should preserve local JSON
// Schema references in tool requests" (express mode, API key).
func TestVertexToolSchemaKeepsLocalRefs(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Done"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer server.Close()
	prov, err := New(Config{APIKey: "test-api-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := prov.LanguageModel("gemini-2.5-flash")
	if err != nil {
		t.Fatal(err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{"locale":{"$ref":"#/$defs/Locale","description":"Locale for formatting"}},"required":["locale"],"additionalProperties":false,"$defs":{"Locale":{"type":"string","enum":["de","en"]}}}`), &params); err != nil {
		t.Fatal(err)
	}
	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "Hello"},
		Tools:  []types.Tool{{Type: types.ToolTypeFunction, Name: "format-date", Description: "Format a date", Parameters: params}},
	}); err != nil {
		t.Fatal(err)
	}
	tools := body["tools"].([]interface{})
	decl := tools[0].(map[string]interface{})["functionDeclarations"].([]interface{})[0].(map[string]interface{})
	got, _ := json.Marshal(decl["parametersJsonSchema"])
	var gotV, wantV interface{}
	_ = json.Unmarshal(got, &gotV)
	_ = json.Unmarshal([]byte(`{"type":"object","properties":{"locale":{"$ref":"#/$defs/Locale","description":"Locale for formatting"}},"required":["locale"],"additionalProperties":false,"$defs":{"Locale":{"type":"string","enum":["de","en"]}}}`), &wantV)
	if !reflect.DeepEqual(gotV, wantV) {
		t.Fatalf("parametersJsonSchema = %s", got)
	}
}
