package prompt

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// URL file parts are WHATWG-normalized as TS does (file-part-data.ts
// convertUrlToFilePartData / convertUrlStringToFilePartData), with the
// original string kept for the legacy URL string fields.

// TS file-part-data.test.ts "preserves the original string when URL parsing
// changes a non-HTTP URI" (a string URL: Go's legacy URL fields).
func TestR84genLegacyURLStringKeepsOriginal(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{URL: "gs://my-bucket/folder/My File.pdf", MediaType: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.Type != types.FileDataTypeURL ||
		got.FileData.URL != "gs://my-bucket/folder/My%20File.pdf" ||
		got.FileData.OriginalURL != "gs://my-bucket/folder/My File.pdf" ||
		got.URL != "gs://my-bucket/folder/My%20File.pdf" {
		t.Fatalf("FileData = %+v, URL = %q", got.FileData, got.URL)
	}

	// TS "converts a URL string into { type: "url", url }": no originalUrl
	// when serialization does not change the string.
	got, err = NormalizeFileContent(types.FileContent{URL: "https://example.com/file.pdf", MediaType: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.URL != "https://example.com/file.pdf" || got.FileData.OriginalURL != "" {
		t.Fatalf("FileData = %+v", got.FileData)
	}
}

// TS tagged { type: 'url', url: URL }: providers get url.toString(); the
// caller's originalUrl is kept as given, and none is added.
func TestR84genTaggedURLNormalized(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{FileData: types.FileData{
		Type: types.FileDataTypeURL, URL: "HTTPS://Example.COM:443/a/./b/../c d.png?q=é#f g", MediaType: "image/png",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.URL != "https://example.com/a/c%20d.png?q=%C3%A9#f%20g" || got.FileData.OriginalURL != "" {
		t.Fatalf("FileData = %+v", got.FileData)
	}

	// TS "preserves the original opaque URI in a tagged URL".
	got, err = NormalizeFileContent(types.FileContent{FileData: types.FileData{
		Type: types.FileDataTypeURL, URL: "gs://my-bucket/folder/My File.pdf",
		OriginalURL: "gs://my-bucket/folder/My File.pdf", MediaType: "application/pdf",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.URL != "gs://my-bucket/folder/My%20File.pdf" || got.FileData.OriginalURL != "gs://my-bucket/folder/My File.pdf" {
		t.Fatalf("FileData = %+v", got.FileData)
	}
}

// TS `new URL(url)` throws TypeError "Invalid URL" for a URL that does not
// parse (Go: every URL field is an explicit URL).
func TestR84genInvalidURLError(t *testing.T) {
	for _, part := range []types.FileContent{
		{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "not a url"}, MediaType: "image/png"},
		{URL: "/relative/path.png", MediaType: "image/png"},
		{URL: "https://exa mple.com/x.png", MediaType: "image/png"},
	} {
		_, err := NormalizeFileContent(part)
		var invalid *InvalidURLError
		if !errors.As(err, &invalid) || err.Error() != "Invalid URL" {
			t.Fatalf("NormalizeFileContent(%+v) error = %v, want Invalid URL", part, err)
		}
	}
	_, err := NormalizeImageContent(types.ImageContent{URL: "::"})
	if err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("image part error = %v", err)
	}
	_, err = NormalizeToolResultFileContent(types.FileContentBlock{URL: "x", MediaType: "image/png"})
	if err == nil || err.Error() != "Invalid URL" {
		t.Fatalf("tool result block error = %v", err)
	}
}

// TS url.protocol === 'data:' is case-insensitive in the source string.
func TestR84genUppercaseDataURLIsInlineData(t *testing.T) {
	got, err := NormalizeFileContent(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "DATA:text/plain;base64,aGVsbG8="}})
	if err != nil {
		t.Fatal(err)
	}
	if got.FileData.Type != types.FileDataTypeData || string(got.FileData.Data) != "hello" || got.MediaType != "text/plain" {
		t.Fatalf("FileData = %+v, MediaType = %q", got.FileData, got.MediaType)
	}
}

func TestR84genImageAndToolResultURLs(t *testing.T) {
	file, err := NormalizeImageContent(types.ImageContent{URL: "https://Example.com/My Image.PNG"})
	if err != nil {
		t.Fatal(err)
	}
	if file.FileData.URL != "https://example.com/My%20Image.PNG" || file.FileData.OriginalURL != "https://Example.com/My Image.PNG" || file.MediaType != "image" {
		t.Fatalf("image file = %+v", file)
	}

	block, err := NormalizeToolResultFileContent(types.FileContentBlock{URL: "gs://b/My Hero.png", MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if block.FileData.URL != "gs://b/My%20Hero.png" || block.FileData.OriginalURL != "gs://b/My Hero.png" || block.URL != "gs://b/My%20Hero.png" {
		t.Fatalf("block = %+v", block)
	}
	block, err = NormalizeToolResultFileContent(types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "gs://b/My Hero.png"}, MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if block.FileData.URL != "gs://b/My%20Hero.png" || block.FileData.OriginalURL != "" {
		t.Fatalf("tagged block = %+v", block)
	}

	reasoning, err := NormalizeReasoningFileContent(types.ReasoningFileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "HTTPS://X.COM/a"}, MediaType: "image/png"})
	if err != nil {
		t.Fatal(err)
	}
	if reasoning.FileData.URL != "https://x.com/a" {
		t.Fatalf("reasoning file = %+v", reasoning.FileData)
	}
}

// TS downloadAssets plans downloads and matches supportedUrls on
// url.toString(), and keys downloaded assets by it.
func TestR84genDownloadPlanningUsesNormalizedURL(t *testing.T) {
	var checked []string
	var requested []string
	download := func(_ context.Context, requests []DownloadRequest) ([]*DownloadResult, error) {
		results := make([]*DownloadResult, len(requests))
		for i, r := range requests {
			requested = append(requested, r.URL)
			if !r.IsURLSupportedByModel {
				results[i] = &DownloadResult{Data: []byte("%PDF"), MediaType: "application/pdf"}
			}
		}
		return results, nil
	}
	supported := func(mediaType, url string) bool {
		checked = append(checked, url)
		return url == "https://supported.example/a%20b.png"
	}
	got, err := NormalizePromptWithDownloadSupport(context.Background(), types.Prompt{Messages: []types.Message{{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.FileContent{URL: "HTTPS://Supported.Example:443/a b.png", MediaType: "image/png"},
			types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://Other.example/./x/../doc.pdf"}, MediaType: "application/pdf"},
		},
	}}}, false, download, supported)
	if err != nil {
		t.Fatal(err)
	}
	wantURLs := []string{"https://supported.example/a%20b.png", "https://other.example/doc.pdf"}
	for i, want := range wantURLs {
		if checked[i] != want || requested[i] != want {
			t.Fatalf("checked %q, requested %q, want %q", checked, requested, wantURLs)
		}
	}
	parts := got.Messages[0].Content
	kept := parts[0].(types.FileContent)
	if kept.FileData.Type != types.FileDataTypeURL || kept.FileData.URL != wantURLs[0] || kept.FileData.OriginalURL != "HTTPS://Supported.Example:443/a b.png" {
		t.Fatalf("supported part = %+v", kept.FileData)
	}
	downloaded := parts[1].(types.FileContent)
	if downloaded.FileData.Type != types.FileDataTypeData || string(downloaded.FileData.Data) != "%PDF" {
		t.Fatalf("downloaded part = %+v", downloaded.FileData)
	}

	// The exported DownloadUnsupportedFileURLs plans and applies by the
	// normalized URL even when called on messages that were not normalized.
	requested = nil
	out, err := DownloadUnsupportedFileURLs(context.Background(), []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{types.FileContent{URL: "HTTPS://Other.example/doc.pdf", MediaType: "application/pdf"}},
	}}, download, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(requested) != 1 || requested[0] != "https://other.example/doc.pdf" {
		t.Fatalf("requested = %q", requested)
	}
	if f := out[0].Content[0].(types.FileContent); f.FileData.Type != types.FileDataTypeData {
		t.Fatalf("not applied: %+v", f)
	}
}

// TS accepts any URL that parses (javascript:, mailto:, file:); only the
// downloader limits schemes.
func TestR84genNonHTTPSchemesAccepted(t *testing.T) {
	for raw, want := range map[string]string{
		"mailto:Some One <a@b.c>": "mailto:Some One <a@b.c>",
		"urn:isbn:0451450523":     "urn:isbn:0451450523",
		"s3://Bucket/Key Name":    "s3://Bucket/Key%20Name",
	} {
		got, err := NormalizeFileData(types.FileData{Type: types.FileDataTypeURL, URL: raw, MediaType: "application/pdf"})
		if err != nil || got.URL != want {
			t.Fatalf("NormalizeFileData(%q) = %+v, %v; want %q", raw, got, err, want)
		}
	}
}
