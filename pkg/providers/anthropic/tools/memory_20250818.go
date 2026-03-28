package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Memory20250818 creates the Anthropic memory tool that enables Claude to store and retrieve
// information across conversations through a memory file directory.
//
// Claude can create, read, update, and delete files that persist between sessions,
// allowing it to build knowledge over time without keeping everything in the context window.
// The memory tool operates client-side — you control where and how data is stored.
//
// Requires beta header: context-management-2025-06-27 (injected automatically).
//
// Supported models: Claude Sonnet 4.5, Claude Sonnet 4, Claude Opus 4.1, Claude Opus 4
//
// Example:
//
//	tool := anthropicTools.Memory20250818()
func Memory20250818() types.Tool {
	return types.Tool{
		Name: "anthropic.memory_20250818",
		Description: `Memory tool for storing and retrieving information across conversations.

Supported commands:
- view: Read a file or list a directory. Optional view_range [start_line, end_line] for partial reads.
- create: Create a new file with the given file_text content.
- str_replace: Replace old_str with new_str in a file.
- insert: Insert insert_text after line insert_line in a file.
- delete: Delete a file.
- rename: Rename old_path to new_path.

Use this tool to persist information that should survive across conversation sessions.`,
		Parameters: map[string]interface{}{
			"type": "object",
			"oneOf": []interface{}{
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command":    map[string]interface{}{"type": "string", "enum": []string{"view"}},
						"path":       map[string]interface{}{"type": "string", "description": "Absolute path to file or directory."},
						"view_range": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}, "minItems": 2, "maxItems": 2, "description": "Optional [start_line, end_line] range."},
					},
					"required": []string{"command", "path"},
				},
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command":   map[string]interface{}{"type": "string", "enum": []string{"create"}},
						"path":      map[string]interface{}{"type": "string", "description": "Absolute path to the file to create."},
						"file_text": map[string]interface{}{"type": "string", "description": "Content of the file to create."},
					},
					"required": []string{"command", "path", "file_text"},
				},
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command": map[string]interface{}{"type": "string", "enum": []string{"str_replace"}},
						"path":    map[string]interface{}{"type": "string", "description": "Absolute path to the file."},
						"old_str": map[string]interface{}{"type": "string", "description": "String to replace."},
						"new_str": map[string]interface{}{"type": "string", "description": "Replacement string."},
					},
					"required": []string{"command", "path", "old_str", "new_str"},
				},
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command":     map[string]interface{}{"type": "string", "enum": []string{"insert"}},
						"path":        map[string]interface{}{"type": "string", "description": "Absolute path to the file."},
						"insert_line": map[string]interface{}{"type": "integer", "description": "Insert after this line number."},
						"insert_text": map[string]interface{}{"type": "string", "description": "Text to insert."},
					},
					"required": []string{"command", "path", "insert_line", "insert_text"},
				},
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command": map[string]interface{}{"type": "string", "enum": []string{"delete"}},
						"path":    map[string]interface{}{"type": "string", "description": "Absolute path to the file to delete."},
					},
					"required": []string{"command", "path"},
				},
				map[string]interface{}{
					"properties": map[string]interface{}{
						"command":  map[string]interface{}{"type": "string", "enum": []string{"rename"}},
						"old_path": map[string]interface{}{"type": "string", "description": "Current absolute path."},
						"new_path": map[string]interface{}{"type": "string", "description": "New absolute path."},
					},
					"required": []string{"command", "old_path", "new_path"},
				},
			},
		},
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("memory tool must be executed by the provider (Anthropic)")
		},
		ProviderExecuted: true,
	}
}
