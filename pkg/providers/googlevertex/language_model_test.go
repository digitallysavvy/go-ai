package googlevertex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestLanguageModel_GenerateText_MockServer tests text generation with a mock server.
func TestLanguageModel_GenerateText_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-1.5-flash:generateContent" {
			t.Errorf("Expected path '/models/gemini-1.5-flash:generateContent', got '%s'", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Expected Authorization 'Bearer test-token', got '%s'", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"parts": [{"text": "Hello! How can I help you today?"}], "role": "model"},
				"finishReason": "STOP",
				"index": 0
			}],
			"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 8, "totalTokenCount": 13}
		}`))
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-flash")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Say hello"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if result.Text == "" {
		t.Error("Expected non-empty text response")
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 13 {
		t.Errorf("Expected total tokens 13, got %v", result.Usage.TotalTokens)
	}
	if result.FinishReason != types.FinishReasonStop {
		t.Errorf("Expected finish reason 'stop', got '%s'", result.FinishReason)
	}
}

// TestLanguageModel_GenerateWithTools_MockServer tests tool calling with a mock server.
func TestLanguageModel_GenerateWithTools_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if tools, ok := reqBody["tools"].([]interface{}); !ok || len(tools) == 0 {
			t.Error("Expected tools to be included in request")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"parts": [{"functionCall": {"name": "get_weather", "args": {"location": "San Francisco"}}}], "role": "model"},
				"finishReason": "STOP",
				"index": 0
			}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15}
		}`))
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-pro")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "What's the weather in San Francisco?"}}},
			},
		},
		Tools: []types.Tool{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{"type": "string", "description": "The location"},
					},
					"required": []string{"location"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate with tools failed: %v", err)
	}
	if len(result.ToolCalls) == 0 {
		t.Fatal("Expected at least one tool call")
	}
	if result.ToolCalls[0].ToolName != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got '%s'", result.ToolCalls[0].ToolName)
	}
}

// TestLanguageModel_JSONMode_MockServer tests JSON mode output.
func TestLanguageModel_JSONMode_MockServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		genConfig, ok := reqBody["generationConfig"].(map[string]interface{})
		if !ok {
			t.Error("Expected generationConfig in request")
		} else if genConfig["responseMimeType"] != "application/json" {
			t.Errorf("Expected responseMimeType 'application/json', got '%v'", genConfig["responseMimeType"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"parts": [{"text": "{\"name\": \"John Doe\", \"age\": 30}"}], "role": "model"},
				"finishReason": "STOP",
				"index": 0
			}],
			"usageMetadata": {"promptTokenCount": 10, "candidatesTokenCount": 8, "totalTokenCount": 18}
		}`))
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-pro")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Generate a person object"}}},
			},
		},
		ResponseFormat: &provider.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		t.Fatalf("Generate with JSON mode failed: %v", err)
	}
	if result.Text == "" {
		t.Error("Expected non-empty JSON response")
	}
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(result.Text), &jsonData); err != nil {
		t.Errorf("Expected valid JSON response, got error: %v", err)
	}
}

// TestLanguageModel_UsageTracking tests detailed usage token tracking.
func TestLanguageModel_UsageTracking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"content": {"parts": [{"text": "Response with detailed token tracking"}], "role": "model"},
				"finishReason": "STOP",
				"index": 0
			}],
			"usageMetadata": {
				"promptTokenCount": 100,
				"candidatesTokenCount": 50,
				"totalTokenCount": 150,
				"cachedContentTokenCount": 30,
				"thoughtsTokenCount": 10,
				"promptTokensDetails": [
					{"modality": "TEXT", "tokenCount": 70},
					{"modality": "IMAGE", "tokenCount": 30}
				]
			}
		}`))
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-pro")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Test"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Total = prompt(100) + candidates(50) + thoughts(10) = 160
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 160 {
		t.Errorf("Expected total tokens 160 (150 + 10 thoughts), got %v", result.Usage.TotalTokens)
	}
	if result.Usage.InputDetails == nil {
		t.Fatal("Expected InputDetails to be set")
	}
	if result.Usage.InputDetails.CacheReadTokens == nil || *result.Usage.InputDetails.CacheReadTokens != 30 {
		t.Errorf("Expected cache read tokens 30, got %v", result.Usage.InputDetails.CacheReadTokens)
	}
	if result.Usage.InputDetails.TextTokens == nil || *result.Usage.InputDetails.TextTokens != 70 {
		t.Errorf("Expected text tokens 70, got %v", result.Usage.InputDetails.TextTokens)
	}
	if result.Usage.InputDetails.ImageTokens == nil || *result.Usage.InputDetails.ImageTokens != 30 {
		t.Errorf("Expected image tokens 30, got %v", result.Usage.InputDetails.ImageTokens)
	}
	if result.Usage.OutputDetails == nil {
		t.Fatal("Expected OutputDetails to be set")
	}
	if result.Usage.OutputDetails.ReasoningTokens == nil || *result.Usage.OutputDetails.ReasoningTokens != 10 {
		t.Errorf("Expected reasoning tokens 10, got %v", result.Usage.OutputDetails.ReasoningTokens)
	}
}

