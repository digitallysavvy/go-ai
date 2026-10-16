package codemode

import "testing"

// Cases checked against Node 24 stripTypeScriptTypes (the TS code-mode runner).
func TestStripInstantiationConditionalAndSlash(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"instantiation semicolon", "const f = id<number>; f(1)", "const f = id; f(1)"},
		{"instantiation newline", "const a = id<string>\nconst b = 2", "const a = id\nconst b = 2"},
		{"instantiation argument", "const a = f(id<string>)", "const a = f(id)"},
		{"instantiation member", "const a = o.m<string>;", "const a = o.m;"},
		{"instantiation then or", "const a = id<string> || 1;", "const a = id || 1;"},
		{"comparison chain kept", "const a = x < y > z;", "const a = x < y > z;"},
		{"optional call kept", "const x = a?.b<string>?.();", "const x = a?.b<string>?.();"},
		{"conditional arrow lookalike", "const r = c ? (x) : y => z", "const r = c ? (x) : y => z"},
		{"conditional arrow lookalike block", "const r = c ? (x) : y => { return z }", "const r = c ? (x) : y => { return z }"},
		{"conditional typed arrow", "const r = c ? (x): number => 1 : 2", "const r = c ? (x) => 1 : 2"},
		{"conditional typed arrow block", "const r = c ? (x): number => { return 1 } : 2", "const r = c ? (x) => { return 1 } : 2"},
		{"slash after object literal", "const s = {}/2; const t: string = \"/\"", "const s = {}/2; const t = \"/\""},
		{"slash after type arguments", "const s = x<y>/2; const t: string = \"/\"", "const s = x/2; const t = \"/\""},
		{"regex after block", "function f(){}\n/a'/.test(s); let n: number = 1", "function f(){}\n/a'/.test(s); let n = 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strip(t, tc.src); got != tc.want {
				t.Fatalf("strip(%q) = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}
