package google

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from packages/google/src/google-batch.test.ts.

func googleTextBatchRequest(id, modelID, prompt string) provider.BatchV4Request {
	return provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID:      id,
			ModelID: modelID,
			Options: provider.GenerateOptions{Prompt: types.Prompt{Text: prompt}},
		},
	}
}

const googleBatchOperationJSON = `{
	"name": "batches/batch-123",
	"done": true,
	"metadata": {
		"name": "batches/batch-123",
		"state": "BATCH_STATE_SUCCEEDED",
		"createTime": "2026-08-04T12:34:56.123Z",
		"batchStats": {"requestCount":"2","successfulRequestCount":"2","failedRequestCount":"0"}
	}
}`

func TestGoogleBatch_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	if b.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", b.SpecificationVersion())
	}
	if b.Provider() != "google.batch" {
		t.Fatalf("Provider = %q, want google.batch", b.Provider())
	}
	if len(b.SupportedURLs()) == 0 {
		t.Fatal("SupportedURLs should not be empty")
	}
}

func TestGoogleBatch_RejectsUnsupportedRequestType(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{{Type: provider.BatchRequestTypeImage, Image: &provider.ImageBatchV4Request{ID: "img-1"}}},
	})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestGoogleBatch_RejectsMixedModels(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			googleTextBatchRequest("flash", "gemini-2.5-flash", "Hello"),
			googleTextBatchRequest("pro", "gemini-2.5-pro", "Hello"),
		},
	})
	if err == nil {
		t.Fatal("expected an error for mixed models")
	}
	var invalidArg *providererrors.InvalidArgumentError
	if !asGoogleInvalidArgumentError(err, &invalidArg) {
		t.Fatalf("err type = %T", err)
	}
}

func asGoogleInvalidArgumentError(err error, target **providererrors.InvalidArgumentError) bool {
	if e, ok := err.(*providererrors.InvalidArgumentError); ok {
		*target = e
		return true
	}
	return false
}

