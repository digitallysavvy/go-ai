package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from the Anthropic tests of TS #21797: retrieved sources stay
// separate from normalized text citations.

const webCitationJSON = `{"type":"web_search_result_location","cited_text":"Go is open source","url":"https://go.dev/","title":"Go","encrypted_index":"enc"}`

func convertCitationResponse(t *testing.T, body string, opts convertOptions) *types.GenerateResult {
	t.Helper()
	var response anthropicResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{}
	return m.convertResponseWithOptions(response, opts)
}

func countSources(parts []types.ContentPart) []types.SourceContent {
	var out []types.SourceContent
	for _, p := range parts {
		if s, ok := p.(types.SourceContent); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestConvertResponseNormalizesWebSearchCitations(t *testing.T) {
	withResults := `{"content":[
		{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://go.dev/","title":"Go","encrypted_content":"x","page_age":null}]},
		{"type":"text","text":"Go is open source.","citations":[` + webCitationJSON + `]}]}`
	res := convertCitationResponse(t, withResults, convertOptions{})
	var text types.TextContent
	for _, p := range res.Content {
		if tc, ok := p.(types.TextContent); ok {
			text = tc
		}
	}
	if len(text.Citations) != 1 || text.Citations[0].Source.ID != "https://go.dev/" || text.Citations[0].Source.URL != "https://go.dev/" || text.Citations[0].CitedText != "Go is open source" {
		t.Fatalf("citations = %#v", text.Citations)
	}
	if !strings.Contains(string(text.ProviderMetadata), `"encrypted_index":"enc"`) {
		t.Fatalf("providerMetadata = %s", text.ProviderMetadata)
	}
	// Retrieved results exist: only the retrieved result is a source; the
	// citation does not add a duplicate.
	if sources := countSources(res.Content); len(sources) != 1 || !strings.Contains(string(sources[0].ProviderMetadata), "pageAge") {
		t.Fatalf("sources = %#v, want only the retrieved result", sources)
	}

	// No search results: the citation-derived source is retained.
	without := `{"content":[{"type":"text","text":"Go is open source.","citations":[` + webCitationJSON + `]}]}`
	res = convertCitationResponse(t, without, convertOptions{})
	if sources := countSources(res.Content); len(sources) != 1 || sources[0].URL != "https://go.dev/" {
		t.Fatalf("fallback sources = %#v", sources)
	}
}

func TestConvertResponseMapsAllCitationKinds(t *testing.T) {
	body := `{"content":[{"type":"text","text":"x","citations":[
		{"type":"char_location","cited_text":"a","document_index":0,"document_title":null,"start_char_index":0,"end_char_index":1},
		{"type":"page_location","cited_text":"b","document_index":1,"document_title":"T","file_id":"file_9","start_page_number":1,"end_page_number":2},
		{"type":"content_block_location","cited_text":"c","document_index":2,"document_title":null,"start_block_index":0,"end_block_index":1},
		{"type":"search_result_location","cited_text":"d","source":"https://src.example/","title":null,"search_result_index":0,"start_block_index":0,"end_block_index":1},
		` + webCitationJSON + `]}]}`
	docs := []citationDocument{{Title: "Doc A", Filename: "a.txt", MediaType: "text/plain"}}
	res := convertCitationResponse(t, body, convertOptions{citationDocuments: docs})
	text := res.Content[0].(types.TextContent)
	if len(text.Citations) != 5 {
		t.Fatalf("citations = %d, want 5", len(text.Citations))
	}
	c := text.Citations
	if c[0].Source.SourceType != "document" || c[0].Source.ID != "0" || c[0].Source.Title != "Doc A" || c[0].Source.Filename != "a.txt" {
		t.Errorf("char_location = %#v", c[0].Source)
	}
	if c[1].Source.ID != "file_9" || c[1].Source.Title != "T" || c[1].Source.MediaType != "text/plain" {
		t.Errorf("page_location (missing document) = %#v", c[1].Source)
	}
	if c[2].Source.Title != "Document 2" || c[2].Source.ID != "2" {
		t.Errorf("content_block_location = %#v", c[2].Source)
	}
	if c[3].Source.SourceType != "url" || c[3].Source.ID != "https://src.example/" || c[3].Source.Title != "" {
		t.Errorf("search_result_location = %#v", c[3].Source)
	}
	if c[4].Source.SourceType != "url" || c[4].Source.URL != "https://go.dev/" {
		t.Errorf("web_search_result_location = %#v", c[4].Source)
	}
}

func TestBatchConvertKeepsRawCitationsWithoutDocumentSources(t *testing.T) {
	body := `{"content":[{"type":"text","text":"x","citations":[
		{"type":"page_location","cited_text":"b","document_index":0,"document_title":"T","start_page_number":1,"end_page_number":2},
		` + webCitationJSON + `]}]}`
	res := convertCitationResponse(t, body, convertOptions{rawBatchCitations: true})
	text := res.Content[0].(types.TextContent)
	if len(text.Citations) != 2 || text.Citations[0].Source.Title != "T" {
		t.Fatalf("citations = %#v", text.Citations)
	}
	sources := countSources(res.Content)
	if len(sources) != 1 || sources[0].SourceType != "url" {
		t.Fatalf("sources = %#v, want only the web-search fallback", sources)
	}
}

func streamChunks(t *testing.T, sse string) []*provider.StreamChunk {
	t.Helper()
	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sse)), false)
	var chunks []*provider.StreamChunk
	for {
		c, err := stream.Next()
		if err == io.EOF {
			return chunks
		}
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, c)
	}
}

