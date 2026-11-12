package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r94provgFilesServer serves the resumable start, the upload (with the given
// status and body) and nothing else.
func r94provgFilesServer(t *testing.T, startStatus, uploadStatus int, uploadBody string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			w.Header().Set("X-Goog-Upload-URL", srv.URL+"/data")
			w.WriteHeader(startStatus)
		case "/data":
			w.WriteHeader(uploadStatus)
			_, _ = w.Write([]byte(uploadBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func r94provgUpload(t *testing.T, srv *httptest.Server) error {
	t.Helper()
	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf"})
	return err
}

// google-files.ts checks `response.ok` on both raw fetch steps: an unfollowed
// 3xx (no Location) fails the upload like a 4xx.
func TestR94provgFilesNon2xxFails(t *testing.T) {
	done := `{"file":{"name":"files/a","mimeType":"application/pdf","uri":"u","state":"ACTIVE"}}`
	err := r94provgUpload(t, r94provgFilesServer(t, 304, 200, done))
	if err == nil || !strings.Contains(err.Error(), "Failed to initiate resumable upload: 304") {
		t.Fatalf("start 304: err = %v", err)
	}
	err = r94provgUpload(t, r94provgFilesServer(t, 200, 300, done))
	if err == nil || !strings.Contains(err.Error(), "Failed to upload file data: 300") {
		t.Fatalf("upload 300: err = %v", err)
	}
	if err := r94provgUpload(t, r94provgFilesServer(t, 200, 200, done)); err != nil {
		t.Fatalf("2xx: %v", err)
	}
}

// TS reads uploadResult.file.state: an upload body without a file object is
// the TypeError, not an empty result.
func TestR94provgFilesUploadBodyWithoutFile(t *testing.T) {
	for body, want := range map[string]string{
		`{}`:            "Cannot read properties of undefined (reading 'state')",
		`{"file":null}`: "Cannot read properties of null (reading 'state')",
		`null`:          "Cannot read properties of null (reading 'file')",
		`[]`:            "Cannot read properties of undefined (reading 'state')",
	} {
		err := r94provgUpload(t, r94provgFilesServer(t, 200, 200, body))
		if err == nil || err.Error() != want {
			t.Errorf("body %s: err = %v, want %q", body, err, want)
		}
	}
}
