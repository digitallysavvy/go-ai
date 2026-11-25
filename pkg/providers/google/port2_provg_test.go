package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS: google-extraction.test.ts 'extracts typed fields with Gemini structured output'
func TestPortprovgGoogleExtractionModel(t *testing.T) {
	fixture, err := os.ReadFile("testdata/port2/extraction.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1beta"})
	var _ provider.ExtractionModelProvider = p
	model, err := p.ExtractionModel("gemini-3.8-flash")
	if err != nil {
		t.Fatal(err)
	}
	decimals := 2
	result, err := model.DoExtract(context.Background(), provider.ExtractionCallOptions{
		State:         []provider.DecisionStatePart{provider.DecisionTextPart("Invoice INV-2041. Total due $1,250.00 by March 3. Contacts: Ana, Bo.")},
		ReferenceDate: "2026-11-11",
		Fields: map[string]provider.ExtractionField{
			"invoiceNumber": {Type: "text"},
			"total":         {Type: "number", Decimals: &decimals},
			"dueDate":       {Type: "date"},
			"contacts":      {Type: "list", Items: "text"},
			"purchaseOrder": {Type: "text"},
		},
		FieldOrder: []string{"invoiceNumber", "total", "dueDate", "contacts", "purchaseOrder"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider() != "google.extraction" || model.ModelID() != "gemini-3.8-flash" {
		t.Fatalf("provider %q model %q", model.Provider(), model.ModelID())
	}
	want := map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "text", Value: "INV-2041"},
		"total":         {Type: "number", Value: float64(1250)},
		"dueDate":       {Type: "date", Value: "2027-03-03"},
		"contacts":      {Type: "list", Value: []interface{}{"Ana", "Bo"}},
		"purchaseOrder": {Type: "text", Value: nil},
	}
	if !reflect.DeepEqual(result.Answers, want) {
		t.Fatalf("answers = %#v", result.Answers)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if gotPath != "/v1beta/models/gemini-3.8-flash:generateContent" {
		t.Fatalf("path = %q", gotPath)
	}
	cfg, _ := gotBody["generationConfig"].(map[string]interface{})
	if cfg["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig = %v", cfg)
	}
	schema, _ := cfg["responseJsonSchema"].(map[string]interface{})
	if !reflect.DeepEqual(schema["required"], []interface{}{"f0", "f1", "f2", "f3", "f4"}) {
		t.Fatalf("required = %v", schema["required"])
	}
}
