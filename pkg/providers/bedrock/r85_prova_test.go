package bedrock

import (
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

func r85provaArgs(t *testing.T, modelID string, opts *provider.GenerateOptions) *converseArgs {
	t.Helper()
	if opts.Prompt.Text == "" && len(opts.Prompt.Messages) == 0 {
		opts.Prompt = types.Prompt{Text: "hi"}
	}
	m := NewLanguageModel(New(Config{APIKey: "k", Region: "us-east-1"}), modelID)
	args, err := m.getArgs(opts)
	if err != nil {
		t.Fatalf("%s: getArgs: %v", modelID, err)
	}
	return args
}

func r85provaFields(args *converseArgs) map[string]interface{} {
	f, _ := args.Body["additionalModelRequestFields"].(map[string]interface{})
	return f
}

// The jsonTool instruction keeps the caller's schema key order (TS
// JSON.stringify); the TS replay of this case is skipped because tsfixture
// hands the schema over as a sorted Go map.
func TestR85provaBedrockJSONInstructionKeepsSchemaOrder(t *testing.T) {
	s, err := schema.NewOrderedSchema([]byte(`{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`))
	if err != nil {
		t.Fatal(err)
	}
	args := r85provaArgs(t, "us.anthropic.claude-opus-5-5", &provider.GenerateOptions{
		ResponseFormat:  &provider.ResponseFormat{Type: "json", Schema: s},
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"structuredOutputMode": "jsonTool"}},
	})
	system, _ := args.Body["system"].([]map[string]interface{})
	if len(system) != 1 || !strings.Contains(system[0]["text"].(string), `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`) {
		t.Fatalf("system = %#v", args.Body["system"])
	}
}

// TS 22b44614a2: reasoning none on Anthropic models sends thinking disabled,
// nothing on Claude 3.5 and older, and a between_tools / adaptive fallback
// with a warning on models that reject disabled thinking.
func TestR85provaBedrockDisabledThinking(t *testing.T) {
	none := types.ReasoningNone
	cases := []struct {
		model      string
		wantType   interface{}
		wantEffort interface{}
		warn       string
	}{
		{"anthropic.claude-sonnet-4-5-20250929-v1:0", "disabled", nil, ""},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", nil, nil, ""},
		{"anthropic.claude-3-haiku-20240307-v1:0", nil, nil, ""},
		{"anthropic.claude-sonnet-5-5", "between_tools", nil, "thinking cannot be disabled for anthropic.claude-sonnet-5-5. Using 'between_tools' thinking instead."},
		{"anthropic.claude-opus-5-5", "adaptive", "low", "thinking cannot be disabled for anthropic.claude-opus-5-5. Using 'adaptive' thinking instead."},
	}
	for _, c := range cases {
		args := r85provaArgs(t, c.model, &provider.GenerateOptions{Reasoning: &none})
		fields := r85provaFields(args)
		var gotType interface{}
		if th, ok := fields["thinking"].(map[string]interface{}); ok {
			gotType = th["type"]
		}
		if gotType != c.wantType {
			t.Errorf("%s: thinking.type = %v, want %v", c.model, gotType, c.wantType)
		}
		var gotEffort interface{}
		if oc, ok := fields["output_config"].(map[string]interface{}); ok {
			gotEffort = oc["effort"]
		}
		if gotEffort != c.wantEffort {
			t.Errorf("%s: output_config.effort = %v, want %v", c.model, gotEffort, c.wantEffort)
		}
		found := c.warn == ""
		for _, w := range args.Warnings {
			if w.Details == c.warn && w.Type == "compatibility" && w.Feature == "reasoning" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: warnings = %#v, want %q", c.model, args.Warnings, c.warn)
		}
	}
}

