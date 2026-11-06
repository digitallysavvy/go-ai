package githubcopilot

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// The built-in tool schemas carry the zod enums and integer bounds of TS
// GITHUB_COPILOT_BUILTIN_TOOLS.
func TestBuiltinToolSchemasMatchZodStrictness(t *testing.T) {
	task := func(m map[string]any) map[string]any {
		in := map[string]any{"name": "n", "prompt": "p", "agent_type": "explore", "description": "d"}
		for k, v := range m {
			in[k] = v
		}
		return in
	}
	cases := []struct {
		name  string
		tool  string
		input map[string]any
		ok    bool
	}{
		{"bash mode ok", "bash", map[string]any{"command": "ls", "mode": "async"}, true},
		{"bash bad mode", "bash", map[string]any{"command": "ls", "mode": "fg"}, false},
		{"sql bad database", "sql", map[string]any{"description": "d", "query": "q", "database": "x"}, false},
		{"sql ok database", "sql", map[string]any{"description": "d", "query": "q", "database": "session_store"}, true},
		{"read_agent fractional since_turn", "read_agent", map[string]any{"agent_id": "a", "since_turn": 1.5}, false},
		{"read_agent negative since_turn", "read_agent", map[string]any{"agent_id": "a", "since_turn": -1}, false},
		{"read_agent zero since_turn", "read_agent", map[string]any{"agent_id": "a", "since_turn": 0}, true},
		{"list_agents bad scope", "list_agents", map[string]any{"scope": "parent"}, false},
		{"list_agents scope all", "list_agents", map[string]any{"scope": "all"}, true},
		{"write_agent scope all rejected", "write_agent", map[string]any{"message": "m", "scope": "all"}, false},
		{"grep bad output_mode", "grep", map[string]any{"pattern": "x", "output_mode": "lines"}, false},
		{"task ok", "task", task(map[string]any{"mode": "background", "context_tier": "long_context"}), true},
		{"task bad agent_type", "task", task(map[string]any{"agent_type": "other"}), false},
		{"task bad mode", "task", task(map[string]any{"mode": "async"}), false},
		{"task bad context_tier", "task", task(map[string]any{"context_tier": "short"}), false},
		{"task extra key allowed", "task", task(map[string]any{"extra": 1}), true},
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
