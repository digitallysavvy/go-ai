package tools

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TextEditor20250728Command represents the command type for text editor tool
type TextEditor20250728Command string

const (
	CommandView       TextEditor20250728Command = "view"
	CommandCreate     TextEditor20250728Command = "create"
	CommandStrReplace TextEditor20250728Command = "str_replace"
	CommandInsert     TextEditor20250728Command = "insert"
)

// TextEditor20250728Input represents the input parameters for the text editor tool
type TextEditor20250728Input struct {
	// Command is the operation to perform
	Command TextEditor20250728Command `json:"command"`

	// Path is the absolute path to file or directory
	Path string `json:"path"`

	// FileText is the content for create command
	FileText *string `json:"file_text,omitempty"`

	// InsertLine is the line number to insert after (for insert command)
	InsertLine *int `json:"insert_line,omitempty"`

	// NewStr is the replacement string for str_replace command
	NewStr *string `json:"new_str,omitempty"`

	// InsertText is the text to insert for insert command
	InsertText *string `json:"insert_text,omitempty"`

	// OldStr is the string to replace for str_replace command
	OldStr *string `json:"old_str,omitempty"`

	// ViewRange is the line range for view command [start, end]
	ViewRange []int `json:"view_range,omitempty"`
}

// TextEditor20250728Args represents the configuration arguments for creating the text editor tool
type TextEditor20250728Args struct {
	// MaxCharacters is the optional maximum number of characters to view in files
	// Only compatible with text_editor_20250728 and later versions
	MaxCharacters *int
}

// TextEditor20250728 creates a text editor tool that enables Claude to view and modify text files.
//
// This tool helps Claude debug, fix, and improve code or other text documents by providing
// direct file interaction capabilities.
//
// Commands supported:
//   - view: View file contents or directory listing
//   - create: Create a new file with specified content
//   - str_replace: Replace a string in a file
//   - insert: Insert text after a specific line number
//
// Note: This version does not support the "undo_edit" command (removed in Claude 4 models).
//
// Supported models: Claude Sonnet 4, Claude Opus 4, Claude Opus 4.1
//
// Example:
//
//	tool := anthropicTools.TextEditor20250728(TextEditor20250728Args{
//	    MaxCharacters: ptr(100000), // Optional: limit file view size
//	})
//
//	// Or with defaults:
//	tool := anthropicTools.TextEditor20250728(TextEditor20250728Args{})
func TextEditor20250728(args TextEditor20250728Args) types.Tool {
	parameters := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type": "string",
				"enum": []string{
					string(CommandView),
					string(CommandCreate),
					string(CommandStrReplace),
					string(CommandInsert),
				},
				"description": "The command to run. Allowed options are: view, create, str_replace, insert. Note: undo_edit is not supported in Claude 4 models.",
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
				"description": "Optional parameter of str_replace command containing the new string (if not given, no string will be added).",
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
				"type": "array",
				"items": map[string]interface{}{
					"type": "integer",
				},
				"description": "Optional parameter of view command when path points to a file. If none is given, the full file is shown. If provided, the file will be shown in the indicated line number range, e.g. [11, 12] will show lines 11 and 12. Indexing at 1 to start. Setting [start_line, -1] shows all lines from start_line to the end of the file.",
			},
		},
		"required": []string{"command", "path"},
	}

	description := `Text editor tool for viewing and modifying text files.

Commands:
- view: View file contents or directory listing. Optional view_range parameter controls which lines to show.
- create: Create a new file with file_text content.
- str_replace: Replace old_str with new_str in the file.
- insert: Insert insert_text after line insert_line.

This tool enables Claude to:
- Debug code
- Fix bugs
- Improve code quality
- Edit configuration files
- Manage text documents

Note: The undo_edit command is not supported in Claude 4 models.`

	if args.MaxCharacters != nil {
		description += fmt.Sprintf("\n\nFile viewing is limited to %d characters to prevent context overflow.", *args.MaxCharacters)
	}

	tool := types.Tool{
		Name:        "anthropic.text_editor_20250728",
		Description: description,
		Parameters:  parameters,
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return nil, fmt.Errorf("text editor tool must be executed by the provider (Anthropic). Set ProviderExecuted: true")
		},
		ProviderExecuted: true,
	}

	return tool
}
