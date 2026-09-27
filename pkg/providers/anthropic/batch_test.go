package anthropic

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

// Ported from packages/anthropic/src/anthropic-batch.test.ts.

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

func TestBatch_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	if b.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", b.SpecificationVersion())
	}
	if b.Provider() != "anthropic.batch" {
		t.Fatalf("Provider = %q, want anthropic.batch", b.Provider())
	}
	urls := b.SupportedURLs()
	if len(urls) == 0 {
		t.Fatal("SupportedURLs should not be empty")
	}
}

func TestBatch_RejectsUnsupportedRequestType(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{{Type: provider.BatchRequestTypeImage, Image: &provider.ImageBatchV4Request{ID: "1"}}},
	})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBatch_StartsBatchAndCombinesBetas(t *testing.T) {
	var capturedBody map[string]interface{}
	var capturedBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages/batches" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		capturedBeta = r.Header.Get("anthropic-beta")
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msgbatch_1", "type": "message_batch", "processing_status": "in_progress",
			"request_counts": {"processing": 2, "succeeded": 0, "errored": 0, "canceled": 0, "expired": 0},
			"created_at": "2024-01-01T00:00:00Z", "expires_at": "2024-01-02T00:00:00Z"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			textBatchRequest("req-1", ClaudeSonnet4_5, "Hello"),
			textBatchRequest("req-2", ClaudeSonnet4_5, "World"),
		},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"my-explicit-beta"}},
		},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if result.BatchID != "msgbatch_1" {
		t.Fatalf("BatchID = %q", result.BatchID)
	}
	if result.Status != provider.BatchStatusPending {
		t.Fatalf("Status = %q", result.Status)
	}
	if result.RequestCounts == nil || result.RequestCounts.Pending != 2 {
		t.Fatalf("RequestCounts = %+v", result.RequestCounts)
	}
	requests, ok := capturedBody["requests"].([]interface{})
	if !ok || len(requests) != 2 {
		t.Fatalf("requests = %+v", capturedBody["requests"])
	}
	first := requests[0].(map[string]interface{})
	if first["custom_id"] != "req-1" {
		t.Fatalf("custom_id = %v", first["custom_id"])
	}
	params, ok := first["params"].(map[string]interface{})
	if !ok || params["model"] != ClaudeSonnet4_5 {
		t.Fatalf("params = %+v", params)
	}
	betas := strings.Split(capturedBeta, ",")
	found := false
	for _, beta := range betas {
		if beta == "my-explicit-beta" {
			found = true
		}
	}
	if !found {
		t.Fatalf("anthropic-beta = %q, want to include my-explicit-beta", capturedBeta)
	}
}

func TestBatch_RejectsPerRequestBetas(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	req := textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")
	req.Text.Options.ProviderOptions = map[string]interface{}{
		"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"per-request-beta"}},
	}
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBatch_RejectsInvalidRequestIDs(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{textBatchRequest("has a space", ClaudeSonnet4_5, "Hello")},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid request ID")
	}
	var invalidArgErr *providererrors.InvalidArgumentError
	if !asInvalidArgumentError(err, &invalidArgErr) {
		t.Fatalf("err type = %T", err)
	}
}

func TestBatch_RejectsDuplicateRequestIDs(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			textBatchRequest("dup", ClaudeSonnet4_5, "Hello"),
			textBatchRequest("dup", ClaudeSonnet4_5, "World"),
		},
	})
	if err == nil {
		t.Fatal("expected an error for a duplicate request ID")
	}
}

func TestBatch_RejectsJSONToolFallback(t *testing.T) {
	notNative := false
	p := New(Config{APIKey: "k", SupportsNativeStructuredOutput: &notNative})
	b := p.ExperimentalBatch()
	req := textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")
	req.Text.Options.ResponseFormat = &provider.ResponseFormat{
		Type:   "json",
		Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"a": map[string]interface{}{"type": "string"}}},
	}
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBatch_RejectsAliasedProviderToolNames(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	req := textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")
	req.Text.Options.Tools = []types.Tool{
		{Type: types.ToolTypeProviderDefined, Name: "anthropic.computer_toolset_20260801", ProviderID: "anthropic.computer_20250124"},
	}
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBatch_WebhookURLProducesWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":""}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests:   []provider.BatchV4Request{textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")},
		WebhookURL: "https://example.com/webhook",
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if w.Warning.Feature == "webhookUrl" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want a webhookUrl unsupported warning", result.Warnings)
	}
}