// TS 22b44614a2: an explicit disabled reasoningConfig on a model that rejects
// it warns as unsupported, and an effort above high is lowered to high with
// between_tools thinking.
func TestR85provaBedrockExplicitDisabledLowersEffort(t *testing.T) {
	args := r85provaArgs(t, "anthropic.claude-sonnet-5-5", &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{
			"reasoningConfig": map[string]interface{}{"type": "disabled", "maxReasoningEffort": "max"},
		}},
	})
	fields := r85provaFields(args)
	if th, _ := fields["thinking"].(map[string]interface{}); th["type"] != "between_tools" {
		t.Fatalf("thinking = %v", fields["thinking"])
	}
	if oc, _ := fields["output_config"].(map[string]interface{}); oc["effort"] != "high" {
		t.Fatalf("output_config = %v", fields["output_config"])
	}
	want := map[string]string{
		"providerOptions.amazonBedrock.reasoningConfig":                    "thinking cannot be disabled for anthropic.claude-sonnet-5-5. Using 'between_tools' thinking instead.",
		"providerOptions.amazonBedrock.reasoningConfig.maxReasoningEffort": "effort 'max' is not supported by anthropic.claude-sonnet-5-5 with 'between_tools' thinking. The effort has been lowered to 'high'.",
	}
	for _, w := range args.Warnings {
		if d, ok := want[w.Feature]; ok && d == w.Details && w.Type == "unsupported" {
			delete(want, w.Feature)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing warnings %v in %#v", want, args.Warnings)
	}
}

// TS 2cd0a97424: with the JSON response tool, toolChoice none drops the user
// tools and a named toolChoice keeps only that tool.
func TestR85provaBedrockJSONToolFiltersUserTools(t *testing.T) {
	s := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"name": map[string]interface{}{"type": "string"}}, "required": []interface{}{"name"}}
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Type: types.ToolTypeFunction, Name: "b", Parameters: map[string]interface{}{"type": "object"}},
	}
	for _, c := range []struct {
		choice types.ToolChoice
		want   []string
	}{
		{types.ToolChoice{Type: types.ToolChoiceNone}, []string{"json"}},
		{types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "b"}, []string{"b", "json"}},
		{types.ToolChoice{Type: types.ToolChoiceAuto}, []string{"a", "b", "json"}},
	} {
		args := r85provaArgs(t, "anthropic.claude-sonnet-4-5-20250929-v1:0", &provider.GenerateOptions{
			Tools:           tools,
			ToolChoice:      c.choice,
			ResponseFormat:  &provider.ResponseFormat{Type: "json", Schema: s},
			ProviderOptions: map[string]interface{}{"bedrock": map[string]interface{}{"structuredOutputMode": "jsonTool"}},
		})
		tc, _ := args.Body["toolConfig"].(map[string]interface{})
		specs, _ := tc["tools"].([]map[string]interface{})
		var names []string
		for _, spec := range specs {
			names = append(names, spec["toolSpec"].(map[string]interface{})["name"].(string))
		}
		if strings.Join(names, ",") != strings.Join(c.want, ",") {
			t.Errorf("%v: tools = %v, want %v", c.choice, names, c.want)
		}
	}
}

// TS 62a4dcd641: a named tool choice on an Anthropic model that accepts
// forced tool use keeps every function tool and disables parallel tool use
// by default; false opts out.
func TestR85provaBedrockNamedToolChoiceKeepsTools(t *testing.T) {
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "a", Parameters: map[string]interface{}{"type": "object"}},
		{Type: types.ToolTypeFunction, Name: "b", Parameters: map[string]interface{}{"type": "object"}},
	}
	choice := types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "b"}
	res := prepareBedrockTools(tools, choice, true, "anthropic.claude-sonnet-4-5-20250929-v1:0", "", nil, nil, false)
	if len(res.ToolConfig.Tools) != 2 {
		t.Fatalf("tools = %#v", res.ToolConfig.Tools)
	}
	tcChoice, _ := res.AdditionalTools["tool_choice"].(map[string]interface{})
	if tcChoice["type"] != "tool" || tcChoice["name"] != "b" || tcChoice["disable_parallel_tool_use"] != true {
		t.Fatalf("tool_choice = %#v", res.AdditionalTools)
	}
	off := false
	res = prepareBedrockTools(tools, choice, true, "anthropic.claude-sonnet-4-5-20250929-v1:0", "", nil, &off, false)
	if len(res.ToolConfig.Tools) != 2 || res.AdditionalTools != nil || res.ToolConfig.ToolChoice == nil {
		t.Fatalf("opt-out: %#v / %#v", res.ToolConfig, res.AdditionalTools)
	}
	res = prepareBedrockTools(tools, choice, true, "meta.llama3-70b-instruct-v1:0", "", nil, nil, false)
	if len(res.ToolConfig.Tools) != 1 || res.AdditionalTools != nil {
		t.Fatalf("non-Anthropic: %#v / %#v", res.ToolConfig, res.AdditionalTools)
	}
}

