package codex

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str() map[string]any { return map[string]any{"type": "string"} }

func strEnum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

// builtinTools mirrors TS `CODEX_BUILTIN_TOOLS`: every native Codex tool the
// app-server can invoke as a model-callable tool. Other native operations
// (e.g. todo planning) surface only as side-effect events (todo_list) and
// are not model-callable tools, so they are not declared here.
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
		// No common-name equivalent — keyed by native name, mirrors TS
		// `apply_patch: { ...tool({ description: 'Apply a patch to files',
		// inputSchema: z.string() }), toolUseKind: 'edit' }`.
		"apply_patch": {
			Tool:        types.Tool{Name: "apply_patch", Description: "Apply a patch to files", Parameters: str()},
			ToolUseKind: harness.BuiltinToolUseKindEdit,
		},
		// Mirrors TS `view_image: { ...tool({ description: 'View a local
		// image file', inputSchema: z.object({ path, detail?, environment_id?
		// }) }), toolUseKind: 'readonly' }`.
		"view_image": {
			Tool: types.Tool{
				Name:        "view_image",
				Description: "View a local image file",
				Parameters: obj(map[string]any{
					"path":           str(),
					"detail":         strEnum("high", "original"),
					"environment_id": str(),
				}, "path"),
			},
			ToolUseKind: harness.BuiltinToolUseKindReadonly,
		},
	}
}
