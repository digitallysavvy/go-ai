package codex

import "github.com/digitallysavvy/go-ai/pkg/harness"

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str() map[string]any { return map[string]any{"type": "string"} }

// builtinTools mirrors TS `CODEX_BUILTIN_TOOLS`: Codex's other native
// operations (apply_patch, todo planning) surface only as side-effect events
// (file-change, todo_list) and are not model-callable tools, so they are not
// declared here.
func builtinTools() map[string]harness.BuiltinTool {
	return map[string]harness.BuiltinTool{
		"bash": harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
			NativeName: "shell", ToolUseKind: harness.BuiltinToolUseKindBash,
			Description: "Execute a shell command",
			InputSchema: obj(map[string]any{"command": str()}, "command"),
		}),
		"webSearch": harness.CommonTool(harness.BuiltinToolWebSearch, harness.CommonToolOptions{
			NativeName: "web_search", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Search the web",
			InputSchema: obj(map[string]any{"query": str()}, "query"),
		}),
	}
}
