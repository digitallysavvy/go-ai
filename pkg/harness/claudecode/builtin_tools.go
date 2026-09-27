package claudecode

import (
	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// JSON Schema construction helpers, kept terse because this file mostly
// transcribes the TS `z.object({...})` builtin tool schemas
// (claude-code-harness.ts CLAUDE_CODE_BUILTIN_TOOLS) into JSON Schema 7.

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str() map[string]any   { return map[string]any{"type": "string"} }
func num() map[string]any   { return map[string]any{"type": "number"} }
func boolT() map[string]any { return map[string]any{"type": "boolean"} }
func arr(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
func enumT(values ...string) map[string]any {
	anyValues := make([]any, len(values))
	for i, v := range values {
		anyValues[i] = v
	}
	return map[string]any{"type": "string", "enum": anyValues}
}
func emptyObj() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// plainTool declares a non-common builtin tool (TS bare `tool({...})`).
func plainTool(name, description string, kind harness.BuiltinToolUseKind, schema map[string]any) harness.BuiltinTool {
	return harness.BuiltinTool{
		Tool:        types.Tool{Name: name, Description: description, Parameters: schema},
		ToolUseKind: kind,
	}
}

var listMcpResourcesInputSchema = obj(map[string]any{"server": str()})
var readMcpResourceInputSchema = obj(map[string]any{"server": str(), "uri": str()}, "server", "uri")

// builtinTools mirrors TS `CLAUDE_CODE_BUILTIN_TOOLS`, keyed by what the
// bridge emits as `toolName` on the wire (commonName ?? nativeName).
func builtinTools() map[string]harness.BuiltinTool {
	tools := map[string]harness.BuiltinTool{
		"read": harness.CommonTool(harness.BuiltinToolRead, harness.CommonToolOptions{
			NativeName: "Read", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Read file contents (text, image, PDF, notebook)",
			InputSchema: obj(map[string]any{
				"file_path": str(), "offset": num(), "limit": num(), "pages": str(),
			}, "file_path"),
		}),
		"write": harness.CommonTool(harness.BuiltinToolWrite, harness.CommonToolOptions{
			NativeName: "Write", ToolUseKind: harness.BuiltinToolUseKindEdit,
			Description: "Overwrite or create a file at an absolute path",
			InputSchema: obj(map[string]any{"file_path": str(), "content": str()}, "file_path", "content"),
		}),
		"edit": harness.CommonTool(harness.BuiltinToolEdit, harness.CommonToolOptions{
			NativeName: "Edit", ToolUseKind: harness.BuiltinToolUseKindEdit,
			Description: "Edit a file by exact string replacement",
			InputSchema: obj(map[string]any{
				"file_path": str(), "old_string": str(), "new_string": str(), "replace_all": boolT(),
			}, "file_path", "old_string", "new_string"),
		}),
		"bash": harness.CommonTool(harness.BuiltinToolBash, harness.CommonToolOptions{
			NativeName: "Bash", ToolUseKind: harness.BuiltinToolUseKindBash,
			Description: "Execute a shell command, optionally in background",
			InputSchema: obj(map[string]any{
				"command": str(), "timeout": num(), "description": str(),
				"run_in_background": boolT(), "dangerouslyDisableSandbox": boolT(),
			}, "command"),
		}),
		"glob": harness.CommonTool(harness.BuiltinToolGlob, harness.CommonToolOptions{
			NativeName: "Glob", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Fast file-pattern search using glob syntax",
			InputSchema: obj(map[string]any{"pattern": str(), "path": str()}, "pattern"),
		}),
		"grep": harness.CommonTool(harness.BuiltinToolGrep, harness.CommonToolOptions{
			NativeName: "Grep", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Regex search over file contents via ripgrep",
			InputSchema: obj(map[string]any{
				"pattern": str(), "path": str(), "glob": str(),
				"output_mode": enumT("content", "files_with_matches", "count"),
				"-B":          num(), "-A": num(), "-C": num(), "context": num(),
				"-n": boolT(), "-i": boolT(), "-o": boolT(), "type": str(),
				"head_limit": num(), "offset": num(), "multiline": boolT(),
			}, "pattern"),
		}),
		"webSearch": harness.CommonTool(harness.BuiltinToolWebSearch, harness.CommonToolOptions{
			NativeName: "WebSearch", ToolUseKind: harness.BuiltinToolUseKindReadonly,
			Description: "Issue web search queries with optional domain filters",
			InputSchema: obj(map[string]any{
				"query": str(), "allowed_domains": arr(str()), "blocked_domains": arr(str()),
			}, "query"),
		}),
	}

	standardQuestions := harness.StandardBuiltinTools()[harness.BuiltinToolAskUserQuestions]
	tools["askUserQuestions"] = harness.BuiltinTool{
		Tool:        standardQuestions,
		NativeName:  "AskUserQuestion",
		CommonName:  harness.BuiltinToolAskUserQuestions,
		ToolUseKind: harness.BuiltinToolUseKindReadonly,
	}

	tools["WebFetch"] = plainTool("WebFetch", "Fetch a URL and run a prompt against its content", "",
		obj(map[string]any{"url": str(), "prompt": str()}, "url", "prompt"))
	tools["NotebookEdit"] = plainTool("NotebookEdit", "Edit, insert, or delete a Jupyter notebook cell", "",
		obj(map[string]any{
			"notebook_path": str(), "new_source": str(), "cell_id": str(),
			"cell_type": enumT("code", "markdown"), "edit_mode": enumT("replace", "insert", "delete"),
		}, "notebook_path", "new_source"))
	tools["TodoWrite"] = plainTool("TodoWrite", "Replace the session todo list", "",
		obj(map[string]any{
			"todos": arr(obj(map[string]any{
				"content": str(), "status": enumT("pending", "in_progress", "completed"), "activeForm": str(),
			}, "content", "status", "activeForm")),
		}, "todos"))
	tools["Agent"] = plainTool("Agent", "Spawn a subagent with a task", "",
		obj(map[string]any{
			"description": str(), "prompt": str(), "subagent_type": str(),
			"model": enumT("sonnet", "opus", "haiku"), "run_in_background": boolT(),
			"name": str(), "team_name": str(),
			"mode":      enumT("acceptEdits", "auto", "bypassPermissions", "default", "dontAsk", "plan"),
			"isolation": enumT("worktree"),
		}, "description", "prompt"))
	tools["TaskCreate"] = plainTool("TaskCreate", "Create a task in the session-local task list", "",
		obj(map[string]any{
			"subject": str(), "description": str(), "activeForm": str(),
			"metadata": map[string]any{"type": "object"},
		}, "subject", "description"))
	tools["TaskGet"] = plainTool("TaskGet", "Retrieve a task by id", "",
		obj(map[string]any{"taskId": str()}, "taskId"))
	tools["TaskUpdate"] = plainTool("TaskUpdate", "Update fields of an existing task", "",
		obj(map[string]any{
			"taskId": str(), "subject": str(), "description": str(), "activeForm": str(),
			"status":       enumT("pending", "in_progress", "completed", "deleted"),
			"addBlocks":    arr(str()),
			"addBlockedBy": arr(str()),
			"owner":        str(),
			"metadata":     map[string]any{"type": "object"},
		}, "taskId"))
	tools["TaskList"] = plainTool("TaskList", "Return all tasks in the session-local task list", "", emptyObj())
	tools["TaskStop"] = plainTool("TaskStop", "Stop a running background task by id", "",
		obj(map[string]any{"task_id": str(), "shell_id": str()}))
	tools["TaskOutput"] = plainTool("TaskOutput", "Poll for output from a background task", "",
		obj(map[string]any{"task_id": str(), "block": boolT(), "timeout": num()}, "task_id", "block", "timeout"))
	tools["Monitor"] = plainTool("Monitor", "Run and monitor a shell command or WebSocket", harness.BuiltinToolUseKindBash,
		obj(map[string]any{
			"description": str(), "timeout_ms": num(), "persistent": boolT(), "command": str(),
			"ws": obj(map[string]any{"url": str(), "protocols": arr(str())}, "url"),
		}))
	tools["ListMcpResources"] = plainTool("ListMcpResources", "List resources available from MCP servers", harness.BuiltinToolUseKindReadonly, listMcpResourcesInputSchema)
	tools["ListMcpResourcesTool"] = plainTool("ListMcpResourcesTool", "List resources available from MCP servers", harness.BuiltinToolUseKindReadonly, listMcpResourcesInputSchema)
	tools["ReadMcpResource"] = plainTool("ReadMcpResource", "Read a specific MCP resource by URI", harness.BuiltinToolUseKindReadonly, readMcpResourceInputSchema)
	tools["ReadMcpResourceTool"] = plainTool("ReadMcpResourceTool", "Read a specific MCP resource by URI", harness.BuiltinToolUseKindReadonly, readMcpResourceInputSchema)
	tools["ReadMcpResourceDirTool"] = plainTool("ReadMcpResourceDirTool", "List direct children of an MCP directory resource", harness.BuiltinToolUseKindReadonly, readMcpResourceInputSchema)
	tools["RefreshMcpTools"] = plainTool("RefreshMcpTools", "Refresh tools from one or all connected MCP servers", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{"server": str()}))
	tools["ExitPlanMode"] = plainTool("ExitPlanMode", "Exit plan mode with optional permission approvals", "",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"allowedPrompts": arr(obj(map[string]any{"tool": enumT("Bash"), "prompt": str()}, "tool", "prompt")),
			},
			"additionalProperties": true,
		})
	tools["EnterPlanMode"] = plainTool("EnterPlanMode", "Enter plan mode", harness.BuiltinToolUseKindReadonly, emptyObj())
	tools["EnterWorktree"] = plainTool("EnterWorktree", "Create or enter an isolated git worktree", "",
		obj(map[string]any{"name": str(), "path": str()}))
	tools["ExitWorktree"] = plainTool("ExitWorktree", "Exit the current worktree session", "",
		obj(map[string]any{"action": enumT("keep", "remove"), "discard_changes": boolT()}, "action"))
	tools["Skill"] = plainTool("Skill", "Activate a skill by name", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{"skill": str(), "args": str()}, "skill"))
	tools["ToolSearch"] = plainTool("ToolSearch", "Search deferred MCP / catalog tools so the model can load them on demand", "",
		obj(map[string]any{"query": str(), "max_results": num()}, "query"))
	tools["Artifact"] = plainTool("Artifact", "Publish or list claude.ai artifacts", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{
			"action": enumT("publish", "list"), "file_path": str(), "favicon": str(), "limit": num(),
			"scope": enumT("mine", "shared", "all"), "title": str(), "description": str(),
			"label": str(), "url": str(), "force": boolT(),
		}))
	tools["CronCreate"] = plainTool("CronCreate", "Schedule a recurring or one-shot prompt", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"cron": str(), "prompt": str(), "recurring": boolT(), "durable": boolT()}, "cron", "prompt"))
	tools["CronDelete"] = plainTool("CronDelete", "Delete a scheduled prompt by id", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"id": str()}, "id"))
	tools["CronList"] = plainTool("CronList", "List scheduled prompts for the current session", harness.BuiltinToolUseKindReadonly, emptyObj())
	tools["LSP"] = plainTool("LSP", "Query a language server for code intelligence", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{
			"operation": enumT("goToDefinition", "findReferences", "hover", "documentSymbol",
				"workspaceSymbol", "goToImplementation", "prepareCallHierarchy", "incomingCalls", "outgoingCalls"),
			"filePath": str(), "line": num(), "character": num(), "query": str(),
		}, "operation", "filePath", "line", "character"))
	tools["PowerShell"] = plainTool("PowerShell", "Execute a PowerShell command, optionally in background", harness.BuiltinToolUseKindBash,
		obj(map[string]any{
			"command": str(), "timeout": num(), "description": str(),
			"run_in_background": boolT(), "dangerouslyDisableSandbox": boolT(),
		}, "command"))
	tools["PushNotification"] = plainTool("PushNotification", "Send a notification for proactive or scheduled work", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"message": str(), "status": enumT("proactive")}, "message", "status"))
	tools["RemoteTrigger"] = plainTool("RemoteTrigger", "List, manage, or run a claude.ai Routine trigger", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{
			"action": enumT("list", "get", "create", "update", "run"), "trigger_id": str(),
			"body": map[string]any{"type": "object"},
		}, "action"))
	tools["ReportFindings"] = plainTool("ReportFindings", "Return verified code-review findings", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{
			"level": enumT("low", "medium", "high", "xhigh", "max"),
			"findings": arr(obj(map[string]any{
				"file": str(), "line": num(), "summary": str(), "short_summary": str(),
				"failure_scenario": str(), "category": str(),
				"verdict": enumT("CONFIRMED", "PLAUSIBLE"),
				"outcome": enumT("fixed", "skipped", "no_change_needed"),
			}, "file", "summary", "failure_scenario")),
		}, "findings"))
	tools["ScheduleWakeup"] = plainTool("ScheduleWakeup", "Schedule or stop the next iteration of a dynamic loop", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"delaySeconds": num(), "reason": str(), "prompt": str(), "stop": boolT()}))
	tools["SendMessage"] = plainTool("SendMessage", "Send a plain-text or protocol message to another agent", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"to": str(), "summary": str(), "message": str()}, "to", "message"))
	tools["SendUserFile"] = plainTool("SendUserFile", "Send one or more files to the user", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{
			"files": map[string]any{"anyOf": []any{str(), arr(str())}}, "caption": str(),
			"status": enumT("normal", "proactive"), "display": enumT("render", "attach"),
		}, "files", "status"))
	tools["ShareOnboardingGuide"] = plainTool("ShareOnboardingGuide", "Create, update, inspect, or delete an onboarding guide", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{"mode": enumT("check", "update", "create", "delete"), "short_code": str()}))
	tools["WaitForMcpServers"] = plainTool("WaitForMcpServers", "Wait for MCP servers that are still connecting", harness.BuiltinToolUseKindReadonly,
		obj(map[string]any{"servers": arr(str())}))
	tools["Workflow"] = plainTool("Workflow", "Run or resume a dynamic multi-agent workflow", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{
			"script": str(), "name": str(), "description": str(), "title": str(),
			"args": map[string]any{"type": "object"}, "scriptPath": str(), "resumeFromRunId": str(),
		}))
	tools["DesignSync"] = plainTool("DesignSync", "Read or update claude.ai Design projects", harness.BuiltinToolUseKindEdit,
		obj(map[string]any{
			"method": enumT("list_projects", "get_project", "list_files", "get_file", "finalize_plan",
				"write_files", "delete_files", "register_assets", "unregister_assets", "create_project", "report_validate"),
			"projectId": str(), "path": str(),
			"writes":  arr(str()),
			"deletes": arr(str()),
			"planId":  str(),
			"files": arr(obj(map[string]any{
				"path": str(), "localPath": str(), "data": str(), "encoding": enumT("base64"), "mimeType": str(),
			}, "path")),
			"paths": arr(str()),
			"name":  str(),
			"assets": arr(obj(map[string]any{
				"name": str(), "path": str(), "subtitle": str(),
				"viewport": obj(map[string]any{"width": num(), "height": num()}, "width"),
				"group":    str(),
			}, "name", "path")),
			"localDir": str(),
			"counts": obj(map[string]any{
				"total": num(), "bad": num(), "thin": num(), "variantsIdentical": num(), "iterations": num(),
			}, "total", "bad", "thin", "variantsIdentical", "iterations"),
		}, "method"))

	return tools
}
