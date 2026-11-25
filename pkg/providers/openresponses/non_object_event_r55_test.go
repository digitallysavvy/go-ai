package openresponses

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func drainOpenResponsesTypes(t *testing.T, sse string, raw bool) ([]provider.ChunkType, *provider.StreamChunk, error) {
	t.Helper()
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(sse)), nil, "open-responses")
	s.includeRawChunks = raw
	defer s.Close() //nolint:errcheck
	var got []provider.ChunkType
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return got, finish, nil
			}
			return got, finish, err
		}
		got = append(got, c.Type)
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
}

// TS validates chunks with z.any(): JSON that is not an object passes and
// matches no event, so it is neither an error part nor an error finish reason.
func TestStreamNonObjectJSONPayloadIsIgnored(t *testing.T) {
	for _, payload := range []string{`5`, `"x"`, `[]`, `true`} {
		sse := "data: " + payload + "\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
		got, finish, err := drainOpenResponsesTypes(t, sse, true)
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		want := []provider.ChunkType{provider.ChunkTypeRaw, provider.ChunkTypeRaw, provider.ChunkTypeFinish}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Fatalf("%s: chunk types = %v, want %v", payload, got, want)
		}
		if finish.FinishReason != "stop" {
			t.Fatalf("%s: finish reason = %q, want stop", payload, finish.FinishReason)
		}
	}
}

// A null payload passes z.any() but `chunk.type` throws, which errors the
// stream (no finish part).
func TestStreamNullPayloadErrorsTheStream(t *testing.T) {
	got, finish, err := drainOpenResponsesTypes(t, "data: null\n\ndata: {\"type\":\"response.completed\",\"response\":{}}\n\n", false)
	if err == nil || !strings.Contains(err.Error(), "Cannot read properties of null") {
		t.Fatalf("err = %v, chunks %v, want the TypeError", err, got)
	}
	if finish != nil {
		t.Fatalf("unexpected finish %v", finish)
	}
}
