package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilesAPI_UploadFile(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
			w.WriteHeader(http.StatusOK)
		case "/upload-session":
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
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
			w.WriteHeader(http.StatusOK)
		case "/upload-session":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"file": map[string]interface{}{
					"name":  "files/abc123",
					"state": "PROCESSING",
				},
			})
		case "/files/abc123":
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

// Call headers reach the upload, int poll options are honored, and a failed
// status poll is an error (previously the empty error body ended the loop as
// if the file were ready).
func TestFilesAPI_UploadFile_HeadersIntPollOptionsAndPollFailure(t *testing.T) {
	var srv *httptest.Server
	var initHeader string
	polls := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			initHeader = r.Header.Get("X-Custom")
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"file": map[string]interface{}{"name": "files/abc", "state": "PROCESSING"}})
		case "/files/abc":
			polls++
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":{"code":403,"message":"nope","status":"PERMISSION_DENIED"}}`))
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	start := time.Now()
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType:       "application/pdf",
		Headers:         map[string]string{"X-Custom": "yes"},
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 10}},
	})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err = %v, want the poll failure", err)
	}
	if initHeader != "yes" {
		t.Fatalf("X-Custom on init = %q", initHeader)
	}
	if time.Since(start) > time.Second || polls != 1 {
		t.Fatalf("int pollIntervalMs ignored (elapsed %v, polls %d)", time.Since(start), polls)
	}
}

// Ports of google-files.test.ts "error handling" and "response metadata":
// failures carry the TS messages and error codes, and absent optional fields
// are left out of providerMetadata.
func TestFilesAPI_UploadFile_TSErrorsAndMetadata(t *testing.T) {
	upload := func(initStatus, uploadStatus int, noURL bool, file string) (*types.UploadFileResult, error) {
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/upload/v1beta/files":
				if initStatus != 200 {
					http.Error(w, "boom", initStatus)
					return
				}
				if !noURL {
					w.Header().Set("x-goog-upload-url", srv.URL+"/resume")
				}
			case "/resume":
				if uploadStatus != 200 {
					http.Error(w, "boom", uploadStatus)
					return
				}
				_, _ = w.Write([]byte(`{"file":` + file + `}`))
			}
		}))
		defer srv.Close()
		return New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).Files().UploadFile(context.Background(), types.UploadFileOptions{
			Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/octet-stream",
		})
	}
	const minimal = `{"name":"files/minimal","mimeType":"text/plain","uri":"https://x/files/minimal","state":"ACTIVE"}`

	for name, tc := range map[string]struct {
		init, up int
		noURL    bool
		file     string
		message  string
		code     string
	}{
		"initiation fails": {500, 200, false, minimal, "Failed to initiate resumable upload: 500 boom", "GOOGLE_FILES_UPLOAD_ERROR"},
		"no upload url":    {200, 200, true, minimal, "No upload URL returned from initiation request", "GOOGLE_FILES_UPLOAD_ERROR"},
		"upload fails":     {200, 500, false, minimal, "Failed to upload file data: 500 boom", "GOOGLE_FILES_UPLOAD_ERROR"},
		"processing fails": {200, 200, false, `{"name":"files/bad","mimeType":"text/plain","uri":"u","state":"FAILED"}`, "File processing failed for files/bad", "GOOGLE_FILES_UPLOAD_FAILED"},
	} {
		_, err := upload(tc.init, tc.up, tc.noURL, tc.file)
		var perr *providerrors.ProviderError
		if !errors.As(err, &perr) || !strings.HasPrefix(perr.Message, tc.message) || perr.ErrorCode != tc.code {
			t.Errorf("%s: err = %v, want %q with code %s", name, err, tc.message, tc.code)
		}
	}

	res, err := upload(200, 200, false, minimal)
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["google"].(map[string]interface{})
	for _, absent := range []string{"displayName", "sizeBytes", "createTime", "updateTime", "expirationTime", "sha256Hash"} {
		if _, ok := meta[absent]; ok {
			t.Errorf("providerMetadata.google must not carry %s: %v", absent, meta)
		}
	}
}

// TS parseProviderOptions rejects non-positive/non-number poll options and a
// non-string displayName with "invalid google provider options" before any request.
func TestFilesAPI_UploadFile_InvalidProviderOptions(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	for name, g := range map[string]interface{}{
		"zero interval":    map[string]interface{}{"pollIntervalMs": 0},
		"negative timeout": map[string]interface{}{"pollTimeoutMs": float64(-5)},
		"string interval":  map[string]interface{}{"pollIntervalMs": "10"},
		"number name":      map[string]interface{}{"displayName": 3},
		"not an object":    "x",
	} {
		_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
			Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
			MediaType:       "application/pdf",
			ProviderOptions: map[string]interface{}{"google": g},
		})
		var ia *providerrors.InvalidArgumentError
		if !errors.As(err, &ia) || !strings.Contains(ia.Message, "invalid google provider options") {
			t.Errorf("%s: err = %v, want InvalidArgumentError", name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

// TS sends display_name whenever displayName != null, including "".
func TestFilesAPI_UploadFile_EmptyDisplayNameIsSent(t *testing.T) {
	var srv *httptest.Server
	var initBody string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			b, _ := io.ReadAll(r.Body)
			initBody = string(b)
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"file": map[string]interface{}{"name": "files/a", "mimeType": "text/plain", "uri": "u", "state": "ACTIVE"}})
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType:       "text/plain",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"displayName": ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if initBody != `{"file":{"display_name":""}}` {
		t.Fatalf("init body = %s", initBody)
	}
}
