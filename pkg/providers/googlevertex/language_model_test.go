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

// TestLanguageModel_GenerateText_MockServer tests text generation with a mock server
func TestLanguageModel_GenerateText_MockServer(t *testing.T) {
	// Create test server to capture request and return mock response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request path
		expectedPath := "/models/gemini-1.5-flash:generateContent"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path '%s', got '%s'", expectedPath, r.URL.Path)
		}

		// Verify Authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected Authorization 'Bearer test-token', got '%s'", auth)
		}

		// Return mock response
		response := vertexResponse{
			Candidates: []struct {
				Content struct {
					Parts []vertexPart `json:"parts"`
					Role  string       `json:"role"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
				Index        int    `json:"index"`
			}{
				{
					Content: struct {
						Parts []vertexPart `json:"parts"`
						Role  string       `json:"role"`
					}{
						Parts: []vertexPart{
							{Text: "Hello! How can I help you today?"},
						},
						Role: "model",
					},
					FinishReason: "STOP",
					Index:        0,
				},
			},
			UsageMetadata: &vertexUsageMetadata{
				PromptTokenCount:     5,
				CandidatesTokenCount: 8,
				TotalTokenCount:      13,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with test server
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

	// Generate text
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Say hello"},
					},
				},
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

// TestLanguageModel_GenerateWithTools tests tool calling with mock server
func TestLanguageModel_GenerateWithTools_MockServer(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify tools are included in request
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		tools, ok := reqBody["tools"].([]interface{})
		if !ok || len(tools) == 0 {
			t.Error("Expected tools to be included in request")
		}

		// Return mock response with function call
		response := vertexResponse{
			Candidates: []struct {
				Content struct {
					Parts []vertexPart `json:"parts"`
					Role  string       `json:"role"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
				Index        int    `json:"index"`
			}{
				{
					Content: struct {
						Parts []vertexPart `json:"parts"`
						Role  string       `json:"role"`
					}{
						Parts: []vertexPart{
							{
								FunctionCall: &struct {
									Name string                 `json:"name"`
									Args map[string]interface{} `json:"args"`
								}{
									Name: "get_weather",
									Args: map[string]interface{}{
										"location": "San Francisco",
									},
								},
							},
						},
						Role: "model",
					},
					FinishReason: "STOP",
					Index:        0,
				},
			},
			UsageMetadata: &vertexUsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 5,
				TotalTokenCount:      15,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with test server
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

	// Generate with tools
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "What's the weather in San Francisco?"},
					},
				},
			},
		},
		Tools: []types.Tool{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The location to get weather for",
						},
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

