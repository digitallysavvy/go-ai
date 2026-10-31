package codemode

import "testing"

// Node writes the guarding `;` over an erased declaration only when the
// previous statement is not already terminated by one. Expected strings are
// Node 24.20 stripTypeScriptTypes output.
func TestStripNoSecondSemicolonAfterTerminatedStatement(t *testing.T) {
	for _, c := range [][2]string{
		{"declare function f(): A\n(1)", ";                      \n(1)"},
		{"let y = 1;\ndeclare function f(): A\n(1)", "let y = 1;\n                       \n(1)"},
		{"let y = 1\ndeclare function f(): A\n(1)", "let y = 1\n;                      \n(1)"},
		{"{}\ndeclare function f(): A\n(1)", "{}\n;                      \n(1)"},
		{"g();\ntype T = A\n(1)", "g();\n          \n(1)"},
		{"g()\ntype T = A\n(1)", "g()\n;         \n(1)"},
		{"let y = 1;\n\ndeclare function f(): A\n(1)", "let y = 1;\n\n                       \n(1)"},
		{"let y = 1;\ndeclare const f: A\n(1)", "let y = 1;\n                  \n(1)"},
		{"let y = 1;\ninterface I {}\n(1)", "let y = 1;\n              \n(1)"},
		{"let y = 1;\ntype T = A\n(1)", "let y = 1;\n          \n(1)"},
		{"let y = 1;\ntype T = A;\n(1)", "let y = 1;\n           \n(1)"},
		{"let y = 1;\ndeclare function f(): A;\n(1)", "let y = 1;\n                        \n(1)"},
		{"function g() {}\ndeclare function f(): A\n(1)", "function g() {}\n;                      \n(1)"},
		{"if (a) {}\ndeclare function f(): A\n(1)", "if (a) {}\n;                      \n(1)"},
		{"let y = 1;\ndeclare function f(): A\n[1]", "let y = 1;\n                       \n[1]"},
		{"let y = 1;\ndeclare function f(): A\n`x`", "let y = 1;\n                       \n`x`"},
		{"let y = 1;\ndeclare function f(): A\n+1", "let y = 1;\n                       \n+1"},
	} {
		got, err := stripTypeScriptAnnotations(c[0])
		if err != nil || got != c[1] {
			t.Errorf("%q\n got  %q (%v)\n want %q", c[0], got, err, c[1])
		}
	}
}
