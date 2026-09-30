package deepagents

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func objectSchema(required []string, props map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func stringProp(desc string) map[string]any {
	m := map[string]any{"type": "string"}
	if desc != "" {
		m["description"] = desc
	}
	return m
}

// BuiltinTools is every model-callable Deep Agents built-in, keyed by what
// the bridge emits (commonName ?? nativeName); every tool the bridge can
// call must be listed or the run_prompt driver rejects the tool call as
// unknown. Mirrors TS `DEEPAGENTS_BUILTIN_TOOLS`.
func BuiltinTools() map[string]harness.BuiltinTool {
	tools := map[string]harness.BuiltinTool{
		string(harness.BuiltinToolRead): harness.CommonTool(harness.BuiltinToolRead, harness.CommonToolOptions{
			NativeName: "read_file", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Read file contents",
			InputSchema: objectSchema([]string{"file_path"}, map[string]any{"file_path": stringProp("")}),
		}),
		string(harness.BuiltinToolWrite): harness.CommonTool(harness.BuiltinToolWrite, harness.CommonToolOptions{
			NativeName: "write_file", ToolUseKind: harness.BuiltinToolUseKindEdit,
			Description: "Create a file",
			InputSchema: objectSchema([]string{"file_path", "content"}, map[string]any{
				"file_path": stringProp(""), "content": stringProp(""),
			}),
		}),
		string(harness.BuiltinToolEdit): harness.CommonTool(harness.BuiltinToolEdit, harness.CommonToolOptions{
			NativeName: "edit_file", ToolUseKind: harness.BuiltinToolUseKindEdit,
			Description: "Perform exact string replacements in a file",
			InputSchema: objectSchema([]string{"file_path", "old_string", "new_string"}, map[string]any{
				"file_path": stringProp(""), "old_string": stringProp(""), "new_string": stringProp(""),
			}),
		}),
		string(harness.BuiltinToolBash): harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
			NativeName: "execute", ToolUseKind: harness.BuiltinToolUseKindBash,
			Description: "Run a shell command",
			InputSchema: objectSchema([]string{"command"}, map[string]any{"command": stringProp("")}),
		}),
		string(harness.BuiltinToolGrep): harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
			NativeName: "grep", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Search file contents",
			InputSchema: objectSchema([]string{"pattern"}, map[string]any{"pattern": stringProp("")}),
		}),
		string(harness.BuiltinToolGlob): harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
			NativeName: "glob", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Find files matching a glob pattern",
			InputSchema: objectSchema([]string{"pattern"}, map[string]any{"pattern": stringProp("")}),
		}),
	}
	// No common-name equivalent — keyed by native name.
	tools["ls"] = nativeTool("ls", "List files in a directory", objectSchema(nil, map[string]any{"path": stringProp("")}))
	tools["task"] = nativeTool("task", "Spawn a subagent to handle a delegated task",
		objectSchema(nil, map[string]any{"description": stringProp(""), "subagent_type": stringProp("")}))
	tools["write_todos"] = nativeTool("write_todos", "Manage a structured todo list",
		objectSchema(nil, map[string]any{"todos": map[string]any{"type": "array", "items": map[string]any{}}}))
	return tools
}

func nativeTool(name, description string, schema map[string]any) harness.BuiltinTool {
	return harness.BuiltinTool{
		Tool: types.Tool{Name: name, Description: description, Parameters: schema},
	}
}
