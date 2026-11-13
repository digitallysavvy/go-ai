package responses

import "testing"

// TS validateTypes returns the zod parse, which keeps only the schema's
// exact keys; v0.5.2 read the args by exact map keys too. A key that differs
// only in case (here the optional `user`) is not the field (review round 95,
// guard extension to interface{} decode helpers: decodeStrict).
func TestR95integDecodeStrictKeysMatchExactly(t *testing.T) {
	a, err := localShellActionFromArgs(map[string]interface{}{
		"action": map[string]interface{}{"type": "exec", "command": []interface{}{"ls"}, "env": map[string]interface{}{}, "User": "root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.User != nil || len(a.Command) != 1 {
		t.Fatalf("action = %+v, want no user (only the exact key fills it)", a)
	}
}
