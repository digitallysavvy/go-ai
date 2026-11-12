package mcp

import (
	"context"
	"errors"
	"testing"
)

// TS requestOnce: `response.result.resultType == null` is the modern
// missing-resultType error; a resultType that is present but not a string
// fails the result schema ("Failed to parse server response"), and a null
// result throws a TypeError inside the same try, so it is a parse failure
// too.
func TestR93toolsModernResultTypeNotString(t *testing.T) {
	for _, tc := range []struct{ result, want string }{
		{`{"resultType":5,"tools":[]}`, "Failed to parse server response"},
		{`null`, "Failed to parse server response"},
		{`{"resultType":null,"tools":[]}`, "Modern MCP result is missing resultType"},
		{`{"tools":[]}`, "Modern MCP result is missing resultType"},
	} {
		c, _ := r85toolsSetup(t, tc.result)
		_, err := c.ListTools(context.Background())
		var mcpErr *MCPClientError
		if !errors.As(err, &mcpErr) || mcpErr.Message != tc.want {
			t.Errorf("%s: err = %v, want %q", tc.result, err, tc.want)
		}
		_, err = c.CallTool(context.Background(), "deploy", nil)
		if !errors.As(err, &mcpErr) || mcpErr.Message != tc.want {
			t.Errorf("%s: call err = %v, want %q", tc.result, err, tc.want)
		}
	}
}
