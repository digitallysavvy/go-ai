package cursor

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Tool variants, display titles, ACP kinds, and raw input projections were
// captured from the official Cursor CLI 2026.08.11-e8db854 ACP
// implementation, mirroring TS `CURSOR_BUILTIN_TOOLS`. `mcpToolCall` is
// dynamic and `truncatedToolCall` is a transport sentinel, so neither is
// exposed as a built-in here either.
var BuiltinTools = map[string]harness.BuiltinTool{
	"bash": withTitle(harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
		NativeName:  "shellToolCall",
		ToolUseKind: harness.BuiltinToolUseKindBash,
		InputSchema: obj([]string{"command"}, props("command", str())),
	}), "Terminal"),
	"delete": tool("delete", "deleteToolCall", harness.BuiltinToolUseKindEdit, "Delete",
		obj([]string{"path"}, props("path", str()))),
	"glob": withTitle(harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
		NativeName:  "globToolCall",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props("pattern", str())),
	}), "Find"),
	"grep": withTitle(harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName:  "grepToolCall",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props("pattern", str(), "path", str())),
	}), "grep"),
	"read": tool("read", "readToolCall", harness.BuiltinToolUseKindReadonly, "Read",
		obj([]string{"path"}, props("path", str()))),
	"updateTodos": tool("updateTodos", "updateTodosToolCall", "", "Update TODOs", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"_toolName": map[string]any{"type": "string", "const": "updateTodos"},
			"todos": map[string]any{
				"type": "array",
				"items": obj([]string{"id", "content", "status"}, props(
					"id", str(), "content", str(), "status", str(),
				)),
			},
		},
	}),
	"readTodos": tool("readTodos", "readTodosToolCall", harness.BuiltinToolUseKindReadonly, "Read TODOs",
		obj(nil, map[string]any{})),
	"edit": tool("edit", "editToolCall", harness.BuiltinToolUseKindEdit, "Edit",
		obj([]string{"path"}, props("path", str()))),
	"ls": tool("ls", "lsToolCall", harness.BuiltinToolUseKindReadonly, "List",
		obj([]string{"path"}, props("path", str()))),
	"readLints": tool("readLints", "readLintsToolCall", harness.BuiltinToolUseKindReadonly, "Read Lints",
		obj([]string{"paths"}, map[string]any{"paths": arr(str())})),
	"semanticSearch": tool("semanticSearch", "semSearchToolCall", harness.BuiltinToolUseKindReadonly, "Codebase Search",
		obj([]string{"query"}, props("query", str()))),
	"createPlan": tool("createPlan", "createPlanToolCall", "", "Create Plan", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"_toolName": map[string]any{"type": "string", "const": "createPlan"},
			"name":      str(),
			"plan":      str(),
		},
		"required": []string{"_toolName"},
	}),
	"webSearch": tool("webSearch", "webSearchToolCall", harness.BuiltinToolUseKindReadonly, "Web Search",
		obj([]string{"searchTerm"}, props("searchTerm", str()))),
	"task": tool("task", "taskToolCall", "", "Task", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"_toolName":    map[string]any{"type": "string", "const": "task"},
			"prompt":       str(),
			"description":  str(),
			"subagentType": map[string]any{},
		},
		"required": []string{"_toolName"},
	}),
	"listMcpResources": tool("listMcpResources", "listMcpResourcesToolCall", harness.BuiltinToolUseKindReadonly, "List MCP Resources",
		obj(nil, props("server", str()))),
	"readMcpResource": tool("readMcpResource", "readMcpResourceToolCall", harness.BuiltinToolUseKindReadonly, "Fetch MCP Resource",
		obj(nil, props("server", str(), "uri", str(), "downloadPath", str()))),
	"applyAgentDiff": tool("applyAgentDiff", "applyAgentDiffToolCall", harness.BuiltinToolUseKindEdit, "Apply Agent Diff",
		obj(nil, props("path", str()))),
	"fetch": tool("fetch", "fetchToolCall", harness.BuiltinToolUseKindReadonly, "Fetch",
		obj([]string{"url"}, props("url", str()))),
	"switchMode": tool("switchMode", "switchModeToolCall", "", "Switch Mode",
		obj([]string{"targetModeId"}, props("targetModeId", str(), "explanation", str()))),
	"generateImage": tool("generateImage", "generateImageToolCall", "", "Generate Image", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"_toolName":   map[string]any{"type": "string", "const": "generateImage"},
			"description": str(),
			"filename":    str(),
		},
		"required": []string{"_toolName"},
	}),
	"recordScreen": tool("recordScreen", "recordScreenToolCall", "", "Record Screen", obj(nil, map[string]any{})),
	"computerUse": tool("computerUse", "computerUseToolCall", harness.BuiltinToolUseKindBash, "Computer Use",
		obj(nil, map[string]any{"action": str(), "coordinate": map[string]any{}, "text": str()})),
	"writeShellStdin": tool("writeShellStdin", "writeShellStdinToolCall", harness.BuiltinToolUseKindBash, "Write to stdin",
		obj(nil, map[string]any{"shellId": map[string]any{"type": "number"}, "chars": str()})),
	"reflect": tool("reflect", "reflectToolCall", harness.BuiltinToolUseKindReadonly, "Reflect",
		obj(nil, props("reflection", str()))),
	"setupVmEnvironment": tool("setupVmEnvironment", "setupVmEnvironmentToolCall", harness.BuiltinToolUseKindBash, "Setup VM Environment",
		obj(nil, map[string]any{})),
	"replaceEnv": tool("replaceEnv", "replaceEnvToolCall", harness.BuiltinToolUseKindBash, "Replace Environment",
		obj(nil, map[string]any{})),
	"startGrindExecution": tool("startGrindExecution", "startGrindExecutionToolCall", harness.BuiltinToolUseKindBash, "Start Grind Execution",
		obj(nil, map[string]any{})),
	"startGrindPlanning": tool("startGrindPlanning", "startGrindPlanningToolCall", harness.BuiltinToolUseKindReadonly, "Start Grind Planning",
		obj(nil, map[string]any{})),
	"webFetch": tool("webFetch", "webFetchToolCall", harness.BuiltinToolUseKindReadonly, "Web Fetch",
		obj([]string{"url"}, props("url", str()))),
	"reportBugfixResults": tool("reportBugfixResults", "reportBugfixResultsToolCall", "", "Report Bugfix Results",
		obj(nil, map[string]any{})),
}

func withTitle(bt harness.BuiltinTool, title string) harness.BuiltinTool {
	bt.Title = title
	return bt
}

func tool(name, nativeName string, kind harness.BuiltinToolUseKind, title string, schema map[string]any) harness.BuiltinTool {
	return harness.BuiltinTool{
		Tool: types.Tool{
			Name:       name,
			Title:      title,
			Parameters: schema,
		},
		NativeName:  nativeName,
		ToolUseKind: kind,
	}
}

func obj(required []string, properties map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func props(kv ...any) map[string]any {
	out := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		name, _ := kv[i].(string)
		out[name] = kv[i+1]
	}
	return out
}

func str() map[string]any { return map[string]any{"type": "string"} }

func arr(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
