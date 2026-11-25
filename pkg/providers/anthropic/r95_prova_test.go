package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provaSkillStreamUploadUnsupported: TS uploadSkill runs
// convertInlineFileDataToUint8Array on every file, which cancels a stream
// and throws UnsupportedFunctionalityError('streaming file upload') before
// any request.
func TestR95provaSkillStreamUploadUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, APIKey: "k"})
	stream := &r94provaClosingReader{Reader: strings.NewReader("data")}
	_, err := p.Skills().(*SkillsAPI).UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{
			{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}},
			{Path: "s/data.bin", Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream}},
		},
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) || unsupported.Functionality != "streaming file upload" ||
		err.Error() != "'streaming file upload' functionality not supported." {
		t.Fatalf("err = %T %v", err, err)
	}
	if !stream.closed {
		t.Fatal("stream not released")
	}
}

// TestR95provaCitationDocumentMediaType: the citation documents are the
// citation-enabled PDF / text file parts the converter sends, whichever
// field carries the media type (MediaType, the deprecated MimeType, or
// FileData.MediaType), so a page_location citation resolves to a document
// source (TS extractCitationDocuments reads the part's mediaType).
func TestR95provaCitationDocumentMediaType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"m",
			"content":[{"type":"text","text":"Growth.","citations":[{"type":"page_location","cited_text":"Revenue up",
			"document_index":0,"document_title":null,"start_page_number":5,"end_page_number":6}]}],
			"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":3}}`))
	}))
	defer srv.Close()
	citations := map[string]interface{}{"anthropic": map[string]interface{}{"citations": map[string]interface{}{"enabled": true}}}
	for name, part := range map[string]types.FileContent{
		"MimeType": {MimeType: "application/pdf", Data: []byte("%PDF-1.4"), Filename: "report.pdf", ProviderOptions: citations},
		"FileData": {FileData: types.FileData{Type: types.FileDataTypeData, Data: []byte("%PDF-1.4"), MediaType: "application/pdf"}, Filename: "report.pdf", ProviderOptions: citations},
	} {
		t.Run(name, func(t *testing.T) {
			model := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ClaudeSonnet4_5, nil)
			result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{part, types.TextContent{Text: "q"}}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var source *types.SourceContent
			for _, c := range result.Content {
				if s, ok := c.(types.SourceContent); ok {
					source = &s
				}
			}
			if source == nil || source.SourceType != "document" || source.MediaType != "application/pdf" ||
				source.Title != "report.pdf" || source.Filename != "report.pdf" {
				t.Fatalf("source = %+v, content = %#v", source, result.Content)
			}
			text, _ := result.Content[0].(types.TextContent)
			if len(text.Citations) != 1 || text.Citations[0].Source.MediaType != "application/pdf" || text.Citations[0].Source.Title != "report.pdf" {
				t.Fatalf("citations = %#v", text.Citations)
			}
		})
	}
}
