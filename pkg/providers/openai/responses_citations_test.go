package openai

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
)

// Ported from the OpenAI Responses tests of TS #21797: retrieved sources are
// exposed as sources, normalized citations attach to the supporting text.

func citationMessageItem() json.RawMessage {
	return json.RawMessage(`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"See Go docs and notes.","annotations":[
		{"type":"url_citation","start_index":4,"end_index":12,"url":"https://go.dev/doc/","title":"Go Docs"},
		{"type":"file_citation","file_id":"file_1","filename":"notes.txt","index":0},
		{"type":"file_path","file_id":"file_2","index":3}
	]}]}`)
}

func convertCitationResponse(t *testing.T, items ...json.RawMessage) *types.GenerateResult {
	t.Helper()
	m := NewResponsesLanguageModel(New(Config{APIKey: "k"}), "gpt-4o")
	res, err := m.convertResponse(responses.ResponsesAPIResponse{ID: "r", Model: "gpt-4o", Output: items, Usage: &responses.ResponsesAPIUsage{}}, true, "web_search", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestResponsesConvertResponseAttachesCitations(t *testing.T) {
	res := convertCitationResponse(t, citationMessageItem())
	text, ok := res.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T", res.Content[0])
	}
	if len(text.Citations) != 2 {
		t.Fatalf("citations = %#v, want 2 (file_path excluded)", text.Citations)
	}
	url := text.Citations[0]
	if url.Source.SourceType != "url" || url.Source.ID != "https://go.dev/doc/" || url.Source.Title != "Go Docs" ||
		url.StartIndex == nil || *url.StartIndex != 4 || url.EndIndex == nil || *url.EndIndex != 12 {
		t.Fatalf("url citation = %#v", url)
	}
	file := text.Citations[1]
	if file.Source.SourceType != "document" || file.Source.ID != "file_1" || file.StartIndex != nil {
		t.Fatalf("file citation = %#v", file)
	}
	// Without retrieval data the citation-derived sources are retained
	// (url, file_citation, file_path).
	sources := 0
	for _, part := range res.Content {
		if _, ok := part.(types.SourceContent); ok {
			sources++
		}
	}
	if sources != 3 {
		t.Fatalf("fallback sources = %d, want 3", sources)
	}
}

func TestResponsesConvertResponseRetrievedSourcesReplaceFallbacks(t *testing.T) {
	ws := json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"go","sources":[{"type":"url","url":"https://go.dev/doc/"},{"type":"url","url":"https://go.dev/blog/"}]}}`)
	fs := json.RawMessage(`{"type":"file_search_call","id":"fs_1","queries":["q"],"results":[{"file_id":"file_1","filename":"notes.txt","score":0.9,"text":"a"},{"file_id":"file_1","filename":"notes.txt","score":0.8,"text":"b"},{"file_id":"file_9","filename":"other.txt","score":0.1,"text":"c"}]}`)
	res := convertCitationResponse(t, ws, fs, citationMessageItem())

	var urls, docs []types.SourceContent
	for _, part := range res.Content {
		if s, ok := part.(types.SourceContent); ok {
			if s.SourceType == "url" {
				urls = append(urls, s)
			} else {
				docs = append(docs, s)
			}
		}
	}
	if len(urls) != 2 || urls[0].URL != "https://go.dev/doc/" || urls[1].URL != "https://go.dev/blog/" {
		t.Fatalf("url sources = %#v, want the 2 retrieved URLs only", urls)
	}
	// Two unique retrieved files (deduplicated) + the file_path annotation.
	if len(docs) != 3 {
		t.Fatalf("document sources = %#v, want 3", docs)
	}
	if !strings.Contains(string(docs[0].ProviderMetadata), `"type":"file_search"`) || !strings.Contains(string(docs[0].ProviderMetadata), `"fileId":"file_1"`) {
		t.Fatalf("retrieved file metadata = %s", docs[0].ProviderMetadata)
	}
	if docs[2].MediaType != "application/octet-stream" {
		t.Fatalf("file_path source = %#v", docs[2])
	}
}

func drainResponsesStream(t *testing.T, sse string) []*provider.StreamChunk {
	t.Helper()
	stream := newResponsesStream(io.NopCloser(strings.NewReader(sse)), false)
	defer stream.Close() //nolint:errcheck
	var out []*provider.StreamChunk
	for {
		chunk, err := stream.Next()
		if err != nil {
			return out
		}
		out = append(out, chunk)
	}
}

func TestResponsesStreamCitationsOnTextEndAndDeferredFallback(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"delta":"See docs"}

data: {"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","start_index":4,"end_index":8,"url":"https://go.dev/doc/","title":"Go Docs"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_item.added","output_index":1,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}}

data: {"type":"response.output_item.done","output_index":1,"item":{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","sources":[{"type":"url","url":"https://go.dev/blog/"}]}}}

`
	chunks := drainResponsesStream(t, sse)
	var textEnd *provider.StreamChunk
	var sources []string
	for _, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeTextEnd:
			textEnd = c
		case provider.ChunkTypeSource:
			sources = append(sources, c.SourceContent.URL)
		}
	}
	if textEnd == nil || len(textEnd.Citations) != 1 || textEnd.Citations[0].Source.URL != "https://go.dev/doc/" ||
		textEnd.Citations[0].StartIndex == nil || *textEnd.Citations[0].StartIndex != 4 {
		t.Fatalf("text-end = %#v", textEnd)
	}
	// The retrieval event arrived after the annotation, so the citation-derived
	// fallback source is dropped and only the retrieved URL remains.
	if len(sources) != 1 || sources[0] != "https://go.dev/blog/" {
		t.Fatalf("sources = %v, want only the retrieved URL", sources)
	}
}

