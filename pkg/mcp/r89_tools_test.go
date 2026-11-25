package mcp

import "testing"

// TS mcp-client.ts onResponse looks up the handler by Number(response.id),
// so a numeric string (or boolean) id answers the matching numeric request.
func TestR89toolsPendingKeyNumberCoercion(t *testing.T) {
	for _, tc := range []struct {
		id   interface{}
		want interface{}
	}{
		{float64(3), uint64(3)},
		{"3", uint64(3)},
		{" 3 ", uint64(3)},
		{"3e0", uint64(3)},
		{"0x3", uint64(3)},
		{true, uint64(1)},
		{false, uint64(0)},
		{"abc", "abc"},
		{"1.5", "1.5"},
		{"-1", "-1"},
	} {
		if got := pendingKey(tc.id); got != tc.want {
			t.Errorf("pendingKey(%#v) = %#v, want %#v", tc.id, got, tc.want)
		}
	}
}
