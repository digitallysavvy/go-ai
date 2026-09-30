package grokbuild

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BuiltinTools mirrors TS `GROK_BUILD_BUILTIN_TOOLS`. Tool names and inputs
// were captured from the model request produced by @xai-official/grok
// 0.2.111, the version used by the pinned Grok Build ACP implementation.
var BuiltinTools = map[string]harness.BuiltinTool{
	"askUserQuestions": {
		Tool:       harness.StandardBuiltinTools()[harness.BuiltinToolAskUserQuestions],
		CommonName: harness.BuiltinToolAskUserQuestions,
	},
	"bash": harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
		NativeName:  "run_terminal_command",
		ToolUseKind: harness.BuiltinToolUseKindBash,
		InputSchema: obj([]string{"command"}, props(
			"command", str(), "timeout", number(), "description", str(), "background", boolean(),
		)),
	}),
	"edit": harness.CommonTool(harness.BuiltinToolEdit, harness.CommonToolOptions{
		NativeName:  "search_replace",
		ToolUseKind: harness.BuiltinToolUseKindEdit,
		InputSchema: obj([]string{"file_path", "old_string", "new_string"}, props(
			"file_path", str(), "old_string", str(), "new_string", str(), "replace_all", boolean(),
		)),
	}),
	"grep": harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName:  "grep",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props(
			"pattern", str(), "path", str(), "glob", str(), "-B", number(), "-A", number(),
			"-C", number(), "-i", boolean(), "type", str(), "head_limit", number(), "multiline", boolean(),
		)),
	}),
	"webSearch": harness.CommonTool(harness.BuiltinToolWebSearch, harness.CommonToolOptions{
		NativeName:  "web_search",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"query"}, props("query", str(), "allowed_domains", arr(str()))),
	}),
	"write": harness.CommonTool(harness.BuiltinToolWrite, harness.CommonToolOptions{
		NativeName:  "write",
		ToolUseKind: harness.BuiltinToolUseKindEdit,
		InputSchema: obj([]string{"file_path", "content"}, props("file_path", str(), "content", str())),
	}),
	"read_file": tool("read_file", harness.BuiltinToolUseKindReadonly, obj([]string{"target_file"}, props(
		"target_file", str(), "offset", number(), "limit", number(), "pages", str(), "format", str(),
	))),
	"list_dir":                 tool("list_dir", harness.BuiltinToolUseKindReadonly, obj([]string{"target_directory"}, props("target_directory", str()))),
	"kill_command_or_subagent": tool("kill_command_or_subagent", "", obj([]string{"task_id"}, props("task_id", str()))),
	"todo_write": tool("todo_write", "", obj([]string{"todos"}, props(
		"merge", boolean(), "todos", arr(obj([]string{"id"}, props("id", str(), "content", str(), "status", str()))),
	))),
	"get_command_or_subagent_output": tool("get_command_or_subagent_output", harness.BuiltinToolUseKindReadonly,
		obj(nil, props("task_ids", arr(str()), "timeout_ms", number()))),
	"spawn_subagent": tool("spawn_subagent", "", obj([]string{"prompt", "description"}, props(
		"prompt", str(), "description", str(), "subagent_type", str(), "background", boolean(),
		"capability_mode", str(), "isolation", str(), "resume_from", str(), "cwd", str(), "model", str(),
	))),
	"scheduler_create": tool("scheduler_create", "", obj(nil, props(
		"task_id", str(), "interval", str(), "prompt", str(), "durable", boolean(),
		"foreground", boolean(), "fire_immediately", boolean(),
	))),
	"scheduler_delete": tool("scheduler_delete", "", obj([]string{"id"}, props("id", str()))),
	"scheduler_list":   tool("scheduler_list", harness.BuiltinToolUseKindReadonly, obj(nil, map[string]any{})),
	"monitor": tool("monitor", "", obj([]string{"command", "description"}, props(
		"command", str(), "description", str(), "timeout_ms", number(), "persistent", boolean(),
	))),
	"search_tool": tool("search_tool", harness.BuiltinToolUseKindReadonly, obj([]string{"query"}, props("query", str(), "limit", number()))),
	"use_tool": tool("use_tool", "", obj([]string{"tool_name", "tool_input"}, props(
		"tool_name", str(), "tool_input", map[string]any{"type": "object"},
	))),
	"workflow": tool("workflow", "", obj(nil, props(
		"agent_budget", number(), "name", str(), "script", str(), "script_path", str(),
		"resume_from_run_id", str(), "validate_only", boolean(),
	))),
	"enter_plan_mode": tool("enter_plan_mode", "", obj(nil, map[string]any{})),
	"exit_plan_mode":  tool("exit_plan_mode", "", obj(nil, map[string]any{})),
	"image_gen":       tool("image_gen", "", obj([]string{"prompt"}, props("prompt", str(), "aspect_ratio", str()))),
	"image_edit": tool("image_edit", "", obj([]string{"prompt", "image"}, props(
		"prompt", str(), "image", arr(str()), "aspect_ratio", str(),
	))),
	"image_to_video": tool("image_to_video", "", obj([]string{"image"}, props(
		"prompt", str(), "image", str(), "duration", number(), "resolution_name", str(),
	))),
	"reference_to_video": tool("reference_to_video", "", obj([]string{"prompt", "images", "aspect_ratio"}, props(
		"prompt", str(), "images", arr(str()), "aspect_ratio", str(), "duration", number(), "resolution_name", str(),
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
