package posixpath

import (
	"reflect"
	"testing"
)

// Expected values were generated with Node v24 path.posix.
func TestNodeCompatibility(t *testing.T) {
	cases := []struct{ in, normalize, joinWork, dirname, basename string }{
		{"", ".", "/work", ".", ""},
		{".", ".", "/work", ".", "."},
		{"./", "./", "/work/", ".", "."},
		{"repo/", "repo/", "/work/repo/", ".", "repo"},
		{"repo/../ai-sdk", "ai-sdk", "/work/ai-sdk", "repo/..", "ai-sdk"},
		{"./ai-sdk", "ai-sdk", "/work/ai-sdk", ".", "ai-sdk"},
		{"../x", "../x", "/x", "..", "x"},
		{"a/../../b", "../b", "/b", "a/../..", "b"},
		{"/a/./b/../c/", "/a/c/", "/work/a/c/", "/a/./b/..", "c"},
		{"//a//b", "/a/b", "/work/a/b", "//a/", "b"},
		{"a/b/..", "a", "/work/a", "a/b", ".."},
		{"..", "..", "/", ".", ".."},
		{"../", "../", "/", ".", ".."},
		{"/..", "/", "/", "/", ".."},
	}
	for _, tc := range cases {
		if got := Normalize(tc.in); got != tc.normalize {
			t.Errorf("Normalize(%q) = %q, want %q", tc.in, got, tc.normalize)
		}
		if got := Join("/work", tc.in); got != tc.joinWork {
			t.Errorf("Join(/work, %q) = %q, want %q", tc.in, got, tc.joinWork)
		}
		if got := Dirname(tc.in); got != tc.dirname {
			t.Errorf("Dirname(%q) = %q, want %q", tc.in, got, tc.dirname)
		}
		if got := Basename(tc.in); got != tc.basename {
			t.Errorf("Basename(%q) = %q, want %q", tc.in, got, tc.basename)
		}
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve("/work", ".harness-bootstrap/demo"); got != "/work/.harness-bootstrap/demo" {
		t.Fatal(got)
	}
	if got := Resolve("/work", "/abs/x/"); got != "/abs/x" {
		t.Fatal(got)
	}
}

// Expected order generated with Node v24 `[...].sort((a, b) => a.localeCompare(b))`.
func TestLocaleCompareMatchesNode(t *testing.T) {
	words := []string{"b.txt", "B.txt", "a_b", "a-b", "a.b", "aB", "ab", "Ab", ".hidden", "_x", "-x", "1a", "a1", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "bridge.mjs", "Zeta", "zeta", "é", "e", "f"}
	want := []string{"_x", "-x", ".hidden", "1a", "a_b", "a-b", "a.b", "a1", "ab", "aB", "Ab", "b.txt", "B.txt", "bridge.mjs", "e", "é", "f", "package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "zeta", "Zeta"}
	SortStrings(words)
	if !reflect.DeepEqual(words, want) {
		t.Fatalf("got %q\nwant %q", words, want)
	}
}

func TestIsWin32Abs(t *testing.T) {
	for in, want := range map[string]bool{"C:\\skills": true, "/x": true, "\\x": true, "skills": false, "C:": false} {
		if got := IsWin32Abs(in); got != want {
			t.Errorf("IsWin32Abs(%q) = %v", in, got)
		}
	}
}
