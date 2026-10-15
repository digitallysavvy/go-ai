package bedrock

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS emits {type:'raw', rawValue: {[eventType]: payload}} for every event
// ahead of the parts it produces when includeRawChunks is set.
func TestConverseStreamIncludeRawChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write(buildBedrockEventStreamBody([][2]string{
			{"contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Hi"}}`},
			{"contentBlockStop", `{"contentBlockIndex":0}`},
			{"messageStop", `{"stopReason":"end_turn"}`},
			{"metadata", `{"usage":{"inputTokens":1,"outputTokens":2,"totalTokens":3}}`},
		}))
	}))
	defer server.Close()
	model := newHTTPTestBedrockModel(t, server, "anthropic.claude-3-5-sonnet-20241022-v2:0")

	run := func(include bool) []*provider.StreamChunk {
		stream, err := model.DoStream(context.Background(), &provider.GenerateOptions{
			Prompt: types.Prompt{Text: "hi"}, IncludeRawChunks: include,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close() //nolint:errcheck
		var out []*provider.StreamChunk
		for {
			c, err := stream.Next()
			if err == io.EOF {
				return out
			}
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, c)
		}
	}

	var raws []map[string]interface{}
	nonRaw := 0
	for _, c := range run(true) {
		if c.Type == provider.ChunkTypeRaw {
			m, ok := c.Raw.(map[string]interface{})
			if !ok {
				t.Fatalf("raw = %#v", c.Raw)
			}
			raws = append(raws, m)
			continue
		}
		nonRaw++
	}
	if len(raws) != 4 || raws[0]["contentBlockDelta"] == nil || raws[2]["messageStop"] == nil {
		t.Fatalf("raw chunks = %#v", raws)
	}
	plain := run(false)
	for _, c := range plain {
		if c.Type == provider.ChunkTypeRaw {
			t.Fatal("raw chunk without includeRawChunks")
		}
	}
	if len(plain) != nonRaw {
		t.Fatalf("non-raw chunk count changed: %d vs %d", len(plain), nonRaw)
	}
}

func TestImageConvertResponseDecodesImageBytes(t *testing.T) {
	m := NewImageModel(nil, "amazon.nova-canvas-v1:0")
	res, err := m.convertResponse([]byte(`{"images":["aGVsbG8="]}`), http.Header{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(res.Image) != "hello" || string(res.Images[0]) != "hello" || res.Base64Images[0] != "aGVsbG8=" {
		t.Fatalf("Image=%q Images=%q Base64Images=%q", res.Image, res.Images[0], res.Base64Images[0])
	}
}