func TestBatch_CancelsBatch(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msgbatch_1","type":"message_batch","processing_status":"canceling","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":""}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Canceller)
	_, err := b.DoCancelBatch(t.Context(), provider.BatchV4OperationOptions{BatchID: "msgbatch_1"})
	if err != nil {
		t.Fatalf("DoCancelBatch: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/messages/batches/msgbatch_1/cancel" {
		t.Fatalf("method=%s path=%s", gotMethod, gotPath)
	}
}

func TestBatch_ListsAndNormalizesBatches(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":5,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":""}
			],
			"has_more": true, "last_id": "b1"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{Limit: 10, Cursor: "prev"})
	if err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if !strings.Contains(gotQuery, "limit=10") || !strings.Contains(gotQuery, "after_id=prev") {
		t.Fatalf("query = %q", gotQuery)
	}
	if len(result.Batches) != 1 || result.Batches[0].BatchID != "b1" {
		t.Fatalf("Batches = %+v", result.Batches)
	}
	if result.Batches[0].Status != provider.BatchStatusCompleted {
		t.Fatalf("Status = %q", result.Batches[0].Status)
	}
	if result.NextCursor != "b1" {
		t.Fatalf("NextCursor = %q", result.NextCursor)
	}
}

func TestBatch_OmitsNextCursorWhenNoMore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"last_id":null}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{})
	if err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if result.NextCursor != "" {
		t.Fatalf("NextCursor = %q, want empty", result.NextCursor)
	}
}

func TestBatch_RejectsResultRetrievalWhilePending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":""}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err == nil {
		t.Fatal("expected an error while the batch is pending")
	}
}

func TestBatch_RejectsCompletedBatchWithoutOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":""}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err == nil {
		t.Fatal("expected an error when the batch completed without results_url")
	}
}

func TestBatch_RejectsArchivedResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","archived_at":"2024-01-01T00:00:00Z","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"","expires_at":"","results_url":"https://example.com/results"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err == nil {
		t.Fatal("expected an error for archived results")
	}
}

func TestBatch_StreamsAllResultVariants(t *testing.T) {
	resultsBody := strings.Join([]string{
		`{"custom_id":"ok","result":{"type":"succeeded","message":{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-sonnet-4-5","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}}}`,
		`{"custom_id":"err","result":{"type":"errored","error":{"type":"error","error":{"type":"invalid_request_error","message":"bad request"},"request_id":"req_123"}}}`,
		`{"custom_id":"cancelled","result":{"type":"canceled"}}`,
		`{"custom_id":"expired","result":{"type":"expired"}}`,
	}, "\n")

	// results_url must be an absolute URL, so the mux is registered against
	// the server after it's started (its address is embedded in the batch
	// status response).
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":1,"canceled":1,"expired":1},"created_at":"","expires_at":"","results_url":"` + srv.URL + `/results"}`))
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(resultsBody))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
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

	if got := items["ok"]; got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult == nil || got.TextResult.Text != "hi" {
		t.Fatalf("ok = %+v", got)
	}
	if got := items["err"]; got == nil || got.Status != provider.BatchItemFailed || got.Error == nil || got.Error.Message != "bad request" {
		t.Fatalf("err = %+v", got)
	}
	if got := items["cancelled"]; got == nil || got.Status != provider.BatchItemCancelled {
		t.Fatalf("cancelled = %+v", got)
	}
	if got := items["expired"]; got == nil || got.Status != provider.BatchItemExpired {
		t.Fatalf("expired = %+v", got)
	}
}

func asInvalidArgumentError(err error, target **providererrors.InvalidArgumentError) bool {
	if e, ok := err.(*providererrors.InvalidArgumentError); ok {
		*target = e
		return true
	}
	return false
}
