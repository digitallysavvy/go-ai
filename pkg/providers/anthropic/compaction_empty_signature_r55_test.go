package anthropic

import (
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS checks `part.signature != null`, so an empty compaction signature is
// kept in providerMetadata and still lets on-demand content stream.
func TestR55StreamCompactionEmptySignatureIsPresent(t *testing.T) {
	sseData := "" +
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-opus-5\",\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"compaction\",\"content\":\"S\",\"signature\":\"\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)), false)
	var start, text *provider.StreamChunk
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeTextStart:
			start = c
		case provider.ChunkTypeText:
			text = c
		}
	}
	if start == nil {
		t.Fatal("no text-start")
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(start.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	if want := map[string]interface{}{"type": "compaction", "signature": ""}; !reflect.DeepEqual(meta["anthropic"], want) {
		t.Fatalf("metadata = %#v, want %#v", meta["anthropic"], want)
	}
	if text == nil || text.Text != "S" {
		t.Fatalf("text = %#v, want S", text)
	}
}

func TestR55GenerateCompactionEmptySignatureIsPresent(t *testing.T) {
	var resp anthropicResponse
	if err := json.Unmarshal([]byte(`{"content":[{"type":"compaction","content":"S","signature":""}],"stop_reason":"compaction","usage":{"input_tokens":1,"output_tokens":1}}`), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(New(Config{APIKey: "k"}), "claude-sonnet-4-5", nil)
	res := m.convertResponse(resp, false)
	for _, part := range res.Content {
		if tc, ok := part.(types.TextContent); ok {
			var meta map[string]map[string]interface{}
			if err := json.Unmarshal(tc.ProviderMetadata, &meta); err != nil {
				t.Fatal(err)
			}
			if _, ok := meta["anthropic"]["signature"]; !ok {
				t.Fatalf("metadata = %#v, want an empty signature", meta)
			}
			return
		}
	}
	t.Fatal("no compaction text")
}
