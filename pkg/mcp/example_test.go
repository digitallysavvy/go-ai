package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

// This example starts a tiny MCP server over HTTP so it runs offline. In a
// real program, point CreateHTTPMCPClient at your server URL (or use
// CreateStdioMCPClient to launch a local server process).
func ExampleMCPClient() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo":      map[string]interface{}{"name": "demo", "version": "1.0.0"},
			}
		case "tools/list":
			result = map[string]interface{}{"tools": []interface{}{map[string]interface{}{
				"name":        "echo",
				"description": "Echo the input back",
				"inputSchema": map[string]interface{}{"type": "object"},
			}}}
		case "tools/call":
			result = map[string]interface{}{"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hello"},
			}}
		default:
			if len(req.ID) == 0 { // notifications carry no id and need no reply
				w.WriteHeader(http.StatusAccepted)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"error": map[string]interface{}{"code": -32601, "message": "method not found"},
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()

	ctx := context.Background()
	client, err := mcp.CreateHTTPMCPClient(srv.URL, nil)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer client.Close()
	if err := client.Connect(ctx); err != nil {
		fmt.Println("error:", err)
		return
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, t := range tools {
		fmt.Println("tool:", t.Name)
	}

	res, err := client.CallTool(ctx, "echo", map[string]interface{}{"text": "hello"})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("content items:", len(res.Content))
	// Output:
	// tool: echo
	// content items: 1
}
