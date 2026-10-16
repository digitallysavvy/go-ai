package xai

import (
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS streams url_citation sources with generateId() ids; an empty id would
// surface as an empty sourceId in the UI message stream.
func TestXAIResponsesStreamSourcesHaveIDs(t *testing.T) {
	sse := `data: {"type":"response.output_text.annotation.added","item_id":"m1","output_index":0,"content_index":0,"annotation_index":0,"annotation":{"type":"url_citation","url":"https://a.example","title":"A"}}

data: {"type":"response.output_text.done","item_id":"m1","output_index":0,"content_index":0,"text":"x","annotations":[{"type":"url_citation","url":"https://b.example","title":"B"}]}

data: [DONE]

`
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck
	ids := map[string]bool{}
	n := 0
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Type == provider.ChunkTypeSource {
			n++
			if chunk.SourceContent.ID == "" {
				t.Fatalf("source %s has empty id", chunk.SourceContent.URL)
			}
			ids[chunk.SourceContent.ID] = true
		}
	}
	if n != 2 || len(ids) != 2 {
		t.Fatalf("sources = %d, unique ids = %d", n, len(ids))
	}
}
