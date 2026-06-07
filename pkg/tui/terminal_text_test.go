package tui

import (
	"reflect"
	"testing"
)

func TestVisibleLengthAndWrapMatchTypeScriptLayout(t *testing.T) {
	if got := wrapVisibleLine("hello from the terminal", 10); !reflect.DeepEqual(got, []string{"hello from", "the", "terminal"}) {
		t.Fatalf("word wrap = %#v", got)
	}
	if got := wrapVisibleLine("hello 世界", 8); !reflect.DeepEqual(got, []string{"hello", "世界"}) {
		t.Fatalf("wide wrap = %#v", got)
	}
	if got := visibleLength("e\u0301"); got != 1 {
		t.Fatalf("combining visible length = %d, want 1", got)
	}
	if got := wrapVisibleLine("e\u0301clair", 6); !reflect.DeepEqual(got, []string{"e\u0301clair"}) {
		t.Fatalf("combining wrap = %#v", got)
	}
	if got := stripANSI(sliceVisible("\x1b[92mhello from the terminal\x1b[0m", 10)); got != "hello from" {
		t.Fatalf("ansi slice = %q", got)
	}
	if got := sliceVisible("\x1b[92m"+stringOf('g', 20)+"\x1b[0m", 20); got != "\x1b[92m"+stringOf('g', 20)+"\x1b[0m" {
		t.Fatalf("ansi reset not preserved: %q", got)
	}
}

func stringOf(r rune, n int) string {
	out := make([]rune, n)
	for i := range out {
		out[i] = r
	}
	return string(out)
}
