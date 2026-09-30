package cohere

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// v2 mock response JSON
const cohereV2MockResponse = `{"generation_id":"test","message":{"role":"assistant","content":[{"type":"text","text":"hello"}],"tool_calls":null},"finish_reason":"COMPLETE","usage":{"tokens":{"input_tokens":5,"output_tokens":3}}}`

func TestCohereNoWarningWhenReasoningNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cohereV2MockResponse))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")

	opts := &provider.GenerateOptions{}
	result, err := model.DoGenerate(t.Context(), opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Warnings) != 0 {
		t.Errorf("expected no warnings when Reasoning is nil, got: %+v", result.Warnings)
	}
	if result.Text != "hello" {
		t.Errorf("expected text 'hello', got: %q", result.Text)
	}
}

func TestCohereImageURLMessageSerialization(t *testing.T) {
	var got map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(cohereV2MockResponse))
	}))
	defer srv.Close()

	prov := New(Config{BaseURL: srv.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/cat.png", MediaType: "image/png"},
				},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("DoGenerate error: %v", err)
	}

	msgs := got["messages"].([]interface{})
	msg := msgs[0].(map[string]interface{})
	content := msg["content"].([]interface{})
	part := content[0].(map[string]interface{})
	if part["type"] != "image_url" {
		t.Fatalf("part.type = %v, want image_url", part["type"])
	}
	img := part["image_url"].(map[string]interface{})
	if img["url"] != "https://example.com/cat.png" {
		t.Fatalf("image_url.url = %v", img["url"])
	}
}

func TestCohereNonImageFileBecomesDocument(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.TextContent{Text: "Analyze: "},
				types.FileContent{
					FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("This is file content"), MediaType: "text/plain"},
					Filename: "note.txt",
				},
			},
		}}},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}

	messages := body["messages"].([]map[string]interface{})
	if messages[0]["content"] != "Analyze: " {
		t.Fatalf("message content = %#v", messages[0]["content"])
	}
	documents := body["documents"].([]map[string]interface{})
	data := documents[0]["data"].(map[string]interface{})
	if data["text"] != "This is file content" || data["title"] != "note.txt" {
		t.Fatalf("unexpected document data: %#v", data)
	}
}

func TestCohereUnsupportedFileURLReturnsError(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	_, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{
					FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/file.pdf", MediaType: "application/pdf"},
				},
			},
		}}},
	})
	if err == nil {
		t.Fatal("expected unsupported file URL error")
	}
}

func TestCohereUnsupportedCapabilityErrors(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	if _, err := prov.ImageModel("x"); err == nil || err.Error() != "cohere does not support image generation" {
		t.Fatalf("ImageModel error = %v", err)
	}
	if _, err := prov.SpeechModel("x"); err == nil || err.Error() != "cohere does not support speech synthesis" {
		t.Fatalf("SpeechModel error = %v", err)
	}
	if _, err := prov.TranscriptionModel("x"); err == nil || err.Error() != "cohere does not support transcription" {
		t.Fatalf("TranscriptionModel error = %v", err)
	}
}

func TestCohereReasoningNoneDisablesThinking(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	level := types.ReasoningNone
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildRequestBody(opts)
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking field to be a map, got: %T", body["thinking"])
	}
	if thinking["type"] != "disabled" {
		t.Errorf("expected thinking.type='disabled', got: %v", thinking["type"])
	}
}

func TestCohereReasoningHighBudget(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	level := types.ReasoningHigh
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildRequestBody(opts)
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking field to be a map, got: %T", body["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("expected thinking.type='enabled', got: %v", thinking["type"])
	}
	if thinking["token_budget"] != 19661 {
		t.Errorf("expected token_budget=19661, got: %v", thinking["token_budget"])
	}
}

func TestCohereReasoningDefaultOmitted(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	level := types.ReasoningDefault
	opts := &provider.GenerateOptions{Reasoning: &level}
	body, err := model.buildRequestBody(opts)
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	if _, ok := body["thinking"]; ok {
		t.Errorf("expected no thinking field when Reasoning is ReasoningDefault, got: %v", body["thinking"])
	}
}

