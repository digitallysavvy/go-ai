package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func testBatchTextRequest(id, modelID string) provider.BatchV4Request {
	return provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID:      id,
			ModelID: modelID,
			Options: provider.GenerateOptions{
				Prompt: types.Prompt{
					Messages: []types.Message{
						{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hello"}}},
					},
				},
			},
		},
	}
}

// TS: gateway-batch.test.ts "should reject unsupported request types before sending a request"
func TestGatewayBatch_DoStartBatch_RejectsUnsupportedRequestType(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			{Type: provider.BatchRequestTypeImage, Image: &provider.ImageBatchV4Request{ID: "image-1", ModelID: "m"}},
		},
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
	if called {
		t.Fatalf("expected no HTTP request to be sent")
	}
}

// TS: gateway-batch.test.ts "should fail before sending a request when models are mixed"
func TestGatewayBatch_DoStartBatch_RejectsMixedModels(t *testing.T) {
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			testBatchTextRequest("req-1", "test-model-1"),
			testBatchTextRequest("req-2", "test-model-2"),
		},
	})
	var invalidArg *providererrors.InvalidArgumentError
	if err == nil {
		t.Fatalf("expected error")
	}
	if !ok(err, &invalidArg) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
	if called {
		t.Fatalf("expected no HTTP request to be sent")
	}
}

func ok(err error, target **providererrors.InvalidArgumentError) bool {
	if e, isType := err.(*providererrors.InvalidArgumentError); isType {
		*target = e
		return true
	}
	return false
}

// TS: gateway-batch.test.ts "should send the shared request model in the model header" +
// "should send the idempotency-key header from providerOptions.gateway.idempotencyKey without forwarding it in the body"
func TestGatewayBatch_DoStartBatch_HeadersAndIdempotencyKey(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"batchId":"job_123","status":"pending"}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1")},
		Headers:  map[string]string{"Custom-Header": "batch-value"},
		ProviderOptions: map[string]interface{}{
			"gateway": map[string]interface{}{"idempotencyKey": "idem-abc", "order": []interface{}{"openai"}},
		},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}

	if got := gotHeaders.Get("ai-model-id"); got != "test-model-1" {
		t.Fatalf("ai-model-id header = %q, want test-model-1", got)
	}
	if got := gotHeaders.Get("Custom-Header"); got != "batch-value" {
		t.Fatalf("Custom-Header = %q, want batch-value", got)
	}
	if got := gotHeaders.Get("idempotency-key"); got != "idem-abc" {
		t.Fatalf("idempotency-key header = %q, want idem-abc", got)
	}

	providerOptions, _ := gotBody["providerOptions"].(map[string]interface{})
	gatewayOptions, _ := providerOptions["gateway"].(map[string]interface{})
	if _, present := gatewayOptions["idempotencyKey"]; present {
		t.Fatalf("idempotencyKey must not be forwarded in the request body: %+v", gatewayOptions)
	}
	if order, _ := gatewayOptions["order"].([]interface{}); len(order) != 1 || order[0] != "openai" {
		t.Fatalf("gateway.order not forwarded: %+v", gatewayOptions)
	}
}

// TS: gateway-batch.test.ts "should send the modality, per-request model ids, and provider options"
func TestGatewayBatch_DoStartBatch_RequestBodyShape(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"batchId":"job_123","status":"pending"}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	maxTokens := 32
	temp := 0.0
	req2 := testBatchTextRequest("req-2", "test-model-1")
	req2.Text.Options.MaxTokens = &maxTokens
	req2.Text.Options.Temperature = &temp

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1"), req2},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}

	requests, _ := gotBody["requests"].([]interface{})
	if len(requests) != 2 {
		t.Fatalf("requests length = %d, want 2", len(requests))
	}
	first, _ := requests[0].(map[string]interface{})
	if first["id"] != "req-1" || first["type"] != "text" || first["modelId"] != "test-model-1" {
		t.Fatalf("first request = %+v", first)
	}
	second, _ := requests[1].(map[string]interface{})
	secondOptions, _ := second["options"].(map[string]interface{})
	if secondOptions["maxTokens"] != float64(32) {
		t.Fatalf("second request maxTokens = %v, want 32", secondOptions["maxTokens"])
	}
	if secondOptions["temperature"] != float64(0) {
		t.Fatalf("second request temperature = %v, want 0", secondOptions["temperature"])
	}
}

