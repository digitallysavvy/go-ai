package githubcopilot

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BuiltinTools reflects the stable, non-MCP tool surface emitted by GitHub
// Copilot CLI >=1.0.82. Mirrors TS `GITHUB_COPILOT_BUILTIN_TOOLS`.
var BuiltinTools = map[string]harness.BuiltinTool{
	"bash": harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
		NativeName:  "bash",
		ToolUseKind: harness.BuiltinToolUseKindBash,
		InputSchema: obj([]string{"command"}, props(
			"command", str(), "description", str(), "shellId", str(),
			"mode", str(), "detach", boolean(), "initial_wait", number(),
		)),
	}),
	"read_bash": titled(tool("read_bash", harness.BuiltinToolUseKindReadonly,
		obj([]string{"shellId", "delay"}, props("shellId", str(), "delay", number()))), "Reading shell output"),
	"stop_bash": titled(tool("stop_bash", harness.BuiltinToolUseKindBash,
		obj([]string{"shellId"}, props("shellId", str()))), "Stopping shell session"),
	"list_bash": titled(tool("list_bash", harness.BuiltinToolUseKindReadonly, obj(nil, map[string]any{})), "list_bash"),
	"view": titled(tool("view", harness.BuiltinToolUseKindReadonly, obj([]string{"path"}, props(
		"path", str(), "view_range", arr(number()), "forceReadLargeFiles", boolean(),
	))), "Viewing "),
	"create": titled(tool("create", harness.BuiltinToolUseKindEdit, obj([]string{"path", "file_text"}, props(
		"path", str(), "file_text", str(),
	))), "Creating "),
	"edit": titled(tool("edit", harness.BuiltinToolUseKindEdit, obj([]string{"path", "new_str"}, props(
		"path", str(), "old_str", str(), "new_str", str(),
	))), "Editing "),
	"web_fetch": titled(tool("web_fetch", harness.BuiltinToolUseKindReadonly, obj([]string{"url"}, props(
		"url", str(), "max_length", number(), "start_index", number(), "raw", boolean(),
	))), "Fetching "),
	"skill": titled(tool("skill", "", obj([]string{"skill"}, props("skill", str()))), "Using skill: "),
	"sql": tool("sql", "", obj([]string{"description", "query"}, props(
		"description", str(), "query", str(), "database", str(),
	))),
	"read_agent": titled(tool("read_agent", harness.BuiltinToolUseKindReadonly, obj([]string{"agent_id"}, props(
		"agent_id", str(), "wait", boolean(), "timeout", number(), "since_turn", number(),
	))), "read_agent"),
	"list_agents": titled(tool("list_agents", harness.BuiltinToolUseKindReadonly, obj(nil, props(
		"include_completed", boolean(), "scope", str(),
	))), "list_agents"),
	"write_agent": titled(tool("write_agent", "", obj([]string{"message"}, props(
		"message", str(), "agent_id", str(), "agent_ids", arr(str()), "scope", str(),
	))), "write_agent"),
	"grep": titled(harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
		NativeName:  "grep",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props(
			"pattern", str(), "path", str(), "paths", arr(str()), "glob", str(),
			"output_mode", str(), "case_insensitive", boolean(), "multiline", boolean(),
			"head_limit", number(), "context", number(), "before_context", number(), "after_context", number(),
		)),
	}), "Searching for "),
	"glob": titled(harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
		NativeName:  "glob",
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
		InputSchema: obj([]string{"pattern"}, props("pattern", str(), "paths", arr(str()))),
	}), "Finding files matching "),
	"task": tool("task", "", obj([]string{"name", "prompt", "agent_type", "description"}, props(
		"name", str(), "prompt", str(), "agent_type", str(), "description", str(),
		"model", str(), "context_tier", str(), "mode", str(),
	))),
}

func titled(bt harness.BuiltinTool, title string) harness.BuiltinTool {
	bt.Title = title
	return bt
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
