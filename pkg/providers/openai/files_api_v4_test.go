package openai

import (
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/openai/src/files/openai-files.test.ts.

func TestFilesAPI_UploadFile_ExpiresAfterBracketedFields(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form = decodeMultipartFields(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-abc123","filename":"test.csv","bytes":1024,"created_at":1700000000,"purpose":"assistants","status":"processed"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"purpose": "assistants", "expiresAfter": float64(3600)},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if form["expires_after[anchor]"] != "created_at" || form["expires_after[seconds]"] != "3600" {
		t.Fatalf("form = %+v", form)
	}
	if _, ok := form["expires_after"]; ok {
		t.Fatal("should not send a bare expires_after field")
	}
}

func TestFilesAPI_UploadFile_OmitsExpiresAfterWhenNotRequested(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		form = decodeMultipartFields(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-abc123"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}},
		MediaType: "application/octet-stream",
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if _, ok := form["expires_after[anchor]"]; ok {
		t.Fatal("expires_after[anchor] should be omitted")
	}
	if _, ok := form["expires_after[seconds]"]; ok {
		t.Fatal("expires_after[seconds] should be omitted")
	}
}

func TestFilesAPI_UploadFile_ResultFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-exp1","object":"file","bytes":2048,"created_at":1700000000,"filename":"batch.jsonl","purpose":"batch","status":"processed","expires_at":1700172800}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1, 2, 3}},
		MediaType: "application/jsonl",
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if res.ByteSize == nil || *res.ByteSize != 2048 {
		t.Fatalf("ByteSize = %v", res.ByteSize)
	}
	if res.CreatedAt == nil || res.CreatedAt.Unix() != 1700000000 {
		t.Fatalf("CreatedAt = %v", res.CreatedAt)
	}
	if res.ExpiresAt == nil || res.ExpiresAt.Unix() != 1700172800 {
		t.Fatalf("ExpiresAt = %v", res.ExpiresAt)
	}
}

func TestFilesAPI_UploadFile_Stream(t *testing.T) {
	var gotContentType string
	var form map[string]string
	var fileName, fileContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		form, fileName, fileContent = decodeMultipartFieldsAndFile(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-stream1"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: strings.NewReader(`{"a":1}` + "\n" + `{"b":2}` + "\n")},
		MediaType: "application/jsonl",
		Filename:  "batch.jsonl",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"purpose": "batch", "expiresAfter": float64(172800)},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if res.ProviderReference["openai"] != "file-stream1" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data; boundary=ai-sdk-multipart-") {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	if form["purpose"] != "batch" || form["expires_after[anchor]"] != "created_at" || form["expires_after[seconds]"] != "172800" {
		t.Fatalf("form = %+v", form)
	}
	if fileName != "batch.jsonl" {
		t.Fatalf("filename = %q", fileName)
	}
	if fileContent != "{\"a\":1}\n{\"b\":2}\n" {
		t.Fatalf("file content = %q", fileContent)
	}
}

func TestFilesAPI_UploadFile_StreamDefaultsFilenameToBlob(t *testing.T) {
	var fileName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, fileName, _ = decodeMultipartFieldsAndFile(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-1"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: strings.NewReader("x")},
		MediaType: "application/jsonl",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"purpose": "batch"},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if fileName != "blob" {
		t.Fatalf("filename = %q, want blob", fileName)
	}
}

func TestFilesAPI_UploadFile_RejectsInvalidProviderOptionsBeforeRequest(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	closed := false
	stream := &closeTrackingReader{r: strings.NewReader("x"), onClose: func() { closed = true }}

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(t.Context(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: stream},
		MediaType: "application/jsonl",
		ProviderOptions: map[string]interface{}{
			"openai": map[string]interface{}{"expiresAfter": "not-a-number"},
		},
	})
	if err == nil {
		t.Fatal("expected an error for invalid providerOptions.openai.expiresAfter")
	}
	if called {
		t.Fatal("no HTTP request should have been made")
	}
	if !closed {
		t.Fatal("the stream should have been closed when providerOptions validation failed")
	}
}

