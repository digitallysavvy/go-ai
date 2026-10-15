package openai

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

// A raw JSON schema keeps its property order on the wire (a JS object keeps
// insertion order) and still goes through the OpenAI normalization.
func TestRawSchemaKeepsPropertyOrderOnWire(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"zeta":{"type":"string","pattern":"(?=x)"},"alpha":{"type":"number"},"keys":{"propertyNames":{"type":"string"},"type":"object"}}}`)
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	m, _ := p.ChatModel("gpt-4o")
	_, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:         chatTestPrompt(),
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
		Tools:          []types.Tool{{Name: "t", Parameters: schema}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(rawBody, `"zeta"`); n != 2 {
		t.Fatalf("schema missing from body (%d): %s", n, rawBody)
	}
	for i := 0; i < 2; i++ {
		zi := strings.Index(rawBody, `"zeta"`)
		ai := strings.Index(rawBody, `"alpha"`)
		if zi < 0 || ai < 0 || zi > ai {
			t.Fatalf("property order lost: %s", rawBody)
		}
		rawBody = rawBody[ai+1:]
	}
	if strings.Contains(rawBody, "propertyNames") {
		t.Fatalf("schema not normalized: %s", rawBody)
	}
}
