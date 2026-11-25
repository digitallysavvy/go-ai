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
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// A raw JSON schema keeps its property order on the wire (a JS object keeps
// insertion order) and still goes through the OpenAI normalization.
func TestRawSchemaKeepsPropertyOrderOnWire(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"zeta":{"type":"string","pattern":"(?=x)"},"alpha":{"type":"number"},"keys":{"propertyNames":{"type":"string"},"type":"object"}}}`)
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rawBody = string(b)
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
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

// An *schema.OrderedSchema (tool parameters or response schema) behaves like
// raw JSON: normalized, and written in its source key order, by both the Chat
// and the Responses models.
func TestOrderedSchemaKeepsPropertyOrderOnWire(t *testing.T) {
	text := `{"type":"object","properties":{"zeta":{"type":"string","pattern":"(?=x)"},"alpha":{"type":"number"},"keys":{"propertyNames":{"type":"string"},"type":"object"}}}`
	for _, kind := range []string{"chat", "responses"} {
		t.Run(kind, func(t *testing.T) {
			ordered, err := schema.NewOrderedSchema([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			var rawBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				rawBody = string(b)
				if kind == "chat" {
					_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"{}"},"finish_reason":"stop"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"id":"r","output":[],"status":"completed"}`))
				}
			}))
			defer srv.Close()
			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			var m provider.LanguageModel
			if kind == "chat" {
				m, _ = p.ChatModel("gpt-4o")
			} else {
				m, _ = p.ResponsesModel("gpt-4o")
			}
			_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:         chatTestPrompt(),
				ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
				Tools:          []types.Tool{{Name: "t", Parameters: ordered}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(rawBody, `"zeta"`); n != 2 {
				t.Fatalf("schema missing from body (%d): %s", n, rawBody)
			}
			rest := rawBody
			for i := 0; i < 2; i++ {
				zi := strings.Index(rest, `"zeta"`)
				ai := strings.Index(rest, `"alpha"`)
				if zi < 0 || ai < 0 || zi > ai {
					t.Fatalf("property order lost: %s", rawBody)
				}
				rest = rest[ai+1:]
			}
			if strings.Contains(rawBody, "propertyNames") || strings.Contains(rawBody, "(?=x)") {
				t.Fatalf("schema not normalized: %s", rawBody)
			}
		})
	}
}
