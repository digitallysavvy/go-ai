package xai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// xaiBatchTestServer is a minimal multi-endpoint xAI Batch API double: it
// dispatches on method+path and records every request it receives.
type xaiBatchTestServer struct {
	ts *httptest.Server

	mu          sync.Mutex
	requests    []xaiBatchTestRequest
	filesBody   map[string]interface{}
	createBody  map[string]interface{}
	statusBody  map[string]interface{}
	cancelBody  map[string]interface{}
	listBody    map[string]interface{}
	resultsBody []map[string]interface{} // one entry per call, last entry repeats
}

type xaiBatchTestRequest struct {
	Method  string
	Path    string
	Query   map[string][]string
	Headers http.Header
	Body    []byte
	// MultipartFields holds decoded multipart form field values (excluding
	// the file part), in append order, when the request was multipart.
	MultipartFieldOrder []string
	FileContent         []byte
	FileContentType     string
}

func newXAIBatchTestServer(t *testing.T) *xaiBatchTestServer {
	t.Helper()
	s := &xaiBatchTestServer{}
	s.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record(r)

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			writeJSON(w, s.filesBody)
		case r.Method == http.MethodPost && r.URL.Path == "/batches":
			writeJSON(w, s.createBody)
		case r.Method == http.MethodGet && r.URL.Path == "/batches":
			writeJSON(w, s.listBody)
		case r.Method == http.MethodPost && r.URL.Path == "/batches/batch_123:cancel":
			writeJSON(w, s.cancelBody)
		case r.Method == http.MethodGet && r.URL.Path == "/batches/batch_123":
			writeJSON(w, s.statusBody)
		case r.Method == http.MethodGet && r.URL.Path == "/batches/batch_123/results":
			s.mu.Lock()
			idx := s.resultsCallIndexLocked()
			body := s.resultsBody[idx]
			s.mu.Unlock()
			writeJSON(w, body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return s
}

// resultsCallIndexLocked returns the results-page index for the current
// call, clamped to the last configured page (repeats it if exceeded). Caller
// must hold s.mu.
func (s *xaiBatchTestServer) resultsCallIndexLocked() int {
	count := 0
	for _, req := range s.requests {
		if req.Method == http.MethodGet && req.Path == "/batches/batch_123/results" {
			count++
		}
	}
	idx := count - 1
	if idx >= len(s.resultsBody) {
		idx = len(s.resultsBody) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return idx
}

func writeJSON(w http.ResponseWriter, body map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *xaiBatchTestServer) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := xaiBatchTestRequest{Method: r.Method, Path: r.URL.Path, Headers: r.Header.Clone()}
	rec.Query = map[string][]string(r.URL.Query())

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		reader, err := r.MultipartReader()
		if err == nil {
			for {
				part, err := reader.NextPart()
				if err != nil {
					break
				}
				if part.FormName() == "file" {
					content, _ := io.ReadAll(part)
					rec.FileContent = content
					rec.FileContentType = part.Header.Get("Content-Type")
				} else {
					_, _ = io.ReadAll(part)
				}
				rec.MultipartFieldOrder = append(rec.MultipartFieldOrder, part.FormName())
			}
		}
	} else {
		body, _ := io.ReadAll(r.Body)
		rec.Body = body
	}
	s.requests = append(s.requests, rec)
}

func (s *xaiBatchTestServer) close() { s.ts.Close() }

func (s *xaiBatchTestServer) request(i int) xaiBatchTestRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[i]
}

func (s *xaiBatchTestServer) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func newTestXAIBatchProvider(baseURL string) *Batch {
	p := New(Config{APIKey: "test-api-key", BaseURL: baseURL})
	return NewBatch(p)
}

func xaiBatchResponseBody(overrides map[string]interface{}) map[string]interface{} {
	body := map[string]interface{}{
		"batch_id":              "batch_123",
		"name":                  "ai-sdk-text-batch",
		"create_time":           "2026-08-25T12:00:00Z",
		"expire_time":           "2099-08-26T12:00:00Z",
		"cancel_time":           nil,
		"cancel_by_xai_message": nil,
		"state": map[string]interface{}{
			"num_requests":  2,
			"num_pending":   0,
			"num_success":   2,
			"num_error":     0,
			"num_cancelled": 0,
		},
	}
	for k, v := range overrides {
		body[k] = v
	}
	return body
}