// TestCohereBuildRequestBodyScalarFields verifies buildRequestBody forwards
// topP, topK, frequencyPenalty, presencePenalty, seed, and stopSequences to
// their Cohere wire names (p, k, frequency_penalty, presence_penalty, seed,
// stop_sequences), matching TS getArgs's "standardized settings" block
// (cohere-chat-language-model.ts:124-132).
func TestCohereBuildRequestBodyScalarFields(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	topP := 0.5
	freqPenalty := 0.2
	presPenalty := 0.3
	seed := 42
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:           types.Prompt{Text: "hi"},
		TopP:             &topP,
		TopK:             intPtr(10),
		FrequencyPenalty: &freqPenalty,
		PresencePenalty:  &presPenalty,
		Seed:             &seed,
		StopSequences:    []string{"stop1", "stop2"},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	if body["p"] != 0.5 {
		t.Errorf("p = %v, want 0.5", body["p"])
	}
	if body["k"] != 10 {
		t.Errorf("k = %v, want 10", body["k"])
	}
	if body["frequency_penalty"] != 0.2 {
		t.Errorf("frequency_penalty = %v, want 0.2", body["frequency_penalty"])
	}
	if body["presence_penalty"] != 0.3 {
		t.Errorf("presence_penalty = %v, want 0.3", body["presence_penalty"])
	}
	if body["seed"] != 42 {
		t.Errorf("seed = %v, want 42", body["seed"])
	}
	stopSeqs, ok := body["stop_sequences"].([]string)
	if !ok || len(stopSeqs) != 2 || stopSeqs[0] != "stop1" || stopSeqs[1] != "stop2" {
		t.Errorf("stop_sequences = %#v", body["stop_sequences"])
	}
}

func intPtr(i int) *int { return &i }

// TestCohereBuildRequestBodyScalarFieldsOmittedWhenUnset verifies none of the
// new scalar fields appear on the wire when unset (matching TS's
// `JSON.stringify` dropping `undefined` values).
func TestCohereBuildRequestBodyScalarFieldsOmittedWhenUnset(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	body, err := model.buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	for _, key := range []string{"p", "k", "frequency_penalty", "presence_penalty", "seed", "stop_sequences", "response_format", "tools", "tool_choice", "thinking"} {
		if _, ok := body[key]; ok {
			t.Errorf("expected %q to be omitted, got %v", key, body[key])
		}
	}
}

// TestCohereBuildRequestBodyResponseFormat verifies json responseFormat maps
// to {type: "json_object", json_schema: <schema>}, matching TS getArgs's
// response_format block.
func TestCohereBuildRequestBodyResponseFormat(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	schema := map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"text": map[string]interface{}{"type": "string"}},
		"required":   []interface{}{"text"},
	}
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json", Schema: schema},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	rf, ok := body["response_format"].(map[string]interface{})
	if !ok {
		t.Fatalf("response_format = %#v, want map", body["response_format"])
	}
	if rf["type"] != "json_object" {
		t.Errorf("response_format.type = %v, want json_object", rf["type"])
	}
	if !reflect.DeepEqual(rf["json_schema"], schema) {
		t.Errorf("response_format.json_schema = %#v, want %#v", rf["json_schema"], schema)
	}
}

// TestCohereBuildRequestBodyResponseFormatNoSchema verifies json_schema is
// omitted when no schema is provided.
func TestCohereBuildRequestBodyResponseFormatNoSchema(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:         types.Prompt{Text: "hi"},
		ResponseFormat: &provider.ResponseFormat{Type: "json"},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	rf := body["response_format"].(map[string]interface{})
	if _, ok := rf["json_schema"]; ok {
		t.Errorf("expected json_schema to be omitted, got %v", rf["json_schema"])
	}
}

// TestCohereBuildRequestBodyToolsAndToolChoice verifies tools/tool_choice are
// wired onto the wire body, matching TS "should pass tools".
func TestCohereBuildRequestBodyToolsAndToolChoice(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:     types.Prompt{Text: "hi"},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceNone},
		Tools: []types.Tool{
			{
				Type: types.ToolTypeFunction,
				Name: "test-tool",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	if body["tool_choice"] != "NONE" {
		t.Fatalf("tool_choice = %v, want NONE", body["tool_choice"])
	}
	tools, ok := body["tools"].([]map[string]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	fn := tools[0]["function"].(map[string]interface{})
	if fn["name"] != "test-tool" {
		t.Fatalf("function.name = %v", fn["name"])
	}
	if _, ok := fn["description"]; ok {
		t.Errorf("expected description to be omitted when empty, got %v", fn["description"])
	}
}

// TestCohereProviderOptionsThinkingOverridesReasoning verifies
// providerOptions.cohere.thinking takes precedence over top-level Reasoning,
// matching TS "should prefer providerOptions over top-level reasoning".
func TestCohereProviderOptionsThinkingOverridesReasoning(t *testing.T) {
	model := &LanguageModel{modelID: "command-r-plus"}
	level := types.ReasoningNone
	body, err := model.buildRequestBody(&provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "hi"},
		Reasoning: &level,
		ProviderOptions: map[string]interface{}{
			"cohere": map[string]interface{}{
				"thinking": map[string]interface{}{"type": "enabled"},
			},
		},
	})
	if err != nil {
		t.Fatalf("buildRequestBody error: %v", err)
	}
	thinking, ok := body["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v, want type=enabled", body["thinking"])
	}
}

