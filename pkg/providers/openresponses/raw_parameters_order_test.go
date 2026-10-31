package openresponses

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFunctionToolRawParametersKeepOrder(t *testing.T) {
	ft := FunctionTool{Type: "function", Name: "t", Parameters: map[string]interface{}{"a": 1}, rawParameters: json.RawMessage(`{"z":1,"a":2}`)}
	b, err := json.Marshal(ft)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"parameters":{"z":1,"a":2}`) {
		t.Fatalf("got %s", b)
	}
	b, _ = json.Marshal(FunctionTool{Type: "function", Name: "t"})
	if string(b) != `{"type":"function","name":"t"}` {
		t.Fatalf("got %s", b)
	}
}
