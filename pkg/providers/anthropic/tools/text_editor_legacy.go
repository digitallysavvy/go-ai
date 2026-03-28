package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TextEditor20241022 creates a text editor tool for viewing and modifying text files.
//
// Supports view, create, str_replace, insert, and undo_edit commands.
// Requires beta header: computer-use-2024-10-22 (injected automatically).
//
// Supported models: Claude Sonnet 3.5
//
// Example:
//
//	tool := anthropicTools.TextEditor20241022()
func TextEditor20241022() types.Tool {
	return types.Tool{
		Name:        "anthropic.text_editor_20241022",
		Description: buildLegacyTextEditorDescription(true),
		Parameters:  buildLegacyTextEditorSchema(true),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("text editor tool must be executed by the provider (Anthropic)")
		},
		ProviderExecuted: true,
	}
}

// TextEditor20250124 creates a text editor tool for viewing and modifying text files.
//
// Supports view, create, str_replace, insert, and undo_edit commands.
// Requires beta header: computer-use-2025-01-24 (injected automatically).
//
// Supported models: Claude Sonnet 3.7
//
// Example:
//
//	tool := anthropicTools.TextEditor20250124()
func TextEditor20250124() types.Tool {
	return types.Tool{
		Name:        "anthropic.text_editor_20250124",
		Description: buildLegacyTextEditorDescription(true),
		Parameters:  buildLegacyTextEditorSchema(true),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("text editor tool must be executed by the provider (Anthropic)")
		},
		ProviderExecuted: true,
	}
}

// TextEditor20250429 creates a text editor tool for viewing and modifying text files.
//
// Supports view, create, str_replace, and insert commands. Does NOT support undo_edit.
// Requires beta header: computer-use-2025-01-24 (injected automatically).
//
// Deprecated: Use TextEditor20250728 instead.
//
// Example:
//
//	tool := anthropicTools.TextEditor20250429()
func TextEditor20250429() types.Tool {
	return types.Tool{
		Name:        "anthropic.text_editor_20250429",
		Description: buildLegacyTextEditorDescription(false),
		Parameters:  buildLegacyTextEditorSchema(false),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("text editor tool must be executed by the provider (Anthropic)")
		},
		ProviderExecuted: true,
	}
}

func buildLegacyTextEditorSchema(includeUndoEdit bool) map[string]interface{} {
	commands := []string{"view", "create", "str_replace", "insert"}
	if includeUndoEdit {
		commands = append(commands, "undo_edit")
	}
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"enum":        commands,
				"description": "The command to run.",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Absolute path to file or directory, e.g. /repo/file.py or /repo.",
			},
			"file_text": map[string]interface{}{
				"type":        "string",
				"description": "Required parameter of create command, with the content of the file to be created.",
			},
			"insert_line": map[string]interface{}{
				"type":        "integer",
				"description": "Required parameter of insert command. The new_str will be inserted AFTER the line insert_line of path.",
			},
			"new_str": map[string]interface{}{
				"type":        "string",
				"description": "Optional parameter of str_replace command containing the new string.",
			},
			"insert_text": map[string]interface{}{
				"type":        "string",
				"description": "Required parameter of insert command containing the text to insert.",
			},
			"old_str": map[string]interface{}{
				"type":        "string",
				"description": "Required parameter of str_replace command containing the string in path to replace.",
			},
			"view_range": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "integer"},
				"description": "Optional parameter of view command. [start_line, end_line] to show a range. Indexing starts at 1. Use -1 for end_line to show all remaining lines.",
			},
		},
		"required": []string{"command", "path"},
	}
}

func buildLegacyTextEditorDescription(includeUndoEdit bool) string {
	desc := `Text editor tool for viewing and modifying text files.

Commands:
- view: View file contents or directory listing
- create: Create a new file with file_text content
- str_replace: Replace old_str with new_str in the file
- insert: Insert insert_text after line insert_line`
	if includeUndoEdit {
		desc += "\n- undo_edit: Undo the last edit made to the file"
	}
	desc += "\n\nThis tool enables Claude to directly interact with files for debugging and editing."
	return desc
}