// TS 61a315e333: reasoning none on OpenAI models with unknown efforts is sent
// as is, gpt-oss rounds it to low, Nova 2 sends nothing.
func TestR85provaBedrockNoneReasoning(t *testing.T) {
	none := types.ReasoningNone
	var w []types.Warning
	if rc := resolveBedrockReasoningConfig(&none, nil, false, true, "openai.gpt-5.5", &w); rc == nil || rc.MaxReasoningEffort != "none" || len(w) != 0 {
		t.Fatalf("gpt-5.5: %#v %#v", rc, w)
	}
	w = nil
	if rc := resolveBedrockReasoningConfig(&none, nil, false, true, "openai.gpt-oss-120b-1:0", &w); rc == nil || rc.MaxReasoningEffort != "low" || len(w) != 1 {
		t.Fatalf("gpt-oss: %#v %#v", rc, w)
	}
	w = nil
	if rc := resolveBedrockReasoningConfig(&none, nil, false, false, ModelAmazonNova2LiteV1, &w); rc == nil || rc.MaxReasoningEffort != "" || rc.Type != "" || len(w) != 0 {
		t.Fatalf("nova: %#v %#v", rc, w)
	}
	w = nil
	if rc := resolveBedrockReasoningConfig(&none, nil, false, false, "meta.llama3-70b-instruct-v1:0", &w); rc == nil || len(w) != 1 || w[0].Type != "unsupported" {
		t.Fatalf("llama: %#v %#v", rc, w)
	}
}

// TS b6de62e95a convert-to-amazon-bedrock-chat-messages.test.ts "should make
// duplicate document names unique": Bedrock rejects duplicate document names.
func TestR85provaBedrockUniqueDocumentNames(t *testing.T) {
	filenames := []string{
		"invoice.pdf", "invoice.pdf", "rates.v1.pdf", "rates.v2.pdf",
		"/workspace/.eve/attachments/ab12/a.pdf", "invoice (2).pdf", "document-1.pdf", "",
		strings.Repeat("a", 201) + ".pdf", strings.Repeat("a", 201) + ".pdf",
	}
	var parts []types.ContentPart
	for _, f := range filenames {
		parts = append(parts, types.FileContent{Data: []byte("pdf"), MediaType: "application/pdf", Filename: f})
	}
	_, messages, err := convertToBedrockChatMessages([]types.Message{{Role: types.RoleUser, Content: parts}}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"invoice", "invoice (2)", "rates", "rates (2)", "workspace", "invoice (2) (2)",
		"document-1", "document-1 (2)", strings.Repeat("a", 200), strings.Repeat("a", 196) + " (2)",
	}
	content, _ := messages[0]["content"].([]map[string]interface{})
	if len(content) != len(want) {
		t.Fatalf("content = %#v", messages[0]["content"])
	}
	for i, c := range content {
		doc, _ := c["document"].(map[string]interface{})
		if doc["name"] != want[i] {
			t.Errorf("document %d name = %v, want %q", i, doc["name"], want[i])
		}
	}
}

