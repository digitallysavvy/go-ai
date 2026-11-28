package jsonparser

import (
	"fmt"
	"strings"
	"testing"
)

func buildBenchJSON(targetBytes int) string {
	var b strings.Builder
	b.WriteString(`{"title":"Streaming Benchmark","items":[`)
	i := 0
	for b.Len() < targetBytes {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"id":%d,"name":"item-%d","active":true,"score":%d.5,"desc":"sample payload text for realistic streaming JSON parsing"}`, i, i, i)
		i++
	}
	b.WriteString(`]}`)
	return b.String()
}

func BenchmarkFixJSONSizes(b *testing.B) {
	for _, sz := range []struct {
		name  string
		bytes int
	}{
		{"1k", 1024},
		{"8k", 8192},
		{"32k", 32768},
	} {
		full := buildBenchJSON(sz.bytes)
		partial := full[:len(full)*3/4]
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(partial)))
			for b.Loop() {
				out := FixJSON(partial)
				if out == "" {
					b.Fatal("empty FixJSON output")
				}
			}
		})
	}
}

func BenchmarkParsePartialJSONSizes(b *testing.B) {
	for _, sz := range []struct {
		name  string
		bytes int
	}{
		{"1k", 1024},
		{"8k", 8192},
		{"32k", 32768},
	} {
		full := buildBenchJSON(sz.bytes)
		partial := full[:len(full)*3/4]
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(partial)))
			for b.Loop() {
				res := ParsePartialJSON(partial)
				if res.State != ParseStateRepaired || res.Value == nil {
					b.Fatalf("unexpected state: %v err=%v", res.State, res.Error)
				}
			}
		})
	}
}
