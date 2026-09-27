package openai

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

func openaiIntPtr(v int) *int { return &v }

// Ported from packages/openai/src/openai-batch.test.ts.

func openaiTextBatchRequest(id, modelID, prompt string) provider.BatchV4Request {
	return provider.BatchV4Request{
		Type: provider.BatchRequestTypeText,
		Text: &provider.TextBatchV4Request{
			ID:      id,
			ModelID: modelID,
			Options: provider.GenerateOptions{Prompt: types.Prompt{Text: prompt}},
		},
	}
}

func TestOpenAIBatch_Metadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	if b.SpecificationVersion() != "v4" {
		t.Fatalf("SpecificationVersion = %q", b.SpecificationVersion())
	}
	if b.Provider() != "openai.batch" {
		t.Fatalf("Provider = %q, want openai.batch", b.Provider())
	}
	if len(b.SupportedURLs()) == 0 {
		t.Fatal("SupportedURLs should not be empty")
	}
}

func TestOpenAIBatch_RejectsUnsupportedRequestType(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{{Type: provider.BatchRequestTypeImage, Image: &provider.ImageBatchV4Request{ID: "image-1"}}},
	})
	if err == nil || !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestOpenAIBatch_RejectsMixedModels(t *testing.T) {
	p := New(Config{APIKey: "k"})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			openaiTextBatchRequest("a", "gpt-5.6", "Hello"),
			openaiTextBatchRequest("b", "gpt-4o", "World"),
		},
	})
	if err == nil {
		t.Fatal("expected an error for mixed models")
	}
}

