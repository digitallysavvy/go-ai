package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func jsonString(t *testing.T, v interface{}) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func buildReq(t *testing.T, m *LanguageModel, opts *provider.GenerateOptions, streaming bool) (map[string]interface{}, []types.Warning) {
	t.Helper()
	body, _, warnings, err := m.buildRequest(context.Background(), opts, streaming)
	if err != nil {
		t.Fatal(err)
	}
	return body, warnings
}

// TS google-model-capabilities.test.ts (f126649).
func TestGetModelCapabilities(t *testing.T) {
	cases := []struct {
		id                   string
		gemini2, fs, gemini3 bool
	}{
		{"gemini-pro", false, false, false},
		{"gemini-pro-vision", false, false, false},
		{"gemini-1.5-flash", false, false, false},
		{"gemini-robotics-er-1.5-preview", false, false, false},
		{"gemini-2.0-flash", true, false, false},
		{"gemini-2.5-flash", true, true, false},
		{"gemini-3.1-pro-preview", true, true, true},
		{"gemini-99-pro-preview", true, true, true},
		{"gemini-ultra-latest", true, true, true},
		{"nano-banana-pro-preview", true, false, false},
	}
	for _, tt := range cases {
		got := GetModelCapabilities(tt.id)
		want := ModelCapabilities{SupportsGemini2Tools: tt.gemini2, SupportsFileSearch: tt.fs, UsesGemini3Features: tt.gemini3}
		if got != want {
			t.Errorf("%s: got %+v, want %+v", tt.id, got, want)
		}
	}
}

