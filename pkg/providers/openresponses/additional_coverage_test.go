package openresponses

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestOpenResponsesModelMetadataAndErrorWrap(t *testing.T) {
	t.Parallel()

	p := New(Config{BaseURL: "http://localhost:1234/v1", Name: "open-responses"})
	m := NewLanguageModel(p, "local-model")
	if m.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q", m.SpecificationVersion())
	}
	if m.Provider() != "open-responses.responses" || m.ModelID() != "local-model" {
		t.Fatalf("provider/model mismatch: %s/%s", m.Provider(), m.ModelID())
	}
	if !m.SupportsTools() || !m.SupportsStructuredOutput() || !m.SupportsImageInput() {
		t.Fatal("model capability flags should all be true")
	}
	if urls := m.SupportedURLs(); len(urls["image/*"]) != 1 || urls["image/*"][0] != `^https?://.*$` {
		t.Fatalf("SupportedURLs() = %#v", urls)
	}
	if err := m.handleError(io.EOF); err == nil || !strings.Contains(err.Error(), "open-responses provider error") {
		t.Fatalf("handleError mismatch: %v", err)
	}
}

func TestOpenResponsesDoGenerateAndDoStream(t *testing.T) {
	t.Parallel()

	var mode string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1",
			"output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],
			"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}
		}`))
	}))
	defer srv.Close()

	p := New(Config{BaseURL: srv.URL, Name: "open-responses"})
	m := NewLanguageModel(p, "local-model")

	gen, err := m.DoGenerate(t.Context(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if gen.Text != "done" {
		t.Fatalf("DoGenerate text = %q, want done", gen.Text)
	}

	mode = "stream"
	stream, err := m.DoStream(t.Context(), &provider.GenerateOptions{
		Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}},
	})
	if err != nil {
		t.Fatalf("DoStream() error = %v", err)
	}
	defer func() { _ = stream.Close() }()

	var sawText, sawFinish bool
	for {
		chunk, err := stream.Next()
		if err != nil {
			break
		}
		if chunk.Type == provider.ChunkTypeText && chunk.Text == "hello" {
			sawText = true
		}
		if chunk.Type == provider.ChunkTypeFinish {
			sawFinish = true
		}
	}
	if !sawText || !sawFinish {
		t.Fatalf("stream did not produce expected chunks (text=%v finish=%v)", sawText, sawFinish)
	}
}

func TestOpenResponsesConvertUserAndFileHelpers(t *testing.T) {
	t.Parallel()

	imageURL := convertImageContentToURL(types.ImageContent{Image: []byte{0xFF, 0xD8}, MimeType: "image/jpeg"}, nil)
	if !strings.HasPrefix(imageURL, "data:image/jpeg;base64,") {
		t.Fatalf("convertImageContentToURL mismatch: %q", imageURL)
	}

	fileURL := convertFileToImageURL(types.FileContent{Text: "abc", MediaType: "image/png"}, nil)
	if !strings.HasPrefix(fileURL, "data:image/png;base64,") {
		t.Fatalf("convertFileToImageURL mismatch: %q", fileURL)
	}

	_, err := convertUserContent([]types.ContentPart{
		types.TextContent{Text: "hello"},
		types.FileContent{
			FileData: types.FileData{
				Type:      types.FileDataTypeReference,
				Reference: map[string]string{"open-responses": "file_abc"},
			},
			MediaType: "application/pdf",
		},
	}, &[]types.Warning{}, "open-responses")
	if err == nil || !strings.Contains(err.Error(), "provider references are not supported") {
		t.Fatalf("convertUserContent() error = %v, want unsupported provider reference", err)
	}
}

func TestOpenResponsesToolResultConverters(t *testing.T) {
	t.Parallel()

	warnings := []types.Warning{}
	out, err := convertStructuredToolResultOutput(types.ToolResultOutput{
		Type: types.ToolResultOutputContent,
		Content: []types.ToolResultContentBlock{
			types.FileContentBlock{FileData: types.FileData{Type: types.FileDataTypeText, Text: "note"}},
			types.FileContentBlock{URL: "https://example.com/a.bin", MediaType: "application/octet-stream"},
		},
	}, &warnings, "open-responses")
	if err != nil {
		t.Fatalf("convertStructuredToolResultOutput() error = %v", err)
	}
	parts, ok := out.([]interface{})
	if !ok || len(parts) < 1 {
		t.Fatalf("expected structured output parts, got %#v", out)
	}

	// Validate stream wrapper methods Read/Close/Err paths.
	s := newOpenResponsesStream(io.NopCloser(strings.NewReader("")), nil)
	buf := make([]byte, 1)
	_, _ = s.Read(buf)
	_ = s.Close()
	if s.Err() != nil {
		t.Fatalf("Err() should be nil when stream ended cleanly")
	}
}

func TestMapOpenResponsesFinishReason_DefaultBranch(t *testing.T) {
	t.Parallel()
	if got := MapOpenResponsesFinishReason("something_else", false); got != types.FinishReasonOther {
		t.Fatalf("unexpected mapping for unknown reason: %v", got)
	}
}

func TestOpenResponsesConvertToolFileContentBlockWarnings(t *testing.T) {
	t.Parallel()
	warnings := []types.Warning{}
	_, ok, err := convertToolFileContentBlock(types.FileContentBlock{}, &warnings, "open-responses")
	if err != nil {
		t.Fatalf("convertToolFileContentBlock() error = %v", err)
	}
	if ok {
		t.Fatal("expected no converted block for empty file content")
	}
	if len(warnings) == 0 {
		t.Fatal("expected warning for unsupported empty file content block")
	}
}
