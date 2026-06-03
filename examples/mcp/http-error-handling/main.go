package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/mcp"
)

func main() {
	transport := mcp.NewHTTPTransport(mcp.HTTPTransportConfig{
		URL:       "https://mcp.example.com/mcp",
		TimeoutMS: 30000,
	})
	client := mcp.NewMCPClient(transport, mcp.MCPClientConfig{
		ClientName:    "go-ai-example",
		ClientVersion: "1.0.0",
	})

	if err := client.Connect(context.Background()); err != nil {
		var clientErr *mcp.MCPClientError
		if errors.As(err, &clientErr) {
			switch clientErr.StatusCode {
			case http.StatusUnauthorized:
				fmt.Printf("refresh MCP credentials for %s\n", clientErr.URL)
			case http.StatusNotFound:
				fmt.Printf("check MCP URL %s: %s\n", clientErr.URL, clientErr.ResponseBody)
			default:
				fmt.Printf("MCP HTTP %d from %s: %s\n", clientErr.StatusCode, clientErr.URL, clientErr.ResponseBody)
			}
			return
		}
		log.Fatal(err)
	}
	defer client.Close()
}