// TestLanguageModel_JSONMode tests JSON mode output
func TestLanguageModel_JSONMode_MockServer(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify responseMimeType is set in request
		var reqBody map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&reqBody)

		genConfig, ok := reqBody["generationConfig"].(map[string]interface{})
		if !ok {
			t.Error("Expected generationConfig in request")
		}

		if genConfig["responseMimeType"] != "application/json" {
			t.Errorf("Expected responseMimeType 'application/json', got '%v'", genConfig["responseMimeType"])
		}

		// Return mock JSON response
		response := vertexResponse{
			Candidates: []struct {
				Content struct {
					Parts []vertexPart `json:"parts"`
					Role  string       `json:"role"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
				Index        int    `json:"index"`
			}{
				{
					Content: struct {
						Parts []vertexPart `json:"parts"`
						Role  string       `json:"role"`
					}{
						Parts: []vertexPart{
							{Text: `{"name": "John Doe", "age": 30}`},
						},
						Role: "model",
					},
					FinishReason: "STOP",
					Index:        0,
				},
			},
			UsageMetadata: &vertexUsageMetadata{
				PromptTokenCount:     10,
				CandidatesTokenCount: 8,
				TotalTokenCount:      18,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create provider with test server
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

	// Generate with JSON mode
	jsonType := "json_object"
	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Generate a person object"},
					},
				},
			},
		},
		ResponseFormat: &provider.ResponseFormat{
			Type: jsonType,
		},
	})

	if err != nil {
		t.Fatalf("Generate with JSON mode failed: %v", err)
	}

	if result.Text == "" {
		t.Error("Expected non-empty JSON response")
	}

	// Verify response is valid JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(result.Text), &jsonData); err != nil {
		t.Errorf("Expected valid JSON response, got error: %v", err)
	}
}

// TestLanguageModel_UsageTracking tests detailed usage token tracking
func TestLanguageModel_UsageTracking(t *testing.T) {
	// Create test server with detailed usage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := vertexResponse{
			Candidates: []struct {
				Content struct {
					Parts []vertexPart `json:"parts"`
					Role  string       `json:"role"`
				} `json:"content"`
				FinishReason string `json:"finishReason"`
				Index        int    `json:"index"`
			}{
				{
					Content: struct {
						Parts []vertexPart `json:"parts"`
						Role  string       `json:"role"`
					}{
						Parts: []vertexPart{
							{Text: "Response with detailed token tracking"},
						},
						Role: "model",
					},
					FinishReason: "STOP",
					Index:        0,
				},
			},
			UsageMetadata: &vertexUsageMetadata{
				PromptTokenCount:        100,
				CandidatesTokenCount:    50,
				TotalTokenCount:         150,
				CachedContentTokenCount: 30,
				ThoughtsTokenCount:      10,
				PromptTokensDetails: []struct {
					Modality   string `json:"modality,omitempty"`
					TokenCount int    `json:"tokenCount,omitempty"`
				}{
					{Modality: "TEXT", TokenCount: 70},
					{Modality: "IMAGE", TokenCount: 30},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
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
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Test"},
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify basic usage
	if result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 160 {
		t.Errorf("Expected total tokens 160 (150 + 10 thoughts), got %v", result.Usage.TotalTokens)
	}

	// Verify input details (cache)
	if result.Usage.InputDetails == nil {
		t.Fatal("Expected InputDetails to be set")
	}
	if result.Usage.InputDetails.CacheReadTokens == nil || *result.Usage.InputDetails.CacheReadTokens != 30 {
		t.Errorf("Expected cache read tokens 30, got %v", result.Usage.InputDetails.CacheReadTokens)
	}

	// Verify text and image tokens
	if result.Usage.InputDetails.TextTokens == nil || *result.Usage.InputDetails.TextTokens != 70 {
		t.Errorf("Expected text tokens 70, got %v", result.Usage.InputDetails.TextTokens)
	}
	if result.Usage.InputDetails.ImageTokens == nil || *result.Usage.InputDetails.ImageTokens != 30 {
		t.Errorf("Expected image tokens 30, got %v", result.Usage.InputDetails.ImageTokens)
	}

	// Verify output details (reasoning)
	if result.Usage.OutputDetails == nil {
		t.Fatal("Expected OutputDetails to be set")
	}
	if result.Usage.OutputDetails.ReasoningTokens == nil || *result.Usage.OutputDetails.ReasoningTokens != 10 {
		t.Errorf("Expected reasoning tokens 10, got %v", result.Usage.OutputDetails.ReasoningTokens)
	}
}

// Integration tests with real Vertex AI API (requires credentials)

func TestVertexLanguageModel_GenerateText_Integration(t *testing.T) {
	// Skip if no credentials
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		t.Skip("Google Vertex AI credentials not configured (set GOOGLE_VERTEX_PROJECT, GOOGLE_VERTEX_LOCATION, GOOGLE_VERTEX_ACCESS_TOKEN)")
	}

	prov, err := New(Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
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
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Say 'Hello from Vertex AI' and nothing else"},
					},
				},
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
	// Skip if no credentials
	project := os.Getenv("GOOGLE_VERTEX_PROJECT")
	location := os.Getenv("GOOGLE_VERTEX_LOCATION")
	token := os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN")

	if project == "" || location == "" || token == "" {
		t.Skip("Google Vertex AI credentials not configured")
	}

	prov, err := New(Config{
		Project:     project,
		Location:    location,
		AccessToken: token,
	})
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
				{
					Role: types.RoleUser,
					Content: []types.ContentPart{
						types.TextContent{Text: "Count from 1 to 5"},
					},
				},
			},
		},
	})

	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close()

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
