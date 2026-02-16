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
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

var model provider.LanguageModel

// Request/Response types
type GenerateRequest struct {
	Prompt      string   `json:"prompt" validate:"required"`
	System      string   `json:"system"`
	MaxTokens   *int     `json:"maxTokens"`
	Temperature *float64 `json:"temperature"`
}

type GenerateResponse struct {
	Text   string             `json:"text"`
	Usage  types.Usage        `json:"usage"`
	Finish types.FinishReason `json:"finishReason"`
}

type StreamRequest struct {
	Prompt      string   `json:"prompt" validate:"required"`
	System      string   `json:"system"`
	MaxTokens   *int     `json:"maxTokens"`
	Temperature *float64 `json:"temperature"`
}

type ToolRequest struct {
	Query    string `json:"query" validate:"required"`
	MaxSteps *int   `json:"maxSteps"`
	System   string `json:"system"`
}

type ToolResponse struct {
	Result string             `json:"result"`
	Steps  []types.StepResult `json:"steps"`
	Usage  types.Usage        `json:"usage"`
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

	// Create Echo instance
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORS())
	e.Use(middleware.RequestID())

	// Custom error handler
	e.HTTPErrorHandler = customHTTPErrorHandler

	// Routes
	e.GET("/", handleRoot)
	e.GET("/health", handleHealth)
	e.POST("/generate", handleGenerate)
	e.POST("/stream", handleStream)
	e.POST("/tools", handleTools)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Echo server starting on port %s", port)
	log.Printf("Available endpoints:")
	log.Printf("  POST /generate - Text generation")
	log.Printf("  POST /stream   - Streaming SSE")
	log.Printf("  POST /tools    - Tool calling")
	log.Printf("  GET  /health   - Health check")

	if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func handleRoot(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"service": "Go AI SDK Echo Server",
		"version": "1.0.0",
		"endpoints": []map[string]string{
			{"method": "POST", "path": "/generate", "description": "Text generation"},
			{"method": "POST", "path": "/stream", "description": "Streaming (SSE)"},
			{"method": "POST", "path": "/tools", "description": "Tool calling"},
			{"method": "GET", "path": "/health", "description": "Health check"},
		},
	})
}

func handleHealth(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Unix(),
		"model":     model.ModelID(),
		"requestId": c.Response().Header().Get(echo.HeaderXRequestID),
	})
}

func handleGenerate(c echo.Context) error {
	var req GenerateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 60*time.Second)
	defer cancel()

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

	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Generation failed: %v", err))
	}

	return c.JSON(http.StatusOK, GenerateResponse{
		Text:   result.Text,
		Usage:  result.Usage,
		Finish: result.FinishReason,
	})
}

func handleStream(c echo.Context) error {
	var req StreamRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Set SSE headers
	c.Response().Header().Set(echo.HeaderContentType, "text/event-stream")
	c.Response().Header().Set("Cache-Control", "no-cache")
	c.Response().Header().Set("Connection", "keep-alive")
	c.Response().Header().Set("X-Accel-Buffering", "no")

	ctx, cancel := context.WithTimeout(c.Request().Context(), 120*time.Second)
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
		sendSSE(c.Response(), "error", fmt.Sprintf("Stream failed: %v", err))
		return nil
	}

	// Send start event
	sendSSE(c.Response(), "start", "")
	c.Response().Flush()

	// Stream chunks
	for chunk := range stream.Chunks() {
		if chunk.Type == provider.ChunkTypeText {
			sendSSE(c.Response(), "text", chunk.Text)
			c.Response().Flush()
		}
	}

	// Check for errors
	if err := stream.Err(); err != nil {
		sendSSE(c.Response(), "error", err.Error())
		c.Response().Flush()
		return nil
	}

	// Send completion
	usage := stream.Usage()
	usageJSON, _ := json.Marshal(map[string]interface{}{
		"inputTokens":  usage.GetInputTokens(),
		"outputTokens": usage.GetOutputTokens(),
		"totalTokens":  usage.GetTotalTokens(),
	})

	sendSSE(c.Response(), "done", string(usageJSON))
	c.Response().Flush()

	return nil
}

func handleTools(c echo.Context) error {
	var req ToolRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 90*time.Second)
	defer cancel()

	// Define tools
	calculatorTool := types.Tool{
		Name:        "calculator",
		Description: "Perform basic arithmetic operations",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{
					"type":        "number",
					"description": "First number",
				},
				"b": map[string]interface{}{
					"type":        "number",
					"description": "Second number",
				},
			},
			"required": []string{"operation", "a", "b"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			operation := params["operation"].(string)
			a := params["a"].(float64)
			b := params["b"].(float64)

			var result float64
			switch operation {
			case "add":
				result = a + b
			case "subtract":
				result = a - b
			case "multiply":
				result = a * b
			case "divide":
				if b == 0 {
					return nil, fmt.Errorf("division by zero")
				}
				result = a / b
			}

			return map[string]interface{}{
				"result":    result,
				"operation": operation,
				"operands":  []float64{a, b},
			}, nil
		},
	}

	timeTool := types.Tool{
		Name:        "get_time",
		Description: "Get the current time in a specific timezone",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "Timezone name (e.g., America/New_York, Europe/London)",
					"default":     "UTC",
				},
			},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			timezone := "UTC"
			if tz, ok := params["timezone"].(string); ok {
				timezone = tz
			}

			loc, err := time.LoadLocation(timezone)
			if err != nil {
				return nil, fmt.Errorf("invalid timezone: %v", err)
			}

			now := time.Now().In(loc)
			return map[string]interface{}{
				"timezone":  timezone,
				"time":      now.Format(time.RFC3339),
				"unix":      now.Unix(),
				"formatted": now.Format("Monday, January 2, 2006 3:04 PM MST"),
			}, nil
		},
	}

	maxSteps := 5
	if req.MaxSteps != nil {
		maxSteps = *req.MaxSteps
	}

	system := "You are a helpful assistant with access to tools. Use them when appropriate."
	if req.System != "" {
		system = req.System
	}

	opts := ai.GenerateTextOptions{
		Model:    model,
		Prompt:   req.Query,
		System:   system,
		Tools:    []types.Tool{calculatorTool, timeTool},
		MaxSteps: &maxSteps,
	}

	result, err := ai.GenerateText(ctx, opts)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Generation failed: %v", err))
	}

	return c.JSON(http.StatusOK, ToolResponse{
		Result: result.Text,
		Steps:  result.Steps,
		Usage:  result.Usage,
	})
}

func customHTTPErrorHandler(err error, c echo.Context) {
	code := http.StatusInternalServerError
	message := err.Error()

	if he, ok := err.(*echo.HTTPError); ok {
		code = he.Code
		if msg, ok := he.Message.(string); ok {
			message = msg
		}
	}

	if !c.Response().Committed {
		c.JSON(code, map[string]interface{}{
			"error":     message,
			"requestId": c.Response().Header().Get(echo.HeaderXRequestID),
			"timestamp": time.Now().Unix(),
		})
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
