// Package mcp is a client for the Model Context Protocol (MCP). It connects
// to an MCP server, lists the tools, resources and prompts the server offers,
// and calls them. Use it to give a model or an agent the tools of an existing
// MCP server.
//
// Create a client with a transport that matches the server:
//
//   - CreateHTTPMCPClient: streamable HTTP, with optional OAuth.
//   - CreateSSEMCPClient: the older SSE transport.
//   - CreateStdioMCPClient: launches a local server process and talks over
//     standard input and output.
//
// Then connect, list tools, and call one:
//
//	client, err := mcp.CreateHTTPMCPClient("https://mcp.example.com/mcp", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer client.Close()
//	if err := client.Connect(ctx); err != nil {
//		log.Fatal(err)
//	}
//
//	tools, err := client.ListTools(ctx)
//	// ...
//	res, err := client.CallTool(ctx, "echo", map[string]interface{}{"text": "hello"})
//
// To hand the server's tools to an agent or to ai.GenerateText, call
// GetMCPToolsForAgent. It returns []types.Tool whose Execute functions call
// the server.
//
// Guide: https://goaisdk.com/docs/ai-sdk-core/mcp-tools.
//
// This package is the Go counterpart of @ai-sdk/mcp in the Vercel AI SDK for
// TypeScript.
package mcp
