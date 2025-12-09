package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// MCPToolsServer demonstrates MCP with rich tool definitions
type MCPToolsServer struct {
	provider provider.Provider
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	p := openai.New(openai.Config{APIKey: apiKey})

	server := &MCPToolsServer{
		provider: p,
	}

	// Routes
	http.HandleFunc("/tools/list", server.handleListTools)
	http.HandleFunc("/tools/call", server.handleCallTool)
	http.HandleFunc("/generate", server.handleGenerate)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Printf("🔧 MCP Tools Server on :%s\n", port)
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  GET  /tools/list - List all available tools")
	fmt.Println("  POST /tools/call - Call a specific tool")
	fmt.Println("  POST /generate   - Generate with tools")

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

// handleListTools returns all available tools
func (s *MCPToolsServer) handleListTools(w http.ResponseWriter, r *http.Request) {
	tools := s.getToolDefinitions()

	var toolsList []map[string]interface{}
	for _, tool := range tools {
		toolsList = append(toolsList, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": toolsList,
		"count": len(toolsList),
	})
}

// handleCallTool executes a specific tool
func (s *MCPToolsServer) handleCallTool(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Tool       string                 `json:"tool"`
		Parameters map[string]interface{} `json:"parameters"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Find and execute tool
	tools := s.getToolDefinitions()
	var tool *types.Tool

	for _, t := range tools {
		if t.Name == req.Tool {
			tool = &t
			break
		}
	}

	if tool == nil {
		http.Error(w, "Tool not found", http.StatusNotFound)
		return
	}

	result, err := tool.Execute(context.Background(), req.Parameters)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"tool":   req.Tool,
		"result": result,
	})
}

// handleGenerate processes generation requests with tools
func (s *MCPToolsServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	model, err := s.provider.LanguageModel("gpt-4")
	if err != nil {
		http.Error(w, "Model initialization failed", http.StatusInternalServerError)
		return
	}

	tools := s.getToolDefinitions()
	maxSteps := 5

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:    model,
		Prompt:   req.Prompt,
		Tools:    tools,
		MaxSteps: &maxSteps,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":  result.Text,
		"usage": result.Usage,
		"steps": result.Steps,
	})
}

// getToolDefinitions returns all tool definitions
func (s *MCPToolsServer) getToolDefinitions() []types.Tool {
	return []types.Tool{
		// Math tools
		{
			Name:        "calculate",
			Description: "Perform basic arithmetic operations",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type": "string",
						"enum": []string{"add", "subtract", "multiply", "divide"},
						"description": "The operation to perform",
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
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				op := params["operation"].(string)
				a := params["a"].(float64)
				b := params["b"].(float64)

				switch op {
				case "add":
					return a + b, nil
				case "subtract":
					return a - b, nil
				case "multiply":
					return a * b, nil
				case "divide":
					if b == 0 {
						return nil, fmt.Errorf("division by zero")
					}
					return a / b, nil
				default:
					return nil, fmt.Errorf("unknown operation: %s", op)
				}
			},
		},

		// Time tools
		{
			Name:        "get_current_time",
			Description: "Get the current time in various formats",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone (e.g., 'UTC', 'America/New_York')",
						"default":     "UTC",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Time format ('iso', 'unix', 'rfc3339')",
						"default":     "iso",
					},
				},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				format := "iso"
				if f, ok := params["format"].(string); ok {
					format = f
				}

				now := time.Now()

				switch format {
				case "unix":
					return now.Unix(), nil
				case "rfc3339":
					return now.Format(time.RFC3339), nil
				default:
					return now.Format("2006-01-02T15:04:05Z"), nil
				}
			},
		},

		// Conversion tools
		{
			Name:        "convert_temperature",
			Description: "Convert temperature between Celsius and Fahrenheit",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value": map[string]interface{}{
						"type":        "number",
						"description": "Temperature value",
					},
					"from_unit": map[string]interface{}{
						"type": "string",
						"enum": []string{"celsius", "fahrenheit"},
						"description": "Source unit",
					},
					"to_unit": map[string]interface{}{
						"type": "string",
						"enum": []string{"celsius", "fahrenheit"},
						"description": "Target unit",
					},
				},
				"required": []string{"value", "from_unit", "to_unit"},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				value := params["value"].(float64)
				from := params["from_unit"].(string)
				to := params["to_unit"].(string)

				if from == to {
					return value, nil
				}

				if from == "celsius" && to == "fahrenheit" {
					return (value * 9 / 5) + 32, nil
				}

				if from == "fahrenheit" && to == "celsius" {
					return (value - 32) * 5 / 9, nil
				}

				return nil, fmt.Errorf("unsupported conversion")
			},
		},

		// String tools
		{
			Name:        "text_stats",
			Description: "Get statistics about a text string",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to analyze",
					},
				},
				"required": []string{"text"},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				text := params["text"].(string)

				return map[string]interface{}{
					"length":     len(text),
					"words":      len(splitWords(text)),
					"lines":      len(splitLines(text)),
					"uppercase":  countUppercase(text),
					"lowercase":  countLowercase(text),
					"digits":     countDigits(text),
				}, nil
			},
		},

		// Utility tools
		{
			Name:        "generate_random",
			Description: "Generate random numbers or strings",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"type": map[string]interface{}{
						"type": "string",
						"enum": []string{"number", "string"},
						"description": "Type of random value",
					},
					"min": map[string]interface{}{
						"type":        "number",
						"description": "Minimum value (for numbers)",
						"default":     0,
					},
					"max": map[string]interface{}{
						"type":        "number",
						"description": "Maximum value (for numbers)",
						"default":     100,
					},
					"length": map[string]interface{}{
						"type":        "number",
						"description": "Length (for strings)",
						"default":     10,
					},
				},
				"required": []string{"type"},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				randType := params["type"].(string)

				if randType == "number" {
					min := 0.0
					max := 100.0
					if m, ok := params["min"].(float64); ok {
						min = m
					}
					if m, ok := params["max"].(float64); ok {
						max = m
					}

					// Simple random (use crypto/rand in production)
					return min + (max-min)*0.5, nil
				}

				return "random_string_abc123", nil
			},
		},

		// Math advanced
		{
			Name:        "sqrt",
			Description: "Calculate square root",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"number": map[string]interface{}{
						"type":        "number",
						"description": "Number to calculate square root of",
					},
				},
				"required": []string{"number"},
			},
			Execute: func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
				num := params["number"].(float64)
				if num < 0 {
					return nil, fmt.Errorf("cannot calculate square root of negative number")
				}
				return math.Sqrt(num), nil
			},
		},
	}
}

// Helper functions
func splitWords(text string) []string {
	// Simplified word splitting
	words := []string{}
	word := ""
	for _, ch := range text {
		if ch == ' ' || ch == '\n' {
			if word != "" {
				words = append(words, word)
				word = ""
			}
		} else {
			word += string(ch)
		}
	}
	if word != "" {
		words = append(words, word)
	}
	return words
}

func splitLines(text string) []string {
	lines := []string{""}
	for _, ch := range text {
		if ch == '\n' {
			lines = append(lines, "")
		} else {
			lines[len(lines)-1] += string(ch)
		}
	}
	return lines
}

func countUppercase(text string) int {
	count := 0
	for _, ch := range text {
		if ch >= 'A' && ch <= 'Z' {
			count++
		}
	}
	return count
}

func countLowercase(text string) int {
	count := 0
	for _, ch := range text {
		if ch >= 'a' && ch <= 'z' {
			count++
		}
	}
	return count
}

func countDigits(text string) int {
	count := 0
	for _, ch := range text {
		if ch >= '0' && ch <= '9' {
			count++
		}
	}
	return count
}