// TS: gateway-batch.test.ts "should map the start response to a BatchV4StartResult"
func TestGatewayBatch_DoStartBatch_MapsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"batchId": "job_123",
			"status": "pending",
			"rawStatus": "validating",
			"requestCounts": {"total": 2, "pending": 2, "completed": 0, "failed": 0},
			"warnings": [{"requestId": "req-1", "warning": {"type": "other", "message": "a warning"}}],
			"providerMetadata": {"gateway": {"asyncJob": {"jobId": "job_123", "status": "queued"}}}
		}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	result, err := batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1")},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}
	if result.BatchID != "job_123" {
		t.Fatalf("BatchID = %q, want job_123", result.BatchID)
	}
	if result.Status != provider.BatchStatusPending || result.RawStatus != "validating" {
		t.Fatalf("Status/RawStatus = %v/%v", result.Status, result.RawStatus)
	}
	if result.RequestCounts == nil || result.RequestCounts.Total != 2 {
		t.Fatalf("RequestCounts = %+v", result.RequestCounts)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].RequestID != "req-1" || result.Warnings[0].Warning.Type != "other" {
		t.Fatalf("Warnings = %+v", result.Warnings)
	}
	if result.ProviderMetadata == nil {
		t.Fatalf("ProviderMetadata missing")
	}
}

// TS: gateway-batch.test.ts describe('doGetBatchStatus')
func TestGatewayBatch_DoGetBatchStatus(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"completed","requestCounts":{"total":1,"pending":0,"completed":1,"failed":0}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	status, err := batch.DoGetBatchStatus(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_123"})
	if err != nil {
		t.Fatalf("DoGetBatchStatus() error = %v", err)
	}
	if gotPath != "/batch/status" {
		t.Fatalf("path = %q, want /batch/status", gotPath)
	}
	if gotBody["batchId"] != "job_123" {
		t.Fatalf("batchId = %v", gotBody["batchId"])
	}
	if status.Status != provider.BatchStatusCompleted {
		t.Fatalf("Status = %v, want completed", status.Status)
	}
}

// TS: gateway-batch.test.ts describe('doCancelBatch')
func TestGatewayBatch_DoCancelBatch(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"failed","providerMetadata":{"gateway":{"cancelled":true}}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()
	canceller, ok := batch.(provider.BatchV4Canceller)
	if !ok {
		t.Fatalf("gateway Batch does not implement BatchV4Canceller")
	}

	result, err := canceller.DoCancelBatch(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_123"})
	if err != nil {
		t.Fatalf("DoCancelBatch() error = %v", err)
	}
	if gotPath != "/batch/cancel" {
		t.Fatalf("path = %q, want /batch/cancel", gotPath)
	}
	if result.ProviderMetadata == nil {
		t.Fatalf("ProviderMetadata missing")
	}
}

// TS: gateway-batch.test.ts describe('doGetBatchResults')
func TestGatewayBatch_DoGetBatchResults_StreamsNDJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(
			`{"type":"text","id":"req-1","status":"succeeded","result":{"text":"hi","finishReason":"stop","usage":{"totalTokens":3}}}` + "\n" +
				`{"type":"text","id":"req-2","status":"failed","error":{"message":"boom","type":"invalid_request_error"}}` + "\n",
		))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	stream, err := batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults() error = %v", err)
	}
	defer stream.Close()

	item1, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if item1.ID != "req-1" || item1.Status != provider.BatchItemSucceeded || item1.TextResult == nil || item1.TextResult.Text != "hi" {
		t.Fatalf("item1 = %+v", item1)
	}

	item2, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if item2.ID != "req-2" || item2.Status != provider.BatchItemFailed || item2.Error == nil || item2.Error.Message != "boom" {
		t.Fatalf("item2 = %+v", item2)
	}

	if _, err := stream.Next(); err != io.EOF {
		t.Fatalf("Next() at end = %v, want io.EOF", err)
	}
}

