package google

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports the grounding source tests of google-language-model.test.ts
// (extractSources).

func srcOf(t *testing.T, c types.ContentPart) types.SourceContent {
	t.Helper()
	s, ok := c.(types.SourceContent)
	if !ok {
		t.Fatalf("want SourceContent, got %#v", c)
	}
	s.ID = ""
	return s
}

func TestGroundingSourcesGenerate(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"text":"mixed"}],"role":"model"},"finishReason":"STOP","groundingMetadata":{"groundingChunks":[
{"web":{"uri":"https://web.example.com"}},
{"image":{"sourceUri":"https://img-src.example.com","imageUri":"https://img.example.com/a.png","title":"Img"}},
{"retrievedContext":{"uri":"https://external.example.com"}},
{"retrievedContext":{"uri":"gs://bucket/document.pdf"}},
{"retrievedContext":{"uri":"gs://bucket/notes.md","title":"Notes"}},
{"retrievedContext":{"uri":"gs://bucket/blob.bin"}},
{"retrievedContext":{"fileSearchStore":"fileSearchStores/store-123"}},
{"maps":{"uri":"https://maps.example.com/p","title":"Place"}},
{"maps":{"title":"no uri"}}
]}}]}`
	srv, _ := scriptedServer(t, scriptedReply{body: body})
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	res, err := m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res.Content[0].(types.TextContent); !ok {
		t.Fatalf("text must come first: %#v", res.Content)
	}
	var got []types.SourceContent
	for _, c := range res.Content[1:] {
		got = append(got, srcOf(t, c))
	}
	want := []types.SourceContent{
		{SourceType: "url", URL: "https://web.example.com"},
		{SourceType: "url", URL: "https://img-src.example.com", Title: "Img"},
		{SourceType: "url", URL: "https://external.example.com"},
		{SourceType: "document", MediaType: "application/pdf", Title: "Unknown Document", Filename: "document.pdf"},
		{SourceType: "document", MediaType: "text/markdown", Title: "Notes", Filename: "notes.md"},
		{SourceType: "document", MediaType: "application/octet-stream", Title: "Unknown Document", Filename: "blob.bin"},
		{SourceType: "document", MediaType: "application/octet-stream", Title: "Unknown Document", Filename: "store-123"},
		{SourceType: "url", URL: "https://maps.example.com/p", Title: "Place"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %#v\nwant %#v", got, want)
	}
}

func TestGroundingSourcesStreamDeduplicates(t *testing.T) {
	chunk := func(text, chunks string) string {
		return `{"candidates":[{"content":{"parts":[{"text":"` + text + `"}],"role":"model"},"groundingMetadata":{"groundingChunks":[` + chunks + `]}}]}`
	}
	srv, _ := scriptedServer(t, scriptedReply{sse: true, body: sseEvents(
		chunk("a", `{"web":{"uri":"https://example.com","title":"Example"}},{"web":{"uri":"https://unique.com","title":"Unique"}}`),
		chunk("b", `{"web":{"uri":"https://example.com","title":"Dup"}},{"web":{"uri":"https://another.com","title":"Another"}},{"retrievedContext":{"uri":"gs://b/d.pdf"}}`),
		`{"candidates":[{"content":{"parts":[{"text":"c"}],"role":"model"},"finishReason":"STOP"}]}`,
	)})
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "gemini-2.5-flash")
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var got []string
	for {
		c, err := stream.Next()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			break
		}
		if c.Type == provider.ChunkTypeSource {
			if c.SourceContent.SourceType != "url" || c.SourceContent.ID == "" {
				t.Fatalf("bad source %#v", c.SourceContent)
			}
			got = append(got, c.SourceContent.URL+"|"+c.SourceContent.Title)
		}
	}
	want := []string{"https://example.com|Example", "https://unique.com|Unique", "https://another.com|Another"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %v want %v", got, want)
	}
}
