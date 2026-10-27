package fx

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS glob and ls sort with Array.prototype.sort, which compares UTF-16 code
// units: an astral name (a surrogate pair, 0xD83D...) sorts before a name in
// U+E000..U+FFFF such as a fullwidth letter, unlike a UTF-8 byte order.
func TestR46GlobAndLsSortByUTF16CodeUnits(t *testing.T) {
	sb := newFakeSandbox()
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		switch {
		case strings.Contains(opts.Command, "realpath"):
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		case strings.Contains(opts.Command, "-type f"):
			return providerutils.SandboxRunResult{Stdout: "/workspace/Ａ.txt\x00/workspace/\U0001F600.txt\x00/workspace/b.txt\x00"}, nil
		}
		return providerutils.SandboxRunResult{Stdout: "Ａ\x00\U0001F600\x00b\x00"}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	ctx := context.Background()
	got, err := r.execute(ctx, "glob", json.RawMessage(`{"pattern":"*.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b.txt", "\U0001F600.txt", "Ａ.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("glob = %q, want %q", got, want)
	}
	got, err = r.execute(ctx, "ls", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b", "\U0001F600", "Ａ"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ls = %q, want %q", got, want)
	}
}

// JavaScript's sort puts a string before every longer string it prefixes,
// including across a surrogate pair.
func TestR46SortUTF16PrefixFirst(t *testing.T) {
	got := []string{"ab", "a", "😀x", "😀", "Ａ", ""}
	sortUTF16(got)
	want := []string{"", "a", "ab", "😀", "😀x", "Ａ"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortUTF16 = %q, want %q", got, want)
		}
	}
}
