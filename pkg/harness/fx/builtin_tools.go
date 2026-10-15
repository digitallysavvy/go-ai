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
		InputSchema: obj([]string{"pattern"}, props("pattern", str(), "path", str(), "mode", enumStr("matches", "count"))),
	}),
	"grep": harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName:  "grep_files",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props(
			"pattern", str(), "path", str(), "include", str(),
			"case_insensitive", boolean(), "mode", enumStr("matches", "files_with_matches", "count"),
			"head_limit", posInt(), "offset", nonNegInt(), "context_lines", nonNegInt(),
		)),
	}),
	"webSearch": harness.CommonTool(harness.BuiltinToolWebSearch, harness.CommonToolOptions{
		NativeName:  "web_search",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"query"}, props("query", minLength(str(), 2), "allowed_domains", arr(str()), "blocked_domains", arr(str()))),
	}),
	"list_files": tool("list_files", harness.BuiltinToolUseKindReadonly, obj(nil, props("path", str()))),
	"read_file": tool("read_file", harness.BuiltinToolUseKindReadonly,
		obj([]string{"path"}, props("path", str(), "start_line", posInt(), "line_count", posInt()))),
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
	"memory":        tool("memory", "", obj([]string{"action"}, props("action", enumStr("save", "list", "clear"), "fact", str()))),
	"semantic_search": tool("semantic_search", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", str(), "path", str()))),
	"open_file": tool("open_file", "", obj([]string{"path"}, props("path", str()))),
	"web_fetch": tool("web_fetch", harness.BuiltinToolUseKindReadonly, obj([]string{"url"}, props("url", str()))),
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
				"cwd", nullish(str()),
				"profile", nullish(enumStr("clean", "user")),
				"tty", nullish(boolean()),
				"yield_time_ms", nullish(nonNegInt()),
				"timeout_ms", nullish(posInt()),
			)),
			// run, using an explicit executable.
			obj([]string{"action", "command", "shell", "tty"}, props(
				"action", constStr("run"),
				"command", str(),
				"cwd", nullish(str()),
				"shell", obj([]string{"kind", "path"}, props(
					"kind", constStr("executable"),
					"path", str(),
					"clean_start", nullish(boolean()),
				)),
				"tty", boolean(),
				"yield_time_ms", nullish(nonNegInt()),
				"timeout_ms", nullish(posInt()),
			)),
			obj([]string{"action", "session_id"}, props(
				"action", constStr("interact"),
				"session_id", str(),
				"chars", nullish(str()),
				"yield_time_ms", nullish(nonNegInt()),
			)),
			obj([]string{"action", "session_id"}, props(
				"action", constStr("stop"),
				"session_id", str(),
				"force", nullish(boolean()),
			)),
		},
	}),
	"skill": tool("skill", harness.BuiltinToolUseKindReadonly,
		obj([]string{"location"}, props("location", str(), "resource", str()))),
	"install_skill": tool("install_skill", harness.BuiltinToolUseKindEdit,
		obj([]string{"source"}, props("source", str(), "skill", str()))),
	"subagent": tool("subagent", "", obj([]string{"command"}, props("command", subagentCommandSchema()))),
	"mcp_search_tools": tool("mcp_search_tools", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", str(), "limit", posInt()))),
	"capability_search": tool("capability_search", harness.BuiltinToolUseKindReadonly,
		obj([]string{"query"}, props("query", minLength(str(), 1), "server", minLength(str(), 1)))),
	"mcp_select_tool": tool("mcp_select_tool", harness.BuiltinToolUseKindReadonly, obj([]string{"name"}, props("name", str()))),
	"mcp_features": tool("mcp_features", harness.BuiltinToolUseKindReadonly, obj([]string{"action", "server"}, props(
		"action", enumStr("resource_list", "resource_templates", "resource_read", "prompt_list", "prompt_get", "prompt_complete", "resource_complete"),
		"server", str(), "uri", str(), "uri_template", str(),
		"prompt", str(), "argument", str(), "value", str(),
		"arguments", stringRecord(), "context", stringRecord(),
	))),
	"ask_user_question": tool("ask_user_question", "", obj([]string{"questions"}, props(
		"questions", sized(arr(obj([]string{"question", "options"}, props(
			"question", str(),
			"options", sized(arr(obj([]string{"label"}, props("label", str(), "description", str()))), 2, 6),
		))), 1, 4),
		"permission_request_id", exactLength(str(), 64),
	))),
	"vision": tool("vision", "", obj([]string{"focus"}, props(
		"focus", minLength(str(), 1), "image_ids", sized(arr(intT()), 1, -1), "paths", sized(arr(str()), 1, -1),
	))),
	"read_tool_result": tool("read_tool_result", harness.BuiltinToolUseKindReadonly, obj([]string{"handle"}, props(
		"handle", str(), "start_byte", posInt(), "byte_count", posInt(), "query", str(),
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

// intT mirrors zod `z.number().int()`.
func intT() map[string]any { return map[string]any{"type": "integer"} }

// posInt mirrors zod `z.number().int().positive()`.
func posInt() map[string]any { return map[string]any{"type": "integer", "exclusiveMinimum": 0} }

// nonNegInt mirrors zod `z.number().int().nonnegative()`.
func nonNegInt() map[string]any { return map[string]any{"type": "integer", "minimum": 0} }

// intRange mirrors zod `z.number().int().min(lo).max(hi)`.
func intRange(lo, hi int) map[string]any {
	return map[string]any{"type": "integer", "minimum": lo, "maximum": hi}
}

// minLength / maxLength / exactLength mirror zod string `.min(n)` / `.max(n)` /
// `.length(n)`.
func minLength(m map[string]any, n int) map[string]any { m["minLength"] = n; return m }
func maxLength(m map[string]any, n int) map[string]any { m["maxLength"] = n; return m }
func exactLength(m map[string]any, n int) map[string]any {
	m["minLength"], m["maxLength"] = n, n
	return m
}

// sized mirrors zod array `.min(lo).max(hi)`; a negative bound is omitted.
func sized(m map[string]any, lo, hi int) map[string]any {
	if lo >= 0 {
		m["minItems"] = lo
	}
	if hi >= 0 {
		m["maxItems"] = hi
	}
	return m
}

// nullish mirrors zod `.nullish()`: the schema or null (the property itself is
// optional because it is left out of the required list).
func nullish(inner map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{inner, map[string]any{"type": "null"}}}
}

// stringRecord mirrors zod `z.record(z.string(), z.string())`.
func stringRecord() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
}

// nonEmpty is zod `z.string().min(1)`; bounded is `.min(1).max(n)`.
func nonEmpty() map[string]any           { return minLength(str(), 1) }
func bounded(n int) map[string]any       { return maxLength(minLength(str(), 1), n) }
func strEnum(v ...string) map[string]any { return enumStr(v...) }

// subagentNotificationsSchema mirrors TS `subagentNotificationsSchema`.
func subagentNotificationsSchema() map[string]any {
	return obj(nil, props(
		"terminal", obj(nil, props("completed", boolean(), "failed", boolean(), "cancelled", boolean())),
		"milestones", sized(arr(str()), -1, 32),
		"report_interval_ms", posInt(),
		"report_duration_ms", posInt(),
		"stop_conditions", sized(arr(strEnum("terminal", "duration_elapsed")), -1, 8),
	))
}

// subagentCommandSchema mirrors the TS `subagent` tool's `command` object.
func subagentCommandSchema() map[string]any {
	permissionMode := strEnum("ask", "auto", "yolo")
	return obj(nil, props(
		"create", obj([]string{"name", "mode"}, props(
			"name", bounded(128),
			"mode", strEnum("one_off", "persistent"),
			"prompt", bounded(65536),
			"model", bounded(256),
			"effort", bounded(64),
			"permission_mode", permissionMode,
			"notifications", subagentNotificationsSchema(),
		)),
		"inspect", obj([]string{"id", "sections"}, props(
			"id", nonEmpty(),
			"sections", sized(arr(strEnum("status", "messages", "tool_activity", "events", "configuration", "relationship")), 1, 6),
			"cursor", nonEmpty(),
			"limit", intRange(1, 100),
			"wait", obj([]string{"until", "timeout_ms"}, props(
				"until", constStr("settled"),
				"after_generation", nonNegInt(),
				"timeout_ms", intRange(1, 60000),
			)),
		)),
		"message", obj(nil, props(
			"send", obj([]string{"id", "content"}, props("id", nonEmpty(), "content", bounded(65536))),
			"milestone", obj([]string{"name"}, props("name", bounded(128))),
		)),
		"relationship", obj([]string{"action", "id"}, props(
			"action", strEnum("attach", "detach", "reparent"),
			"id", nonEmpty(),
			"parent_id", nonEmpty(),
		)),
		"configure", obj([]string{"id"}, props(
			"id", nonEmpty(),
			"name", bounded(128),
			"model", bounded(256),
			"effort", bounded(64),
			"permission_mode", permissionMode,
			"notifications", subagentNotificationsSchema(),
		)),
		"lifecycle", obj([]string{"id", "action"}, props(
			"id", nonEmpty(),
			"action", strEnum("cancel", "resume", "close", "reopen"),
		)),
	))
}
