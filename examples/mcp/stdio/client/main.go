package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
)

type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	nextID int
}

func NewMCPClient(serverPath string) (*MCPClient, error) {
	cmd := exec.Command(serverPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return &MCPClient{
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewScanner(stdout),
		nextID: 1,
	}, nil
}

func (c *MCPClient) Call(method string, params map[string]interface{}) (map[string]interface{}, error) {
	id := c.nextID
	c.nextID++

	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}

	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	if _, err := c.stdin.Write(append(requestJSON, '\n')); err != nil {
		return nil, err
	}

	if !c.stdout.Scan() {
		return nil, fmt.Errorf("no response")
	}

	var response map[string]interface{}
	if err := json.Unmarshal(c.stdout.Bytes(), &response); err != nil {
		return nil, err
	}

	if errObj, ok := response["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("RPC error: %v", errObj["message"])
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}

	return result, nil
}

func (c *MCPClient) Close() error {
	c.stdin.Close()
	return c.cmd.Wait()
}

func main() {
	client, err := NewMCPClient("../server/server")
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Initialize
	fmt.Println("=== Initializing MCP Client ===")
	result, err := client.Call("initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]interface{}{
			"name":    "go-ai-mcp-client",
			"version": "1.0.0",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Server info: %v\n\n", result["serverInfo"])

	// List tools
	fmt.Println("=== Listing Available Tools ===")
	result, err = client.Call("tools/list", map[string]interface{}{})
	if err != nil {
		log.Fatal(err)
	}

	tools := result["tools"].([]interface{})
	for i, tool := range tools {
		toolMap := tool.(map[string]interface{})
		fmt.Printf("%d. %s: %s\n", i+1, toolMap["name"], toolMap["description"])
	}
	fmt.Println()

	// Call calculator tool
	fmt.Println("=== Calling Calculator Tool ===")
	result, err = client.Call("tools/call", map[string]interface{}{
		"name": "calculator",
		"arguments": map[string]interface{}{
			"operation": "multiply",
			"a":         12.5,
			"b":         8.0,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Result: %v\n\n", result)

	// Generate completion
	fmt.Println("=== Generating Completion ===")
	result, err = client.Call("completion/generate", map[string]interface{}{
		"prompt": "What is 15 * 23? Use the calculator tool.",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Generated: %s\n", result["text"])
	fmt.Printf("Usage: %v\n", result["usage"])
}