func TestOpenAIBatch_CreatesBatchFromPreparedJSONL(t *testing.T) {
	var uploadForm map[string]string
	var uploadFileName string
	var uploadFileContent string
	var batchCreateBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/files":
			uploadForm, uploadFileName, uploadFileContent = decodeMultipartFieldsAndFile(t, r)
			_, _ = w.Write([]byte(`{"id":"file-input","object":"file","filename":"batch.jsonl","purpose":"batch","expires_at":1700172800}`))
		case r.URL.Path == "/batches":
			_ = json.NewDecoder(r.Body).Decode(&batchCreateBody)
			_, _ = w.Write([]byte(`{"id":"batch_123","status":"validating","output_file_id":null,"error_file_id":null,"created_at":1700000000,"expires_at":1700086400,"request_counts":{"total":2,"completed":0,"failed":0},"errors":null}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()

	germanyReq := openaiTextBatchRequest("germany", "gpt-5.6", "What is the capital of Germany?")
	germanyReq.Text.Options.TopK = openaiIntPtr(10)

	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{
			openaiTextBatchRequest("france", "gpt-5.6", "What is the capital of France?"),
			germanyReq,
		},
		WebhookURL: "https://example.com/batch-webhook",
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}

	if result.BatchID != "batch_123" {
		t.Fatalf("BatchID = %q", result.BatchID)
	}
	if result.Status != provider.BatchStatusPending || result.RawStatus != "validating" {
		t.Fatalf("Status = %+v", result.BatchV4Status)
	}
	if result.RequestCounts == nil || result.RequestCounts.Total != 2 || result.RequestCounts.Pending != 2 {
		t.Fatalf("RequestCounts = %+v", result.RequestCounts)
	}
	meta, _ := result.ProviderMetadata["openai"].(map[string]interface{})
	if meta["inputFileId"] != "file-input" {
		t.Fatalf("providerMetadata = %+v", meta)
	}
	if _, ok := meta["inputFileExpiresAt"]; !ok {
		t.Fatal("inputFileExpiresAt should be present when the upload response carries an expiry")
	}

	foundWebhookWarning := false
	foundTopKWarning := false
	for _, w := range result.Warnings {
		if w.Warning.Feature == "webhookUrl" {
			foundWebhookWarning = true
		}
		if w.RequestID == "germany" && w.Warning.Feature == "topK" {
			foundTopKWarning = true
		}
	}
	if !foundWebhookWarning {
		t.Fatalf("warnings = %+v, want a webhookUrl warning", result.Warnings)
	}
	if !foundTopKWarning {
		t.Fatalf("warnings = %+v, want a topK warning for the germany request", result.Warnings)
	}

	if uploadForm["purpose"] != "batch" || uploadForm["expires_after[anchor]"] != "created_at" || uploadForm["expires_after[seconds]"] != "172800" {
		t.Fatalf("upload form = %+v", uploadForm)
	}
	if uploadFileName != "batch.jsonl" {
		t.Fatalf("upload filename = %q", uploadFileName)
	}

	lines := strings.Split(strings.TrimSpace(uploadFileContent), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	var franceLine map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &franceLine); err != nil {
		t.Fatalf("unmarshal line 0: %v", err)
	}
	if franceLine["custom_id"] != "france" || franceLine["method"] != "POST" || franceLine["url"] != "/v1/responses" {
		t.Fatalf("line 0 = %+v", franceLine)
	}
	body, _ := franceLine["body"].(map[string]interface{})
	if body["model"] != "gpt-5.6" {
		t.Fatalf("line 0 body = %+v", body)
	}

	if batchCreateBody["input_file_id"] != "file-input" || batchCreateBody["endpoint"] != "/v1/responses" || batchCreateBody["completion_window"] != "24h" {
		t.Fatalf("batch create body = %+v", batchCreateBody)
	}
}

func TestOpenAIBatch_OmitsInputFileExpiresAtWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/files" {
			_, _ = w.Write([]byte(`{"id":"file-input","object":"file"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":null,"error_file_id":null}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{openaiTextBatchRequest("a", "gpt-5.6", "Hi")},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	meta, _ := result.ProviderMetadata["openai"].(map[string]interface{})
	if _, ok := meta["inputFileExpiresAt"]; ok {
		t.Fatalf("inputFileExpiresAt should be omitted, got %+v", meta)
	}
}

func TestOpenAIBatch_AppliesInputFileExpiresAfterProviderOption(t *testing.T) {
	var uploadForm map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/files" {
			uploadForm = decodeMultipartFields(t, r)
			_, _ = w.Write([]byte(`{"id":"file-input"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests:        []provider.BatchV4Request{openaiTextBatchRequest("a", "gpt-5.6", "Hi")},
		ProviderOptions: map[string]interface{}{"openai": map[string]interface{}{"inputFileExpiresAfter": float64(3600)}},
	})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	if uploadForm["expires_after[seconds]"] != "3600" {
		t.Fatalf("form = %+v", uploadForm)
	}
}

func TestOpenAIBatch_WarnsOnUnsupportedProviderTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/files" {
			_, _ = w.Write([]byte(`{"id":"file-input"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	req := openaiTextBatchRequest("a", "gpt-5.6", "Hi")
	req.Text.Options.Tools = []types.Tool{
		{Type: types.ToolTypeProviderDefined, Name: "image_generation", ProviderID: "openai.image_generation"},
	}
	result, err := b.DoStartBatch(t.Context(), provider.BatchV4StartOptions{Requests: []provider.BatchV4Request{req}})
	if err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w.Warning.Feature, "image_generation") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %+v, want a warning about the unconvertible provider tool", result.Warnings)
	}
}

func TestOpenAIBatch_CancelsBatch(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"cancelling"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Canceller)
	_, err := b.DoCancelBatch(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoCancelBatch: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/batches/batch_123/cancel" {
		t.Fatalf("method=%s path=%s", gotMethod, gotPath)
	}
}

func TestOpenAIBatch_ListsAndNormalizesBatches(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id":"batch_123","status":"in_progress","request_counts":{"total":3,"completed":1,"failed":0},"created_at":1700000000,"expires_at":1700086400},
				{"id":"batch_122","status":"completed","request_counts":{"total":2,"completed":2,"failed":0},"created_at":1700000000,"expires_at":1700086400}
			],
			"has_more": true, "last_id": "batch_122"
		}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch().(provider.BatchV4Lister)
	result, err := b.DoListBatches(t.Context(), provider.BatchV4ListOptions{Limit: 2, Cursor: "batch_122"})
	if err != nil {
		t.Fatalf("DoListBatches: %v", err)
	}
	if !strings.Contains(gotQuery, "limit=2") || !strings.Contains(gotQuery, "after=batch_122") {
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
	if result.NextCursor != "batch_122" {
		t.Fatalf("NextCursor = %q", result.NextCursor)
	}
}

func TestOpenAIBatch_OmitsNextCursorWhenNoMore(t *testing.T) {
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
	if result.NextCursor != "" || len(result.Batches) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIBatch_MapsStatuses(t *testing.T) {
	cases := []struct {
		raw  string
		want provider.BatchStatusValue
	}{
		{"validating", provider.BatchStatusPending},
		{"in_progress", provider.BatchStatusPending},
		{"finalizing", provider.BatchStatusPending},
		{"cancelling", provider.BatchStatusPending},
		{"completed", provider.BatchStatusCompleted},
		{"failed", provider.BatchStatusFailed},
		{"expired", provider.BatchStatusFailed},
		{"cancelled", provider.BatchStatusFailed},
		{"future_status", provider.BatchStatusPending},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"batch_123","status":"` + tc.raw + `"}`))
		}))
		p := New(Config{APIKey: "k", BaseURL: srv.URL})
		b := p.ExperimentalBatch()
		status, err := b.DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
		srv.Close()
		if err != nil {
			t.Fatalf("status %s: %v", tc.raw, err)
		}
		if status.Status != tc.want || status.RawStatus != tc.raw {
			t.Fatalf("status %s: got %+v", tc.raw, status)
		}
	}
}

func TestOpenAIBatch_NormalizesCountsTimestampsAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"failed","request_counts":{"total":5,"completed":2,"failed":1},"errors":{"data":[{"code":"invalid_request","message":"Invalid input file."}]},"created_at":1700000000,"expires_at":1700086400}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	status, err := b.DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchStatus: %v", err)
	}
	if status.RequestCounts.Total != 5 || status.RequestCounts.Pending != 2 || status.RequestCounts.Completed != 2 || status.RequestCounts.Failed != 1 {
		t.Fatalf("RequestCounts = %+v", status.RequestCounts)
	}
	if status.Error == nil || status.Error.Code != "invalid_request" || status.Error.Message != "Invalid input file." {
		t.Fatalf("Error = %+v", status.Error)
	}
}

func TestOpenAIBatch_AcceptsIncompleteErrorDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"failed","errors":{"data":[{"code":"invalid_request"}]}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	status, err := b.DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchStatus: %v", err)
	}
	if status.Error == nil || status.Error.Code != "invalid_request" || status.Error.Message != "OpenAI batch failed." {
		t.Fatalf("Error = %+v", status.Error)
	}
}

func responsesResultBodyJSON(id, text string) string {
	return `{"custom_id":"` + id + `","response":{"status_code":200,"request_id":"openai-` + id + `","body":{"id":"resp_123","created_at":1700000000,"model":"gpt-5.6","output":[{"type":"message","role":"assistant","id":"msg_123","content":[{"type":"output_text","text":"` + text + `"}]}],"usage":{"input_tokens":10,"output_tokens":3}}},"error":null}`
}

func TestOpenAIBatch_StreamsResults(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/batches/batch_123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":"file-output","error_file_id":"file-errors"}`))
	})
	mux.HandleFunc("/files/file-output/content", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(responsesResultBodyJSON("france", "Paris") + "\n"))
	})
	mux.HandleFunc("/files/file-errors/content", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"custom_id":"failed-1","response":null,"error":{"code":"batch_cancelled","message":"cancelled"}}` + "\n"))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
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

	if got := items["france"]; got == nil || got.Status != provider.BatchItemSucceeded || got.TextResult == nil || got.TextResult.Text != "Paris" {
		t.Fatalf("france = %+v", got)
	}
	if got := items["failed-1"]; got == nil || got.Status != provider.BatchItemCancelled {
		t.Fatalf("failed-1 = %+v", got)
	}
}

func TestOpenAIBatch_PreservesReasoningOutput(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/batches/batch_123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":"file-output"}`))
	})
	mux.HandleFunc("/files/file-output/content", func(w http.ResponseWriter, r *http.Request) {
		body := `{"id":"resp_123","created_at":1700000000,"model":"gpt-5.6","output":[` +
			`{"type":"reasoning","id":"reasoning-123","encrypted_content":"encrypted-reasoning","summary":[{"type":"summary_text","text":"I should answer directly."}]},` +
			`{"type":"message","role":"assistant","id":"msg_123","content":[{"type":"output_text","text":"Paris"}]}` +
			`],"usage":{"input_tokens":10,"output_tokens":3}}`
		line := `{"custom_id":"reasoning","response":{"status_code":200,"body":` + body + `},"error":null}`
		_, _ = w.Write([]byte(line + "\n"))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if item.Status != provider.BatchItemSucceeded {
		t.Fatalf("item = %+v", item)
	}
	var foundReasoning, foundText bool
	for _, c := range item.TextResult.Content {
		if rc, ok := c.(types.ReasoningContent); ok {
			foundReasoning = true
			if rc.Text != "I should answer directly." || rc.EncryptedContent != "encrypted-reasoning" {
				t.Fatalf("reasoning content = %+v", rc)
			}
		}
		if tc, ok := c.(types.TextContent); ok && tc.Text == "Paris" {
			foundText = true
		}
	}
	if !foundReasoning || !foundText {
		t.Fatalf("content = %+v", item.TextResult.Content)
	}
}

func TestOpenAIBatch_ErrorsStreamOnMalformedJSONLine(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/batches/batch_123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":"file-output"}`))
	})
	mux.HandleFunc("/files/file-output/content", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(responsesResultBodyJSON("france", "Paris") + "\n{not json}\n"))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err != nil {
		t.Fatalf("DoGetBatchResults: %v", err)
	}
	defer stream.Close()

	item, err := stream.Next()
	if err != nil {
		t.Fatalf("Next (line 1): %v", err)
	}
	if item.ID != "france" || item.Status != provider.BatchItemSucceeded {
		t.Fatalf("item = %+v", item)
	}
	_, err = stream.Next()
	if err == nil || err == io.EOF {
		t.Fatalf("expected a JSON parse error on the malformed line, got %v", err)
	}
}

