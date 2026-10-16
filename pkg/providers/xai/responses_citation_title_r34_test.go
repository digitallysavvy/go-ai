package xai

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// TS maps `annotation.title ?? annotation.url`: an empty title stays empty,
// only a missing (or null) title falls back to the URL.
func TestXAIResponsesCitationTitleNullishFallback(t *testing.T) {
	sse := `data: {"type":"response.output_text.annotation.added","item_id":"m1","output_index":0,"content_index":0,"annotation_index":0,"annotation":{"type":"url_citation","url":"https://a.example","title":""}}

data: {"type":"response.output_text.annotation.added","item_id":"m1","output_index":0,"content_index":0,"annotation_index":1,"annotation":{"type":"url_citation","url":"https://b.example"}}

data: {"type":"response.output_text.done","item_id":"m1","output_index":0,"content_index":0,"text":"x","annotations":[{"type":"url_citation","url":"https://c.example","title":""},{"type":"url_citation","url":"https://d.example","title":null}]}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m2","role":"assistant","status":"completed","content":[{"type":"output_text","text":"t","annotations":[{"type":"url_citation","url":"https://e.example","title":""}]}]}}

data: [DONE]

`
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck
	titles := map[string]string{}
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk.Type == provider.ChunkTypeSource {
			titles[chunk.SourceContent.URL] = chunk.SourceContent.Title
		}
	}
	want := map[string]string{
		"https://a.example": "",
		"https://b.example": "https://b.example",
		"https://c.example": "",
		"https://d.example": "https://d.example",
		"https://e.example": "",
	}
	for u, w := range want {
		if got, ok := titles[u]; !ok || got != w {
			t.Errorf("stream title[%s] = %q (seen=%v), want %q", u, got, ok, w)
		}
	}

	var resp responses.ResponsesAPIResponse
	body := `{"id":"r","model":"grok","status":"completed","output":[{"type":"message","id":"m","content":[{"type":"output_text","text":"hi","annotations":[{"type":"url_citation","url":"https://f.example","title":""},{"type":"url_citation","url":"https://g.example"}]}]}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4.3")
	res, err := m.convertResponse(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			got[s.URL] = s.Title
		}
	}
	if got["https://f.example"] != "" || got["https://g.example"] != "https://g.example" || len(got) != 2 {
		t.Errorf("generate titles = %#v", got)
	}
}