func TestGoogleBatch_StartsInlineBatch(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"name": "batches/batch-123", "done": false,
			"metadata": {"state":"BATCH_STATE_PENDING","createTime":"2026-08-04T12:34:56.123Z",
				"batchStats":{"requestCount":"1","successfulRequestCount":"0","failedRequestCount":"0","pendingRequestCount":"1"}}
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests:   []provider.BatchV4Request{googleTextBatchRequest("france", "gemini-2.5-flash", "What is the capital of France?")},
		WebhookURL: "https://example.com/google-batch-webhook",
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if !strings.HasSuffix(gotPath, "gemini-2.5-flash:batchGenerateContent") {
		t.Fatalf("path = %q", gotPath)
	}
	if result.BatchID != "batches/batch-123" {
		t.Fatalf("BatchID = %q", result.BatchID)
	}
	if result.Status != provider.BatchStatusPending || result.RawStatus != "BATCH_STATE_PENDING" {
		t.Fatalf("status = %+v", result.BatchV4Status)
	}
	if result.RequestCounts == nil || result.RequestCounts.Total != 1 || result.RequestCounts.Pending != 1 {
		t.Fatalf("RequestCounts = %+v", result.RequestCounts)
	}

	batch, _ := gotBody["batch"].(map[string]interface{})
	webhookConfig, _ := batch["webhookConfig"].(map[string]interface{})
	uris, _ := webhookConfig["uris"].([]interface{})
	if len(uris) != 1 || uris[0] != "https://example.com/google-batch-webhook" {
		t.Fatalf("webhookConfig = %+v", webhookConfig)
	}
	inputConfig, _ := batch["inputConfig"].(map[string]interface{})
	requestsWrapper, _ := inputConfig["requests"].(map[string]interface{})
	requests, _ := requestsWrapper["requests"].([]interface{})
	if len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
	entry, _ := requests[0].(map[string]interface{})
	metadata, _ := entry["metadata"].(map[string]interface{})
	if metadata["key"] != "france" {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestGoogleBatch_CancelsBatch(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Canceller)
	_, err := b.DoCancelBatch(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoCancelBatch: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/batches/batch-123:cancel" {
		t.Fatalf("method=%s path=%s", gotMethod, gotPath)
	}
}

func TestGoogleBatch_ListsAndNormalizesBatches(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"operations": [
				{"name":"batches/batch-123","metadata":{"state":"BATCH_STATE_RUNNING","createTime":"2026-08-04T12:34:56.123Z","batchStats":{"requestCount":"3","successfulRequestCount":"1","failedRequestCount":"0","pendingRequestCount":"2"}}},
				{"name":"batches/batch-122","metadata":{"state":"BATCH_STATE_SUCCEEDED","createTime":"2026-08-04T12:34:56.123Z","batchStats":{"requestCount":"2","successfulRequestCount":"2","failedRequestCount":"0"}}}
			],
			"nextPageToken": "page-token-2"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{Limit: 2, Cursor: "page-token-1"})
	if err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if !strings.Contains(gotQuery, "pageSize=2") || !strings.Contains(gotQuery, "pageToken=page-token-1") {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(result.Batches) != 2 {
		t.Fatalf("Batches = %+v", result.Batches)
	}
	if result.Batches[0].Status != provider.BatchStatusPending || result.Batches[0].RequestCounts.Pending != 2 {
		t.Fatalf("Batches[0] = %+v", result.Batches[0])
	}
	if result.Batches[1].Status != provider.BatchStatusCompleted {
		t.Fatalf("Batches[1] = %+v", result.Batches[1])
	}
	if result.NextCursor != "page-token-2" {
		t.Fatalf("NextCursor = %q", result.NextCursor)
	}
}

func TestGoogleBatch_EmptyTerminalPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{})
	if err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if len(result.Batches) != 0 || result.NextCursor != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGoogleBatch_UsesResumableFileUploadOverThreshold(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var createBody map[string]interface{}
	mux.HandleFunc("/upload/v1beta/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-goog-upload-url", srv.URL+"/upload/v1beta/files/session-123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/upload/v1beta/files/session-123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"file":{"name":"files/batch-input","expirationTime":"2026-08-27T12:00:00Z"}}`))
	})
	mux.HandleFunc("/v1beta/models/gemini-2.5-flash:batchGenerateContent", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(googleBatchOperationJSON))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL + "/v1beta"})
	b := p.ExperimentalBatch()

	// A very long prompt pushes the inline request body over the 20MB threshold.
	longPrompt := strings.Repeat("a", googleBatchInlineCreationMaxBytes+1000)
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("big", "gemini-2.5-flash", longPrompt)},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if result.BatchID != "batches/batch-123" {
		t.Fatalf("BatchID = %q", result.BatchID)
	}
	meta, _ := result.ProviderMetadata["google"].(map[string]interface{})
	if meta["inputFileId"] != "files/batch-input" {
		t.Fatalf("providerMetadata = %+v", meta)
	}
	if meta["inputFileExpiresAt"] != "2026-08-27T12:00:00Z" {
		t.Fatalf("providerMetadata = %+v", meta)
	}
	batch, _ := createBody["batch"].(map[string]interface{})
	inputConfig, _ := batch["inputConfig"].(map[string]interface{})
	if inputConfig["fileName"] != "files/batch-input" {
		t.Fatalf("inputConfig = %+v", inputConfig)
	}
}

func googleGenerateContentResponseJSON(id, text string) string {
	return `{"responseId":"` + id + `","candidates":[{"content":{"role":"model","parts":[{"text":"` + text + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":3}}`
}

// newGoogleBatchAndDownloadServer serves batchJSON for the batch-status
// request and downloadBody for the responses-file download request,
// distinguishing them by path prefix rather than an exact mux pattern (the
// download path may contain percent-encoded reserved characters).
func newGoogleBatchAndDownloadServer(t *testing.T, batchJSON, downloadBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/download/") {
			_, _ = w.Write([]byte(downloadBody))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(batchJSON))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGoogleBatch_ReadsInlineResults(t *testing.T) {
	body := `{
		"name": "batches/batch-123", "done": true,
		"metadata": {"state":"BATCH_STATE_SUCCEEDED"},
		"response": {
			"inlinedResponses": {
				"inlinedResponses": [
					{"metadata":{"key":"france"},"response":` + googleGenerateContentResponseJSON("r1", "Paris") + `},
					{"metadata":{"key":"germany"},"error":{"code":8,"message":"Resource has been exhausted.","status":"RESOURCE_EXHAUSTED"}}
				]
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := map[string]*provider.BatchV4ItemResult{}
	for {
		item, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		items[item.ID] = item
	}
	if got := items["france"]; got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult.Text != "Paris" {
		t.Fatalf("france = %+v", got)
	}
	if got := items["germany"]; got == nil || got.Status != provider.BatchItemFailed || got.Error.Type != "RESOURCE_EXHAUSTED" || got.Error.Code != "8" {
		t.Fatalf("germany = %+v", got)
	}
}

func TestGoogleBatch_MapsNumericCancellationError(t *testing.T) {
	srv := newGoogleBatchAndDownloadServer(t,
		`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output"}}}`,
		`{"key":"cancelled-request","error":{"code":1,"message":"The request was cancelled."}}`+"\n")

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.Status != provider.BatchItemCancelled || item.Error.Code != "1" {
		t.Fatalf("item = %+v", item)
	}
}

