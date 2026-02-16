package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

type MCPHTTPServer struct {
	provider *openai.Provider
	tools    map[string]types.Tool
}

func NewMCPHTTPServer(apiKey string) *MCPHTTPServer {
	return &MCPHTTPServer{
		provider: openai.New(openai.Config{APIKey: apiKey}),
		tools:    make(map[string]types.Tool),
	}
}

func (s *MCPHTTPServer) handleTools(w http.ResponseWriter, r *http.Request) {
	toolsList := make([]map[string]interface{}, 0, len(s.tools))
	for _, tool := range s.tools {
		toolsList = append(toolsList, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"tools": toolsList})
}

func (s *MCPHTTPServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt string `json:"prompt"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	model, _ := s.provider.LanguageModel("gpt-4")
	toolsArray := make([]types.Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		toolsArray = append(toolsArray, tool)
	}

	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: req.Prompt,
		Tools:  toolsArray,
	})

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"text":  result.Text,
		"usage": result.Usage,
	})
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	server := NewMCPHTTPServer(apiKey)
	server.tools["calculator"] = types.Tool{
		Name:        "calculator",
		Description: "Basic arithmetic",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{"type": "string"},
				"a":         map[string]interface{}{"type": "number"},
				"b":         map[string]interface{}{"type": "number"},
			},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			op := params["operation"].(string)
			a := params["a"].(float64)
			b := params["b"].(float64)
			switch op {
			case "add":
				return a + b, nil
			case "multiply":
				return a * b, nil
			default:
				return a - b, nil
			}
		},
	}

	http.HandleFunc("/tools", server.handleTools)
	http.HandleFunc("/generate", server.handleGenerate)

	fmt.Println("MCP HTTP Server on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
