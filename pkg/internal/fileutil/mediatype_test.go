package fileutil

import "testing"

func TestDetectMediaTypeAndCategoryHelpers(t *testing.T) {
	mt := DetectMediaType([]byte("hello world"))
	if mt.MimeType == "" || mt.Category == "" {
		t.Fatalf("invalid media type detection: %+v", mt)
	}

	img := MediaType{MimeType: "image/png", Category: "image"}
	if !img.IsImage() || img.IsAudio() || img.IsVideo() || img.IsText() {
		t.Fatalf("unexpected category helper results: %+v", img)
	}
}

func TestDetectMediaTypeFromFilename(t *testing.T) {
	pdf := DetectMediaTypeFromFilename("a.pdf")
	if pdf.MimeType != "application/pdf" || pdf.Extension != ".pdf" {
		t.Fatalf("unexpected pdf detection: %+v", pdf)
	}
	unknown := DetectMediaTypeFromFilename("a.unknownext")
	if unknown.MimeType != "application/octet-stream" {
		t.Fatalf("unexpected fallback mime type: %+v", unknown)
	}
}

func TestValidateMediaType(t *testing.T) {
	if err := ValidateMediaType([]byte("plain text"), "text/plain"); err != nil {
		t.Fatalf("expected exact validation match, got %v", err)
	}
	if err := ValidateMediaType([]byte("plain text"), "text/html"); err != nil {
		t.Fatalf("expected category fallback match, got %v", err)
	}
	if err := ValidateMediaType([]byte("plain text"), "image/png"); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestSplitAndCreateDataURL(t *testing.T) {
	mimeType, encoding, data, err := SplitDataURL("data:image/png;base64,Zm9v")
	if err != nil {
		t.Fatalf("SplitDataURL() error = %v", err)
	}
	if mimeType != "image/png" || encoding != "base64" || data != "Zm9v" {
		t.Fatalf("unexpected split values: %q %q %q", mimeType, encoding, data)
	}

	mimeType, encoding, _, err = SplitDataURL("data:,abc")
	if err != nil {
		t.Fatalf("SplitDataURL empty mime error = %v", err)
	}
	if mimeType != "text/plain" || encoding != "charset=US-ASCII" {
		t.Fatalf("unexpected defaults: %q %q", mimeType, encoding)
	}

	if _, _, _, err := SplitDataURL("not-a-data-url"); err == nil {
		t.Fatal("expected error for invalid prefix")
	}
	if _, _, _, err := SplitDataURL("data:image/png;base64"); err == nil {
		t.Fatal("expected error for missing comma")
	}

	created := CreateDataURL("image/png", []byte("Zm9v"))
	if created != "data:image/png;base64,Zm9v" {
		t.Fatalf("CreateDataURL() = %q", created)
	}
}

func TestExtensionFromMimeTypeAndHelpers(t *testing.T) {
	if ext := extensionFromMimeType("application/json"); ext != ".json" {
		t.Fatalf("application/json ext = %q", ext)
	}
	if ext := extensionFromMimeType("text/plain"); ext == "" {
		t.Fatal("expected extension for text/plain")
	}
	if got := categoryFromMimeType(" application/json "); got != "application" {
		t.Fatalf("categoryFromMimeType() = %q", got)
	}
}
