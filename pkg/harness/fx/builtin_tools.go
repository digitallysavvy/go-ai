package fx

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BuiltinTools mirrors TS `FX_BUILTIN_TOOLS`.
var BuiltinTools = map[string]harness.BuiltinTool{
	"glob": harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
		NativeName:  "glob_files",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props("pattern", str(), "path", str(), "mode", str())),
	}),
	"grep": harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName:  "grep_files",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props(
			"pattern", str(), "path", str(), "include", str(),
			"case_insensitive", boolean(), "mode", str(),
			"head_limit", number(), "offset", number(), "context_lines", number(),
		)),
	}),
	"webSearch": harness.CommonTool(harness.BuiltinToolWebSearch, harness.CommonToolOptions{
		NativeName:  "web_search",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"query"}, props("query", str(), "allowed_domains", arr(str()), "blocked_domains", arr(str()))),
	}),
	"list_files": tool("list_files", harness.BuiltinToolUseKindReadonly, obj(nil, props("path", str()))),
	"read_file": tool("read_file", harness.BuiltinToolUseKindReadonly,
		obj([]string{"path"}, props("path", str(), "start_line", number(), "line_count", number()))),
	"write_file": tool("write_file", harness.BuiltinToolUseKindEdit,
		obj([]string{"path", "content"}, props("path", str(), "content", str()))),
	"edit_file": tool("edit_file", harness.BuiltinToolUseKindEdit,
		obj([]string{"path", "old_string", "new_string"}, props("path", str(), "old_string", str(), "new_string", str()))),
	"delete_file": tool("delete_file", harness.BuiltinToolUseKindEdit, obj([]string{"path"}, props("path", str()))),
	"rename_file": tool("rename_file", harness.BuiltinToolUseKindEdit,
		obj([]string{"old_path", "new_path"}, props("old_path", str(), "new_path", str()))),
	"copy_file": tool("copy_file", harness.BuiltinToolUseKindEdit,
		obj([]string{"source", "destination"}, props("source", str(), "destination", str()))),
	"create_folder": tool("create_folder", harness.BuiltinToolUseKindEdit, obj([]string{"path"}, props("path", str()))),
	"file_info":     tool("file_info", harness.BuiltinToolUseKindReadonly, obj([]string{"path"}, props("path", str()))),
	"memory":        tool("memory", "", obj([]string{"action"}, props("action", str(), "fact", str()))),
	"semantic_search": tool("semantic_search", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", str(), "path", str()))),
	"open_file": tool("open_file", "", obj([]string{"path"}, props("path", str()))),
	"web_fetch": tool("web_fetch", harness.BuiltinToolUseKindReadonly, obj([]string{"url"}, props("url", str()))),
	// terminal is fx's original terminal tool, kept unchanged for
	// back-compat even though fx now also exposes "shell" (TS keeps both in
	// FX_BUILTIN_TOOLS too).
	"terminal": tool("terminal", harness.BuiltinToolUseKindBash, obj([]string{"action"}, props(
		"action", str(), "session_id", str(), "cwd", str(), "command", str(),
	))),
	// shell is fx's flat run/interact/stop tool (TS `shellInputSchema`, a
	// union of 4 variants: run-with-profile, run-with-explicit-executable,
	// interact, stop). JSON Schema `anyOf` mirrors TS `z.union([...])`, which
	// accepts a value matched by *any* member schema, not exclusively one
	// (unlike `oneOf` -- a run call with an explicit `shell` executable also
	// satisfies the profile variant's laxer requirements, and TS accepts
	// that); obj()/props() have no native union support, so this is
	// assembled directly instead of going through them for the outer shape.
	"shell": tool("shell", harness.BuiltinToolUseKindBash, map[string]any{
		"anyOf": []any{
			// run, using a named shell profile.
			obj([]string{"action", "command"}, props(
				"action", constStr("run"),
				"command", str(),
				"cwd", str(),
				"profile", enumStr("clean", "user"),
				"tty", boolean(),
				"yield_time_ms", number(),
				"timeout_ms", number(),
			)),
			// run, using an explicit executable.
			obj([]string{"action", "command", "shell", "tty"}, props(
				"action", constStr("run"),
				"command", str(),
				"cwd", str(),
				"shell", obj([]string{"kind", "path"}, props(
					"kind", constStr("executable"),
					"path", str(),
					"clean_start", boolean(),
				)),
				"tty", boolean(),
				"yield_time_ms", number(),
				"timeout_ms", number(),
			)),
			obj([]string{"action", "session_id"}, props(
				"action", constStr("interact"),
				"session_id", str(),
				"chars", str(),
				"yield_time_ms", number(),
			)),
			obj([]string{"action", "session_id"}, props(
				"action", constStr("stop"),
				"session_id", str(),
				"force", boolean(),
			)),
		},
	}),
	"skill": tool("skill", harness.BuiltinToolUseKindReadonly,
		obj([]string{"name"}, props("name", str(), "location", str(), "resource", str(), "offset", number()))),
	"install_skill": tool("install_skill", harness.BuiltinToolUseKindEdit,
		obj([]string{"source"}, props("source", str(), "skill", str()))),
	"subagent": tool("subagent", "", obj([]string{"command"}, props("command", map[string]any{"type": "object"}))),
	"mcp_search_tools": tool("mcp_search_tools", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", str(), "limit", number()))),
	"capability_search": tool("capability_search", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", str(), "server", str()))),
	"mcp_select_tool": tool("mcp_select_tool", harness.BuiltinToolUseKindReadonly, obj([]string{"name"}, props("name", str()))),
	"mcp_features": tool("mcp_features", harness.BuiltinToolUseKindReadonly, obj([]string{"action", "server"}, props(
		"action", str(), "server", str(), "uri", str(), "uri_template", str(),
		"prompt", str(), "argument", str(), "value", str(),
	))),
	"ask_user_question": tool("ask_user_question", "", obj([]string{"questions"}, props(
		"questions", arr(map[string]any{"type": "object"}), "permission_request_id", str(),
	))),
	"vision": tool("vision", "", obj([]string{"focus"}, props(
		"focus", str(), "image_ids", arr(number()), "paths", arr(str()),
	))),
	"read_tool_result": tool("read_tool_result", harness.BuiltinToolUseKindReadonly, obj([]string{"handle"}, props(
		"handle", str(), "start_byte", number(), "byte_count", number(), "query", str(),
	))),
}

func tool(name string, kind harness.BuiltinToolUseKind, schema map[string]any) harness.BuiltinTool {
	return harness.BuiltinTool{
		Tool:        types.Tool{Name: name, Parameters: schema},
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

func str() map[string]any     { return map[string]any{"type": "string"} }
func number() map[string]any  { return map[string]any{"type": "number"} }
func boolean() map[string]any { return map[string]any{"type": "boolean"} }

func arr(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

// constStr mirrors TS `z.literal(value)` on a string field.
func constStr(value string) map[string]any {
	return map[string]any{"type": "string", "const": value}
}

// enumStr mirrors TS `z.enum([...])`.
func enumStr(values ...string) map[string]any {
	anyValues := make([]any, len(values))
	for i, v := range values {
		anyValues[i] = v
	}
	return map[string]any{"type": "string", "enum": anyValues}
}