func textBatchRequest(id, modelID, prompt string) provider.BatchV4Request {
	return provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID:      id,
			ModelID: modelID,
			Options: provider.GenerateOptions{Prompt: types.Prompt{Text: prompt}},
		},
	}
}

// TS: "rejects unsupported request types before uploading a batch input file"
func TestBatch_DoStartBatch_RejectsUnsupportedRequestType(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()

	batch := newTestXAIBatchProvider(server.ts.URL)
	_, err := batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{{Type: "audio"}},
	})
	var unsupported *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("DoStartBatch() error = %v (%T), want UnsupportedFunctionalityError", err, err)
	}
	if unsupported.Functionality != "batch request type: audio" {
		t.Errorf("Functionality = %q, want %q", unsupported.Functionality, "batch request type: audio")
	}
	if server.requestCount() != 0 {
		t.Errorf("requestCount = %d, want 0 (no upload before validation)", server.requestCount())
	}
}

// TS: "uploads image generation requests to the image batch endpoint"
func TestBatch_DoStartBatch_ImageRequest(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.filesBody = map[string]interface{}{"id": "file_123", "filename": "batch.jsonl"}
	server.createBody = xaiBatchResponseBody(nil)

	batch := newTestXAIBatchProvider(server.ts.URL)
	n := 2
	_, err := batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			{
				Type: provider.BatchRequestTypeImage,
				Image: &provider.ImageBatchV4Request{
					ID:      "image-1",
					ModelID: "grok-imagine-image",
					Options: provider.ImageGenerateOptions{
						Prompt:      "A red panda",
						N:           &n,
						AspectRatio: "16:9",
						ProviderOptions: map[string]interface{}{
							"xai": map[string]interface{}{"quality": "high"},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}

	uploadReq := server.request(0)
	var line map[string]interface{}
	if err := json.Unmarshal(bytesTrimNewline(uploadReq.FileContent), &line); err != nil {
		t.Fatalf("decode JSONL line: %v", err)
	}
	if line["custom_id"] != "image-1" {
		t.Errorf("custom_id = %v, want image-1", line["custom_id"])
	}
	if line["method"] != "POST" {
		t.Errorf("method = %v, want POST", line["method"])
	}
	if line["url"] != "/v1/images/generations" {
		t.Errorf("url = %v, want /v1/images/generations", line["url"])
	}
	body, ok := line["body"].(map[string]interface{})
	if !ok {
		t.Fatalf("body = %v, want a map", line["body"])
	}
	if body["model"] != "grok-imagine-image" || body["prompt"] != "A red panda" {
		t.Errorf("body = %+v, want model/prompt set", body)
	}
	if body["n"] != float64(2) {
		t.Errorf("body[n] = %v, want 2", body["n"])
	}
	if body["response_format"] != "b64_json" {
		t.Errorf("body[response_format] = %v, want b64_json", body["response_format"])
	}
	if body["aspect_ratio"] != "16:9" {
		t.Errorf("body[aspect_ratio] = %v, want 16:9", body["aspect_ratio"])
	}
	if body["quality"] != "high" {
		t.Errorf("body[quality] = %v, want high", body["quality"])
	}
}

// TS: "uploads prepared JSONL requests and creates a file-backed batch"
func TestBatch_DoStartBatch_TextRequests(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.filesBody = map[string]interface{}{"id": "file_123", "filename": "batch.jsonl", "expires_at": 1_700_172_800}
	server.createBody = xaiBatchResponseBody(map[string]interface{}{
		"state": map[string]interface{}{"num_requests": 2, "num_pending": 2, "num_success": 0, "num_error": 0, "num_cancelled": 0},
	})

	batch := newTestXAIBatchProvider(server.ts.URL)
	topK := 10
	result, err := batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			textBatchRequest("france", "grok-4.3", "What is the capital of France?"),
			{
				Type: provider.BatchRequestTypeText,
				Text: &provider.TextBatchV4Request{
					ID:      "germany",
					ModelID: "grok-4.20-non-reasoning",
					Options: provider.GenerateOptions{
						Prompt: types.Prompt{Text: "What is the capital of Germany?"},
						TopK:   &topK,
					},
				},
			},
		},
		WebhookURL: "https://example.com/batch-webhook",
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}

	if result.BatchID != "batch_123" {
		t.Errorf("BatchID = %q, want batch_123", result.BatchID)
	}
	if result.Status != provider.BatchStatusPending {
		t.Errorf("Status = %q, want pending", result.Status)
	}
	if result.RequestCounts == nil || *result.RequestCounts != (provider.BatchRequestCounts{Total: 2, Pending: 2, Completed: 0, Failed: 0}) {
		t.Errorf("RequestCounts = %+v, want {2 2 0 0}", result.RequestCounts)
	}
	if result.CreatedAt != "2026-08-25T12:00:00Z" || result.ExpiresAt != "2099-08-26T12:00:00Z" {
		t.Errorf("CreatedAt/ExpiresAt = %q/%q", result.CreatedAt, result.ExpiresAt)
	}
	xaiMeta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
	if xaiMeta["inputFileId"] != "file_123" {
		t.Errorf("providerMetadata.xai.inputFileId = %v, want file_123", xaiMeta["inputFileId"])
	}
	if xaiMeta["inputFileExpiresAt"] != "2023-11-16T22:13:20.000Z" {
		t.Errorf("providerMetadata.xai.inputFileExpiresAt = %v, want 2023-11-16T22:13:20.000Z", xaiMeta["inputFileExpiresAt"])
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Warning.Feature != "webhookUrl" {
		t.Fatalf("Warnings = %+v, want one webhookUrl warning", result.Warnings)
	}

	uploadReq := server.request(0)
	// TS: `new Blob(fileParts, { type: 'application/jsonl' })`
	if uploadReq.FileContentType != "application/jsonl" {
		t.Errorf("file part Content-Type = %q, want application/jsonl", uploadReq.FileContentType)
	}
	lines := strings.Split(strings.TrimSpace(string(uploadReq.FileContent)), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d JSONL lines, want 2", len(lines))
	}
	var first map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode line 0: %v", err)
	}
	if first["url"] != "/v1/responses" {
		t.Errorf("line 0 url = %v, want /v1/responses", first["url"])
	}
	body0, _ := first["body"].(map[string]interface{})
	if body0["model"] != "grok-4.3" {
		t.Errorf("line 0 body.model = %v, want grok-4.3", body0["model"])
	}
	if _, hasStream := body0["stream"]; hasStream {
		t.Errorf("line 0 body has a stream key, want it omitted like TS")
	}

	var second map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("decode line 1: %v", err)
	}
	body1, _ := second["body"].(map[string]interface{})
	if body1["top_k"] != float64(10) {
		t.Errorf("line 1 body.top_k = %v, want 10", body1["top_k"])
	}

	createReq := server.request(1)
	var createPayload map[string]interface{}
	if err := json.Unmarshal(createReq.Body, &createPayload); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if createPayload["name"] != "ai-sdk-text-batch" || createPayload["input_file_id"] != "file_123" {
		t.Errorf("create payload = %+v, want name/input_file_id set", createPayload)
	}
}

