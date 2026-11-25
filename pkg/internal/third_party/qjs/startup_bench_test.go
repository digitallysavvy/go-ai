package qjs

import (
	"context"
	"io"
	"testing"
)

// BenchmarkNewClose measures the per-run startup cost code mode pays:
// one qjs.New with code mode's options, then Close.
func BenchmarkNewClose(b *testing.B) {
	opt := Option{
		Context:            context.Background(),
		CloseOnContextDone: true,
		NoFSMount:          true,
		DisableModules:     true,
		Stdout:             io.Discard,
		Stderr:             io.Discard,
	}
	rt, err := New(opt)
	if err != nil {
		b.Fatal(err)
	}
	rt.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt, err := New(opt)
		if err != nil {
			b.Fatal(err)
		}
		rt.Close()
	}
}
