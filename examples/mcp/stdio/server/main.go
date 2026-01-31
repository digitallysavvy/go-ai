package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
)

// MCPRequest represents an MCP protocol request
type MCPRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      interface{}            `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
}

// MCPResponse represents an MCP protocol response
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP error
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// MCPServer handles MCP protocol over stdio
type MCPServer struct {
	provider interface{}
	tools    map[string]types.Tool
	apiKey   string
}

func NewMCPServer(apiKey string) *MCPServer {
	return &MCPServer{
		apiKey: apiKey,
		tools:  make(map[string]types.Tool),
	}
}

func (s *MCPServer) RegisterTool(tool types.Tool) {
	s.tools[tool.Name] = tool
}

func (s *MCPServer) Start() error {
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			s.sendError(encoder, nil, -32700, "Parse error", err.Error())
			continue
		}

		response := s.handleRequest(req)
		if err := encoder.Encode(response); err != nil {
			log.Printf("Failed to encode response: %v", err)
		}
	}

	return scanner.Err()
}

func (s *MCPServer) handleRequest(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolCall(req)
	case "completion/generate":
		return s.handleGenerate(req)
	default:
		return MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: &MCPError{
				Code:    -32601,
				Message: "Method not found",
			},
		}
	}
}

func (s *MCPServer) handleInitialize(req MCPRequest) MCPResponse {
	// Initialize AI provider
	p := openai.New(openai.Config{
		APIKey: s.apiKey,
	})

	s.provider = p

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "go-ai-mcp-server",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
				"prompts": map[string]interface{}{},
			},
		},
	}
}

func (s *MCPServer) handleToolsList(req MCPRequest) MCPResponse {
	toolsList := make([]map[string]interface{}, 0, len(s.tools))

	for _, tool := range s.tools {
		toolsList = append(toolsList, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.Parameters,
		})
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": toolsList,
		},
	}
}

func (s *MCPServer) handleToolCall(req MCPRequest) MCPResponse {
	toolName, ok := req.Params["name"].(string)
	if !ok {
		return s.errorResponse(req.ID, -32602, "Invalid params", "tool name required")
	}

	tool, exists := s.tools[toolName]
	if !exists {
		return s.errorResponse(req.ID, -32602, "Invalid params", "tool not found")
	}

	arguments, ok := req.Params["arguments"].(map[string]interface{})
	if !ok {
		arguments = make(map[string]interface{})
	}

	result, err := tool.Execute(context.Background(), arguments, types.ToolExecutionOptions{})
	if err != nil {
		return s.errorResponse(req.ID, -32603, "Execution error", err.Error())
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": fmt.Sprintf("%v", result),
				},
			},
		},
	}
}

func (s *MCPServer) handleGenerate(req MCPRequest) MCPResponse {
	prompt, ok := req.Params["prompt"].(string)
	if !ok {
		return s.errorResponse(req.ID, -32602, "Invalid params", "prompt required")
	}

	if s.provider == nil {
		return s.errorResponse(req.ID, -32603, "Internal error", "provider not initialized")
	}

	// Get model from provider
	p, ok := s.provider.(*openai.Provider)
	if !ok {
		return s.errorResponse(req.ID, -32603, "Internal error", "invalid provider type")
	}

	model, err := p.LanguageModel("gpt-4")
	if err != nil {
		return s.errorResponse(req.ID, -32603, "Model error", err.Error())
	}

	// Convert tools to array
	toolsArray := make([]types.Tool, 0, len(s.tools))
	for _, tool := range s.tools {
		toolsArray = append(toolsArray, tool)
	}

	// Generate text
	result, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{
		Model:  model,
		Prompt: prompt,
		Tools:  toolsArray,
	})

	if err != nil {
		return s.errorResponse(req.ID, -32603, "Generation error", err.Error())
	}

	return MCPResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"text": result.Text,
			"usage": map[string]interface{}{
				"inputTokens":  result.Usage.GetInputTokens(),
				"outputTokens": result.Usage.GetOutputTokens(),
				"totalTokens":  result.Usage.GetTotalTokens(),
			},
		},
	}
}

func (s *MCPServer) errorResponse(id interface{}, code int, message, data string) MCPResponse {
	return MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

func (s *MCPServer) sendError(encoder *json.Encoder, id interface{}, code int, message, data string) {
	response := s.errorResponse(id, code, message, data)
	encoder.Encode(response)
}

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	server := NewMCPServer(apiKey)

	// Register example tools
	server.RegisterTool(types.Tool{
		Name:        "get_time",
		Description: "Get the current time",
		Parameters: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			return fmt.Sprintf("Current time: %s", os.Getenv("TZ")), nil
		},
	})

	server.RegisterTool(types.Tool{
		Name:        "calculator",
		Description: "Perform basic arithmetic",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"operation", "a", "b"},
		},
		Execute: func(ctx context.Context, params map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
			op := params["operation"].(string)
			a := params["a"].(float64)
			b := params["b"].(float64)

			var result float64
			switch op {
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

			return result, nil
		},
	})

	log.Println("MCP Server started on stdio")
	if err := server.Start(); err != nil {
		log.Fatal(err)
	}
}
