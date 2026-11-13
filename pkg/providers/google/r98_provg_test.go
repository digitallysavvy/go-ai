package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS google-files.ts polls with getFromApi, which tags the user-agent with
// the runtime (withUserAgentSuffix(..., getRuntimeEnvironmentUserAgent()));
// the initiation is a raw fetchFn call with getHeaders() only.
func TestR98provgFilesPollCarriesRuntimeUserAgent(t *testing.T) {
	var initUA, pollUA string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/upload/v1beta/files":
			initUA = r.Header.Get("User-Agent")
			w.Header().Set("x-goog-upload-url", srv.URL+"/upload-session")
		case "/upload-session":
			_, _ = w.Write([]byte(`{"file":{"name":"files/a","mimeType":"text/plain","uri":"u","state":"PROCESSING"}}`))
		case "/v1beta/files/a":
			pollUA = r.Header.Get("User-Agent")
			_, _ = w.Write([]byte(`{"name":"files/a","mimeType":"text/plain","uri":"u","state":"ACTIVE"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	if _, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data:            types.FileData{Type: types.FileDataTypeData, Data: []byte{1}},
		MediaType:       "text/plain",
		ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"pollIntervalMs": 1}},
	}); err != nil {
		t.Fatal(err)
	}
	rt := providerutils.RuntimeEnvironmentUserAgent()
	if strings.Contains(initUA, rt) {
		t.Fatalf("raw-fetch initiation carries the runtime tag: %q", initUA)
	}
	if !strings.Contains(pollUA, "ai-sdk-google/") || !strings.HasSuffix(pollUA, " "+rt) {
		t.Fatalf("poll user-agent = %q, want the provider tag and %q", pollUA, rt)
	}
}

// TS google-batch.ts sends the resumable upload initiation with
// postJsonToApi, the upload with postToApi (only its own headers) and the
// results download with getFromApi: each tags the user-agent with the
// runtime, like the batch create.
func TestR98provgBatchUploadAndDownloadCarryRuntimeUserAgent(t *testing.T) {
	rt := providerutils.RuntimeEnvironmentUserAgent()
	var initUA, uploadUA, createUA, downloadUA string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
		initUA = r.Header.Get("User-Agent")
		w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session-123")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/upload/v1beta/files/session-123", func(w http.ResponseWriter, r *http.Request) {
		uploadUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"file":{"name":"files/batch-input"}}`))
	})
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		createUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	})
	mux.HandleFunc("/v1beta/batches/batch-123", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output"}}}`))
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		downloadUA = r.Header.Get("User-Agent")
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	b := p.ExperimentalBatch()
	longPrompt := strings.Repeat("a", googleBatchInlineCreationMaxBytes+1000)
	if _, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("big", "gemini-2.5-flash", longPrompt)},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	_ = stream.Close()

	want := "ai-sdk-google/" // provider tag first, then the runtime
	for name, ua := range map[string]string{"create": createUA, "init": initUA, "download": downloadUA} {
		if !strings.HasPrefix(ua, want) || !strings.HasSuffix(ua, " "+rt) {
			t.Errorf("%s user-agent = %q, want %s... %s", name, ua, want, rt)
		}
	}
	if uploadUA != rt {
		t.Errorf("upload user-agent = %q, want %q", uploadUA, rt)
	}
}