// TS: "appends inputFileExpiresAfter as expires_after before the file part"
func TestBatch_DoStartBatch_ExpiresAfterPrecedesFile(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.filesBody = map[string]interface{}{"id": "file_123", "filename": "batch.jsonl"}
	server.createBody = xaiBatchResponseBody(nil)

	batch := newTestXAIBatchProvider(server.ts.URL)
	_, err := batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests:        []provider.BatchV4Request{textBatchRequest("france", "grok-4.3", "What is the capital of France?")},
		ProviderOptions: map[string]interface{}{"xai": map[string]interface{}{"inputFileExpiresAfter": 172800}},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}

	uploadReq := server.request(0)
	if len(uploadReq.MultipartFieldOrder) != 2 || uploadReq.MultipartFieldOrder[0] != "expires_after" || uploadReq.MultipartFieldOrder[1] != "file" {
		t.Fatalf("field order = %v, want [expires_after file]", uploadReq.MultipartFieldOrder)
	}
}

// TS: "maps xAI state counters and cancellation metadata"
func TestBatch_DoGetBatchStatus_MapsCancellation(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(map[string]interface{}{
		"cancel_time":           "2026-08-25T12:30:00Z",
		"cancel_by_xai_message": "Cancelled by user.",
		"state":                 map[string]interface{}{"num_requests": 4, "num_pending": 0, "num_success": 2, "num_error": 1, "num_cancelled": 1},
	})

	batch := newTestXAIBatchProvider(server.ts.URL)
	status, err := batch.DoGetBatchStatus(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchStatus() error = %v", err)
	}
	if status.Status != provider.BatchStatusFailed {
		t.Errorf("Status = %q, want failed", status.Status)
	}
	if status.RequestCounts == nil || *status.RequestCounts != (provider.BatchRequestCounts{Total: 4, Pending: 0, Completed: 2, Failed: 2}) {
		t.Errorf("RequestCounts = %+v, want {4 0 2 2}", status.RequestCounts)
	}
	if status.Error == nil || status.Error.Message != "Cancelled by user." || status.Error.Code != "batch_cancelled" {
		t.Errorf("Error = %+v, want Cancelled by user./batch_cancelled", status.Error)
	}
}

