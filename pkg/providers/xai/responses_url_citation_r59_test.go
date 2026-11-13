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

// TS annotationSchema is a union of {type:'url_citation', url: string,
// title?: string} and a bare {type}; a source needs the first branch ('url'
// in annotation). An empty url is a source, a null title or a non-string url
// is not, and a malformed annotation never drops the message text.
func TestR59XAIResponsesURLCitationFollowsTSAnnotationSchema(t *testing.T) {
	annotations := `[{"type":"url_citation","url":""},{"type":"url_citation","url":"https://n.test","title":null},{"type":"url_citation","url":7},{"type":"url_citation","url":"https://t.test","title":3},{"type":"url_citation","url":"https://ok.test"}]`
	want := []string{"", "https://ok.test"}

	var resp responses.ResponsesAPIResponse
	body := `{"id":"r","model":"grok","status":"completed","output":[{"type":"message","id":"m","content":[{"type":"output_text","text":"hi","annotations":` + annotations + `}]}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "grok-4.3")
	res, err := m.convertResponse(resp, nil)
	if err != nil {
		t.Fatal(err)
	}
	var urls []string
	for _, c := range res.Content {
		if s, ok := c.(types.SourceContent); ok {
			urls = append(urls, s.URL)
		}
	}
	if res.Text != "hi" || strings.Join(urls, ",") != strings.Join(want, ",") {
		t.Fatalf("generate text = %q, source urls = %q; want %q and %q", res.Text, urls, "hi", want)
	}

	sse := `data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"m2","role":"assistant","status":"completed","content":[{"type":"output_text","text":"t","annotations":` + annotations + `}]}}

data: [DONE]

`
	stream := newXAIResponsesStream(io.NopCloser(strings.NewReader(sse)))
	defer stream.Close() //nolint:errcheck
	urls = nil
	text := ""
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch chunk.Type {
		case provider.ChunkTypeSource:
			urls = append(urls, chunk.SourceContent.URL)
		case provider.ChunkTypeText:
			text += chunk.Text
		}
	}
	if text != "t" || strings.Join(urls, ",") != strings.Join(want, ",") {
		t.Fatalf("stream text = %q, source urls = %q; want %q and %q", text, urls, "t", want)
	}
}
