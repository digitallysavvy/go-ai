package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

var model provider.LanguageModel

// Request/Response types
type GenerateRequest struct {
	Prompt      string                 `json:"prompt"`
	MaxTokens   *int                   `json:"maxTokens,omitempty"`
	Temperature *float64               `json:"temperature,omitempty"`
	System      string                 `json:"system,omitempty"`
	Tools       []string               `json:"tools,omitempty"`
	Extra       map[string]interface{} `json:"extra,omitempty"`
}

type GenerateResponse struct {
	Text   string             `json:"text"`
	Usage  types.Usage        `json:"usage"`
	Finish types.FinishReason `json:"finishReason"`
	Steps  []types.StepResult `json:"steps,omitempty"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create OpenAI provider and model
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	var err error
	model, err = p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleRoot)
	mux.HandleFunc("/generate", handleGenerate)
	mux.HandleFunc("/stream", handleStream)
	mux.HandleFunc("/tools", handleTools)
	mux.HandleFunc("/health", handleHealth)

	// CORS middleware
	handler := corsMiddleware(mux)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 HTTP server starting on port %s", port)
	log.Printf("Available endpoints:")
	log.Printf("  POST /generate - Generate text completion")
	log.Printf("  POST /stream   - Stream text completion (SSE)")
	log.Printf("  POST /tools    - Generate with tool calling")
	log.Printf("  GET  /health   - Health check")

	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

// handleRoot provides API information
func handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := map[string]interface{}{
		"service": "Go AI SDK HTTP Server",
		"version": "1.0.0",
		"endpoints": []map[string]string{
			{"method": "POST", "path": "/generate", "description": "Generate text completion"},
			{"method": "POST", "path": "/stream", "description": "Stream text completion (SSE)"},
			{"method": "POST", "path": "/tools", "description": "Generate with tool calling"},
			{"method": "GET", "path": "/health", "description": "Health check"},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleGenerate handles basic text generation
func handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Prompt == "" {
		sendError(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Build options
	opts := ai.GenerateTextOptions{
		Model:  model,
		Prompt: req.Prompt,
	}

	if req.System != "" {
		opts.System = req.System
	}
	if req.MaxTokens != nil {
		opts.MaxTokens = req.MaxTokens
	}
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
	}

	// Generate text
	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		sendError(w, fmt.Sprintf("Generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Send response
	response := GenerateResponse{
		Text:   result.Text,
		Usage:  result.Usage,
		Finish: result.FinishReason,
		Steps:  result.Steps,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleStream handles streaming text generation with SSE
func handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Prompt == "" {
		sendError(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	// Build options
	opts := ai.StreamTextOptions{
		Model:  model,
		Prompt: req.Prompt,
	}

	if req.System != "" {
		opts.System = req.System
	}
	if req.MaxTokens != nil {
		opts.MaxTokens = req.MaxTokens
	}
	if req.Temperature != nil {
		opts.Temperature = req.Temperature
	}

	// Stream text
	stream, err := ai.StreamText(ctx, opts)
	if err != nil {
		sendSSE(w, "error", fmt.Sprintf("Stream failed: %v", err))
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		sendError(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send start event
	sendSSE(w, "start", "")
	flusher.Flush()

	// Stream chunks
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			sendSSE(w, "text", chunk.Text)
			flusher.Flush()
		}
	}

	// Check for errors
	if err := stream.Err(); err != nil {
		sendSSE(w, "error", err.Error())
		flusher.Flush()
		return
	}

	// Send completion event with usage
	usage := stream.Usage()
	usageJSON, _ := json.Marshal(map[string]interface{}{
		"inputTokens":  usage.GetInputTokens(),
		"outputTokens": usage.GetOutputTokens(),
		"totalTokens":  usage.GetTotalTokens(),
	})

	sendSSE(w, "done", string(usageJSON))
	flusher.Flush()
}

// handleTools handles text generation with tool calling
func handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Prompt == "" {
		sendError(w, "Prompt is required", http.StatusBadRequest)
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Define available tools
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "The city and state, e.g., San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"celsius", "fahrenheit"},
					"description": "Temperature unit",
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			location := params["location"].(string)
			unit := "fahrenheit"
			if u, ok := params["unit"].(string); ok {
				unit = u
			}

			// Simulate weather API call
			temp := 72
			if unit == "celsius" {
				temp = 22
			}

			return map[string]interface{}{
				"location":    location,
				"temperature": temp,
				"unit":        unit,
				"condition":   "sunny",
				"humidity":    65,
			}, nil
		},
	}

	// Build options
	maxSteps := 5
	opts := ai.GenerateTextOptions{
		Model:    model,
		Prompt:   req.Prompt,
		Tools:    []types.Tool{weatherTool},
		MaxSteps: &maxSteps,
	}

	if req.System != "" {
		opts.System = req.System
	}

	// Generate with tools
	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		sendError(w, fmt.Sprintf("Generation failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Send response
	response := GenerateResponse{
		Text:   result.Text,
		Usage:  result.Usage,
		Finish: result.FinishReason,
		Steps:  result.Steps,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth handles health check requests
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"model":     model.ModelID(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// corsMiddleware adds CORS headers to all responses
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// sendError sends a JSON error response
func sendError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{Error: message})
}

// sendSSE sends a Server-Sent Event
func sendSSE(w http.ResponseWriter, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	if data != "" {
		fmt.Fprintf(w, "data: %s\n", data)
	}
	fmt.Fprintf(w, "\n")
}
