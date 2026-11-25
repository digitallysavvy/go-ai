package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Twins of anthropic-extraction.test.ts (TS 6d5094711e).

const port2provaExtractionFixture = `{
  "model": "claude-sonnet-5-5",
  "id": "msg_011Cgx3YTkyZdTH6rYDdNVS1",
  "type": "message",
  "role": "assistant",
  "content": [{"type": "text", "text": "{\"f0\":\"INV-2041\",\"f1\":1250.00,\"f2\":\"2027-03-03\",\"f3\":[\"Ana\",\"Bo\"],\"f4\":null}"}],
  "stop_reason": "end_turn",
  "stop_sequence": null,
  "usage": {"input_tokens": 960, "output_tokens": 52}
}`

func port2provaSchema(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var req struct {
		OutputConfig struct {
			Format struct {
				Type   string                 `json:"type"`
				Schema map[string]interface{} `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("request body: %v", err)
	}
	if req.OutputConfig.Format.Type != "json_schema" {
		t.Fatalf("format type = %q, want json_schema", req.OutputConfig.Format.Type)
	}
	return req.OutputConfig.Format.Schema
}

func TestPort2provaExtractsTypedFieldsWithMessagesStructuredOutput(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(port2provaExtractionFixture))
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model, err := p.ExtractionModel("claude-sonnet-5-5")
	if err != nil {
		t.Fatalf("ExtractionModel: %v", err)
	}
	integer := 2
	result, err := model.DoExtract(t.Context(), provider.ExtractionCallOptions{
		State:         "Invoice INV-2041. Total due $1,250.00 by March 3. Contacts: Ana, Bo.",
		ReferenceDate: "2026-11-11",
		Fields: map[string]provider.ExtractionField{
			"invoiceNumber": {Type: "text"},
			"total":         {Type: "number", Decimals: &integer},
			"dueDate":       {Type: "date"},
			"contacts":      {Type: "list", Items: "text"},
			"purchaseOrder": {Type: "text"},
		},
		FieldOrder: []string{"invoiceNumber", "total", "dueDate", "contacts", "purchaseOrder"},
	})
	if err != nil {
		t.Fatalf("DoExtract: %v", err)
	}
	if model.Provider() != "anthropic.extraction" {
		t.Fatalf("Provider = %q", model.Provider())
	}
	if model.ModelID() != "claude-sonnet-5-5" {
		t.Fatalf("ModelID = %q", model.ModelID())
	}
	want := map[string]provider.ExtractionAnswer{
		"contacts":      {Type: "list", Value: []interface{}{"Ana", "Bo"}},
		"dueDate":       {Type: "date", Value: "2027-03-03"},
		"invoiceNumber": {Type: "text", Value: "INV-2041"},
		"purchaseOrder": {Type: "text", Value: nil},
		"total":         {Type: "number", Value: float64(1250)},
	}
	if !reflect.DeepEqual(result.Answers, want) {
		t.Fatalf("answers = %#v\nwant %#v", result.Answers, want)
	}
	if len(bodies) != 1 {
		t.Fatalf("calls = %d, want 1", len(bodies))
	}
	if paths[0] != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", paths[0])
	}
	schema := port2provaSchema(t, bodies[0])
	if !reflect.DeepEqual(schema["required"], []interface{}{"f0", "f1", "f2", "f3", "f4"}) {
		t.Fatalf("required = %v", schema["required"])
	}
}

func TestPort2provaSplitsMoreThan16FieldsIntoParallelCalls(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		var req struct {
			Messages []struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(b, &req); err != nil || len(req.Messages) == 0 || len(req.Messages[0].Content) == 0 {
			http.Error(w, "bad request", 400)
			return
		}
		var fields struct {
			Fields map[string]struct {
				ID string `json:"id"`
			} `json:"fields"`
		}
		if err := json.Unmarshal([]byte(req.Messages[0].Content[0].Text), &fields); err != nil {
			http.Error(w, "bad fields text: "+err.Error(), 400)
			return
		}
		out := map[string]float64{}
		for key, f := range fields.Fields {
			n, _ := strconv.Atoi(strings.Fields(f.ID)[1])
			out[key] = float64(n)
		}
		text, _ := json.Marshal(out)
		resp := map[string]interface{}{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-5-5",
			"content":     []interface{}{map[string]interface{}{"type": "text", "text": string(text)}},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 5},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1"})
	model, err := p.ExtractionModel("claude-sonnet-5-5")
	if err != nil {
		t.Fatalf("ExtractionModel: %v", err)
	}
	fields := map[string]provider.ExtractionField{}
	var order []string
	for i := 0; i < 17; i++ {
		id := fmt.Sprintf("price %d", i)
		fields[id] = provider.ExtractionField{Type: "number"}
		order = append(order, id)
	}
	result, err := model.DoExtract(t.Context(), provider.ExtractionCallOptions{
		State: "Prices.", Fields: fields, FieldOrder: order,
	})
	if err != nil {
		t.Fatalf("DoExtract: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("calls = %d, want 2", len(bodies))
	}
	counts := map[int]int{}
	for _, b := range bodies {
		props, _ := port2provaSchema(t, b)["properties"].(map[string]interface{})
		n := 0
		for _, v := range props {
			if m, ok := v.(map[string]interface{}); ok && m["anyOf"] != nil {
				n++
			}
		}
		counts[n]++
	}
	if counts[16] != 1 || counts[1] != 1 {
		t.Fatalf("union counts = %v, want one call of 16 and one of 1", counts)
	}
	for _, i := range []int{0, 16} {
		a := result.Answers[fmt.Sprintf("price %d", i)]
		if a.Type != "number" || a.Value != float64(i) {
			t.Fatalf("price %d = %+v", i, a)
		}
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 20 {
		t.Fatalf("summed usage = %+v", result.Usage)
	}
}

func TestPort2provaExtractionModelRejectsEmptyModelID(t *testing.T) {
	if _, err := New(Config{APIKey: "k"}).ExtractionModel(""); err == nil {
		t.Fatal("expected error for empty model ID")
	}
}
