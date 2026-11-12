package topaz

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r93toolsImageServer serves the Topaz image endpoints; the status body and
// the download handler are set per test.
func r93toolsImageServer(t *testing.T, status string, download http.HandlerFunc, form *map[string]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/image/v1/enhance-gen/async", func(w http.ResponseWriter, r *http.Request) {
		if form != nil {
			_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			mr := multipart.NewReader(r.Body, params["boundary"])
			got := map[string]string{}
			for {
				part, err := mr.NextPart()
				if err != nil {
					break
				}
				b, _ := io.ReadAll(part)
				key := part.FormName()
				if part.FileName() != "" {
					key += ":file"
				}
				got[key] = string(b)
			}
			*form = got
		}
		_, _ = w.Write([]byte(`{"process_id":"p"}`))
	})
	mux.HandleFunc("/image/v1/status/p", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(status)) })
	mux.HandleFunc("/image/v1/download/p", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"download_url":"` + srv.URL + `/file"}`))
	})
	mux.HandleFunc("/image/v1/cancel/p", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	if download != nil {
		mux.HandleFunc("/file", download)
	}
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TS topaz-image-model.ts download(): the image fetch is getFromApi with
// failedResponseHandler: topazFailedResponseHandler, so a non-2xx response
// from the download URL is an APICallError whose message comes from the Topaz
// error body (not a DownloadError).
func TestR93toolsTopazImageDownloadFailureIsTopazAPIError(t *testing.T) {
	srv := r93toolsImageServer(t, `{"status":"Completed"}`, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Reason", "expired")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Link expired","errorCode":"E1"}`))
	}, nil)
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ImageModelWonder35)
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Files:           []provider.ImageFile{inputImageFile()},
		ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMillis": 1}},
	})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) {
		t.Fatalf("err = %T %v, want a ProviderError", err, err)
	}
	var dl *providererrors.DownloadError
	if errors.As(err, &dl) {
		t.Fatalf("err = %v, still a DownloadError", err)
	}
	if perr.StatusCode != http.StatusForbidden || perr.Message != "Link expired (E1)" {
		t.Fatalf("status %d message %q", perr.StatusCode, perr.Message)
	}
}

// TS `status.status ?? 'unknown'`: an empty status string is not nullish, so
// the timeout message reports it as is.
func TestR93toolsTopazImageTimeoutEmptyStatus(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"status":""}`, "last status )"},
		{`{"status":null}`, "last status unknown)"},
	} {
		srv := r93toolsImageServer(t, tc.body, nil, nil)
		m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ImageModelWonder35)
		_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
			Files:           []provider.ImageFile{inputImageFile()},
			ProviderOptions: map[string]interface{}{"topaz": map[string]interface{}{"pollIntervalMillis": 10, "pollTimeoutMillis": 1}},
		})
		var te *Error
		if !errors.As(err, &te) || te.Name != "TOPAZ_IMAGE_ENHANCEMENT_TIMEOUT" || !strings.Contains(te.Message, tc.want) {
			t.Fatalf("%s: err = %v", tc.body, err)
		}
	}
}

// TS buildFormData: `file.type === 'url'` sends source_url (even an empty
// one) and never uploads image bytes.
func TestR93toolsTopazImageURLFileAlwaysSourceURL(t *testing.T) {
	var form map[string]string
	srv := r93toolsImageServer(t, `{"status":"Failed"}`, nil, &form)
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), ImageModelWonder35)
	_, _ = m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{
		Files: []provider.ImageFile{{Type: "url", URL: ""}},
	})
	if v, ok := form["source_url"]; !ok || v != "" {
		t.Fatalf("form = %v, want source_url \"\"", form)
	}
	if _, ok := form["image:file"]; ok {
		t.Fatalf("form = %v, uploaded an image part", form)
	}
}

// TS doStart: `bytes = input.type === 'file' ? fileBytes(input) : undefined`
// and `if (bytes != null) uploadVideo(...)`: an empty file is still uploaded.
func TestR93toolsTopazVideoEmptyFileUploaded(t *testing.T) {
	ts := newVideoTestServer(t)
	defer ts.server.Close()
	opts := defaultStartOptions()
	opts.InputReferences = []provider.VideoModelV3File{{Type: "file", MediaType: "video/mp4"}}
	if _, err := ts.model().DoStart(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.uploadHeaders == nil {
		t.Fatal("the empty input video was not uploaded")
	}
	if size := ts.lastExpressReq["source"].(map[string]interface{})["size"]; size != 0.0 {
		t.Fatalf("source.size = %v, want 0", size)
	}
}

// prova CR r93: TS resolves config.headers (the apiKey callback) inside the
// request and rethrows its error as is, so an APIKeyFunc error comes back
// unwrapped (no internal FetchRejection marker) from the image submit, the
// image status and download polls and the video status.
func TestR93toolsTopazAPIKeyFuncErrorAsIs(t *testing.T) {
	sentinel := errors.New("no key")
	calls := 0
	srv := r93toolsImageServer(t, `{"status":"Completed"}`, nil, nil)
	keyFunc := func(context.Context) (string, error) {
		calls++
		return "", sentinel
	}
	m := NewImageModel(New(Config{BaseURL: srv.URL, APIKeyFunc: keyFunc}), ImageModelWonder35)
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Files: []provider.ImageFile{inputImageFile()}}); err != sentinel {
		t.Fatalf("image submit err = %T %v, want the key error itself", err, err)
	}
	if _, err := m.getStatus(context.Background(), "p", nil); err != sentinel {
		t.Fatalf("image status err = %T %v", err, err)
	}
	if _, _, err := m.download(context.Background(), "p", nil); err != sentinel {
		t.Fatalf("image download err = %T %v", err, err)
	}
	vm := NewVideoModel(New(Config{BaseURL: srv.URL, APIKeyFunc: keyFunc}), VideoModelStarlightPrecise26)
	if _, err := vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"requestId":"r"}`)}); err != sentinel {
		t.Fatalf("video status err = %T %v", err, err)
	}
}
