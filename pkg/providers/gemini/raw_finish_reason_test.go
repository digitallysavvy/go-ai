package gemini

import (
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestConvertResponse_RawFinishReason verifies convertResponse surfaces the
// raw candidate.finishReason string, mirroring TS google-language-model.ts's
// `rawFinishReason = candidate?.finishReason ?? confirmedPromptBlockReason`.
func TestConvertResponse_RawFinishReason(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")

	resp := Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "The answer is 42."}}},
			FinishReason: "MAX_TOKENS",
		}},
	}

	result := m.convertResponse(resp, nil)
	if result.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", result.FinishReason)
	}
	if result.RawFinishReason != "MAX_TOKENS" {
		t.Fatalf("RawFinishReason = %q, want %q", result.RawFinishReason, "MAX_TOKENS")
	}
}

// TestStream_RawFinishReason verifies the stream's terminal finish chunk
// carries the raw candidate.finishReason string.
func TestStream_RawFinishReason(t *testing.T) {
	event := mustMarshal(Response{
		Candidates: []Candidate{{
			Content: struct {
				Parts []Part `json:"parts"`
				Role  string `json:"role"`
			}{Parts: []Part{{Text: "last word"}}},
			FinishReason: "MAX_TOKENS",
		}},
	})

	s := newTestStream(sseStream(event))
	defer s.Close() //nolint:errcheck

	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk observed")
	}
	if finish.FinishReason != types.FinishReasonLength {
		t.Fatalf("FinishReason = %v, want length", finish.FinishReason)
	}
	if finish.RawFinishReason != "MAX_TOKENS" {
		t.Fatalf("RawFinishReason = %q, want %q", finish.RawFinishReason, "MAX_TOKENS")
	}
}
