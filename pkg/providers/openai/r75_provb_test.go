package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"

	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TestR75ProvbFunctionToolOutputSchemaNormalized: TS prepareFunctionTool
// normalizes options.outputSchema like the input schema (propertyNames
// removed with a compatibility warning), and its warnings come after the
// async warning and the input schema's (resolveAsyncToolOption runs first).
// Go sent the output schema raw and put the async warning last.
func TestR75ProvbFunctionToolOutputSchemaNormalized(t *testing.T) {
	async := true
	tools := []interface{}{responses.FunctionToolDef{
		Type: "function", Name: "f", Async: &async,
		Parameters:   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"q": map[string]interface{}{"type": "string", "pattern": "^(?=a)"}}},
		OutputSchema: map[string]interface{}{"type": "object", "propertyNames": map[string]interface{}{"type": "string"}},
	}}
	warnings, err := normalizeResponsesToolSchemas(tools, false)
	if err != nil {
		t.Fatal(err)
	}
	fn := tools[0].(responses.FunctionToolDef)
	if _, kept := fn.OutputSchema.(map[string]interface{})["propertyNames"]; kept {
		t.Errorf("output schema not normalized: %v", fn.OutputSchema)
	}
	var features []string
	for _, w := range warnings {
		features = append(features, w.Feature)
	}
	b, _ := json.Marshal(features)
	if string(b) != `["async tool calling for \"f\"","JSON Schema pattern with regex lookaround","JSON Schema propertyNames"]` {
		t.Errorf("warning order = %s (%#v)", b, warnings)
	}
}

// TestR75ProvbFunctionToolRequestBody: the request carries the normalized
// output_schema and an explicit empty allowed_callers list.
func TestR75ProvbFunctionToolRequestBody(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","created_at":1,"status":"completed","model":"gpt-5","output":[{"type":"message","id":"m","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()
	model := NewResponsesLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gpt-5")
	tool := types.Tool{Name: "f", Parameters: map[string]interface{}{"type": "object"}, ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{
		"allowedCallers": []interface{}{},
		"outputSchema":   map[string]interface{}{"type": "object", "propertyNames": map[string]interface{}{"type": "string"}},
	}}}
	res, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{tool}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"allowed_callers":[]`) || strings.Contains(body, "propertyNames") || !strings.Contains(body, `"output_schema":{"type":"object"}`) {
		t.Errorf("request body = %s", body)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || w.Feature == "JSON Schema propertyNames"
	}
	if !found {
		t.Errorf("warnings = %#v", res.Warnings)
	}
}