// TS: "omits inconsistent request counts"
func TestBatch_DoGetBatchStatus_OmitsInconsistentCounts(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(map[string]interface{}{
		"state": map[string]interface{}{"num_requests": 2, "num_pending": 1, "num_success": 2, "num_error": 0, "num_cancelled": 0},
	})

	batch := newTestXAIBatchProvider(server.ts.URL)
	status, err := batch.DoGetBatchStatus(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchStatus() error = %v", err)
	}
	if status.Status != provider.BatchStatusPending {
		t.Errorf("Status = %q, want pending", status.Status)
	}
	if status.RequestCounts != nil {
		t.Errorf("RequestCounts = %+v, want nil", status.RequestCounts)
	}
}

// TS: "cancels a batch"
func TestBatch_DoCancelBatch(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.cancelBody = xaiBatchResponseBody(map[string]interface{}{"cancel_time": "2026-08-25T12:30:00Z"})

	batch := newTestXAIBatchProvider(server.ts.URL)
	canceller, ok := provider.BatchV4(batch).(provider.BatchV4Canceller)
	if !ok {
		t.Fatal("Batch does not implement BatchV4Canceller")
	}
	if _, err := canceller.DoCancelBatch(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"}); err != nil {
		t.Fatalf("DoCancelBatch() error = %v", err)
	}
	req := server.request(0)
	if req.Method != http.MethodPost || req.Path != "/batches/batch_123:cancel" {
		t.Errorf("request = %s %s, want POST /batches/batch_123:cancel", req.Method, req.Path)
	}
}

