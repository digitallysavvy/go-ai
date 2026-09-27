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

// TestBatch_ComputedBetaHeaderWinsOverCallerHeader mirrors TS
// getStartBatchHeaders's combineHeaders ordering: the computed anthropic-beta
// value (from providerOptions.anthropic.anthropicBeta) always wins over an
// anthropic-beta the caller also passed via Headers, not the other way
// around.
func TestBatch_ComputedBetaHeaderWinsOverCallerHeader(t *testing.T) {
	var capturedBeta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msgbatch_1", "type": "message_batch", "processing_status": "in_progress",
			"request_counts": {"processing": 1, "succeeded": 0, "errored": 0, "canceled": 0, "expired": 0},
			"created_at": "2024-01-01T00:00:00Z", "expires_at": "2024-01-02T00:00:00Z"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{textBatchRequest("req-1", ClaudeSonnet4_5, "Hello")},
		ProviderOptions: map[string]interface{}{
			"anthropic": map[string]interface{}{"anthropicBeta": []interface{}{"computed-beta"}},
		},
		Headers: map[string]string{"anthropic-beta": "caller-literal-beta"},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if capturedBeta != "computed-beta" {
		t.Fatalf("anthropic-beta = %q, want computed-beta (computed value must win)", capturedBeta)
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
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
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
		_, _ = w.Write([]byte(`{"id":"msgbatch_1","type":"message_batch","processing_status":"canceling","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
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
				{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":5,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}
			],
			"has_more": true, "last_id": "b1"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{Limit: intPtr(10), Cursor: "prev"})
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
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
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
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`))
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
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","archived_at":"2024-01-01T00:00:00Z","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"https://example.com/results"}`))
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
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":1,"canceled":1,"expired":1},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srv.URL + `/results"}`))
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

// TestBatch_ErroredResultRequiresErrorTypeLiteral mirrors TS
// anthropicBatchResultSchema's discriminated-union member for "errored",
// which requires error.type === "error" (z.literal('error')). A result line
// whose nested envelope uses any other literal fails safeValidateTypes in TS
// and becomes invalidAnthropicBatchResult, not a partially-populated error.
func TestBatch_ErroredResultRequiresErrorTypeLiteral(t *testing.T) {
	resultsBody := `{"custom_id":"bad","result":{"type":"errored","error":{"type":"not_error","error":{"type":"invalid_request_error","message":"bad request"}}}}`

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":1,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srv.URL + `/results"}`))
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

	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.Status != provider.BatchItemFailed || item.Error == nil || item.Error.Code != "invalid_response" {
		t.Fatalf("item = %+v, want invalid_response failure", item)
	}
	if item.Error.Message != "Anthropic returned an invalid Message batch result." {
		t.Fatalf("item.Error.Message = %q", item.Error.Message)
	}
}

// TestBatch_BatchEnvelopeMissingRequiredFieldFails mirrors TS
// anthropicBatchResponseZodSchema requiring id/type/created_at/expires_at:
// createJsonResponseHandler throws when the decoded envelope fails schema
// validation, instead of silently defaulting missing fields.
func TestBatch_BatchEnvelopeMissingRequiredFieldFails(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing id", `{"type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`},
		{"wrong type", `{"id":"b1","type":"not_message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z"}`},
		{"missing created_at", `{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"expires_at":"2024-01-02T00:00:00Z"}`},
		{"missing expires_at", `{"id":"b1","type":"message_batch","processing_status":"in_progress","request_counts":{"processing":1,"succeeded":0,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			p := New(Config{APIKey: "k", BaseURL: srv.URL})
			b := p.ExperimentalBatch()
			_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
				Requests: []provider.BatchV4Request{textBatchRequest("1", "claude-sonnet-4-5", "hi")},
			})
			if err == nil || !providererrors.IsInvalidResponseDataError(err) {
				t.Fatalf("err = %v, want InvalidResponseDataError", err)
			}
		})
	}
}

// TestBatch_ResultsURLOnUntrustedOriginIsRejectedWithoutLeakingCredentials
// guards against following a provider-response results_url off the
// configured base URL's origin: previously the raw API key/version headers
// were attached unconditionally to whatever host results_url named, with no
// SSRF check at all. Now a results_url that isn't same-origin with baseURL
// is validated like any other untrusted download target (fails closed here
// because httptest servers listen on a loopback address, which the SSRF
// blocklist always rejects for untrusted hops) and the untrusted host must
// never even receive a request, let alone the API key.
func TestBatch_ResultsURLOnUntrustedOriginIsRejectedWithoutLeakingCredentials(t *testing.T) {
	var untrustedHit bool
	untrusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		untrustedHit = true
		if r.Header.Get("x-api-key") != "" {
			t.Error("leaked x-api-key to an untrusted origin")
		}
		_, _ = w.Write([]byte(`{"custom_id":"ok","result":{"type":"canceled"}}` + "\n"))
	}))
	defer untrusted.Close()

	mux := http.NewServeMux()
	base := httptest.NewServer(mux)
	defer base.Close()
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":0,"errored":0,"canceled":1,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + untrusted.URL + `/results"}`))
	})

	p := New(Config{APIKey: "secret-key", BaseURL: base.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err == nil {
		t.Fatal("expected the untrusted results_url host to be rejected")
	}
	if untrustedHit {
		t.Fatal("the untrusted origin should never have received a request")
	}
}

// anthropicValidBatchMessageJSON returns a well-formed succeeded-result
// message with the given text, matching the shape anthropic-batch.test.ts's
// messageResultBody helper produces.
func anthropicValidBatchMessageJSON(text string) string {
	return `{"id":"msg_123","type":"message","role":"assistant","model":"claude-3-haiku-20240307",` +
		`"content":[{"type":"text","text":"` + text + `"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":10,"output_tokens":3}}`
}

// TestBatch_FailsInvalidSucceededItemWithoutAbortingLaterResults ports TS
// "fails an invalid succeeded item without aborting later results"
// (anthropic-batch.test.ts:1665): a succeeded message with no content array
// and no usage (just {"type":"message"}) can't be salvaged and must fail
// with invalid_response, without stopping the stream.
func TestBatch_FailsInvalidSucceededItemWithoutAbortingLaterResults(t *testing.T) {
	lines := []string{
		`{"custom_id":"invalid","result":{"type":"succeeded","message":{"type":"message"}}}`,
		`{"custom_id":"valid","result":{"type":"succeeded","message":` + anthropicValidBatchMessageJSON("Paris") + `}}`,
	}
	srv := anthropicBatchResultsServer(t, strings.Join(lines, "\n"))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	if got := items["invalid"]; got == nil || got.Status != provider.BatchItemFailed || got.Error == nil ||
		got.Error.Code != "invalid_response" || got.Error.Message != "Anthropic returned an invalid Message batch result." {
		t.Fatalf("invalid = %+v", got)
	}
	if got := items["valid"]; got == nil || got.Status != provider.BatchItemSucceeded {
		t.Fatalf("valid = %+v", got)
	}
}

// TestBatch_FailsUnknownResultTypeWithoutAbortingLaterResults ports TS
// "fails an unknown result type without aborting later results"
// (anthropic-batch.test.ts:1714).
func TestBatch_FailsUnknownResultTypeWithoutAbortingLaterResults(t *testing.T) {
	lines := []string{
		`{"custom_id":"unknown","result":{"type":"future_result","data":"opaque"}}`,
		`{"custom_id":"valid","result":{"type":"succeeded","message":` + anthropicValidBatchMessageJSON("Paris") + `}}`,
	}
	srv := anthropicBatchResultsServer(t, strings.Join(lines, "\n"))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	if got := items["unknown"]; got == nil || got.Status != provider.BatchItemFailed || got.Error == nil || got.Error.Code != "invalid_response" {
		t.Fatalf("unknown = %+v", got)
	}
	if got := items["valid"]; got == nil || got.Status != provider.BatchItemSucceeded {
		t.Fatalf("valid = %+v", got)
	}
}

// TestBatch_SkipsUnknownContentBlocksButFailsOnMalformedKnownBlock ports TS
// "skips unknown content blocks in a succeeded result"
// (anthropic-batch.test.ts:1760): an unrecognized content-block type is
// dropped and the rest of the message still succeeds; a recognized type
// (text) missing its required field (text) fails the whole item.
func TestBatch_SkipsUnknownContentBlocksButFailsOnMalformedKnownBlock(t *testing.T) {
	futureContentMessage := `{"id":"msg_123","type":"message","role":"assistant","model":"claude-3-haiku-20240307",` +
		`"content":[{"type":"future_content","data":"opaque"},{"type":"text","text":"Paris"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":10,"output_tokens":3}}`
	malformedContentMessage := `{"id":"msg_124","type":"message","role":"assistant","model":"claude-3-haiku-20240307",` +
		`"content":[{"type":"text"},{"type":"text","text":"Paris"}],"stop_reason":"end_turn",` +
		`"usage":{"input_tokens":10,"output_tokens":3}}`
	lines := []string{
		`{"custom_id":"future-content","result":{"type":"succeeded","message":` + futureContentMessage + `}}`,
		`{"custom_id":"malformed-known-content","result":{"type":"succeeded","message":` + malformedContentMessage + `}}`,
	}
	srv := anthropicBatchResultsServer(t, strings.Join(lines, "\n"))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	future := items["future-content"]
	if future == nil || future.Status != provider.BatchItemSucceeded || future.TextResult == nil || future.TextResult.Text != "Paris" {
		t.Fatalf("future-content = %+v (TextResult = %+v)", future, future.TextResult)
	}
	if got := items["malformed-known-content"]; got == nil || got.Status != provider.BatchItemFailed || got.Error == nil || got.Error.Code != "invalid_response" {
		t.Fatalf("malformed-known-content = %+v", got)
	}
}

// TestBatch_PreservesSignedCompactionBlock ports TS "preserves signed
// compaction blocks in batch results" (anthropic-batch.test.ts:1046): a
// succeeded batch message whose only content block is a signed "compaction"
// block converts to a text content part carrying the compaction
// providerMetadata, through the same convertResponseWithOptions path
// TestCompactionResponse_PreserveSignature exercises directly.
func TestBatch_PreservesSignedCompactionBlock(t *testing.T) {
	message := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",` +
		`"content":[{"type":"compaction","content":"Summary of the conversation.","signature":"compaction-signature"}],` +
		`"stop_reason":"compaction","usage":{"input_tokens":0,"output_tokens":0}}`
	resultsBody := `{"custom_id":"compaction","result":{"type":"succeeded","message":` + message + `}}`
	srv := anthropicBatchResultsServer(t, resultsBody)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	got := items["compaction"]
	if got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult == nil {
		t.Fatalf("compaction = %+v", got)
	}
	if len(got.TextResult.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1", len(got.TextResult.Content))
	}
	text, ok := got.TextResult.Content[0].(types.TextContent)
	if !ok {
		t.Fatalf("Content[0] = %T, want types.TextContent", got.TextResult.Content[0])
	}
	if text.Text != "Summary of the conversation." {
		t.Errorf("text.Text = %q", text.Text)
	}
	var meta map[string]map[string]interface{}
	if err := json.Unmarshal(text.ProviderMetadata, &meta); err != nil {
		t.Fatalf("decode providerMetadata: %v", err)
	}
	if meta["anthropic"]["type"] != "compaction" || meta["anthropic"]["signature"] != "compaction-signature" {
		t.Errorf("providerMetadata.anthropic = %+v", meta["anthropic"])
	}
	if got.TextResult.FinishReason != types.FinishReasonOther {
		t.Errorf("FinishReason = %v, want %v", got.TextResult.FinishReason, types.FinishReasonOther)
	}
}

// TestBatch_PreservesClientAndProviderExecutedToolContent ports TS
// "preserves client and provider-executed tool content"
// (anthropic-batch.test.ts:1104): a succeeded batch message mixing a client
// tool_use, two server_tool_use variants (web_search, code_execution) and
// their web_search_tool_result all convert the same way they do outside a
// batch, through the shared convertResponseWithOptions path with
// markCodeExecutionDynamic forced on (batch results have no original tool
// list to consult).
func TestBatch_PreservesClientAndProviderExecutedToolContent(t *testing.T) {
	content := `[` +
		`{"type":"tool_use","id":"toolu_123","name":"get_weather","input":{"city":"Paris"}},` +
		`{"type":"server_tool_use","id":"srvtoolu_123","name":"web_search","input":{"query":"weather Paris"}},` +
		`{"type":"server_tool_use","id":"code_123","name":"code_execution","input":{"code":"print(\"Paris\")"}},` +
		`{"type":"web_search_tool_result","tool_use_id":"srvtoolu_123","content":[` +
		`{"type":"web_search_result","url":"https://example.com/weather","title":"Paris weather","encrypted_content":"encrypted"}` +
		`]}` +
		`]`
	message := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",` +
		`"content":` + content + `,"stop_reason":"tool_use","usage":{"input_tokens":0,"output_tokens":0}}`
	resultsBody := `{"custom_id":"tool-call","result":{"type":"succeeded","message":` + message + `}}`
	srv := anthropicBatchResultsServer(t, resultsBody)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	got := items["tool-call"]
	if got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult == nil {
		t.Fatalf("tool-call = %+v", got)
	}
	res := got.TextResult

	if len(res.ToolCalls) != 3 {
		t.Fatalf("len(ToolCalls) = %d, want 3: %+v", len(res.ToolCalls), res.ToolCalls)
	}
	weather := res.ToolCalls[0]
	if weather.ID != "toolu_123" || weather.ToolName != "get_weather" || weather.Arguments["city"] != "Paris" || weather.ProviderExecuted {
		t.Errorf("ToolCalls[0] (client tool_use) = %+v", weather)
	}
	webSearch := res.ToolCalls[1]
	if webSearch.ID != "srvtoolu_123" || webSearch.ToolName != "web_search" || !webSearch.ProviderExecuted || webSearch.Dynamic {
		t.Errorf("ToolCalls[1] (server_tool_use web_search) = %+v", webSearch)
	}
	codeExec := res.ToolCalls[2]
	if codeExec.ID != "code_123" || codeExec.ToolName != "code_execution" || !codeExec.ProviderExecuted || !codeExec.Dynamic {
		t.Errorf("ToolCalls[2] (server_tool_use code_execution) = %+v", codeExec)
	}
	if codeExec.Arguments["type"] != "programmatic-tool-call" || codeExec.Arguments["code"] != `print("Paris")` {
		t.Errorf("ToolCalls[2].Arguments = %+v", codeExec.Arguments)
	}

	// web_search_tool_result: a ToolResultContent plus a synthesized source
	// content part, both appended to Content in that order.
	var toolResult *types.ToolResultContent
	var source *types.SourceContent
	for i := range res.Content {
		switch c := res.Content[i].(type) {
		case types.ToolResultContent:
			toolResult = &c
		case types.SourceContent:
			source = &c
		}
	}
	if toolResult == nil || toolResult.ToolCallID != "srvtoolu_123" || toolResult.ToolName != "web_search" || !toolResult.ProviderExecuted {
		t.Fatalf("web_search_tool_result ToolResultContent = %+v", toolResult)
	}
	resultList, ok := toolResult.Result.([]map[string]interface{})
	if !ok || len(resultList) != 1 || resultList[0]["url"] != "https://example.com/weather" || resultList[0]["encryptedContent"] != "encrypted" {
		t.Fatalf("web_search_tool_result.Result = %#v", toolResult.Result)
	}
	if source == nil || source.SourceType != "url" || source.URL != "https://example.com/weather" || source.Title != "Paris weather" {
		t.Fatalf("web_search source = %+v", source)
	}
}

// TestBatch_NormalizesAdvisorToolResults ports TS "normalizes advisor tool
// results to the provider output shape" (anthropic-batch.test.ts:1568): the
// three advisor_tool_result content variants (plain, redacted, error) map to
// ToolResultContent with the shared convertAdvisorResult helper, the same as
// outside a batch.
func TestBatch_NormalizesAdvisorToolResults(t *testing.T) {
	content := `[` +
		`{"type":"advisor_tool_result","tool_use_id":"advisor-plain","content":{"type":"advisor_result","text":"Use a queue.","stop_reason":"end_turn"}},` +
		`{"type":"advisor_tool_result","tool_use_id":"advisor-redacted","content":{"type":"advisor_redacted_result","encrypted_content":"opaque-advice","stop_reason":"max_tokens"}},` +
		`{"type":"advisor_tool_result","tool_use_id":"advisor-error","content":{"type":"advisor_tool_result_error","error_code":"max_uses_exceeded"}}` +
		`]`
	message := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",` +
		`"content":` + content + `,"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0}}`
	resultsBody := `{"custom_id":"advisor-results","result":{"type":"succeeded","message":` + message + `}}`
	srv := anthropicBatchResultsServer(t, resultsBody)
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "b1"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	items := drainAnthropicBatchResults(t, stream)
	got := items["advisor-results"]
	if got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult == nil {
		t.Fatalf("advisor-results = %+v", got)
	}
	content2 := got.TextResult.Content
	if len(content2) != 3 {
		t.Fatalf("len(Content) = %d, want 3: %+v", len(content2), content2)
	}

	plain, ok := content2[0].(types.ToolResultContent)
	if !ok || plain.ToolCallID != "advisor-plain" || plain.ToolName != "advisor" || plain.Error != "" {
		t.Fatalf("Content[0] (advisor_result) = %+v (ok=%v)", content2[0], ok)
	}
	plainResult, ok := plain.Result.(map[string]interface{})
	if !ok || plainResult["type"] != "advisor_result" || plainResult["text"] != "Use a queue." || plainResult["stopReason"] != "end_turn" {
		t.Fatalf("Content[0].Result = %#v", plain.Result)
	}

	redacted, ok := content2[1].(types.ToolResultContent)
	if !ok || redacted.ToolCallID != "advisor-redacted" || redacted.ToolName != "advisor" || redacted.Error != "" {
		t.Fatalf("Content[1] (advisor_redacted_result) = %+v (ok=%v)", content2[1], ok)
	}
	redactedResult, ok := redacted.Result.(map[string]interface{})
	if !ok || redactedResult["type"] != "advisor_redacted_result" || redactedResult["encryptedContent"] != "opaque-advice" || redactedResult["stopReason"] != "max_tokens" {
		t.Fatalf("Content[1].Result = %#v", redacted.Result)
	}

	errored, ok := content2[2].(types.ToolResultContent)
	if !ok || errored.ToolCallID != "advisor-error" || errored.ToolName != "advisor" || errored.Error != "max_uses_exceeded" {
		t.Fatalf("Content[2] (advisor_tool_result_error) = %+v (ok=%v)", content2[2], ok)
	}
	erroredResult, ok := errored.Result.(map[string]interface{})
	if !ok || erroredResult["type"] != "advisor_tool_result_error" || erroredResult["errorCode"] != "max_uses_exceeded" {
		t.Fatalf("Content[2].Result = %#v", errored.Result)
	}
}

// anthropicBatchResultsServer starts a batch server that reports batch "b1"
// as ended (with results_url same-origin), streaming resultsBody as the
// results file's NDJSON content.
func anthropicBatchResultsServer(t *testing.T, resultsBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/messages/batches/b1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"b1","type":"message_batch","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0},"created_at":"2024-01-01T00:00:00Z","expires_at":"2024-01-02T00:00:00Z","results_url":"` + srv.URL + `/results"}`))
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(resultsBody + "\n"))
	})
	return srv
}

func drainAnthropicBatchResults(t *testing.T, stream provider.BatchV4ItemResultStream) map[string]*provider.BatchV4ItemResult {
	t.Helper()
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
	return items
}

func asInvalidArgumentError(err error, target **providererrors.InvalidArgumentError) bool {
	if e, ok := err.(*providererrors.InvalidArgumentError); ok {
		*target = e
		return true
	}
	return false
}