// TS parity: a "not_found" Gateway error (e.g. an unknown batch id) maps to
// GatewayNotFoundError, not a generic 404.
func TestGatewayBatch_DoGetBatchStatus_NotFoundMapsToGatewayNotFoundError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Resource not found","type":"not_found"}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoGetBatchStatus(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_missing"})
	var notFound *gatewayerrors.GatewayNotFoundError
	if err == nil {
		t.Fatalf("expected error")
	}
	if e, isType := err.(*gatewayerrors.GatewayNotFoundError); isType {
		notFound = e
	}
	if notFound == nil {
		t.Fatalf("err = %v (%T), want GatewayNotFoundError", err, err)
	}
}

// TS parity: a "not_found" Gateway error also maps to GatewayNotFoundError
// for doCancelBatch (gateway-batch.test.ts:654).
func TestGatewayBatch_DoCancelBatch_NotFoundMapsToGatewayNotFoundError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Resource not found","type":"not_found"}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()
	canceller, ok := batch.(provider.BatchV4Canceller)
	if !ok {
		t.Fatalf("gateway Batch does not implement BatchV4Canceller")
	}

	_, err = canceller.DoCancelBatch(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_missing"})
	if _, isType := err.(*gatewayerrors.GatewayNotFoundError); !isType {
		t.Fatalf("err = %v (%T), want GatewayNotFoundError", err, err)
	}
}

// TS parity: a "not_found" Gateway error also maps to GatewayNotFoundError
// for doGetBatchResults (gateway-batch.test.ts:562).
func TestGatewayBatch_DoGetBatchResults_NotFoundMapsToGatewayNotFoundError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Resource not found","type":"not_found"}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoGetBatchResults(context.Background(), provider.BatchV4OperationOptions{BatchID: "job_missing"})
	if _, isType := err.(*gatewayerrors.GatewayNotFoundError); !isType {
		t.Fatalf("err = %v (%T), want GatewayNotFoundError", err, err)
	}
}

// TS: gateway-batch.test.ts "should include callbackUrl in the request body when webhookUrl is provided"
func TestGatewayBatch_DoStartBatch_CallbackURL(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"batchId":"job_123","status":"pending"}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests:   []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1")},
		WebhookURL: "https://example.com/hook",
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}
	if gotBody["callbackUrl"] != "https://example.com/hook" {
		t.Fatalf("callbackUrl = %v, want https://example.com/hook", gotBody["callbackUrl"])
	}
}

// TS: gateway-batch.test.ts "should omit callbackUrl from the request body when webhookUrl is not provided"
func TestGatewayBatch_DoStartBatch_OmitsCallbackURLWhenAbsent(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"batchId":"job_123","status":"pending"}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()

	_, err = batch.DoStartBatch(context.Background(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{testBatchTextRequest("req-1", "test-model-1")},
	})
	if err != nil {
		t.Fatalf("DoStartBatch() error = %v", err)
	}
	if _, present := gotBody["callbackUrl"]; present {
		t.Fatalf("callbackUrl present = %v, want omitted", gotBody["callbackUrl"])
	}
}

// TS: gateway-batch.test.ts "should expose the three batch methods as functions (batch capability duck-type)"
func TestGatewayBatch_ImplementsBatchV4Capabilities(t *testing.T) {
	p, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New error = %v", err)
	}
	batch := p.ExperimentalBatch()
	if batch.Provider() != "gateway.batch" {
		t.Fatalf("Provider() = %q, want gateway.batch", batch.Provider())
	}
	if batch.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion() = %q, want v4", batch.SpecificationVersion())
	}
	if _, ok := batch.(provider.BatchV4Canceller); !ok {
		t.Fatalf("expected batch to implement BatchV4Canceller")
	}
	if _, ok := batch.(provider.BatchV4Lister); ok {
		t.Fatalf("gateway batch does not (yet) implement BatchV4Lister")
	}
}
