package cohere

import (
	"io"
	"strings"
	"testing"
)

// TS cohere doStream enqueues a reasoning-delta for every content-delta that
// has a `thinking` key, including an empty string.
func TestStreamEmptyReasoningDeltaEmitted(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"content-start","index":0,"delta":{"message":{"content":{"type":"thinking","thinking":""}}}}`,
		`data: {"type":"content-delta","index":0,"delta":{"message":{"content":{"thinking":""}}}}`,
		`data: {"type":"content-end","index":0}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	s := newCohereV2Stream(io.NopCloser(strings.NewReader(body)))
	var got []string
	for {
		chunk, err := s.Next()
		if err != nil {
			break
		}
		got = append(got, string(chunk.Type))
	}
	if g := strings.Join(got, ","); g != "reasoning-start,reasoning,reasoning-end,finish" {
		t.Fatalf("chunks = %s", g)
	}
}