// TS: "lists and normalizes a page of batches" + "omits the next cursor..."
func TestBatch_DoListBatches(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.listBody = map[string]interface{}{
		"batches": []map[string]interface{}{
			xaiBatchResponseBody(map[string]interface{}{
				"batch_id": "batch_123",
				"state":    map[string]interface{}{"num_requests": 3, "num_pending": 2, "num_success": 1, "num_error": 0, "num_cancelled": 0},
			}),
			xaiBatchResponseBody(map[string]interface{}{"batch_id": "batch_122"}),
		},
		"pagination_token": "next/page",
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	lister, ok := provider.BatchV4(batch).(provider.BatchV4Lister)
	if !ok {
		t.Fatal("Batch does not implement BatchV4Lister")
	}
	result, err := lister.DoListBatches(context.Background(), provider.BatchV4ListOptions{Limit: intPtr(2), Cursor: "previous/page"})
	if err != nil {
		t.Fatalf("DoListBatches() error = %v", err)
	}
	if len(result.Batches) != 2 {
		t.Fatalf("got %d batches, want 2", len(result.Batches))
	}
	if result.Batches[0].BatchID != "batch_123" || result.Batches[0].Status != provider.BatchStatusPending {
		t.Errorf("batches[0] = %+v", result.Batches[0])
	}
	if result.Batches[1].BatchID != "batch_122" || result.Batches[1].Status != provider.BatchStatusCompleted {
		t.Errorf("batches[1] = %+v", result.Batches[1])
	}
	if result.NextCursor != "next/page" {
		t.Errorf("NextCursor = %q, want next/page", result.NextCursor)
	}

	req := server.request(0)
	if got := req.Query["limit"]; len(got) != 1 || got[0] != "2" {
		t.Errorf("query limit = %v, want [2]", got)
	}
	if got := req.Query["pagination_token"]; len(got) != 1 || got[0] != "previous/page" {
		t.Errorf("query pagination_token = %v, want [previous/page]", got)
	}
}

func TestBatch_DoListBatches_OmitsNextCursorWhenAbsent(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.listBody = map[string]interface{}{"batches": []map[string]interface{}{}, "pagination_token": nil}

	batch := newTestXAIBatchProvider(server.ts.URL)
	lister := provider.BatchV4(batch).(provider.BatchV4Lister)
	result, err := lister.DoListBatches(context.Background(), provider.BatchV4ListOptions{})
	if err != nil {
		t.Fatalf("DoListBatches() error = %v", err)
	}
	if len(result.Batches) != 0 || result.NextCursor != "" {
		t.Errorf("result = %+v, want empty batches and no cursor", result)
	}
}

// TS: "rejects result retrieval while the batch is pending"
func TestBatch_DoGetBatchResults_RejectsWhilePending(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(map[string]interface{}{
		"state": map[string]interface{}{"num_requests": 2, "num_pending": 1, "num_success": 1, "num_error": 0, "num_cancelled": 0},
	})

	batch := newTestXAIBatchProvider(server.ts.URL)
	_, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	var invalidArg *providererrors.InvalidArgumentError
	if !errors.As(err, &invalidArg) {
		t.Fatalf("DoGetBatchResults() error = %v (%T), want InvalidArgumentError", err, err)
	}
	if invalidArg.Message != `xAI batch "batch_123" is not complete.` {
		t.Errorf("Message = %q", invalidArg.Message)
	}
}

func successfulTextResultWire(id, text string) map[string]interface{} {
	return map[string]interface{}{
		"batch_request_id": id,
		"batch_result": map[string]interface{}{
			"response": map[string]interface{}{
				"chat_get_completion": chatResultBodyWire(text),
			},
			"error": map[string]interface{}{"code": 0, "message": ""},
		},
	}
}

func chatResultBodyWire(text string) map[string]interface{} {
	return map[string]interface{}{
		"id":      "response_123",
		"object":  "chat.completion",
		"created": 1_700_000_000,
		"model":   "grok-4.3",
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":              "assistant",
					"content":           text,
					"reasoning_content": "Reasoning",
					"tool_calls":        nil,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":             10,
			"completion_tokens":         3,
			"total_tokens":              14,
			"prompt_tokens_details":     map[string]interface{}{"cached_tokens": 2},
			"completion_tokens_details": map[string]interface{}{"reasoning_tokens": 1},
			"cost_in_usd_ticks":         123,
		},
		"citations":    []string{"https://example.com/source"},
		"service_tier": "default",
	}
}

