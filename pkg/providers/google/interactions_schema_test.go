package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ai.GenerateObject passes a schema.Schema as ResponseFormat.Schema and Go
// callers pass one as Tool.Parameters; neither may reach the wire as {}.
func TestInteractionsSchemaSchemaReachesTheWire(t *testing.T) {
	var raw []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var m map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&m)
		raw, _ = json.Marshal(m)
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","steps":[{"type":"text","text":"{}"}],"usage":{}}`)
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatal(err)
	}
	s := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"age": map[string]interface{}{"type": "integer"}},
	})
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
		Tools:          []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: s}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"age"`); n != 2 {
		t.Fatalf("schema reached the wire %d times, want 2: %s", n, raw)
	}
}
