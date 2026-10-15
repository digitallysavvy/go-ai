package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ai.GenerateObject passes a schema.Schema as ResponseFormat.Schema and Go
// callers pass one as Tool.Parameters; neither may reach the wire as {}.
func TestSchemaSchemaReachesTheWire(t *testing.T) {
	s := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"age": map[string]interface{}{"const": 3}},
	})
	body, _, _, err := makeTestModel("gemini-2.5-flash").buildRequest(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s},
		Tools:          []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: s}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	if strings.Count(string(b), `"age"`) != 2 || !strings.Contains(string(b), `"enum":[3]`) {
		t.Fatalf("schema lost: %s", b)
	}
}

func TestRawResponseSchemaKeepsPropertyOrder(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"zeta":{"const":1},"alpha":{"type":"string"}}}`)
	body, _, _, err := makeTestModel("gemini-2.5-flash").buildRequest(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: raw},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	if !strings.Contains(string(b), `"properties":{"zeta":{"enum":[1]},"alpha":{"type":"string"}}`) {
		t.Fatalf("property order lost: %s", b)
	}
}

// TS leaves rag_corpus out of the body when the tool has no ragCorpus.
func TestVertexRagStoreOmitsUndefinedCorpus(t *testing.T) {
	entry := buildNativeToolEntry(types.Tool{Type: "provider", ProviderID: "google.vertex_rag_store", ProviderArgs: map[string]interface{}{}})
	b, _ := json.Marshal(entry)
	if string(b) != `{"retrieval":{"vertex_rag_store":{"rag_resources":{}}}}` {
		t.Fatalf("entry = %s", b)
	}
	entry = buildNativeToolEntry(types.Tool{Type: "provider", ProviderID: "google.vertex_rag_store", ProviderArgs: map[string]interface{}{"ragCorpus": "projects/p/locations/l/ragCorpora/c", "topK": 3}})
	b, _ = json.Marshal(entry)
	if !strings.Contains(string(b), `"rag_corpus":"projects/p/locations/l/ragCorpora/c"`) || !strings.Contains(string(b), `"similarity_top_k":3`) {
		t.Fatalf("entry = %s", b)
	}
}

// A schema.OrderedSchema keeps its property order for the response schema and
// for function tools.
func TestOrderedSchemaKeepsOrderOnTheWire(t *testing.T) {
	ordered, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _, _, err := makeTestModel("gemini-2.5-flash").buildRequest(context.Background(), &provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "x"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: ordered},
		Tools:          []types.Tool{{Name: "t", Type: types.ToolTypeFunction, Parameters: ordered}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(body)
	if n := strings.Count(string(b), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`); n != 2 {
		t.Fatalf("property order kept %d times, want 2: %s", n, b)
	}
}