// TS: "paginates and converts successful and failed results"
func TestBatch_DoGetBatchResults_PaginatesAndConverts(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results":          []map[string]interface{}{successfulTextResultWire("france", "Paris")},
			"pagination_token": "next/page",
		},
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "failed",
					"batch_result":     map[string]interface{}{"error": map[string]interface{}{"code": 3, "message": "Invalid request."}},
					"error_message":    "Invalid request.",
				},
				{
					"batch_request_id": "cancelled",
					"batch_result":     map[string]interface{}{"error": map[string]interface{}{"code": 1, "message": "Cancelled."}},
				},
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(results), results)
	}

	first := results[0]
	if first.Type != provider.BatchRequestTypeText || first.ID != "france" || first.Status != provider.BatchItemSucceeded {
		t.Fatalf("results[0] = %+v", first)
	}
	if first.TextResult == nil {
		t.Fatal("results[0].TextResult is nil")
	}
	if first.TextResult.Text != "Paris" {
		t.Errorf("TextResult.Text = %q, want Paris", first.TextResult.Text)
	}
	if first.TextResult.FinishReason != types.FinishReasonStop {
		t.Errorf("FinishReason = %q, want stop", first.TextResult.FinishReason)
	}
	if first.TextResult.Usage.InputTokens == nil || *first.TextResult.Usage.InputTokens != 10 {
		t.Errorf("Usage.InputTokens = %v, want 10", first.TextResult.Usage.InputTokens)
	}
	if first.TextResult.Usage.InputDetails == nil || *first.TextResult.Usage.InputDetails.CacheReadTokens != 2 || *first.TextResult.Usage.InputDetails.NoCacheTokens != 8 {
		t.Errorf("Usage.InputDetails = %+v, want cacheRead=2 noCache=8", first.TextResult.Usage.InputDetails)
	}
	if first.TextResult.Usage.OutputTokens == nil || *first.TextResult.Usage.OutputTokens != 4 {
		t.Errorf("Usage.OutputTokens = %v, want 4", first.TextResult.Usage.OutputTokens)
	}
	xaiMeta, _ := first.TextResult.ProviderMetadata["xai"].(map[string]interface{})
	if xaiMeta["costInUsdTicks"] != int64(123) || xaiMeta["serviceTier"] != "default" {
		t.Errorf("providerMetadata.xai = %+v", xaiMeta)
	}
	foundSource := false
	for _, c := range first.TextResult.Content {
		if sc, ok := c.(types.SourceContent); ok && sc.URL == "https://example.com/source" {
			foundSource = true
		}
	}
	if !foundSource {
		t.Errorf("Content = %+v, want a source content part", first.TextResult.Content)
	}

	failed := results[1]
	if failed.ID != "failed" || failed.Status != provider.BatchItemFailed {
		t.Fatalf("results[1] = %+v", failed)
	}
	if failed.Error == nil || failed.Error.Message != "Invalid request." || failed.Error.Code != "3" {
		t.Errorf("results[1].Error = %+v, want Invalid request./3", failed.Error)
	}

	cancelled := results[2]
	if cancelled.ID != "cancelled" || cancelled.Status != provider.BatchItemCancelled {
		t.Fatalf("results[2] = %+v", cancelled)
	}
	if cancelled.Error == nil || cancelled.Error.Message != "Cancelled." || cancelled.Error.Code != "1" {
		t.Errorf("results[2].Error = %+v, want Cancelled./1", cancelled.Error)
	}

	// batch status + 2 result pages
	if server.requestCount() != 3 {
		t.Errorf("requestCount = %d, want 3", server.requestCount())
	}
}

// TS: "converts image generation results" + "returns moderated image
// generation results as failed items"
func TestBatch_DoGetBatchResults_ImageResults(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "image-1",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{
							"image_generation": map[string]interface{}{
								"data": []map[string]interface{}{
									{"b64_json": "aGVsbG8=", "revised_prompt": "A vivid red panda"},
								},
								"usage": map[string]interface{}{"cost_in_usd_ticks": 42},
							},
						},
						"error": map[string]interface{}{"code": 0, "message": ""},
					},
				},
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	item := results[0]
	if item.Type != provider.BatchRequestTypeImage || item.Status != provider.BatchItemSucceeded {
		t.Fatalf("item = %+v", item)
	}
	if item.ImageResult == nil || len(item.ImageResult.Base64Images) != 1 || item.ImageResult.Base64Images[0] != "aGVsbG8=" {
		t.Fatalf("ImageResult = %+v", item.ImageResult)
	}
	xaiMeta, _ := item.ImageResult.ProviderMetadata["xai"].(XAIImageMetadata)
	if xaiMeta.CostInUsdTicks == nil || *xaiMeta.CostInUsdTicks != 42 {
		t.Errorf("ProviderMetadata.xai.CostInUsdTicks = %v, want 42", xaiMeta.CostInUsdTicks)
	}
}