// amazon-bedrock-native-structured-output.test.ts (TS 2cd0a97424): Claude 5
// structured outputs through Converse.
func TestR85provaBedrockConverseClaude5StructuredOutput(t *testing.T) {
	answer := map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"answer": map[string]interface{}{"type": "string"}},
		"required": []interface{}{"answer"}, "additionalProperties": false,
	}
	toolNames := func(args *converseArgs) string {
		tc, _ := args.Body["toolConfig"].(map[string]interface{})
		specs, _ := tc["tools"].([]map[string]interface{})
		var names []string
		for _, s := range specs {
			names = append(names, s["toolSpec"].(map[string]interface{})["name"].(string))
		}
		return strings.Join(names, ",")
	}
	outputConfig := func(args *converseArgs) interface{} {
		return r85provaFields(args)["output_config"]
	}
	open := map[string]interface{}{"type": "object", "additionalProperties": true}
	tools := []types.Tool{
		{Type: types.ToolTypeFunction, Name: "lookup", Parameters: answer},
		{Type: types.ToolTypeFunction, Name: "unused", Parameters: answer},
	}
	for _, c := range []struct {
		choice types.ToolChoice
		want   string
	}{
		{types.ToolChoice{Type: types.ToolChoiceNone}, "json"},
		{types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "lookup"}, "lookup,json"},
		{types.ToolChoice{Type: types.ToolChoiceAuto}, "lookup,unused,json"},
		{types.ToolChoice{Type: types.ToolChoiceRequired}, "lookup,unused,json"},
	} {
		args := r85provaArgs(t, "anthropic.claude-opus-5", &provider.GenerateOptions{
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: open}, ToolChoice: c.choice, Tools: tools,
		})
		if outputConfig(args) != nil || toolNames(args) != c.want {
			t.Errorf("%v: tools = %s, want %s", c.choice, toolNames(args), c.want)
		}
	}
	for _, id := range []string{"anthropic.claude-opus-5", "anthropic.claude-opus-5-5", "anthropic.claude-sonnet-5", "anthropic.claude-sonnet-5-5", "anthropic.claude-haiku-5-5"} {
		args := r85provaArgs(t, id, &provider.GenerateOptions{
			ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: answer}, Tools: tools[:1],
		})
		oc, _ := outputConfig(args).(map[string]interface{})
		if format, _ := oc["format"].(map[string]interface{}); format["type"] != "json_schema" || toolNames(args) != "lookup" {
			t.Errorf("%s: output_config = %v tools = %s", id, outputConfig(args), toolNames(args))
		}
	}
	optional := map[string]interface{}{}
	for i := 0; i < 11; i++ {
		optional[fmt.Sprintf("f%d", i)] = map[string]interface{}{"type": "string"}
	}
	for name, s := range map[string]interface{}{
		"an open object":               open,
		"a recursive reference":        map[string]interface{}{"type": "object", "properties": map[string]interface{}{"child": map[string]interface{}{"$ref": "#"}}, "required": []interface{}{"child"}, "additionalProperties": false},
		"too many optional properties": map[string]interface{}{"type": "object", "properties": optional, "additionalProperties": false},
	} {
		args := r85provaArgs(t, "anthropic.claude-opus-5", &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s}})
		tc, _ := args.Body["toolConfig"].(map[string]interface{})
		if outputConfig(args) != nil || toolNames(args) != "json" || fmt.Sprint(tc["toolChoice"]) != "map[any:map[]]" {
			t.Errorf("%s opus-5: output_config = %v toolConfig = %v", name, outputConfig(args), tc)
		}
		args = r85provaArgs(t, "anthropic.claude-sonnet-5-5", &provider.GenerateOptions{ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: s}})
		system, _ := args.Body["system"].([]map[string]interface{})
		if outputConfig(args) != nil || args.Body["toolConfig"] != nil || len(system) == 0 || !strings.Contains(system[0]["text"].(string), "JSON schema:") {
			t.Errorf("%s sonnet-5-5: body = %v", name, args.Body)
		}
	}
}

// TS 4ae6986cd8: a string tool input holding a JSON object is sent as that
// object (key order kept); other strings are wrapped as rawInvalidInput.
func TestR85provaBedrockStringToolInput(t *testing.T) {
	if got := toBedrockToolInput(`"{\"b\":2,\"a\":1}"`, nil); fmt.Sprint(got) != "map[a:1 b:2]" {
		t.Errorf("object text: %v", got)
	}
	if got := toBedrockToolInput(`"[1]"`, nil); fmt.Sprint(got) != "map[rawInvalidInput:[1]]" {
		t.Errorf("array text: %v", got)
	}
	if got := bedrockToolInputOrderHint(`"{\"b\":2,\"a\":1}"`); got != `{"b":2,"a":1}` {
		t.Errorf("hint = %q", got)
	}
}

// CR r85 provb -> prova item 1: JSON bodies are decoded like TS
// response.text() (BOM dropped, invalid UTF-8 replaced).
func TestR85provaBedrockJSONBodyDecoded(t *testing.T) {
	got, err := readJSONResponseBody(strings.NewReader("\uFEFF{\"a\":\"\xff\"}"))
	if err != nil || string(got) != "{\"a\":\"�\"}" {
		t.Fatalf("got %q, %v", got, err)
	}
}
