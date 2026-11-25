package codemode

import (
	"context"
	"testing"
)

// BenchmarkRunCodeModeStartup measures one trivial code-mode run end to
// end: sandbox startup, hardening, the script and teardown.
func BenchmarkRunCodeModeStartup(b *testing.B) {
	ctx := context.Background()
	if _, err := RunCodeMode(ctx, RunInput{JS: "return 1"}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := RunCodeMode(ctx, RunInput{JS: "return 1"}); err != nil {
			b.Fatal(err)
		}
	}
}
