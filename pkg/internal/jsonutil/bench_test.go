package jsonutil

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

func BenchmarkStreamingParser(b *testing.B) {
	const chunkSize = 8
	for _, sz := range []struct {
		name  string
		bytes int
	}{
		{"1k", 1024},
		{"8k", 8192},
	} {
		full := buildBenchJSON(sz.bytes)
		chunks := make([]string, 0, (len(full)+chunkSize-1)/chunkSize)
		for i := 0; i < len(full); i += chunkSize {
			end := min(i+chunkSize, len(full))
			chunks = append(chunks, full[i:end])
		}
		b.Run(sz.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(full)))
			for b.Loop() {
				p := NewStreamingParser()
				for _, ch := range chunks {
					p.Append(ch)
					p.TryParse()
				}
				if p.GetLastValid() == nil {
					b.Fatal("expected parsed object")
				}
			}
		})
	}
}
