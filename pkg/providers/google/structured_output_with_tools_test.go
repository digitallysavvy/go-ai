package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/google/src/structured-output-with-tools.test.ts
// (TS #21989).

func jsonToolOpts(choice types.ToolChoice) *provider.GenerateOptions {
	return &provider.GenerateOptions{
		Prompt: types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Description: "A date", Schema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{"date": map[string]interface{}{"type": "string"}},
		}},
		Tools:      []types.Tool{{Name: "resolveDate", Description: "d", Parameters: map[string]interface{}{"type": "object"}}},
		ToolChoice: choice,
	}
}

func serveGenerate(t *testing.T, response string, captured *map[string]interface{}) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
}

func functionDeclNames(t *testing.T, body map[string]interface{}) []string {
	t.Helper()
	var names []string
	for _, tool := range body["tools"].([]interface{}) {
		decls, _ := tool.(map[string]interface{})["functionDeclarations"].([]interface{})
		for _, d := range decls {
			names = append(names, d.(map[string]interface{})["name"].(string))
		}
	}
	return names
}

func TestGoogleJSONResponseToolForPreGemini3(t *testing.T) {
	var body map[string]interface{}
	srv := serveGenerate(t, `{"candidates":[{"content":{"role":"model","parts":[{"text":"chat"},{"functionCall":{"name":"json","args":{"date":"2031-06-17"}},"thoughtSignature":"sig"}]},"finishReason":"STOP"}],"usageMetadata":{}}`, &body)
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash-lite")
	res, err := m.DoGenerate(context.Background(), jsonToolOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	gc, _ := body["generationConfig"].(map[string]interface{})
	if gc["responseMimeType"] != nil || gc["responseSchema"] != nil || gc["responseJsonSchema"] != nil {
		t.Fatalf("generationConfig = %#v, want no JSON response format", gc)
	}
	if names := functionDeclNames(t, body); len(names) != 2 || names[1] != "json" {
		t.Fatalf("function declarations = %v", names)
	}
	mode := body["toolConfig"].(map[string]interface{})["functionCallingConfig"].(map[string]interface{})["mode"]
	if mode != "ANY" {
		t.Fatalf("mode = %v, want ANY", mode)
	}
	var texts []types.TextContent
	for _, p := range res.Content {
		if tc, ok := p.(types.TextContent); ok {
			texts = append(texts, tc)
		}
	}
	if len(texts) != 1 || texts[0].Text != `{"date":"2031-06-17"}` || len(res.ToolCalls) != 0 {
		t.Fatalf("content = %#v toolCalls = %#v", res.Content, res.ToolCalls)
	}
	if len(texts[0].ProviderMetadata) == 0 {
		t.Fatal("thought signature metadata should be preserved on the text")
	}
	if res.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish = %s, want stop", res.FinishReason)
	}
}

func TestGoogleGemini3KeepsNativeStructuredOutputWithAutoToolChoice(t *testing.T) {
	var body map[string]interface{}
	srv := serveGenerate(t, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{}"}]},"finishReason":"STOP"}]}`, &body)
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-3-pro-preview")
	if _, err := m.DoGenerate(context.Background(), jsonToolOpts(types.ToolChoice{Type: types.ToolChoiceAuto})); err != nil {
		t.Fatal(err)
	}
	if names := functionDeclNames(t, body); len(names) != 1 || names[0] != "resolveDate" {
		t.Fatalf("function declarations = %v, want only the application tool", names)
	}
	if gc, _ := body["generationConfig"].(map[string]interface{}); gc["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig = %#v, want native JSON response", gc)
	}
}

func TestGoogleGemini3FallsBackToJSONResponseToolWithForcedToolChoice(t *testing.T) {
	var body map[string]interface{}
	srv := serveGenerate(t, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{}"}]},"finishReason":"STOP"}]}`, &body)
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-3-pro-preview")
	if _, err := m.DoGenerate(context.Background(), jsonToolOpts(types.ToolChoice{Type: types.ToolChoiceRequired})); err != nil {
		t.Fatal(err)
	}
	if names := functionDeclNames(t, body); len(names) != 2 || names[1] != "json" {
		t.Fatalf("function declarations = %v", names)
	}
}

func TestGoogleJSONResponseToolKeepsApplicationCalls(t *testing.T) {
	var body map[string]interface{}
	srv := serveGenerate(t, `{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"resolveDate","args":{}}}]},"finishReason":"STOP"}]}`, &body)
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	res, err := m.DoGenerate(context.Background(), jsonToolOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ToolName != "resolveDate" || res.FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("result = %#v", res)
	}
}

func TestGoogleJSONResponseToolStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"chat"}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"json","args":{"date":"2031-06-17"}}}]},"finishReason":"STOP"}],"usageMetadata":{}}

`))
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	stream, err := m.DoStream(context.Background(), jsonToolOpts(types.ToolChoice{}))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	text := ""
	var finish types.FinishReason
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeText:
			text += c.Text
		case provider.ChunkTypeToolCall:
			t.Fatalf("unexpected tool call %#v", c)
		case provider.ChunkTypeFinish:
			finish = c.FinishReason
		}
	}
	if text != `{"date":"2031-06-17"}` || finish != types.FinishReasonStop {
		t.Fatalf("text=%q finish=%q", text, finish)
	}
}
