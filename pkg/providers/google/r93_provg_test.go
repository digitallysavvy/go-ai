package google

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS mapGoogleBatchStatus strips one BATCH_STATE_ or JOB_STATE_ prefix
// (/^(?:BATCH|JOB)_STATE_/), never both.
func TestR93provgBatchStatusSinglePrefix(t *testing.T) {
	for raw, want := range map[string]provider.BatchStatusValue{
		"BATCH_STATE_SUCCEEDED":           provider.BatchStatusCompleted,
		"JOB_STATE_FAILED":                provider.BatchStatusFailed,
		"BATCH_STATE_JOB_STATE_SUCCEEDED": provider.BatchStatusPending,
	} {
		if got := mapGoogleBatchStatus(raw, true, true, false); got != want {
			t.Fatalf("%s: got %s, want %s", raw, got, want)
		}
	}
}

// TS convertGoogleImageBatchResult takes every inline image file part, an
// empty payload included, so the item is an image result rather than a
// failed text item with an unsupported "file" block.
func TestR93provgBatchEmptyInlineImageIsImageResult(t *testing.T) {
	b := NewBatch(New(Config{APIKey: "k"}))
	resp := json.RawMessage(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":""}}]},"finishReason":"STOP"}]}`)
	item := b.convertGoogleBatchResultLine("k", resp, nil)
	if item.Status != provider.BatchItemSucceeded || item.Type != provider.BatchRequestTypeImage || item.ImageResult == nil || len(item.ImageResult.Base64Images) != 1 || item.ImageResult.Base64Images[0] != "" {
		t.Fatalf("item = %+v", item)
	}
}

// TS google-files.ts sends the resumable upload start and the upload with
// fetchFn directly, outside handleFetchError: a connection failure is the
// fetch rejection itself, not the "Cannot connect to API" APICallError.
func TestR93provgFilesRawFetchError(t *testing.T) {
	p := New(Config{APIKey: "k", BaseURL: r91provgClosedURL(t) + "/v1beta"})
	_, err := p.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf"})
	var urlErr *url.Error
	var perr *providererrors.ProviderError
	if err == nil || !errors.As(err, &urlErr) || errors.As(err, &perr) || strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("init error = %T %v", err, err)
	}

	// The upload step: the start succeeds and names an upload URL that refuses
	// connections.
	closed := r91provgClosedURL(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Goog-Upload-URL", closed+"/upload")
	}))
	defer srv.Close()
	p2 := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	_, err = p2.Files().UploadFile(context.Background(), types.UploadFileOptions{Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{1}}, MediaType: "application/pdf"})
	if err == nil || !errors.As(err, &urlErr) || errors.As(err, &perr) {
		t.Fatalf("upload error = %T %v", err, err)
	}
}

// TS builtinToolResultToSources url_context_result: an entry whose status is
// present and not 'success' (an empty string included) gives no source.
func TestR93provgURLContextEmptyStatusSkipped(t *testing.T) {
	block := interactionsContentBlock{Type: "url_context_result", Result: []interface{}{
		map[string]interface{}{"url": "https://a.test", "status": ""},
		map[string]interface{}{"url": "https://b.test", "status": "success"},
		map[string]interface{}{"url": "https://c.test"},
		map[string]interface{}{"url": "https://d.test", "status": nil},
	}}
	n := 0
	srcs := builtinToolResultSources(block, func() string { n++; return "id" })
	var urls []string
	for _, s := range srcs {
		urls = append(urls, s.(types.SourceContent).URL)
	}
	if strings.Join(urls, ",") != "https://b.test,https://c.test,https://d.test" {
		t.Fatalf("urls = %v", urls)
	}
}

// TS doCreateClientSecret calls fetchFn directly: a connection failure is the
// fetch rejection, not the "Cannot connect to API" APICallError, and the
// API key in ?key= stays out of the error.
func TestR93provgRealtimeClientSecretRawFetchError(t *testing.T) {
	p := New(Config{APIKey: "SECRET-KEY-123", BaseURL: r91provgClosedURL(t) + "/v1beta"})
	m := &GoogleRealtimeModel{provider: p, modelID: "gemini-live-2.5-flash"}
	_, err := m.DoCreateClientSecret(context.Background(), provider.ClientSecretOptions{})
	var urlErr *url.Error
	var perr *providererrors.ProviderError
	if err == nil || !errors.As(err, &urlErr) || errors.As(err, &perr) || strings.Contains(err.Error(), "SECRET-KEY-123") || strings.Contains(err.Error(), "Cannot connect to API") {
		t.Fatalf("error = %T %v", err, err)
	}
}
