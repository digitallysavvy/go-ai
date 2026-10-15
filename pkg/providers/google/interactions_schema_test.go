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

// TS falls back to the x-gemini-service-tier response header when the body has
// no service_tier, for generate and for streams.
func TestInteractionsServiceTierFromHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-gemini-service-tier", "priority")
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"event_type\":\"interaction.complete\",\"interaction\":{\"id\":\"v1\",\"status\":\"completed\",\"usage\":{}}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, `{"id":"v1","status":"completed","model":"gemini-2.5-flash","steps":[{"type":"model_output","content":[{"type":"text","text":"hi"}]}],"usage":{}}`)
	}))
	defer server.Close()
	model, err := New(Config{APIKey: "k", BaseURL: server.URL}).Interactions(ModelGemini25Flash)
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}}
	res, err := model.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	var meta map[string]map[string]interface{}
	switch pm := interface{}(res.ProviderMetadata).(type) {
	case map[string]interface{}:
		b, _ := json.Marshal(pm)
		_ = json.Unmarshal(b, &meta)
	}
	if meta["google"]["serviceTier"] != "priority" {
		t.Fatalf("generate providerMetadata = %v", res.ProviderMetadata)
	}

	st, err := model.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := st.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || !strings.Contains(string(finish.ProviderMetadata), `"serviceTier":"priority"`) {
		t.Fatalf("finish chunk = %#v", finish)
	}
}
