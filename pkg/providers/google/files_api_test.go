package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/upload-session":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"file": map[string]interface{}{
					"name":      "files/abc123",
					"mimeType":  "application/pdf",
					"uri":       "https://generativelanguage.googleapis.com/v1beta/files/abc123",
					"state":     "ACTIVE",
					"sizeBytes": "1024",
				},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/pdf",
		Filename:  "f.pdf",
	})
	if err != nil {
		t.Fatalf("UploadFile() err = %v", err)
	}
	if res.ProviderReference["google"] == "" {
		t.Fatal("missing provider reference")
	}
	if len(res.Warnings) == 0 || res.Warnings[0].Feature != "filename" {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}

// TestFilesAPI_UploadFile_RespondsPromptlyToContextCancellation is a
// regression test: the PROCESSING poll loop checked the overall
// pollTimeoutMs deadline once per iteration, then called a plain
// time.Sleep(pollIntervalMs) instead of selecting on ctx.Done() during the
// wait. Cancelling the context mid-sleep went unnoticed until the sleep
// finished, delaying cancellation by up to a full poll interval.
func TestFilesAPI_UploadFile_RespondsPromptlyToContextCancellation(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/upload-session":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"file": map[string]interface{}{
					"name":  "files/abc123",
					"state": "PROCESSING",
				},
			})
		case r.URL.Path == "/files/abc123":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":  "files/abc123",
				"state": "PROCESSING",
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := p.Files().UploadFile(ctx, types.UploadFileOptions{
		Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType: "application/pdf",
		ProviderOptions: map[string]interface{}{
			"google": map[string]interface{}{
				"pollIntervalMs": float64(5000),
				"pollTimeoutMs":  float64(60000),
			},
		},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if elapsed > 1*time.Second {
		t.Fatalf("UploadFile took %v to notice ctx cancellation (expected well under the 5s poll interval)", elapsed)
	}
}