// Batch image results without b64_json fall back to downloading the `url`,
// mirroring TS XaiBatch#convertImageBatchResponse (reused via
// ImageModel.responseImages). A data: URL is used so the fallback exercises
// real decoding without a network round trip to an SSRF-blocked test host.
func TestBatch_DoGetBatchResults_ImageURLFallback(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "image-url",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{
							"image_generation": map[string]interface{}{
								"data": []map[string]interface{}{
									{"url": "data:image/png;base64,aGVsbG8="},
								},
							},
						},
						"error": map[string]interface{}{"code": 0, "message": ""},
					},
				},
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	item := results[0]
	if item.Type != provider.BatchRequestTypeImage || item.Status != provider.BatchItemSucceeded {
		t.Fatalf("item = %+v, want a succeeded image result", item)
	}
	if item.ImageResult == nil || len(item.ImageResult.Images) != 1 || string(item.ImageResult.Images[0]) != "hello" {
		t.Fatalf("ImageResult = %+v, want decoded image bytes %q", item.ImageResult, "hello")
	}
	if len(item.ImageResult.Base64Images) != 0 {
		t.Errorf("Base64Images = %v, want empty for a URL-downloaded image", item.ImageResult.Base64Images)
	}
}

func TestBatch_DoGetBatchResults_ModeratedImageIsFailed(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "image-1",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{
							"image_generation": map[string]interface{}{
								"data": []map[string]interface{}{
									{"url": nil, "b64_json": nil, "respect_moderation": false},
								},
							},
						},
						"error": map[string]interface{}{"code": 0, "message": ""},
					},
				},
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	item := results[0]
	if item.Type != provider.BatchRequestTypeImage || item.Status != provider.BatchItemFailed {
		t.Fatalf("item = %+v", item)
	}
	if item.Error == nil || item.Error.Message != "Image generation was blocked due to a content policy violation." {
		t.Errorf("Error = %+v", item.Error)
	}
}

// TS: "preserves tool calls and fails invalid items without stopping later
// results"
func TestBatch_DoGetBatchResults_ToolCallsAndInvalidItems(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "invalid",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{"chat_get_completion": map[string]interface{}{"choices": 42}},
					},
				},
				{
					"batch_request_id": "tool-call",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{
							"chat_get_completion": map[string]interface{}{
								"choices": []map[string]interface{}{
									{
										"index": 0,
										"message": map[string]interface{}{
											"role":              "assistant",
											"content":           nil,
											"reasoning_content": nil,
											"tool_calls": []map[string]interface{}{
												{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "weather", "arguments": "{}"}},
											},
										},
										"finish_reason": "tool_calls",
									},
								},
							},
						},
					},
				},
				successfulTextResultWire("valid", "Berlin"),
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: %+v", len(results), results)
	}

	if results[0].ID != "invalid" || results[0].Status != provider.BatchItemFailed || results[0].Error == nil || results[0].Error.Code != "invalid_response" {
		t.Fatalf("results[0] = %+v", results[0])
	}

	toolCall := results[1]
	if toolCall.ID != "tool-call" || toolCall.Status != provider.BatchItemSucceeded {
		t.Fatalf("results[1] = %+v", toolCall)
	}
	if len(toolCall.TextResult.ToolCalls) != 1 || toolCall.TextResult.ToolCalls[0].ID != "call_1" || toolCall.TextResult.ToolCalls[0].ToolName != "weather" {
		t.Errorf("ToolCalls = %+v", toolCall.TextResult.ToolCalls)
	}

	valid := results[2]
	if valid.ID != "valid" || valid.Status != provider.BatchItemSucceeded || valid.TextResult.Text != "Berlin" {
		t.Fatalf("results[2] = %+v", valid)
	}
}

