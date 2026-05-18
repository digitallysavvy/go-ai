package xai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var teamIDSeen bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/files" {
			t.Fatalf("upload path = %q, want %q", r.URL.Path, "/v1/files")
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
