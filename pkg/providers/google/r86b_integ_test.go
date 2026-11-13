package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r86b-tools-to-prova 2 (applied by the integrator): TS reads
// `initResponse.headers.get('x-goog-upload-url')` and passes it to fetch,
// which parses it with new URL: a raw UTF-8 byte reads as Latin-1 and is
// percent-encoded (`/up/caf\xc3\xa9` -> /up/caf%C3%83%C2%A9), and a value
// that does not parse is a TypeError. Go ignored the http.NewRequest error
// and dereferenced the nil request (panic).
func TestR86bintegFilesUploadURLReadsAsFetch(t *testing.T) {
	var mu sync.Mutex
	var uploads []string
	uploadURL := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upload/v1beta/files" {
			mu.Lock()
			w.Header()["X-Goog-Upload-Url"] = []string{uploadURL}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		mu.Lock()
		uploads = append(uploads, r.URL.EscapedPath())
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	api := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"}).Files()
	upload := func(u string) error {
		mu.Lock()
		uploadURL, uploads = u, nil
		mu.Unlock()
		_, err := api.UploadFile(context.Background(), types.UploadFileOptions{
			Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}, MediaType: "text/plain", Filename: "a.txt",
		})
		return err
	}

	_ = upload(srv.URL + "/up/caf\xc3\xa9")
	mu.Lock()
	if len(uploads) != 1 || uploads[0] != "/up/caf%C3%83%C2%A9" {
		t.Errorf("upload requested %v, want /up/caf%%C3%%83%%C2%%A9 (TS)", uploads)
	}
	mu.Unlock()

	err := upload("http://[::1")
	if err == nil || !strings.Contains(err.Error(), "Failed to parse URL from http://[::1") {
		t.Errorf("invalid upload URL: err = %v, want the fetch TypeError text", err)
	}
}

// cr-r86b-prova-to-carry (prova's known difference "Google Live API key with a
// base URL that carries a query string"): TS url.searchParams.set('key')
// keeps the base URL's own parameters in order with URLSearchParams escaping.
func TestR86bintegLiveKeyKeepsBaseQueryOrder(t *testing.T) {
	for name, got := range map[string]string{
		"transcription": r94integURL(getLiveTranscriptionWebSocketURL("https://h.example/v1beta?b=1%202&a=~", "k~1")),
	} {
		if !strings.HasSuffix(got, "?b=1+2&a=%7E&key=k%7E1") {
			t.Errorf("%s URL = %s, want query b=1+2&a=%%7E&key=k%%7E1 (TS)", name, got)
		}
	}
}