func TestGoogleBatch_ReturnsFailedItemForBlockedResponse(t *testing.T) {
	srv := newGoogleBatchAndDownloadServer(t,
		`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output"}}}`,
		`{"key":"blocked-request","response":{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}}`+"\n")

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.Status != provider.BatchItemFailed || item.Error.Type != "SAFETY" || item.Error.Code != "prompt_blocked" {
		t.Fatalf("item = %+v", item)
	}
	meta, _ := item.ProviderMetadata["google"].(map[string]interface{})
	feedback, _ := meta["promptFeedback"].(map[string]interface{})
	if feedback["blockReason"] != "SAFETY" {
		t.Fatalf("providerMetadata = %+v", item.ProviderMetadata)
	}
}

func TestGoogleBatch_ReadsOutputFileFromOperationResponse(t *testing.T) {
	srv := newGoogleBatchAndDownloadServer(t,
		`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED"},"response":{"responsesFile":"files/batch-output"}}`,
		`{"key":"france","response":`+googleGenerateContentResponseJSON("r1", "Paris")+`}`+"\n")

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()
	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.Status != provider.BatchItemSucceeded || item.TextResult.Text != "Paris" {
		t.Fatalf("item = %+v", item)
	}
}

func TestGoogleBatch_EncodesResponseFilePathSegments(t *testing.T) {
	var downloadRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/download/") || strings.HasPrefix(r.URL.EscapedPath(), "/download/") {
			downloadRawPath = r.URL.EscapedPath()
			_, _ = w.Write([]byte(`{"key":"france","response":` + googleGenerateContentResponseJSON("r1", "Paris") + `}` + "\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output?alt=json#fragment"}}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !strings.Contains(downloadRawPath, "batch-output%3Falt%3Djson%23fragment") {
		t.Fatalf("download path = %q, want percent-encoded ? and # within the path segment", downloadRawPath)
	}
}

func TestGoogleBatch_EmptyStreamForFailedBatchWithoutOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_FAILED"}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("expected io.EOF for an empty stream, got %v", err)
	}
}

func TestGoogleBatch_RejectsResultRetrievalWhilePending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":false,"metadata":{"state":"BATCH_STATE_RUNNING"}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err == nil {
		t.Fatal("expected an error while pending")
	}
}

func TestGoogleBatch_RejectsCompletedBatchWithoutOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED"}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err == nil {
		t.Fatal("expected an error for a completed batch without output")
	}
}

func TestGoogleBatch_UnsupportedContentFailsItemButContinues(t *testing.T) {
	// An inline image part (mediaType image/png, data) produces a "file"
	// content type, which is unsupported for text batches.
	imageResponse := `{"candidates":[{"content":{"role":"model","parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`
	lines := []string{
		`{"key":"ok","response":` + googleGenerateContentResponseJSON("r1", "Paris") + `}`,
		`{"key":"image","response":` + imageResponse + `}`,
	}
	srv := newGoogleBatchAndDownloadServer(t,
		`{"name":"batches/batch-123","done":true,"metadata":{"state":"BATCH_STATE_SUCCEEDED","output":{"responsesFile":"files/batch-output"}}}`,
		strings.Join(lines, "\n")+"\n")

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/batch-123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := map[string]*provider.BatchV4ItemResult{}
	for {
		item, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		items[item.ID] = item
	}
	if got := items["ok"]; got == nil || got.Status != provider.BatchItemSucceeded {
		t.Fatalf("ok = %+v", got)
	}
	if got := items["image"]; got == nil || got.Status != provider.BatchItemFailed || got.Error.Code != "unsupported_content" {
		t.Fatalf("image = %+v", got)
	}
}
