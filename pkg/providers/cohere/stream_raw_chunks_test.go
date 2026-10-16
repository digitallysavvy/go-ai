package cohere

import (
	"io"
	"strings"
	"testing"
)

// TS cohere doStream enqueues {type:'raw', rawValue} before processing every
// parse result when includeRawChunks is set ([DONE] is not an event).
func TestStreamIncludeRawChunks(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"text","text":""}}}}`,
		`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"text":"hi"}}}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
	s.includeRawChunks = true
	var got []string
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		got = append(got, string(chunk.Type))
	}
	want := []string{"raw", "raw", "text", "finish"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("chunks = %v, want %v", got, want)
	}
}
