package gemini

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func drain(t *testing.T, s *stream) []*provider.StreamChunk {
	t.Helper()
	var out []*provider.StreamChunk
	for {
		c, err := s.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
}

// TS emits {type:'raw', rawValue} for every SSE chunk (valid or not) ahead of
// the parts it produces when includeRawChunks is set.
func TestStreamIncludeRawChunks(t *testing.T) {
	payloads := []string{
		`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`,
		`not json`,
		`{"candidates":[{"content":{"parts":[{"text":"!"}]},"finishReason":"STOP"}]}`,
	}
	s := newTestStream(sseStream(payloads...))
	s.includeRawChunks = true
	chunks := drain(t, s)

	var raws []interface{}
	for i, c := range chunks {
		if c.Type == provider.ChunkTypeRaw {
			raws = append(raws, c.Raw)
			if i+1 < len(chunks) && chunks[i+1].Type == provider.ChunkTypeRaw {
				t.Fatalf("consecutive raw chunks: %v", chunkTypes(chunks))
			}
		}
	}
	if len(raws) != 3 {
		t.Fatalf("raw chunks = %d, want 3: %v", len(raws), chunkTypes(chunks))
	}
	if _, ok := raws[0].(map[string]interface{}); !ok {
		t.Fatalf("raw[0] = %#v, want parsed object", raws[0])
	}
	// TS safeParseJSON: rawValue is undefined for a payload that is not JSON.
	if raws[1] != nil {
		t.Fatalf("raw[1] = %#v, want nil for the unparsable payload", raws[1])
	}
	// The raw chunk for the malformed payload precedes its error chunk.
	for i, c := range chunks {
		if c.Type == provider.ChunkTypeError {
			if i == 0 || chunks[i-1].Type != provider.ChunkTypeRaw {
				t.Fatalf("error chunk not preceded by raw: %v", chunkTypes(chunks))
			}
		}
	}

	off := newTestStream(sseStream(payloads...))
	for _, c := range drain(t, off) {
		if c.Type == provider.ChunkTypeRaw {
			t.Fatal("raw chunk without includeRawChunks")
		}
	}
}

// TS passes usageMetadata through as usage.raw and providerMetadata, so
// fields the SDK does not model survive.
func TestUsageMetadataKeepsUnknownFields(t *testing.T) {
	var resp Response
	if err := json.Unmarshal([]byte(`{"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"futureField":{"a":1},"totalTokenCount":0}}`), &resp); err != nil {
		t.Fatal(err)
	}
	usage := convertUsage(resp.UsageMetadata)
	if usage.Raw["futureField"] == nil {
		t.Fatalf("Raw = %#v", usage.Raw)
	}
	if _, ok := usage.Raw["totalTokenCount"]; !ok {
		t.Fatalf("explicit zero dropped: %#v", usage.Raw)
	}
	// Continuation sums typed counters and keeps the unknown fields.
	b := *resp.UsageMetadata
	sum := addUsageMetadata(resp.UsageMetadata, &b)
	out, _ := json.Marshal(sum)
	var m map[string]interface{}
	_ = json.Unmarshal(out, &m)
	if m["promptTokenCount"] != float64(6) || m["futureField"] == nil {
		t.Fatalf("summed = %s", out)
	}
}
