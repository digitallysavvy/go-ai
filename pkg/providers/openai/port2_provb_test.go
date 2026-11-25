package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

var _ provider.ExtractionModelProvider = (*Provider)(nil)

// Ported from packages/openai/src/openai-extraction.test.ts "extracts typed
// fields with Responses structured output".
func TestPort2provbExtractionModelStructuredOutput(t *testing.T) {
	fixture, err := os.ReadFile("testdata/extraction.json")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var calls int
	var path string
	var body map[string]interface{}
	url, closeServer := newOpenAIIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(closeServer)
	p := New(Config{APIKey: "test-key", BaseURL: url + "/v1"})

	model, err := p.ExtractionModel("gpt-6.1-sol")
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider() != "openai.extraction" || model.ModelID() != "gpt-6.1-sol" {
		t.Fatalf("metadata = %q %q", model.Provider(), model.ModelID())
	}
	two := 2
	res, err := model.DoExtract(context.Background(), provider.ExtractionCallOptions{
		State:         []provider.DecisionStatePart{provider.DecisionTextPart("Invoice INV-2041. Total due $1,250.00 by March 3. Contacts: Ana, Bo.")},
		ReferenceDate: "2026-11-11",
		Fields: map[string]provider.ExtractionField{
			"invoiceNumber": {Type: "text"},
			"total":         {Type: "number", Decimals: &two},
			"dueDate":       {Type: "date"},
			"contacts":      {Type: "list", Items: "text"},
			"purchaseOrder": {Type: "text"},
		},
		FieldOrder: []string{"invoiceNumber", "total", "dueDate", "contacts", "purchaseOrder"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]provider.ExtractionAnswer{
		"contacts":      {Type: "list", Value: []interface{}{"Ana", "Bo"}},
		"dueDate":       {Type: "date", Value: "2027-03-03"},
		"invoiceNumber": {Type: "text", Value: "INV-2041"},
		"purchaseOrder": {Type: "text", Value: nil},
		"total":         {Type: "number", Value: float64(1250)},
	}
	if !reflect.DeepEqual(res.Answers, want) {
		got, _ := json.Marshal(res.Answers)
		t.Fatalf("answers = %s", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	if !strings.HasSuffix(path, "/v1/responses") {
		t.Fatalf("path = %q", path)
	}
	format := body["text"].(map[string]interface{})["format"].(map[string]interface{})
	if format["type"] != "json_schema" || format["name"] != "extraction" || format["strict"] != true {
		t.Fatalf("format = %v", format)
	}
	req := format["schema"].(map[string]interface{})["required"]
	if !reflect.DeepEqual(req, []interface{}{"f0", "f1", "f2", "f3", "f4"}) {
		t.Fatalf("required = %v", req)
	}
}

func TestPort2provbExtractionModelProviderName(t *testing.T) {
	m, err := New(Config{APIKey: "k", Name: "custom"}).ExtractionModel("m")
	if err != nil {
		t.Fatal(err)
	}
	if m.Provider() != "custom.extraction" {
		t.Fatalf("provider = %q", m.Provider())
	}
	if _, err := New(Config{APIKey: "k"}).ExtractionModel(""); err == nil {
		t.Fatal("empty model ID must fail")
	}
}
