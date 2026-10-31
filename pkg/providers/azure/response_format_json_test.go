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
)

// Core sends responseFormat type "json" (TS); "json" is not a wire value, so
// the Azure chat model maps it to json_schema / json_object.
func TestChatModel_JSONResponseFormatMapsToWireTypes(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = nil
		_ = json.Unmarshal(raw, &body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","model":"d","created":1,"choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	defer srv.Close()

	p := mustNewProvider(t, Config{APIKey: "k", BaseURL: srv.URL + "/openai", APIVersion: "v1"})
	model, err := p.ChatModel("d")
	if err != nil {
		t.Fatal(err)
	}
	prompt := types.Prompt{Text: "x"}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt, ResponseFormat: &provider.ResponseFormat{Type: "json"}}); err != nil {
		t.Fatal(err)
	}
	if rf, _ := body["response_format"].(map[string]interface{}); rf["type"] != "json_object" {
		t.Fatalf("no schema: response_format = %#v, want json_object", body["response_format"])
	}

	if _, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: prompt, ResponseFormat: &provider.ResponseFormat{
		Type: "json", Schema: map[string]interface{}{"type": "object"},
	}}); err != nil {
		t.Fatal(err)
	}
	rf, _ := body["response_format"].(map[string]interface{})
	if rf["type"] != "json_schema" || rf["json_schema"] == nil {
		t.Fatalf("schema: response_format = %#v, want json_schema", body["response_format"])
	}
}
