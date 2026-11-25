package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

var r85provaSchema = map[string]interface{}{
	"type":                 "object",
	"properties":           map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
	"required":             []interface{}{"answer"},
	"additionalProperties": false,
}

func r85provaGenerate(t *testing.T, modelID string, opts *provider.GenerateOptions) (map[string]interface{}, *types.GenerateResult) {
	t.Helper()
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"` + modelID + `","content":[{"type":"text","text":"{\"answer\":\"42\"}"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":20}}`))
	}))
	defer srv.Close()
	m, err := New(Config{BaseURL: srv.URL, BearerToken: "test-api-key"}).LanguageModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	opts.Prompt = types.Prompt{Text: "Hello"}
	res, err := m.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return body, res
}

func r85provaToolNames(body map[string]interface{}) string {
	var names []string
	tools, _ := body["tools"].([]interface{})
	for _, tl := range tools {
		names = append(names, tl.(map[string]interface{})["name"].(string))
	}
	return strings.Join(names, ",")
}

// amazon-bedrock-anthropic-structured-output.test.ts (TS 2cd0a97424,
// f4c5f1e959, b0d51fecc0): Claude 5 structured outputs through InvokeModel.
func TestR85provaBedrockAnthropicClaude5StructuredOutput(t *testing.T) {
	lookup := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "lookup", Parameters: r85provaSchema},
		{Type: types.ToolTypeFunction, Name: "unused", Parameters: r85provaSchema},
	}
	open := map[string]interface{}{"type": "object", "additionalProperties": true}
	for _, c := range []struct {
		choice types.ToolChoice
		want   string
	}{
		{types.ToolChoice{Type: types.ToolChoiceNone}, "json"},
		{types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "lookup"}, "lookup,json"},
	} {
		body, _ := r85provaGenerate(t, "us.anthropic.claude-sonnet-5-5", &provider.GenerateOptions{
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: open},
			ToolChoice:     c.choice,
			Tools:          lookup,
		})
		if body["output_config"] != nil || r85provaToolNames(body) != c.want {
			t.Errorf("%v: tools = %s, want %s (%v)", c.choice, r85provaToolNames(body), c.want, body)
		}
	}

	for _, id := range []string{"global.anthropic.claude-sonnet-5-5", "us.anthropic.claude-opus-5-5", "anthropic.claude-fable-5-1", "us.anthropic.claude-haiku-5-5", "us.anthropic.claude-sonnet-5", "us.anthropic.claude-opus-5"} {
		body, res := r85provaGenerate(t, id, &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: r85provaSchema}})
		oc, _ := body["output_config"].(map[string]interface{})
		format, _ := oc["format"].(map[string]interface{})
		if format["type"] != "json_schema" || body["tools"] != nil || res.Text != `{"answer":"42"}` {
			t.Errorf("%s: body = %v text = %q", id, body, res.Text)
		}
	}

	body, _ := r85provaGenerate(t, "us.anthropic.claude-haiku-5-5", &provider.GenerateOptions{
		ResponseFormat:  &provider.ResponseFormat{Type: "json", Schema: r85provaSchema},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"structuredOutputMode": "jsonTool"}},
	})
	if tc, _ := body["tool_choice"].(map[string]interface{}); body["output_config"] != nil || tc["type"] != "any" {
		t.Errorf("jsonTool haiku-5-5: body = %v", body)
	}

	recursive := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"child": map[string]interface{}{"$ref": "#"}}, "required": []interface{}{"child"}, "additionalProperties": false}
	for name, s := range map[string]interface{}{"an open object": open, "a recursive reference": recursive} {
		body, _ := r85provaGenerate(t, "us.anthropic.claude-sonnet-5-5", &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s}})
		tc, _ := body["tool_choice"].(map[string]interface{})
		if body["output_config"] != nil || r85provaToolNames(body) != "json" || tc["type"] != "auto" {
			t.Errorf("%s: body = %v", name, body)
		}
	}

	strict := true
	body, _ = r85provaGenerate(t, "us.anthropic.claude-haiku-5-5", &provider.GenerateOptions{
		Tools: []types.Tool{{Type: types.ToolTypeFunction, Name: "answer", Parameters: r85provaSchema, Strict: &strict}},
	})
	tools, _ := body["tools"].([]interface{})
	if len(tools) != 1 || tools[0].(map[string]interface{})["strict"] != true {
		t.Errorf("strict tool: body = %v", body)
	}
}