const sseHead = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-5\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
const sseTail = "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func textBlockWithCitation(index int, citation string) string {
	i := string(rune('0' + index))
	return "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":" + i + ",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":" + i + ",\"delta\":{\"type\":\"text_delta\",\"text\":\"Go is open.\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":" + i + ",\"delta\":{\"type\":\"citations_delta\",\"citation\":" + citation + "}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":" + i + "}\n\n"
}

func TestStreamTextEndCarriesNormalizedCitationsAndDefersWebSources(t *testing.T) {
	chunks := streamChunks(t, sseHead+textBlockWithCitation(0, webCitationJSON)+sseTail)
	var textEnd *provider.StreamChunk
	var sources []*types.SourceContent
	finishAt, sourceAt := -1, -1
	for i, c := range chunks {
		switch c.Type {
		case provider.ChunkTypeTextEnd:
			textEnd = c
		case provider.ChunkTypeSource:
			sources = append(sources, c.SourceContent)
			sourceAt = i
		case provider.ChunkTypeFinish:
			finishAt = i
		}
	}
	if textEnd == nil || len(textEnd.Citations) != 1 || textEnd.Citations[0].Source.ID != "https://go.dev/" {
		t.Fatalf("text-end = %#v", textEnd)
	}
	if !strings.Contains(string(textEnd.ProviderMetadata), `"web_search_result_location"`) {
		t.Fatalf("text-end providerMetadata = %s", textEnd.ProviderMetadata)
	}
	// No search results were retrieved, so the citation-derived source is
	// emitted just before the finish part.
	if len(sources) != 1 || sources[0].URL != "https://go.dev/" || finishAt != sourceAt+1 {
		t.Fatalf("sources = %v finishAt=%d sourceAt=%d", sources, finishAt, sourceAt)
	}
}

func TestStreamWebSearchResultsSuppressFallbackSources(t *testing.T) {
	searchResult := "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srv_1\",\"content\":[{\"type\":\"web_search_result\",\"url\":\"https://go.dev/\",\"title\":\"Go\",\"encrypted_content\":\"x\",\"page_age\":null}]}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	chunks := streamChunks(t, sseHead+searchResult+textBlockWithCitation(1, webCitationJSON)+sseTail)
	var urls int
	for _, c := range chunks {
		if c.Type == provider.ChunkTypeSource {
			urls++
		}
	}
	if urls != 1 {
		t.Fatalf("sources = %d, want exactly the 1 retrieved result", urls)
	}
}