func TestOpenAIBatch_RejectsResultRetrievalWhilePending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"in_progress"}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err == nil {
		t.Fatal("expected an error while pending")
	}
}

func TestOpenAIBatch_RejectsCompletedBatchWithoutOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":null,"error_file_id":null}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	_, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
	if err == nil {
		t.Fatal("expected an error for a completed batch without output")
	}
}

func TestOpenAIBatch_FailedResultsAndUnsupportedItemsDontStopStream(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()
	mux.HandleFunc("/batches/batch_123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"batch_123","status":"completed","output_file_id":"file-output"}`))
	})
	mux.HandleFunc("/files/file-output/content", func(w http.ResponseWriter, r *http.Request) {
		lines := []string{
			responsesResultBodyJSON("ok", "Paris"),
			`{"custom_id":"http-error","response":{"status_code":429,"body":{"error":{"message":"rate limited","type":"rate_limit_error"}}},"error":null}`,
			`{"custom_id":"no-response","response":null,"error":null}`,
		}
		_, _ = w.Write([]byte(strings.Join(lines, "\n") + "\n"))
	})

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	b := p.ExperimentalBatch()
	stream, err := b.DoGetBatchResults(t.Context(), provider.BatchV4OperationOptions{BatchID: "batch_123"})
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
	if got := items["http-error"]; got == nil || got.Status != provider.BatchItemFailed || got.Error.Message != "rate limited" || got.Error.StatusCode != 429 {
		t.Fatalf("http-error = %+v", got)
	}
	if got := items["no-response"]; got == nil || got.Status != provider.BatchItemFailed || got.Error.Code != "invalid_batch_result" {
		t.Fatalf("no-response = %+v", got)
	}
}

// --- test helpers shared with files_api_v4_test.go are reused via
// decodeMultipartFields/decodeMultipartFieldsAndFile.
