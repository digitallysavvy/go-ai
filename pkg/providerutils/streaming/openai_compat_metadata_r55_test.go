package streaming

import (
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS openai-compatible-chat-language-model.test.ts "should extract response
// metadata after an empty placeholder chunk" (fixture
// placeholder-first-chunk.chunks.txt): the response-metadata part comes from
// the first event with a non-empty id/model/created, even when the provider
// also reports response headers.
func TestOpenAICompatStreamResponseMetadataAfterPlaceholderChunk(t *testing.T) {
	events := []string{
		`{"choices":[],"created":0,"id":"","model":"","object":""}`,
		`{"choices":[{"delta":{"content":"Hello","role":"assistant"},"finish_reason":null,"index":0}],"created":1700000000,"id":"chatcmpl-123","model":"actual-model-name","object":"chat.completion.chunk"}`,
		`{"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":1700000000,"id":"chatcmpl-123","model":"actual-model-name","object":"chat.completion.chunk"}`,
		`{"choices":[],"created":1700000000,"id":"chatcmpl-123","model":"actual-model-name","object":"chat.completion.chunk","usage":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}`,
	}
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: " + e + "\n\n")
	}
	b.WriteString("data: [DONE]\n\n")

	for _, headers := range []map[string]string{nil, {"x-request-id": "r1"}} {
		stream := newTestStream(b.String())
		stream.HTTPResponseHeaders = headers
		chunks := collectStreamChunks(t, stream)
		var metas []*provider.ResponseMetadata
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeResponseMetadata {
				metas = append(metas, c.ResponseMetadata)
			}
		}
		if len(metas) != 1 {
			t.Fatalf("headers=%v: %d response-metadata chunks, want 1", headers, len(metas))
		}
		m := metas[0]
		if m.ID != "chatcmpl-123" || m.ModelID != "actual-model-name" || !m.Timestamp.Equal(time.Unix(1700000000, 0)) {
			t.Fatalf("headers=%v: metadata = %+v", headers, m)
		}
		if len(headers) > 0 && m.Headers["x-request-id"] != "r1" {
			t.Fatalf("headers not carried: %+v", m.Headers)
		}
	}
}

// A stream with no event carrying metadata emits no response-metadata part
// (TS has none) and reports its headers through ResponseHeaders, as TS
// returns them on the doStream result (review round 87e).
func TestOpenAICompatStreamHeadersWithoutMetadataEvent(t *testing.T) {
	stream := newTestStream(`data: {"choices":[{"delta":{"content":"x"},"finish_reason":"stop","index":0}]}` + "\n\ndata: [DONE]\n\n")
	stream.HTTPResponseHeaders = map[string]string{"x-request-id": "r1"}
	for _, c := range collectStreamChunks(t, stream) {
		if c.Type == provider.ChunkTypeResponseMetadata {
			t.Fatalf("unexpected metadata chunk %+v", c.ResponseMetadata)
		}
	}
	var s provider.TextStream = stream
	if rh, ok := s.(provider.StreamResponseHeaders); !ok || rh.ResponseHeaders()["x-request-id"] != "r1" {
		t.Fatal("headers not reported through ResponseHeaders")
	}
}
