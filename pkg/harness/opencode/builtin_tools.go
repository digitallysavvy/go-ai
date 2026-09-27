package opencode

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func objectSchema(props map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": props}
}

func stringProp() map[string]any { return map[string]any{"type": "string"} }

func nativeTool(name, description string, schema map[string]any) harness.BuiltinTool {
	return harness.BuiltinTool{Tool: types.Tool{Name: name, Description: description, Parameters: schema}}
}

// BuiltinTools is every model-callable OpenCode built-in, keyed by what the
// bridge emits (commonName ?? nativeName). Mirrors TS
// `OPENCODE_BUILTIN_TOOLS`.
func BuiltinTools() map[string]harness.BuiltinTool {
	standard := harness.StandardBuiltinTools()
	askUserQuestions := harness.BuiltinTool{
		Tool:        standard[harness.BuiltinToolAskUserQuestions],
		NativeName:  "question",
		CommonName:  harness.BuiltinToolAskUserQuestions,
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
	}

	tools := map[string]harness.BuiltinTool{
		string(harness.BuiltinToolAskUserQuestions): askUserQuestions,
		string(harness.BuiltinToolRead): harness.CommonTool(harness.BuiltinToolRead, harness.CommonToolOptions{
			NativeName: "view", ToolUseKind: harness.BuiltinToolUseKindReadonly, Description: "Read file contents",
			InputSchema: objectSchema(map[string]any{"file_path": stringProp(), "path": stringProp()}),
		}),
		string(harness.BuiltinToolWrite): harness.CommonTool(harness.BuiltinToolWrite, harness.CommonToolOptions{
			NativeName: "write", ToolUseKind: harness.BuiltinToolUseKindEdit, Description: "Write content to a file",
			InputSchema: objectSchema(map[string]any{"file_path": stringProp(), "path": stringProp(), "content": stringProp()}),
		}),
		string(harness.BuiltinToolEdit): harness.CommonTool(harness.BuiltinToolEdit, harness.CommonToolOptions{
			NativeName: "edit", ToolUseKind: harness.BuiltinToolUseKindEdit, Description: "Edit a file by replacing text",
			InputSchema: objectSchema(map[string]any{
				"file_path": stringProp(), "path": stringProp(), "old_string": stringProp(), "new_string": stringProp(),
			}),
		}),
		string(harness.BuiltinToolBash): harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
			NativeName: "bash", ToolUseKind: harness.BuiltinToolUseKindBash, Description: "Execute a shell command",
			InputSchema: objectSchema(map[string]any{"command": stringProp()}),
		}),
		string(harness.BuiltinToolGlob): harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
			NativeName: "glob", ToolUseKind: harness.BuiltinToolUseKindReadonly, Description: "Find files matching a glob pattern",
			InputSchema: objectSchema(map[string]any{"pattern": stringProp(), "path": stringProp()}),
		}),
		string(harness.BuiltinToolGrep): harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
			NativeName: "grep", ToolUseKind: harness.BuiltinToolUseKindReadonly, Description: "Search file contents with regex",
			InputSchema: objectSchema(map[string]any{"pattern": stringProp(), "path": stringProp()}),
		}),
	}

	tools["ls"] = nativeTool("ls", "List directory contents", objectSchema(map[string]any{"path": stringProp()}))
	tools["webfetch"] = nativeTool("webfetch", "Fetch a URL", objectSchema(map[string]any{"url": stringProp(), "prompt": stringProp()}))
	tools["skill"] = nativeTool("skill", "Load an OpenCode skill by name", objectSchema(map[string]any{"name": stringProp()}))
	tools["todowrite"] = nativeTool("todowrite", "Replace the OpenCode session todo list", objectSchema(map[string]any{
		"todos": map[string]any{"type": "array", "items": objectSchema(map[string]any{
			"content": stringProp(), "status": stringProp(), "priority": stringProp(),
		})},
	}))
	tools["agent"] = nativeTool("agent", "Run an OpenCode subagent", objectSchema(map[string]any{
		"agent": stringProp(), "prompt": stringProp(), "description": stringProp(),
		"metadata": map[string]any{"type": "object"},
	}))
	return tools
}