func TestResponsesStreamFallbackSourcesWithoutRetrieval(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","start_index":0,"end_index":1,"url":"https://example.com","title":"Example"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

`
	var sources []string
	for _, c := range drainResponsesStream(t, sse) {
		if c.Type == provider.ChunkTypeSource {
			sources = append(sources, c.SourceContent.URL)
		}
	}
	if len(sources) != 1 || sources[0] != "https://example.com" {
		t.Fatalf("sources = %v, want the citation-derived fallback", sources)
	}
}

func TestResponsesStreamFileSearchSourcesDeduplicated(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"file_search_call","id":"fs_1"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"file_search_call","id":"fs_1","queries":["q"],"results":[{"attributes":{},"file_id":"file_1","filename":"a.txt","score":1,"text":"x"},{"attributes":{},"file_id":"file_1","filename":"a.txt","score":1,"text":"y"}]}}

`
	var docs int
	var order []provider.ChunkType
	for _, c := range drainResponsesStream(t, sse) {
		order = append(order, c.Type)
		if c.Type == provider.ChunkTypeSource {
			docs++
		}
	}
	if docs != 1 || len(order) < 3 || order[1] != provider.ChunkTypeToolResult || order[2] != provider.ChunkTypeSource {
		t.Fatalf("order = %v docs = %d, want tool-result then one deduplicated source", order, docs)
	}
}

// TS #21797: container_file_citation and file_path annotations become sources
// immediately; url/file citation fallbacks are deferred until just before the
// finish part.
func TestResponsesStreamNonDeferredAnnotationsAndFallbackBeforeFinish(t *testing.T) {
	sse := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.output_text.annotation.added","annotation":{"type":"url_citation","start_index":0,"end_index":1,"url":"https://example.com","title":"Example"}}

data: {"type":"response.output_text.annotation.added","annotation":{"type":"container_file_citation","container_id":"cntr_1","file_id":"file_9","filename":"out.csv","start_index":2,"end_index":3}}

data: {"type":"response.output_text.annotation.added","annotation":{"type":"file_path","file_id":"file_p","index":1}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1"}}

data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}

`
	var order []string
	var textEnd *provider.StreamChunk
	for _, c := range drainResponsesStream(t, sse) {
		switch c.Type {
		case provider.ChunkTypeSource:
			order = append(order, "source:"+c.SourceContent.Title)
		case provider.ChunkTypeTextEnd:
			textEnd = c
			order = append(order, "text-end")
		case provider.ChunkTypeFinish:
			order = append(order, "finish")
		}
	}
	want := []string{"source:out.csv", "source:file_p", "text-end", "source:Example", "finish"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	// file_path is not a citation; the url and container citations are.
	if textEnd == nil || len(textEnd.Citations) != 2 {
		t.Fatalf("citations = %#v", textEnd)
	}
}