// --- finishMessage in providerMetadata --------------------------------------

func TestGoogleVertexFinishMessageInMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]interface{}{{"text": "Hello"}},
						"role":  "model",
					},
					"finishReason":  "STOP",
					"finishMessage": "safety filter triggered",
				},
			},
			"usageMetadata": map[string]interface{}{
				"promptTokenCount":     5,
				"candidatesTokenCount": 1,
				"totalTokenCount":      6,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	prov, err := New(Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
		BaseURL:     server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	model, err := prov.LanguageModel("gemini-2.0-flash")
	if err != nil {
		t.Fatal(err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}},
		}},
	})
	if err != nil {
		t.Fatalf("DoGenerate: %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatal("ProviderMetadata must be set when finishMessage is present")
	}
	rawMeta, ok := result.ProviderMetadata["vertex"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("ProviderMetadata[vertex] type = %T, want map[string]json.RawMessage",
			result.ProviderMetadata["vertex"])
	}
	var finishMessage string
	if err := json.Unmarshal(rawMeta["finishMessage"], &finishMessage); err != nil {
		t.Fatalf("unmarshal finishMessage: %v", err)
	}
	if finishMessage != "safety filter triggered" {
		t.Errorf("finishMessage = %v, want %q", finishMessage, "safety filter triggered")
	}
}

// Integration tests with real Vertex AI API (requires credentials).

func TestVertexLanguageModel_GenerateText_Integration(t *testing.T) {
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")
	if project == "" || location == "" || token == "" {
		t.Skip("Google Vertex AI credentials not configured (set GOOGLE_VERTEX_PROJECT, GOOGLE_VERTEX_LOCATION, GOOGLE_VERTEX_ACCESS_TOKEN)")
	}

	prov, err := New(Config{Project: project, Location: location, AccessToken: token})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-flash")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Say 'Hello from Vertex AI' and nothing else"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	if result.Text == "" {
		t.Error("Expected non-empty text response")
	}
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens == 0 {
		t.Error("Expected non-zero token usage")
	}
	t.Logf("Response: %s", result.Text)
	t.Logf("Tokens: %d", *result.Usage.TotalTokens)
}

func TestVertexLanguageModel_StreamText_Integration(t *testing.T) {
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")
	if project == "" || location == "" || token == "" {
		t.Skip("Google Vertex AI credentials not configured")
	}

	prov, err := New(Config{Project: project, Location: location, AccessToken: token})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	model, err := prov.LanguageModel("gemini-1.5-flash")
	if err != nil {
		t.Fatalf("Failed to create model: %v", err)
	}

	stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Count from 1 to 5"}}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close() //nolint:errcheck

	var chunks []string
	for {
		chunk, err := stream.Next()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("Stream error: %v", err)
		}
		if chunk.Type == provider.ChunkTypeText {
			chunks = append(chunks, chunk.Text)
			t.Logf("Chunk: %s", chunk.Text)
		}
	}
	if len(chunks) == 0 {
		t.Error("Expected at least one text chunk")
	}
}