// TestCohereFinishReasonStopSequence verifies STOP_SEQUENCE maps to Stop,
// matching TS map-cohere-finish-reason.ts.
func TestCohereFinishReasonStopSequence(t *testing.T) {
	if got := mapCohereV2FinishReason("STOP_SEQUENCE"); got != types.FinishReasonStop {
		t.Errorf("mapCohereV2FinishReason(STOP_SEQUENCE) = %v, want Stop", got)
	}
}

// TestCohereCitationsBecomeSourceParts verifies non-streaming citations are
// converted into "source" content parts with the cohere providerMetadata
// shape, matching TS's doGenerate citation loop.
func TestCohereCitationsBecomeSourceParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"generation_id":"test",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"Automation of tasks"}],
				"citations":[{
					"start":0,
					"end":20,
					"text":"Automation of tasks",
					"sources":[{"type":"document","id":"doc:0","document":{"id":"doc:0","text":"AI provides automation","title":"benefits.txt"}}],
					"type":"TEXT_CONTENT"
				}]
			},
			"finish_reason":"COMPLETE",
			"usage":{"tokens":{"input_tokens":1,"output_tokens":1}}
		}`))
	}))
	defer server.Close()

	prov := New(Config{BaseURL: server.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")
	result, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err != nil {
		t.Fatalf("DoGenerate error: %v", err)
	}

	var source types.SourceContent
	found := false
	for _, c := range result.Content {
		if s, ok := c.(types.SourceContent); ok {
			source = s
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a SourceContent part, got content: %#v", result.Content)
	}
	if source.SourceType != "document" {
		t.Errorf("sourceType = %q, want document", source.SourceType)
	}
	if source.MediaType != "text/plain" {
		t.Errorf("mediaType = %q, want text/plain", source.MediaType)
	}
	if source.Title != "benefits.txt" {
		t.Errorf("title = %q, want benefits.txt", source.Title)
	}
	if source.ID == "" {
		t.Error("expected a generated ID")
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(source.ProviderMetadata, &meta); err != nil {
		t.Fatalf("failed to unmarshal providerMetadata: %v", err)
	}
	cohereMeta, ok := meta["cohere"].(map[string]interface{})
	if !ok {
		t.Fatalf("providerMetadata.cohere missing: %#v", meta)
	}
	if cohereMeta["start"] != float64(0) || cohereMeta["end"] != float64(20) {
		t.Errorf("start/end = %v/%v", cohereMeta["start"], cohereMeta["end"])
	}
	if cohereMeta["text"] != "Automation of tasks" {
		t.Errorf("text = %v", cohereMeta["text"])
	}
	if cohereMeta["citationType"] != "TEXT_CONTENT" {
		t.Errorf("citationType = %v, want TEXT_CONTENT", cohereMeta["citationType"])
	}
	if _, ok := cohereMeta["sources"]; !ok {
		t.Error("expected sources to be present")
	}
}

func TestCohereMalformedToolArgumentsReturnsValidationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"generation_id":"test",
			"message":{
				"role":"assistant",
				"content":[{"type":"text","text":"hello"}],
				"tool_calls":[{"id":"tc1","type":"function","function":{"name":"lookup","arguments":"{\"a\":"}}]
			},
			"finish_reason":"TOOL_CALL",
			"usage":{"tokens":{"input_tokens":1,"output_tokens":1}}
		}`))
	}))
	defer server.Close()

	prov := New(Config{BaseURL: server.URL, APIKey: "test-key"})
	model := NewLanguageModel(prov, "command-r-plus")
	_, err := model.DoGenerate(t.Context(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "x"}})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !providererrors.IsValidationError(err) {
		t.Fatalf("error type = %T, want validation error", err)
	}
}