func drainXAIBatchResults(t *testing.T, stream provider.BatchV4ItemResultStream) []provider.BatchV4ItemResult {
	t.Helper()
	defer stream.Close() //nolint:errcheck
	var out []provider.BatchV4ItemResult
	for {
		item, err := stream.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("stream.Next() error = %v", err)
		}
		out = append(out, *item)
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("stream.Err() = %v", err)
	}
	return out
}

func bytesTrimNewline(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// TS: "preserves provider-executed tool calls and final text from batch
// transcripts"
func TestBatch_DoGetBatchResults_ProviderExecutedToolCalls(t *testing.T) {
	server := newXAIBatchTestServer(t)
	defer server.close()
	server.statusBody = xaiBatchResponseBody(nil)
	server.resultsBody = []map[string]interface{}{
		{
			"results": []map[string]interface{}{
				{
					"batch_request_id": "provider-tool",
					"batch_result": map[string]interface{}{
						"response": map[string]interface{}{
							"chat_get_completion": map[string]interface{}{
								"choices": []map[string]interface{}{
									{
										"index": 0,
										"message": map[string]interface{}{
											"role":    "assistant",
											"content": nil,
											"tool_calls": []map[string]interface{}{
												{"id": "web-search-1", "type": "function", "function": map[string]interface{}{"name": "web_search", "arguments": `{"query":"Vercel"}`}},
											},
										},
										"finish_reason": "",
									},
									{
										"index": 1,
										"message": map[string]interface{}{
											"role":    "tool",
											"content": "Search results",
											"tool_calls": []map[string]interface{}{
												{"id": "web-search-1", "type": "function", "function": map[string]interface{}{"name": "web_search", "arguments": `{"query":"Vercel"}`}},
											},
										},
										"finish_reason": "",
									},
									{
										"index":         2,
										"message":       map[string]interface{}{"role": "assistant", "content": "Final answer", "tool_calls": nil},
										"finish_reason": "stop",
									},
									{
										"index":         3,
										"message":       map[string]interface{}{"role": "tool", "content": nil, "tool_calls": []map[string]interface{}{}},
										"finish_reason": "",
									},
								},
								"citations": []string{"https://example.com/source"},
							},
						},
					},
				},
			},
			"pagination_token": nil,
		},
	}

	batch := newTestXAIBatchProvider(server.ts.URL)
	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	results := drainXAIBatchResults(t, stream)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	item := results[0]
	if item.Status != provider.BatchItemSucceeded || item.TextResult == nil {
		t.Fatalf("item = %+v", item)
	}
	if item.TextResult.FinishReason != types.FinishReasonStop {
		t.Errorf("FinishReason = %q, want stop", item.TextResult.FinishReason)
	}
	if item.TextResult.Text != "Final answer" {
		t.Errorf("Text = %q, want %q", item.TextResult.Text, "Final answer")
	}
	if len(item.TextResult.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1 entry", item.TextResult.ToolCalls)
	}
	tc := item.TextResult.ToolCalls[0]
	if tc.ID != "web-search-1" || tc.ToolName != "web_search" || !tc.ProviderExecuted || !tc.Dynamic {
		t.Errorf("ToolCalls[0] = %+v, want a provider-executed dynamic web_search call", tc)
	}
	foundToolResult, foundText, foundSource := false, false, false
	for _, c := range item.TextResult.Content {
		switch v := c.(type) {
		case types.ToolResultContent:
			if v.ToolCallID == "web-search-1" && v.Result == "Search results" && v.Dynamic {
				foundToolResult = true
			}
		case types.TextContent:
			if v.Text == "Final answer" {
				foundText = true
			}
		case types.SourceContent:
			if v.URL == "https://example.com/source" {
				foundSource = true
			}
		}
	}
	if !foundToolResult || !foundText || !foundSource {
		t.Errorf("Content = %+v, want tool-result/text/source parts", item.TextResult.Content)
	}
}
