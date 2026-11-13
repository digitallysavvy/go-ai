package types

import (
	"encoding/json"
	"testing"
)

// TS mapToolResultOutput "file-url" / "image-url": url = new URL(item.url),
// originalUrl: item.url when url.toString() differs; a missing mediaType is
// inferred from the extension of the WHATWG pathname (getMediaTypeFromUrl).
func TestR84genLegacyFileURLBlocks(t *testing.T) {
	var out ToolResultOutput
	input := `{"type":"content","value":[` +
		`{"type":"file-url","url":"https://Example.com/My Report.PDF?x=1"},` +
		`{"type":"file-url","url":"https://example.com/report.pdf"},` +
		`{"type":"file-url","url":"https://cdn.example.png"},` +
		`{"type":"file-url","url":"https://example.com/a.png#frag.pdf"},` +
		`{"type":"file-url","url":"not a url.png"},` +
		`{"type":"image-url","url":"gs://b/My Hero.png"}]}`
	if err := json.Unmarshal([]byte(input), &out); err != nil {
		t.Fatal(err)
	}
	want := []struct{ url, original, mediaType string }{
		{"https://Example.com/My Report.PDF?x=1", "https://Example.com/My Report.PDF?x=1", "application/pdf"},
		{"https://example.com/report.pdf", "", "application/pdf"},
		// TS pathname "/": no extension (the old Go check read ".png" off the host).
		{"https://cdn.example.png", "https://cdn.example.png", "application/octet-stream"},
		{"https://example.com/a.png#frag.pdf", "", "image/png"},
		// Does not parse: TS falls back; core conversion then fails with "Invalid URL".
		{"not a url.png", "", "application/octet-stream"},
		{"gs://b/My Hero.png", "gs://b/My Hero.png", "image"},
	}
	for i, w := range want {
		b, ok := out.Content[i].(FileContentBlock)
		if !ok {
			t.Fatalf("block %d = %T", i, out.Content[i])
		}
		if b.FileData.URL != w.url || b.FileData.OriginalURL != w.original || b.MediaType != w.mediaType {
			t.Errorf("block %d = %+v, mediaType %q; want url %q original %q mediaType %q", i, b.FileData, b.MediaType, w.url, w.original, w.mediaType)
		}
	}
}

// TS JSON.stringify of { type: 'url', url, originalUrl? } (URL serializes as
// its href).
func TestR84genFileDataOriginalURLJSON(t *testing.T) {
	data, err := json.Marshal(FileData{Type: FileDataTypeURL, URL: "gs://b/My%20Hero.png", OriginalURL: "gs://b/My Hero.png"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"type":"url","url":"gs://b/My%20Hero.png","originalUrl":"gs://b/My Hero.png"}` {
		t.Fatalf("json = %s", data)
	}
	var back FileData
	if err := json.Unmarshal(data, &back); err != nil || back.OriginalURL != "gs://b/My Hero.png" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	data, _ = json.Marshal(FileData{Type: FileDataTypeURL, URL: "https://x.com/"})
	if string(data) != `{"type":"url","url":"https://x.com/"}` {
		t.Fatalf("json = %s", data)
	}
	if (FileData{OriginalURL: "x"}).IsZero() {
		t.Fatal("IsZero ignores OriginalURL")
	}
}