// TS sanitize-response-json-schema.test.ts: const → enum, input not mutated (2a32459).
func TestSanitizeResponseJSONSchema(t *testing.T) {
	var schema map[string]interface{}
	_ = json.Unmarshal([]byte(`{"type":"object","properties":{"response":{"oneOf":[{"type":"object","properties":{"type":{"type":"string","const":"fruit"}},"required":["type"],"additionalProperties":false}]}},"required":["response"],"additionalProperties":false,"$defs":{"label":{"type":"string","const":"produce"}}}`), &schema)
	got := jsonString(t, sanitizeResponseJSONSchema(schema))
	want := `{"$defs":{"label":{"enum":["produce"],"type":"string"}},"additionalProperties":false,"properties":{"response":{"oneOf":[{"additionalProperties":false,"properties":{"type":{"enum":["fruit"],"type":"string"}},"required":["type"],"type":"object"}]}},"required":["response"],"type":"object"}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if !strings.Contains(jsonString(t, schema), `"const":"fruit"`) {
		t.Error("input schema was mutated")
	}
}

// TS: tools use parametersJsonSchema and response format uses responseJsonSchema (2a32459).
func TestBuildRequest_JSONSchemaKeys(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{"k": map[string]interface{}{"const": "v"}}}
	body, _ := buildReq(t, makeTestModel("gemini-2.5-flash"), &provider.GenerateOptions{
		Tools:          []types.Tool{{Name: "f", Description: "d", Parameters: schema}},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	}, false)
	decl := body["tools"].([]map[string]interface{})[0]["functionDeclarations"].([]map[string]interface{})[0]
	if jsonString(t, decl) != `{"description":"d","name":"f","parametersJsonSchema":{"properties":{"k":{"const":"v"}},"type":"object"}}` {
		t.Errorf("decl = %s", jsonString(t, decl))
	}
	gc := body["generationConfig"].(map[string]interface{})
	if _, ok := gc["responseSchema"]; ok {
		t.Error("responseSchema must not be sent")
	}
	if jsonString(t, gc["responseJsonSchema"]) != `{"properties":{"k":{"enum":["v"]}},"type":"object"}` {
		t.Errorf("responseJsonSchema = %s", jsonString(t, gc["responseJsonSchema"]))
	}
}

// TS google-prepare-tools.test.ts: forced tool choices keep ANY with strict tools (8e90283).
func TestPrepareTools_ForcedChoiceKeepsAnyWithStrict(t *testing.T) {
	tools := []types.Tool{{Name: "f", Strict: true}}
	cases := []struct {
		choice types.ToolChoice
		want   string
	}{
		{types.ToolChoice{}, `{"functionCallingConfig":{"mode":"VALIDATED"}}`},
		{types.ToolChoice{Type: types.ToolChoiceAuto}, `{"functionCallingConfig":{"mode":"VALIDATED"}}`},
		{types.ToolChoice{Type: types.ToolChoiceRequired}, `{"functionCallingConfig":{"mode":"ANY"}}`},
		{types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "f"}, `{"functionCallingConfig":{"allowedFunctionNames":["f"],"mode":"ANY"}}`},
		{types.ToolChoice{Type: types.ToolChoiceNone}, `{"functionCallingConfig":{"mode":"NONE"}}`},
	}
	for _, tt := range cases {
		got := prepareTools(tools, tt.choice, "gemini-2.5-flash", false)
		if s := jsonString(t, got.toolConfig); s != tt.want {
			t.Errorf("%v: toolConfig = %s, want %s", tt.choice.Type, s, tt.want)
		}
	}
}

// TS: combined provider + function tools on Gemini 3 set includeServerSideToolInvocations
// for the Gemini API only (01fa606 / 84f36e0).
func TestPrepareTools_IncludeServerSideToolInvocations(t *testing.T) {
	tools := []types.Tool{{Name: "f"}, {Type: "provider", ProviderID: "google.google_search"}}
	google := prepareTools(tools, types.ToolChoice{}, "gemini-3-pro-preview", false)
	if jsonString(t, google.toolConfig) != `{"functionCallingConfig":{"mode":"VALIDATED"},"includeServerSideToolInvocations":true}` {
		t.Errorf("google toolConfig = %s", jsonString(t, google.toolConfig))
	}
	vertex := prepareTools(tools, types.ToolChoice{Type: types.ToolChoiceRequired}, "gemini-3-pro-preview", true)
	if jsonString(t, vertex.toolConfig) != `{"functionCallingConfig":{"mode":"ANY"}}` {
		t.Errorf("vertex toolConfig = %s", jsonString(t, vertex.toolConfig))
	}
	old := prepareTools(tools, types.ToolChoice{}, "gemini-2.5-flash", false)
	if len(old.warnings) != 1 || old.warnings[0].Feature != "combination of function and provider-defined tools" {
		t.Errorf("warnings = %#v", old.warnings)
	}
	if len(old.tools) != 1 || old.tools[0]["googleSearch"] == nil {
		t.Errorf("tools = %s", jsonString(t, old.tools))
	}
	fs := prepareTools([]types.Tool{{Type: "provider", ProviderID: "google.file_search"}}, types.ToolChoice{}, "gemini-2.0-flash", false)
	if fs.tools != nil || len(fs.warnings) != 1 || fs.warnings[0].Feature != "provider-defined tool google.file_search" {
		t.Errorf("file_search on 2.0: tools=%v warnings=%#v", fs.tools, fs.warnings)
	}
}

// TS: Gemini 2.5 on the Gemini API drops penalties with warnings; Vertex keeps them (e9bc618).
func TestBuildRequest_Gemini25DropsPenalties(t *testing.T) {
	fp, pp := 0.5, 0.2
	opts := &provider.GenerateOptions{FrequencyPenalty: &fp, PresencePenalty: &pp}
	body, warnings := buildReq(t, makeTestModel("gemini-2.5-flash"), opts, false)
	gc, _ := body["generationConfig"].(map[string]interface{})
	if _, ok := gc["frequencyPenalty"]; ok {
		t.Error("frequencyPenalty must be omitted")
	}
	if _, ok := gc["presencePenalty"]; ok {
		t.Error("presencePenalty must be omitted")
	}
	if len(warnings) != 2 || warnings[0].Feature != "frequencyPenalty" || warnings[1].Feature != "presencePenalty" {
		t.Errorf("warnings = %#v", warnings)
	}
	body, _ = buildReq(t, makeVertexTestModel("gemini-2.5-flash"), opts, false)
	if body["generationConfig"].(map[string]interface{})["frequencyPenalty"] != 0.5 {
		t.Error("vertex must keep frequencyPenalty")
	}
	body, _ = buildReq(t, makeTestModel("gemini-3-pro-preview"), opts, false)
	if body["generationConfig"].(map[string]interface{})["presencePenalty"] != 0.2 {
		t.Error("gemini 3 must keep presencePenalty")
	}
}

// TS: standalone threshold → safetySettings for the four categories (96d40bc).
func TestBuildRequest_ThresholdSafetySettings(t *testing.T) {
	body, _ := buildReq(t, makeTestModel("gemini-2.5-flash"), &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"threshold": "BLOCK_LOW_AND_ABOVE"}},
	}, false)
	want := `[{"category":"HARM_CATEGORY_HATE_SPEECH","threshold":"BLOCK_LOW_AND_ABOVE"},{"category":"HARM_CATEGORY_DANGEROUS_CONTENT","threshold":"BLOCK_LOW_AND_ABOVE"},{"category":"HARM_CATEGORY_HARASSMENT","threshold":"BLOCK_LOW_AND_ABOVE"},{"category":"HARM_CATEGORY_SEXUALLY_EXPLICIT","threshold":"BLOCK_LOW_AND_ABOVE"}]`
	if got := jsonString(t, body["safetySettings"]); got != want {
		t.Errorf("safetySettings = %s", got)
	}
	body, _ = buildReq(t, makeTestModel("gemini-2.5-flash"), &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{
			"threshold":      "BLOCK_LOW_AND_ABOVE",
			"safetySettings": []interface{}{map[string]interface{}{"category": "X", "threshold": "Y"}},
		}},
	}, false)
	if got := jsonString(t, body["safetySettings"]); got != `[{"category":"X","threshold":"Y"}]` {
		t.Errorf("explicit safetySettings must win: %s", got)
	}
}

// TS: Vertex-only imageConfig fields are stripped on the Gemini API with a warning (5b4a299).
func TestBuildRequest_VertexOnlyImageConfig(t *testing.T) {
	ic := map[string]interface{}{"aspectRatio": "16:9", "personGeneration": "ALLOW_ALL", "prominentPeople": "ALLOW_PROMINENT_PEOPLE"}
	opts := &provider.GenerateOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"imageConfig": ic}}}
	body, warnings := buildReq(t, makeTestModel("gemini-3-pro-image-preview"), opts, false)
	if got := jsonString(t, body["generationConfig"].(map[string]interface{})["imageConfig"]); got != `{"aspectRatio":"16:9"}` {
		t.Errorf("imageConfig = %s", got)
	}
	if len(warnings) != 1 || warnings[0].Message != "'imageConfig.personGeneration', 'imageConfig.prominentPeople' are Vertex AI options and are ignored with the current Google provider (google)." {
		t.Errorf("warnings = %#v", warnings)
	}
	body, warnings = buildReq(t, makeVertexTestModel("gemini-3-pro-image-preview"), &provider.GenerateOptions{
		ProviderOptions: map[string]interface{}{"vertex": map[string]interface{}{"imageConfig": ic}},
	}, false)
	if got := jsonString(t, body["generationConfig"].(map[string]interface{})["imageConfig"]); !strings.Contains(got, "personGeneration") {
		t.Errorf("vertex imageConfig = %s", got)
	}
	if len(warnings) != 0 {
		t.Errorf("vertex warnings = %#v", warnings)
	}
}

// TS: Vertex requests omit functionCall ids (c57a353).
func TestBuildRequest_VertexOmitsFunctionCallIDs(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{
		{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "c1", ToolName: "t", Arguments: map[string]interface{}{}}}},
	}}}
	body, _ := buildReq(t, makeVertexTestModel("gemini-2.5-flash"), opts, false)
	if strings.Contains(jsonString(t, body["contents"]), `"id"`) {
		t.Errorf("vertex contents carry ids: %s", jsonString(t, body["contents"]))
	}
	body, _ = buildReq(t, makeTestModel("gemini-2.5-flash"), opts, false)
	if !strings.Contains(jsonString(t, body["contents"]), `"id":"c1"`) {
		t.Errorf("google contents missing id: %s", jsonString(t, body["contents"]))
	}
}
