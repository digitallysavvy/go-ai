package mcp

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// CreateMCPTools validates MCP tool definitions and creates tools without an
// execute function or an MCP connection. Use it when tool calls run
// elsewhere, such as in a browser (TS createMCPTools, 93c63f71a1).
//
// definitions is the tools array MCPClient.ListTools returns: a []MCPTool, a
// JSON array as []byte or json.RawMessage, or any value that marshals to one
// (for example []map[string]interface{} or []interface{}). Executable code is
// never forwarded. The tools come back in definition order, one dynamic tool
// per definition.
//
// It returns an *MCPClientError when the definitions are malformed (message
// "Invalid MCP tool definitions", the validation error as cause), when a tool
// name repeats ("Duplicate MCP tool name ..."), or when a definition cannot be
// converted, for example an invalid MCP App resource URI ("Invalid MCP tool
// definition ...", the conversion error as cause).
func CreateMCPTools(definitions interface{}) ([]types.Tool, error) {
	var tools []MCPTool
	var raw []byte
	switch v := definitions.(type) {
	case []MCPTool:
		tools = v
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, &MCPClientError{Message: "Invalid MCP tool definitions", Cause: err}
		}
		raw = encoded
	case []byte:
		raw = v
	case json.RawMessage:
		raw = v
	default:
		encoded, err := json.Marshal(definitions)
		if err != nil {
			return nil, &MCPClientError{Message: "Invalid MCP tool definitions", Cause: err}
		}
		raw = encoded
	}

	// TS ListToolsResultSchema.safeParse({ tools: definitions }).
	wrapped := append(append([]byte(`{"tools":`), raw...), '}')
	if len(raw) == 0 || !json.Valid(wrapped) {
		wrapped = []byte(`{}`)
	}
	if err := validateMCPResultShape("tools/list", wrapped); err != nil {
		return nil, &MCPClientError{Message: "Invalid MCP tool definitions", Cause: err}
	}
	if tools == nil {
		var parsed struct {
			Tools []MCPTool `json:"tools"`
		}
		if err := jsonexact.UnmarshalPlain(wrapped, &parsed); err != nil {
			return nil, &MCPClientError{Message: "Invalid MCP tool definitions", Cause: err}
		}
		tools = parsed.Tools
	}

	converter := &MCPToolConverter{}
	seen := make(map[string]struct{}, len(tools))
	out := make([]types.Tool, 0, len(tools))
	for _, definition := range tools {
		if _, dup := seen[definition.Name]; dup {
			return nil, &MCPClientError{Message: `Duplicate MCP tool name "` + definition.Name + `"`}
		}
		seen[definition.Name] = struct{}{}
		tool, err := converter.buildTool(definition, nil, false)
		if err != nil {
			return nil, &MCPClientError{Message: `Invalid MCP tool definition "` + definition.Name + `"`, Cause: err}
		}
		out = append(out, tool)
	}
	return out, nil
}
