package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports TS google-files.test.ts "should preserve $name in the polling URL"
// (7de3612): provider-returned file names are path-encoded before reuse in the
// credentialed polling URL.
func TestFilesAPI_PollingURLEncodesProviderFileName(t *testing.T) {
	tests := []struct{ name, expectedPath string }{
		{"files/abc123", "/files/abc123"},
		{"files/abc/../../secret", "/files%2Fabc%2F..%2F..%2Fsecret"},
		{"files/.", "/files/%252E"},
		{"files/..", "/files/%252E%252E"},
		{".", "/%252E"},
		{"..", "/%252E%252E"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var srv *httptest.Server
			var polledPath string
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/upload/v1beta/files":
					w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
				case "/upload-session":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"file": map[string]interface{}{"name": tt.name, "state": "PROCESSING", "uri": "u"},
					})
				default:
					polledPath = r.URL.EscapedPath()
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"name": tt.name, "state": "ACTIVE", "uri": "u"})
				}
			}))
			defer srv.Close()

			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
				Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
				MediaType:       "application/pdf",
				ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": float64(1)}},
			})
			if err != nil {
				t.Fatalf("UploadFile() err = %v", err)
			}
			if polledPath != tt.expectedPath {
				t.Fatalf("polled %q, want %q", polledPath, tt.expectedPath)
			}
		})
	}
}