func TestFilesAPI_GetFileMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/files/file-abc123" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-abc123","object":"file","bytes":1024,"created_at":1700000000,"filename":"test.jsonl","purpose":"batch","status":"processed","expires_at":1700172800}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	getter, ok := p.Files().(provider.FileMetadataGetter)
	if !ok {
		t.Fatal("openai FilesAPI should implement provider.FileMetadataGetter")
	}
	result, err := getter.GetFileMetadata(t.Context(), provider.FileMetadataOptions{
		File: types.ProviderReference{"openai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("GetFileMetadata: %v", err)
	}
	if result.ProviderReference["openai"] != "file-abc123" {
		t.Fatalf("ProviderReference = %+v", result.ProviderReference)
	}
	if result.Filename != "test.jsonl" {
		t.Fatalf("Filename = %q", result.Filename)
	}
	if result.ByteSize == nil || *result.ByteSize != 1024 {
		t.Fatalf("ByteSize = %v", result.ByteSize)
	}
	if result.ExpiresAt == nil || result.ExpiresAt.Unix() != 1700172800 {
		t.Fatalf("ExpiresAt = %v", result.ExpiresAt)
	}
}

func TestFilesAPI_GetFileMetadata_OmitsExpiresAtWhenNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-abc123","expires_at":null}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	getter := p.Files().(provider.FileMetadataGetter)
	result, err := getter.GetFileMetadata(t.Context(), provider.FileMetadataOptions{
		File: types.ProviderReference{"openai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("GetFileMetadata: %v", err)
	}
	if result.ExpiresAt != nil {
		t.Fatalf("ExpiresAt = %v, want nil", result.ExpiresAt)
	}
}

func TestFilesAPI_GetFileMetadata_RejectsBlankFileID(t *testing.T) {
	p := New(Config{APIKey: "k"})
	getter := p.Files().(provider.FileMetadataGetter)
	for _, id := range []string{"", "   "} {
		_, err := getter.GetFileMetadata(t.Context(), provider.FileMetadataOptions{
			File: types.ProviderReference{"openai": id},
		})
		if err == nil || !strings.Contains(err.Error(), "file reference is missing an 'openai' file id.") {
			t.Fatalf("id %q: err = %v", id, err)
		}
	}
}

func TestFilesAPI_GetFileMetadata_PreservesDotSegmentFileID(t *testing.T) {
	cases := []struct{ fileID, encoded string }{
		{".", "%252E"},
		{"..", "%252E%252E"},
	}
	for _, tc := range cases {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x"}`))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		getter := p.Files().(provider.FileMetadataGetter)
		_, err := getter.GetFileMetadata(t.Context(), provider.FileMetadataOptions{
			File: types.ProviderReference{"openai": tc.fileID},
		})
		srv.Close()
		if err != nil {
			t.Fatalf("fileID %q: %v", tc.fileID, err)
		}
		if gotPath != "/files/"+tc.encoded {
			t.Fatalf("fileID %q: path = %q, want /files/%s", tc.fileID, gotPath, tc.encoded)
		}
	}
}

func TestFilesAPI_DownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/files/file-abc123/content" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/jsonl; charset=utf-8")
		_, _ = w.Write([]byte("{\"result\":\"ok\"}\n"))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	downloader, ok := p.Files().(provider.FileDownloader)
	if !ok {
		t.Fatal("openai FilesAPI should implement provider.FileDownloader")
	}
	result, err := downloader.DownloadFile(t.Context(), provider.DownloadFileOptions{
		File: types.ProviderReference{"openai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	defer result.Content.Close()
	content, err := io.ReadAll(result.Content)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(content) != "{\"result\":\"ok\"}\n" {
		t.Fatalf("content = %q", string(content))
	}
	if result.MediaType != "application/jsonl" {
		t.Fatalf("MediaType = %q, want application/jsonl (parameters stripped)", result.MediaType)
	}
}

func TestFilesAPI_DeleteFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		if r.URL.Path != "/files/file-abc123" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-abc123","object":"file","deleted":true}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	deleter, ok := p.Files().(provider.FileDeleter)
	if !ok {
		t.Fatal("openai FilesAPI should implement provider.FileDeleter")
	}
	result, err := deleter.DeleteFile(t.Context(), provider.DeleteFileOptions{
		File: types.ProviderReference{"openai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if !result.Deleted {
		t.Fatal("Deleted = false")
	}
	if result.ProviderReference["openai"] != "file-abc123" {
		t.Fatalf("ProviderReference = %+v", result.ProviderReference)
	}
}

// --- test helpers ---------------------------------------------------------

func decodeMultipartFields(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	form, _, _ := decodeMultipartFieldsAndFile(t, r)
	return form
}

func decodeMultipartFieldsAndFile(t *testing.T, r *http.Request) (map[string]string, string, string) {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("ParseMediaType: %v", err)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	form := map[string]string{}
	var fileName, fileContent string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		data, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("read part: %v", err)
		}
		if part.FileName() != "" {
			fileName = part.FileName()
			fileContent = string(data)
			continue
		}
		form[part.FormName()] = string(data)
	}
	return form, fileName, fileContent
}

type closeTrackingReader struct {
	r       io.Reader
	onClose func()
}

func (c *closeTrackingReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *closeTrackingReader) Close() error {
	if c.onClose != nil {
		c.onClose()
	}
	return nil
}
