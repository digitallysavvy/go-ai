package toolbash

import "testing"

// tools-03 (TS bash-tool.ts truncate: output.length / slice count UTF-16
// code units): a character outside the BMP counts two, and a pair the cut
// would split is dropped whole.
func TestR91toolsBashTruncationCountsUTF16(t *testing.T) {
	for _, tc := range []struct {
		max    int
		output string
		want   string
	}{
		{2, "😀😀", "😀\n[Output truncated: 2 characters omitted.]"},
		{3, "😀😀", "😀\n[Output truncated: 1 characters omitted.]"},
		{4, "😀😀", "😀😀"},
		{2, "ab😀", "ab\n[Output truncated: 2 characters omitted.]"},
	} {
		o := newBoundedOutput(tc.max)
		_, _ = o.Write([]byte(tc.output))
		if got := o.String(); got != tc.want {
			t.Errorf("max %d %q: got %q, want %q", tc.max, tc.output, got, tc.want)
		}
	}
}
