package cohere

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
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
