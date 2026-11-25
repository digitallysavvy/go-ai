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
