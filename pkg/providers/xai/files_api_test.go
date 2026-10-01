package xai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var teamIDSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files" {
			t.Fatalf("upload path = %q, want %q", r.URL.Path, "/files")
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("team_id") == "team-123" {
			teamIDSeen = true
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "file-xyz789",
			"filename":   "data.csv",
			"bytes":      512,
			"created_at": 1700000000,
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"teamId": "team-123"},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if !teamIDSeen {
		t.Fatal("expected team_id field")
	}
	if res.ProviderReference["xai"] != "file-xyz789" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
}

func TestFilesAPI_MetadataMatchesTypeScript(t *testing.T) {
	p := New(Config{APIKey: "k"})
	files, ok := p.Files().(*FilesAPI)
	if !ok {
		t.Fatalf("Files() = %T, want *FilesAPI", p.Files())
	}
	if files.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", files.SpecificationVersion())
	}
	if files.Provider() != "xai.files" {
		t.Fatalf("Provider() = %q, want xai.files", files.Provider())
	}
}

func TestFilesAPI_ValidatesTeamIDLikeTypeScript(t *testing.T) {
	p := New(Config{APIKey: "k"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"teamId": 123},
		},
	})
	if err == nil {
		t.Fatal("expected invalid teamId error")
	}
}

func TestFilesAPI_ErrorExtractsMessageLikeTypeScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Invalid file",
				"type":    "invalid_request_error",
				"code":    123,
			},
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
	})
	var providerErr *providererrors.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %T, want ProviderError", err)
	}
	if providerErr.Provider != "xai.files" {
		t.Fatalf("Provider = %q, want xai.files", providerErr.Provider)
	}
	if providerErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d, want 400", providerErr.StatusCode)
	}
	if providerErr.Message != "Invalid file" {
		t.Fatalf("Message = %q, want Invalid file", providerErr.Message)
	}
	if providerErr.ErrorCode != "123" {
		t.Fatalf("ErrorCode = %q, want 123", providerErr.ErrorCode)
	}
}

// cancelTrackingReader is an io.Reader + io.Closer test double that records
// whether Close was called, mirroring the TS ReadableStream `cancel` spy.
type cancelTrackingReader struct {
	r         io.Reader
	cancelled bool
}

func (c *cancelTrackingReader) Read(p []byte) (int, error) { return c.r.Read(p) }
func (c *cancelTrackingReader) Close() error {
	c.cancelled = true
	return nil
}

// TS: "should stream multipart uploads with fields preceding the file part"
func TestFilesAPI_UploadFile_StreamData(t *testing.T) {
	var fieldOrder []string
	var fileContent []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			fieldOrder = append(fieldOrder, part.FormName())
			content, _ := io.ReadAll(part)
			if part.FormName() == "file" {
				fileContent = content
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "file-stream1"})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	result, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: strings.NewReader("{\"a\":1}\n{\"b\":2}\n")},
		MediaType: "application/jsonl",
		Filename:  "batch.jsonl",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"expiresAfter": 172800, "teamId": "team-1"},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if result.ProviderReference["xai"] != "file-stream1" {
		t.Errorf("ProviderReference = %+v", result.ProviderReference)
	}
	wantOrder := []string{"expires_after", "team_id", "file"}
	if len(fieldOrder) != len(wantOrder) {
		t.Fatalf("field order = %v, want %v", fieldOrder, wantOrder)
	}
	for i, name := range wantOrder {
		if fieldOrder[i] != name {
			t.Fatalf("field order = %v, want %v", fieldOrder, wantOrder)
		}
	}
	if string(fileContent) != "{\"a\":1}\n{\"b\":2}\n" {
		t.Errorf("file content = %q, want the streamed chunks joined", fileContent)
	}
}

// TS: "should cancel stream data when expiresAfter is invalid"
func TestFilesAPI_UploadFile_StreamData_CancelledOnInvalidExpiresAfter(t *testing.T) {
	stream := &cancelTrackingReader{r: strings.NewReader("data")}

	p := New(Config{APIKey: "k"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeStream, Stream: stream},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"expiresAfter": 100},
		},
	})
	if err == nil {
		t.Fatal("expected an error for expiresAfter below the minimum")
	}
	if !stream.cancelled {
		t.Error("expected the stream to be closed/cancelled on validation failure")
	}
}

// TS: "should append expires_after before the file part"
func TestFilesAPI_UploadFile_ExpiresAfterPrecedesFile(t *testing.T) {
	var fieldOrder []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			fieldOrder = append(fieldOrder, part.FormName())
			_, _ = io.Copy(io.Discard, part)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "file-1"})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"expiresAfter": 172800, "teamId": "team-1"},
		},
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if len(fieldOrder) < 3 || fieldOrder[len(fieldOrder)-1] != "file" {
		t.Fatalf("field order = %v, want file part last", fieldOrder)
	}
}

// TS: "uploadFile (expiresAfter validation)"
func TestFilesAPI_UploadFile_RejectsInvalidExpiresAfter(t *testing.T) {
	p := New(Config{APIKey: "k"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/octet-stream",
		ProviderOptions: map[string]interface{}{
			"xai": map[string]interface{}{"expiresAfter": 100},
		},
	})
	if err == nil {
		t.Fatal("expected an error for expiresAfter below the minimum")
	}
}

