package harnessutil_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const pngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func userMessage(parts ...types.ContentPart) harness.Prompt {
	return harness.Prompt{Message: &types.Message{Role: types.RoleUser, Content: parts}}
}

// TS: "preserves string prompts and string content"
func TestExtractPromptContent_StringPrompt(t *testing.T) {
	got, err := harnessutil.ExtractPromptContent(harness.TextPrompt("hello"))
	if err != nil || !got.IsText() || got.Text != "hello" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// TS: "joins text-only parts with blank lines"
func TestExtractPromptContent_JoinsTextOnly(t *testing.T) {
	got, err := harnessutil.ExtractPromptContent(userMessage(types.TextContent{Text: "First"}, types.TextContent{Text: "Second"}))
	if err != nil || !got.IsText() || got.Text != "First\n\nSecond" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// TS: "normalizes inline images and preserves content order"
func TestExtractPromptContent_NormalizesInlineImages(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(pngB64)
	got, err := harnessutil.ExtractPromptContent(userMessage(
		types.TextContent{Text: "Before"},
		types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "data:image/png;base64," + pngB64}},
		types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, Data: raw}},
		types.FileContent{MediaType: "image/png", Data: raw},
		types.ImageContent{Image: raw},
		types.ImageContent{URL: pngB64},
		types.TextContent{Text: "After"},
	))
	if err != nil {
		t.Fatal(err)
	}
	img := harnessutil.PromptContentPart{Type: "image", Data: pngB64, MediaType: "image/png"}
	want := []harnessutil.PromptContentPart{
		{Type: "text", Text: "Before"}, img, img, img, img, img, {Type: "text", Text: "After"},
	}
	if !reflect.DeepEqual(got.Parts, want) {
		t.Fatalf("parts = %+v\nwant   %+v", got.Parts, want)
	}
}

// TS: "rejects $name with a harness capability error"
func TestExtractPromptContent_Rejects(t *testing.T) {
	tests := []struct {
		name string
		part types.ContentPart
	}{
		{"remote image URLs", types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/a.png"}}},
		{"provider references", types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"test": "file_1"}}}},
		{"files of media type 'application/pdf'", types.FileContent{MediaType: "application/pdf", Data: []byte("x")}},
		{"data URLs that are not base64 encoded", types.FileContent{MediaType: "image/svg+xml", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "data:image/svg+xml,%3Csvg%3E%3C/svg%3E"}}},
		{"images whose media type cannot be detected", types.FileContent{MediaType: "image", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "aGVsbG8="}}},
		{"text data for images", types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeText, Text: "x"}}},
		{"content parts other than text, image, or file parts", types.ReasoningContent{Text: "x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := harnessutil.ExtractPromptContent(userMessage(tc.part))
			var unsupported *harness.CapabilityUnsupportedError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want CapabilityUnsupportedError", err)
			}
			if !strings.Contains(err.Error(), "The harness layer does not support "+tc.name) {
				t.Fatalf("err = %q", err)
			}
		})
	}
}

// TS: harnessPromptContentSchema accepts/rejects
func TestPromptContent_JSON(t *testing.T) {
	for _, in := range []string{`"hello"`, `[{"type":"text","text":"hello"}]`, `[{"type":"image","data":"iVBORw0KGgo=","mediaType":"image/png"}]`} {
		var c harnessutil.PromptContent
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		out, _ := json.Marshal(c)
		if string(out) != in {
			t.Fatalf("round trip %s -> %s", in, out)
		}
	}
	var c harnessutil.PromptContent
	if err := json.Unmarshal([]byte(`[{"type":"audio","data":"x"}]`), &c); err == nil {
		t.Fatal("expected audio part to be rejected")
	}
}
