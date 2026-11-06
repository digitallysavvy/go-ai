package grokbuild

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// The built-in tool schemas carry the zod enums, integer bounds and
// nullable fields of TS GROK_BUILD_BUILTIN_TOOLS.
func TestBuiltinToolSchemasMatchZodStrictness(t *testing.T) {
	cases := []struct {
		name  string
		tool  string
		input map[string]any
		ok    bool
	}{
		{"bash null timeout", "bash", map[string]any{"command": "ls", "timeout": nil}, true},
		{"bash timeout max", "bash", map[string]any{"command": "ls", "timeout": 36000000}, true},
		{"bash timeout over", "bash", map[string]any{"command": "ls", "timeout": 36000001}, false},
		{"bash timeout fractional", "bash", map[string]any{"command": "ls", "timeout": 1.5}, false},
		{"grep null path", "grep", map[string]any{"pattern": "x", "path": nil, "glob": nil, "type": nil}, true},
		{"grep fractional -A", "grep", map[string]any{"pattern": "x", "-A": 1.5}, false},
		{"grep null -A rejected", "grep", map[string]any{"pattern": "x", "-A": nil}, false},
		{"read_file fractional limit", "read_file", map[string]any{"target_file": "f", "limit": 0.5}, false},
		{"read_file null pages", "read_file", map[string]any{"target_file": "f", "pages": nil}, true},
		{"todo bad status", "todo_write", map[string]any{"todos": []any{map[string]any{"id": "1", "status": "done"}}}, false},
		{"todo null status", "todo_write", map[string]any{"todos": []any{map[string]any{"id": "1", "status": nil, "content": nil}}}, true},
		{"output negative timeout", "get_command_or_subagent_output", map[string]any{"timeout_ms": -1}, false},
		{"spawn bad capability", "spawn_subagent", map[string]any{"prompt": "p", "description": "d", "capability_mode": "rw"}, false},
		{"spawn bad isolation", "spawn_subagent", map[string]any{"prompt": "p", "description": "d", "isolation": "vm"}, false},
		{"spawn ok", "spawn_subagent", map[string]any{"prompt": "p", "description": "d", "capability_mode": "read-only", "isolation": nil}, true},
		{"search limit over", "search_tool", map[string]any{"query": "q", "limit": 256}, false},
		{"search limit null", "search_tool", map[string]any{"query": "q", "limit": nil}, true},
		{"workflow budget zero", "workflow", map[string]any{"agent_budget": 0}, false},
		{"workflow budget over", "workflow", map[string]any{"agent_budget": 1025}, false},
		{"workflow args any", "workflow", map[string]any{"args": []any{1, "a"}, "name": nil}, true},
		{"use_tool input not object", "use_tool", map[string]any{"tool_name": "t", "tool_input": "x"}, false},
		{"image_to_video fractional duration", "image_to_video", map[string]any{"image": "i", "duration": 1.5}, false},
		{"image_to_video null", "image_to_video", map[string]any{"image": "i", "prompt": nil, "duration": nil}, true},
		{"scheduler nulls", "scheduler_create", map[string]any{"task_id": nil, "durable": nil, "foreground": nil}, true},
		{"scheduler fire_immediately null rejected", "scheduler_create", map[string]any{"fire_immediately": nil}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bt, ok := BuiltinTools[c.tool]
			if !ok {
				t.Fatalf("no builtin tool %q", c.tool)
			}
			err := schema.NewSimpleJSONSchema(bt.Tool.Parameters.(map[string]any)).Validator().Validate(c.input)
			if (err == nil) != c.ok {
				t.Fatalf("validate(%v) err = %v, want ok=%v", c.input, err, c.ok)
			}
		})
	}
}