// TS: "should retrieve file metadata via GET"
func TestFilesAPI_GetFileMetadata(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "file-abc123", "bytes": 1024, "created_at": 1700000000,
			"expires_at": 1700172800, "filename": "test.jsonl",
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	result, err := p.Files().(provider.FileMetadataGetter).GetFileMetadata(context.Background(), provider.FileMetadataOptions{
		File: types.ProviderReference{"xai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("GetFileMetadata() err = %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/files/file-abc123" {
		t.Errorf("path = %q, want /files/file-abc123", gotPath)
	}
	if result.ProviderReference["xai"] != "file-abc123" {
		t.Errorf("ProviderReference = %+v", result.ProviderReference)
	}
	if result.ByteSize == nil || *result.ByteSize != 1024 {
		t.Errorf("ByteSize = %v, want 1024", result.ByteSize)
	}
	if result.CreatedAt == nil || result.CreatedAt.Unix() != 1700000000 {
		t.Errorf("CreatedAt = %v, want unix 1700000000", result.CreatedAt)
	}
	if result.ExpiresAt == nil || result.ExpiresAt.Unix() != 1700172800 {
		t.Errorf("ExpiresAt = %v, want unix 1700172800", result.ExpiresAt)
	}
}

// TS: "should reject a blank xai file id"
func TestFilesAPI_GetFileMetadata_RejectsBlankFileID(t *testing.T) {
	p := New(Config{APIKey: "k"})
	for _, id := range []string{"", "   "} {
		_, err := p.Files().(provider.FileMetadataGetter).GetFileMetadata(context.Background(), provider.FileMetadataOptions{
			File: types.ProviderReference{"xai": id},
		})
		var invalidArg *providererrors.InvalidArgumentError
		if !errors.As(err, &invalidArg) {
			t.Fatalf("GetFileMetadata(%q) error = %v, want InvalidArgumentError", id, err)
		}
	}
}

// TS: "should reject a reference without an xai file id"
func TestFilesAPI_GetFileMetadata_RejectsMissingXAIKey(t *testing.T) {
	p := New(Config{APIKey: "k"})
	_, err := p.Files().(provider.FileMetadataGetter).GetFileMetadata(context.Background(), provider.FileMetadataOptions{
		File: types.ProviderReference{"openai": "file-abc123"},
	})
	if err == nil {
		t.Fatal("expected an error for a reference without an xai key")
	}
}

// TS: "should encode a dot-segment file id so it cannot retarget the path"
func TestFilesAPI_GetFileMetadata_EncodesDotSegments(t *testing.T) {
	cases := []struct {
		id, wantPath string
	}{
		{".", "/files/%252E"},
		{"..", "/files/%252E%252E"},
	}
	for _, tc := range cases {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.EscapedPath()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "file-1"})
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		_, err := p.Files().(provider.FileMetadataGetter).GetFileMetadata(context.Background(), provider.FileMetadataOptions{
			File: types.ProviderReference{"xai": tc.id},
		})
		srv.Close()
		if err != nil {
			t.Fatalf("GetFileMetadata(%q) err = %v", tc.id, err)
		}
		if gotPath != tc.wantPath {
			t.Errorf("path for id %q = %q, want %q", tc.id, gotPath, tc.wantPath)
		}
	}
}

// TS: "should download file content as a stream" +
// "should expose the response content type as mediaType (parameters stripped)"
func TestFilesAPI_DownloadFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/file-abc123/content" || r.Method != http.MethodGet {
			t.Errorf("request = %s %s, want GET /files/file-abc123/content", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/jsonl; charset=utf-8")
		_, _ = w.Write([]byte(`{"result":"ok"}` + "\n"))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	result, err := p.Files().(provider.FileDownloader).DownloadFile(context.Background(), provider.DownloadFileOptions{
		File: types.ProviderReference{"xai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("DownloadFile() err = %v", err)
	}
	defer result.Content.Close() //nolint:errcheck

	body, err := io.ReadAll(result.Content)
	if err != nil {
		t.Fatalf("read content: %v", err)
	}
	if string(body) != "{\"result\":\"ok\"}\n" {
		t.Errorf("content = %q, want the file body", body)
	}
	if result.MediaType != "application/jsonl" {
		t.Errorf("MediaType = %q, want application/jsonl (parameters stripped)", result.MediaType)
	}
}

// TS: "should omit mediaType when the response has no content type". Uses a
// raw TCP listener instead of httptest.Server because net/http's
// ResponseWriter sniffs and sets a Content-Type automatically when a handler
// never sets one, which would defeat this test.
func TestFilesAPI_DownloadFile_OmitsMediaTypeWhenAbsent(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close() //nolint:errcheck
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		if _, err := http.ReadRequest(bufio.NewReader(conn)); err != nil {
			return
		}
		body := []byte("bytes")
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", len(body))
		_, _ = conn.Write(body)
	}()

	p := New(Config{APIKey: "k", BaseURL: "http://" + ln.Addr().String()})
	result, err := p.Files().(provider.FileDownloader).DownloadFile(context.Background(), provider.DownloadFileOptions{
		File: types.ProviderReference{"xai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("DownloadFile() err = %v", err)
	}
	defer result.Content.Close() //nolint:errcheck
	<-done
	if result.MediaType != "" {
		t.Errorf("MediaType = %q, want empty", result.MediaType)
	}
}

// TS: "should delete a file via DELETE"
func TestFilesAPI_DeleteFile(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "file-abc123", "object": "file", "deleted": true})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	result, err := p.Files().(provider.FileDeleter).DeleteFile(context.Background(), provider.DeleteFileOptions{
		File: types.ProviderReference{"xai": "file-abc123"},
	})
	if err != nil {
		t.Fatalf("DeleteFile() err = %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/files/file-abc123" {
		t.Errorf("path = %q, want /files/file-abc123", gotPath)
	}
	if !result.Deleted {
		t.Error("Deleted = false, want true")
	}
	if result.ProviderReference["xai"] != "file-abc123" {
		t.Errorf("ProviderReference = %+v", result.ProviderReference)
	}
}
