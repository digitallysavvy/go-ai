package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/gin-gonic/gin"
)

var model provider.LanguageModel

// Request/Response types
type ChatRequest struct {
	Message     string                 `json:"message" binding:"required"`
	System      string                 `json:"system"`
	MaxTokens   *int                   `json:"maxTokens"`
	Temperature *float64               `json:"temperature"`
	Extra       map[string]interface{} `json:"extra"`
}

type ChatResponse struct {
	Response string      `json:"response"`
	Usage    types.Usage `json:"usage"`
}

type StreamRequest struct {
	Prompt      string   `json:"prompt" binding:"required"`
	System      string   `json:"system"`
	MaxTokens   *int     `json:"maxTokens"`
	Temperature *float64 `json:"temperature"`
}

type AgentRequest struct {
	Query      string   `json:"query" binding:"required"`
	MaxSteps   *int     `json:"maxSteps"`
	System     string   `json:"system"`
}

type AgentResponse struct {
	Result      string             `json:"result"`
	Steps       []types.StepResult `json:"steps"`
	ToolCalls   []types.ToolCall   `json:"toolCalls"`
	Usage       types.Usage        `json:"usage"`
}

func main() {
	// Get API key
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create provider and model
	p := openai.New(openai.Config{
		APIKey: apiKey,
	})

	var err error
	model, err = p.LanguageModel("gpt-4")
	if err != nil {
		log.Fatal(err)
	}

	// Setup Gin in release mode
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// CORS middleware
	r.Use(corsMiddleware())

	// Routes
	r.GET("/", handleRoot)
	r.GET("/health", handleHealth)
	r.POST("/chat", handleChat)
	r.POST("/stream", handleStream)
	r.POST("/agent", handleAgent)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Gin server starting on port %s", port)
	log.Printf("Available endpoints:")
	log.Printf("  POST /chat   - Chat completion")
	log.Printf("  POST /stream - Streaming SSE")
	log.Printf("  POST /agent  - Agent with tools")
	log.Printf("  GET  /health - Health check")

	if err := r.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}

func handleRoot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service": "Go AI SDK Gin Server",
		"version": "1.0.0",
		"endpoints": []gin.H{
			{"method": "POST", "path": "/chat", "description": "Chat completion"},
			{"method": "POST", "path": "/stream", "description": "Streaming (SSE)"},
			{"method": "POST", "path": "/agent", "description": "Agent with tools"},
			{"method": "GET", "path": "/health", "description": "Health check"},
		},
	})
}

func handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"model":     model.ModelID(),
	})
}

func handleChat(c *gin.Context) {
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	opts := ai.GenerateTextOptions{
		Model:  model,
		Prompt: req.Message,
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

	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, ChatResponse{
		Response: result.Text,
		Usage:    result.Usage,
	})
}

func handleStream(c *gin.Context) {
	var req StreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Set SSE headers
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

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

	stream, err := ai.StreamText(ctx, opts)
	if err != nil {
		sendSSE(c.Writer, "error", err.Error())
		return
	}

	// Send start event
	sendSSE(c.Writer, "start", "")
	c.Writer.Flush()

	// Stream chunks
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			sendSSE(c.Writer, "text", chunk.Text)
			c.Writer.Flush()
		}
	}

	// Check for errors
	if err := stream.Err(); err != nil {
		sendSSE(c.Writer, "error", err.Error())
		c.Writer.Flush()
		return
	}

	// Send completion
	usage := stream.Usage()
	sendSSE(c.Writer, "done", fmt.Sprintf(`{"totalTokens":%d}`, usage.TotalTokens))
	c.Writer.Flush()
}

func handleAgent(c *gin.Context) {
	var req AgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 90*time.Second)
	defer cancel()

	// Define tools
	weatherTool := types.Tool{
		Name:        "get_weather",
		Description: "Get the current weather for a location",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "City and state, e.g., San Francisco, CA",
				},
				"unit": map[string]interface{}{
					"type": "string",
					"enum": []string{"celsius", "fahrenheit"},
				},
			},
			"required": []string{"location"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			location := params["location"].(string)
			unit := "fahrenheit"
			if u, ok := params["unit"].(string); ok {
				unit = u
			}

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

	searchTool := types.Tool{
		Name:        "search",
		Description: "Search for information on the web",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "Search query",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			query := params["query"].(string)
			// Simulate search results
			return map[string]interface{}{
				"query": query,
				"results": []map[string]interface{}{
					{"title": "Result 1", "snippet": "Information about " + query},
					{"title": "Result 2", "snippet": "More details on " + query},
				},
			}, nil
		},
	}

	maxSteps := 5
	if req.MaxSteps != nil {
		maxSteps = *req.MaxSteps
	}

	system := "You are a helpful assistant with access to tools. Use them when needed."
	if req.System != "" {
		system = req.System
	}

	opts := ai.GenerateTextOptions{
		Model:    model,
		Prompt:   req.Query,
		System:   system,
		Tools:    []types.Tool{weatherTool, searchTool},
		MaxSteps: &maxSteps,
	}

	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, AgentResponse{
		Result:    result.Text,
		Steps:     result.Steps,
		ToolCalls: result.ToolCalls,
		Usage:     result.Usage,
	})
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusOK)
			return
		}

		c.Next()
	}
}

func sendSSE(w http.ResponseWriter, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	if data != "" {
		fmt.Fprintf(w, "data: %s\n", data)
	}
	fmt.Fprintf(w, "\n")
}
