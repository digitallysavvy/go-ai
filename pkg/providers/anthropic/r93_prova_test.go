package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS map-anthropic-citation.ts: `id: citation.file_id ??
// String(citation.document_index)` keeps a present empty file_id, and
// `documents[citation.document_index]` names no document for a fractional
// index (title `Document ${index}` in JS number form); createCitationSource
// returns no source for it.
func TestR93provaCitationIndexAndFileID(t *testing.T) {
	docs := []citationDocument{{Title: "A", Filename: "a.pdf", MediaType: "application/pdf"}, {Title: "B", MediaType: "text/plain"}}

	got := mapAnthropicCitation(map[string]interface{}{
		"type": "char_location", "cited_text": "x", "document_index": float64(0), "document_title": nil,
		"start_char_index": float64(0), "end_char_index": float64(1), "file_id": "",
	}, docs)
	if got.Source.ID != "" || got.Source.Title != "A" || got.Source.MediaType != "application/pdf" {
		t.Errorf("empty file_id: source = %+v, want id \"\" and document A", got.Source)
	}

	got = mapAnthropicCitation(map[string]interface{}{
		"type": "page_location", "cited_text": "x", "document_index": 0.5, "document_title": nil,
		"start_page_number": float64(1), "end_page_number": float64(2),
	}, docs)
	if got.Source.ID != "0.5" || got.Source.Title != "Document 0.5" || got.Source.MediaType != "text/plain" || got.Source.Filename != "" {
		t.Errorf("fractional index: source = %+v, want id/title of index 0.5 and no document", got.Source)
	}

	if src, ok := createCitationSource(map[string]interface{}{
		"type": "page_location", "cited_text": "x", "document_index": 0.5, "document_title": nil,
		"start_page_number": float64(1), "end_page_number": float64(2),
	}, docs, func() string { return "id" }); ok {
		t.Errorf("fractional index: createCitationSource = %+v, want no source", src)
	}
}

// TS doGenerate / convertAnthropicMessageMetadata: stop_details fields are
// kept when `!= null` (an empty string included), skills carry `version:
// skill.version` and an iteration keeps a present (empty) model.
func TestR93provaMetadataKeepsEmptyStrings(t *testing.T) {
	body := `{"type":"message","id":"m","model":"claude","content":[{"type":"text","text":"hi"}],
"stop_reason":"refusal","stop_details":{"type":"refusal","category":"","explanation":null},
"container":{"expires_at":"2026-01-01T00:00:00Z","id":"c1","skills":[{"type":"anthropic","skill_id":"pptx","version":""}]},
"usage":{"input_tokens":1,"output_tokens":2,"iterations":[{"type":"message","model":"","input_tokens":1,"output_tokens":2}]}}`
	var response anthropicResponse
	if err := json.Unmarshal([]byte(body), &response); err != nil {
		t.Fatal(err)
	}
	m := &LanguageModel{provider: New(Config{APIKey: "k"})}
	meta := m.convertResponse(response, false).ProviderMetadata["anthropic"].(map[string]interface{})

	stop, _ := meta["stopDetails"].(map[string]interface{})
	if v, ok := stop["category"]; !ok || v != "" {
		t.Errorf("stopDetails = %#v, want category \"\"", stop)
	}
	if _, ok := stop["explanation"]; ok {
		t.Errorf("stopDetails = %#v, want no explanation for null", stop)
	}
	skills, _ := meta["container"].(map[string]interface{})["skills"].([]map[string]interface{})
	if len(skills) != 1 {
		t.Fatalf("skills = %#v", meta["container"])
	}
	if v, ok := skills[0]["version"]; !ok || v != "" {
		t.Errorf("skill = %#v, want version \"\"", skills[0])
	}
	iterations, _ := meta["iterations"].([]map[string]interface{})
	if len(iterations) != 1 {
		t.Fatalf("iterations = %#v", meta["iterations"])
	}
	if v, ok := iterations[0]["model"]; !ok || v != "" {
		t.Errorf("iteration = %#v, want model \"\"", iterations[0])
	}
}

// TS AnthropicFiles / AnthropicSkills: an error thrown while resolving the
// request headers (an API key callback) reaches the caller as is; the Go
// FetchRejection marker must not wrap it.
func TestR93provaFilesSkillsReturnHeaderErrorAsIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL.Path)
	}))
	defer srv.Close()
	keyErr := errors.New("r93prova key store down")
	p := New(Config{BaseURL: srv.URL, APIKeyFunc: func(context.Context) (string, error) { return "", keyErr }})
	ref := types.ProviderReference{"anthropic": "file_1"}

	files := p.Files().(*FilesAPI)
	_, err := files.GetFileMetadata(context.Background(), provider.FileMetadataOptions{File: ref})
	if err != keyErr {
		t.Errorf("GetFileMetadata error = %T %v, want the callback error itself", err, err)
	}
	_, err = files.DeleteFile(context.Background(), provider.DeleteFileOptions{File: ref})
	if err != keyErr {
		t.Errorf("DeleteFile error = %T %v, want the callback error itself", err, err)
	}
	_, err = files.DownloadFile(context.Background(), provider.DownloadFileOptions{File: ref})
	if err != keyErr {
		t.Errorf("DownloadFile error = %T %v, want the callback error itself", err, err)
	}
	_, err = p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain"})
	if err != keyErr {
		t.Errorf("UploadFile error = %T %v, want the callback error itself", err, err)
	}
	_, err = p.Skills().(*SkillsAPI).UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "s/SKILL.md", Data: types.FileData{Type: types.FileDataTypeText, Text: "x"}}},
	})
	if err != keyErr {
		t.Errorf("UploadSkill error = %T %v, want the callback error itself", err, err)
	}
}
