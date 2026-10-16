package anthropic

import (
	"encoding/json"
	"testing"
)

// TS omits `title` from a web_search tool-result entry when the API sent null.
func TestConvertWebSearchToolResult_NullTitleOmitted(t *testing.T) {
	raw := json.RawMessage(`[{"type":"web_search_result","url":"https://a.test","title":null,"page_age":null,"encrypted_content":"enc"},{"type":"web_search_result","url":"https://b.test","title":"B","page_age":"1d","encrypted_content":"e2"}]`)
	res, sources := convertWebSearchToolResult(raw, func() string { return "id" })
	items := res.([]map[string]interface{})
	if _, has := items[0]["title"]; has {
		t.Fatalf("null title must be omitted, got %v", items[0])
	}
	if items[1]["title"] != "B" {
		t.Fatalf("title lost: %v", items[1])
	}
	if sources[0].Title != "" || sources[1].Title != "B" {
		t.Fatalf("sources: %+v", sources)
	}
}

func TestConvertWebFetchToolResult_NullRetrievedAtAndAbsentCitations(t *testing.T) {
	raw := json.RawMessage(`{"type":"web_fetch_result","url":"https://a.test","retrieved_at":null,"content":{"type":"document","title":null,"source":{"type":"text","media_type":"text/plain","data":"x"}}}`)
	b, _ := json.Marshal(convertWebFetchToolResult(raw))
	want := `{"content":{"source":{"data":"x","mediaType":"text/plain","type":"text"},"title":null,"type":"document"},"retrievedAt":null,"type":"web_fetch_result","url":"https://a.test"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
}
