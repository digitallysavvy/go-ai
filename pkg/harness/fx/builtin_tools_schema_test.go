package fx

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// The built-in tool schemas carry the zod constraints of TS FX_BUILTIN_TOOLS
// (enums, integer bounds, lengths, nullish fields, nested subagent tree).
func TestBuiltinToolSchemasMatchZodStrictness(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	cases := []struct {
		name  string
		tool  string
		input map[string]any
		ok    bool
	}{
		{"glob mode enum", "glob", map[string]any{"pattern": "*", "mode": "count"}, true},
		{"glob bad mode", "glob", map[string]any{"pattern": "*", "mode": "nope"}, false},
		{"grep head_limit zero", "grep", map[string]any{"pattern": "x", "head_limit": 0}, false},
		{"grep offset zero", "grep", map[string]any{"pattern": "x", "offset": 0}, true},
		{"grep fractional", "grep", map[string]any{"pattern": "x", "context_lines": 1.5}, false},
		{"webSearch short query", "webSearch", map[string]any{"query": "a"}, false},
		{"read_file start_line zero", "read_file", map[string]any{"path": "p", "start_line": 0}, false},
		{"memory bad action", "memory", map[string]any{"action": "erase"}, false},
		{"shell nullish run", "shell", map[string]any{"action": "run", "command": "ls", "cwd": nil, "tty": nil, "timeout_ms": nil}, true},
		{"shell timeout zero", "shell", map[string]any{"action": "run", "command": "ls", "timeout_ms": 0}, false},
		{"shell tty wrong type", "shell", map[string]any{"action": "run", "command": "ls", "tty": "yes"}, false},
		{"mcp_features bad action", "mcp_features", map[string]any{"action": "x", "server": "s"}, false},
		{"mcp_features record", "mcp_features", map[string]any{"action": "prompt_get", "server": "s", "arguments": map[string]any{"a": 1}}, false},
		{"ask one option", "ask_user_question", map[string]any{"questions": []any{map[string]any{"question": "q", "options": []any{map[string]any{"label": "a"}}}}}, false},
		{"ask ok", "ask_user_question", map[string]any{"questions": []any{map[string]any{"question": "q", "options": []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}}}}}, true},
		{"ask request id length", "ask_user_question", map[string]any{"permission_request_id": "short", "questions": []any{map[string]any{"question": "q", "options": []any{map[string]any{"label": "a"}, map[string]any{"label": "b"}}}}}, false},
		{"vision empty focus", "vision", map[string]any{"focus": ""}, false},
		{"vision empty paths", "vision", map[string]any{"focus": "f", "paths": []any{}}, false},
		{"subagent create ok", "subagent", map[string]any{"command": map[string]any{"create": map[string]any{"name": "n", "mode": "one_off"}}}, true},
		{"subagent bad mode", "subagent", map[string]any{"command": map[string]any{"create": map[string]any{"name": "n", "mode": "x"}}}, false},
		{"subagent long name", "subagent", map[string]any{"command": map[string]any{"create": map[string]any{"name": long(129), "mode": "persistent"}}}, false},
		{"subagent inspect no sections", "subagent", map[string]any{"command": map[string]any{"inspect": map[string]any{"id": "i", "sections": []any{}}}}, false},
		{"subagent wait timeout", "subagent", map[string]any{"command": map[string]any{"inspect": map[string]any{"id": "i", "sections": []any{"status"}, "wait": map[string]any{"until": "settled", "timeout_ms": 60001}}}}, false},
		{"subagent lifecycle", "subagent", map[string]any{"command": map[string]any{"lifecycle": map[string]any{"id": "i", "action": "reopen"}}}, true},
		{"subagent extra key allowed", "subagent", map[string]any{"command": map[string]any{}, "extra": 1}, true},
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
