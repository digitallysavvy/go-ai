package ai

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
)

func isInvalidArgumentError(err error) bool {
	var target *providererrors.InvalidArgumentError
	return errors.As(err, &target)
}

type mockBatchV4 struct {
	providerName string
	startFn      func(context.Context, provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error)
	statusFn     func(context.Context, provider.BatchV4OperationOptions) (*provider.BatchV4Status, error)
	resultsFn    func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error)
}

func (m *mockBatchV4) SpecificationVersion() string { return "v4" }
func (m *mockBatchV4) Provider() string             { return m.providerName }
func (m *mockBatchV4) SupportedURLs() map[string][]string {
	return map[string][]string{"*/*": {`.*`}}
}

func (m *mockBatchV4) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	return m.startFn(ctx, opts)
}

func (m *mockBatchV4) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	return m.statusFn(ctx, opts)
}

func (m *mockBatchV4) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	return m.resultsFn(ctx, opts)
}

// mockBatchV4NoURLSupport wraps mockBatchV4 with an empty SupportedURLs map,
// forcing every file URL in a batch prompt to be downloaded.
type mockBatchV4NoURLSupport struct {
	*mockBatchV4
}

func (m *mockBatchV4NoURLSupport) SupportedURLs() map[string][]string {
	return nil
}

type mockBatchV4WithCanceller struct {
	*mockBatchV4
	cancelFn func(context.Context, provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error)
}

func (m *mockBatchV4WithCanceller) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	return m.cancelFn(ctx, opts)
}

type mockBatchV4WithLister struct {
	*mockBatchV4
	listFn func(context.Context, provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error)
}

func (m *mockBatchV4WithLister) DoListBatches(ctx context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	return m.listFn(ctx, opts)
}

type mockBatchItemStream struct {
	items  []provider.BatchV4ItemResult
	idx    int
	err    error
	closed bool
}

func (s *mockBatchItemStream) Next() (*provider.BatchV4ItemResult, error) {
	if s.idx >= len(s.items) {
		return nil, io.EOF
	}
	item := s.items[s.idx]
	s.idx++
	return &item, nil
}
func (s *mockBatchItemStream) Err() error   { return s.err }
func (s *mockBatchItemStream) Close() error { s.closed = true; return nil }

func textBatchRequest(id, model string) BatchRequest {
	return BatchRequest{Text: &BatchTextRequest{ID: id, Model: model, Prompt: "hi"}}
}

func TestExperimentalStartBatch_RejectsEmptyRequests(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: nil,
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError", err)
	}
}

func TestExperimentalStartBatch_RejectsDuplicateIDs(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{textBatchRequest("req-1", "m"), textBatchRequest("req-1", "m")},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (duplicate ID)", err)
	}
}

func TestExperimentalStartBatch_RejectsEmptyID(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{textBatchRequest("  ", "m")},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (empty ID)", err)
	}
}

func TestExperimentalStartBatch_RejectsIncompleteRequest(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{{}},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (neither Text nor Image set)", err)
	}
}

func TestExperimentalStartBatch_RejectsIncompatibleToolDefinitions(t *testing.T) {
	toolA := types.Tool{Name: "search", Description: "v1", Parameters: map[string]interface{}{"type": "object"}}
	toolB := types.Tool{Name: "search", Description: "v2 (different)", Parameters: map[string]interface{}{"type": "object"}}

	req1 := textBatchRequest("req-1", "m")
	req1.Text.Tools = []types.Tool{toolA}
	req2 := textBatchRequest("req-2", "m")
	req2.Text.Tools = []types.Tool{toolB}

	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: &mockBatchV4{providerName: "mock.batch"},
		Requests: []BatchRequest{req1, req2},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (incompatible tool definitions)", err)
	}
}

func TestExperimentalStartBatch_AllowsIdenticalToolDefinitions(t *testing.T) {
	tool := types.Tool{Name: "search", Description: "v1", Parameters: map[string]interface{}{"type": "object"}}
	req1 := textBatchRequest("req-1", "m")
	req1.Text.Tools = []types.Tool{tool}
	req2 := textBatchRequest("req-2", "m")
	req2.Text.Tools = []types.Tool{tool}

	var gotRequests []provider.BatchV4Request
	mock := &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(_ context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			gotRequests = opts.Requests
			return &provider.BatchV4StartResult{
				BatchV4Status: provider.BatchV4Status{Status: provider.BatchStatusPending},
				BatchID:       "job_1",
			}, nil
		},
	}

	result, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: mock,
		Requests: []BatchRequest{req1, req2},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartBatch() error = %v", err)
	}
	if len(gotRequests) != 2 {
		t.Fatalf("gotRequests = %d, want 2", len(gotRequests))
	}
	if result.ID != "job_1" || result.Provider != "mock.batch" || result.Version != 2 {
		t.Fatalf("result batch reference = %+v", result.BatchReference)
	}
	if result.Status != provider.BatchStatusPending {
		t.Fatalf("Status = %v", result.Status)
	}
}

// TestExperimentalStartBatch_AppliesToolOrder mirrors TypeScript's
// `prepareTools({ tools, toolOrder, toolsContext })` in
// packages/ai/src/batch/batch.ts: a text request's ToolOrder reorders the
// tools sent to the provider.
func TestExperimentalStartBatch_AppliesToolOrder(t *testing.T) {
	toolA := types.Tool{Name: "a", Parameters: map[string]interface{}{"type": "object"}}
	toolB := types.Tool{Name: "b", Parameters: map[string]interface{}{"type": "object"}}

	var gotRequests []provider.BatchV4Request
	mock := &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(_ context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			gotRequests = opts.Requests
			return &provider.BatchV4StartResult{BatchID: "job_1"}, nil
		},
	}

	req := textBatchRequest("req-1", "m")
	req.Text.Tools = []types.Tool{toolA, toolB}
	req.Text.ToolOrder = []string{"b", "a"}

	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: mock,
		Requests: []BatchRequest{req},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartBatch() error = %v", err)
	}
	if len(gotRequests) != 1 || gotRequests[0].Text == nil {
		t.Fatalf("gotRequests = %+v", gotRequests)
	}
	sentTools := gotRequests[0].Text.Options.Tools
	if len(sentTools) != 2 || sentTools[0].Name != "b" || sentTools[1].Name != "a" {
		t.Fatalf("sentTools = %+v, want [b, a]", sentTools)
	}
}

// TestExperimentalStartBatch_NormalizesPromptDownloads mirrors
// TypeScript's use of convertToLanguageModelPrompt with the batch
// interface's supportedUrls in packages/ai/src/batch/batch.ts: a file URL
// the batch interface cannot pass through directly must be downloaded and
// inlined before the request reaches DoStartBatch, exactly like
// generateText/streamText.
func TestExperimentalStartBatch_NormalizesPromptDownloads(t *testing.T) {
	originalDownload := DefaultDownload
	defer func() { DefaultDownload = originalDownload }()

	var downloadedURLs []string
	DefaultDownload = func(_ context.Context, requests []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
		results := make([]*promptutils.DownloadResult, len(requests))
		for i, req := range requests {
			downloadedURLs = append(downloadedURLs, req.URL)
			results[i] = &promptutils.DownloadResult{Data: []byte("inlined-bytes"), MediaType: "image/png"}
		}
		return results, nil
	}

	var gotRequests []provider.BatchV4Request
	// SupportedURLs returns nothing, so every file URL must be downloaded
	// (mirrors TypeScript's empty supportedUrls map treatment).
	mock := &mockBatchV4NoURLSupport{
		mockBatchV4: &mockBatchV4{
			providerName: "mock.batch",
			startFn: func(_ context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
				gotRequests = opts.Requests
				return &provider.BatchV4StartResult{BatchID: "job_1"}, nil
			},
		},
	}

	req := BatchRequest{Text: &BatchTextRequest{
		ID:    "req-1",
		Model: "m",
		Messages: []types.Message{{
			Role: types.RoleUser,
			Content: []types.ContentPart{
				types.FileContent{URL: "https://example.com/image.png", MediaType: "image/png"},
			},
		}},
	}}

	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: mock,
		Requests: []BatchRequest{req},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartBatch() error = %v", err)
	}

	if len(downloadedURLs) != 1 || downloadedURLs[0] != "https://example.com/image.png" {
		t.Fatalf("downloadedURLs = %v, want the file URL to be downloaded", downloadedURLs)
	}

	if len(gotRequests) != 1 || gotRequests[0].Text == nil {
		t.Fatalf("gotRequests = %+v", gotRequests)
	}
	sentMessages := gotRequests[0].Text.Options.Prompt.Messages
	if len(sentMessages) != 1 || len(sentMessages[0].Content) != 1 {
		t.Fatalf("sentMessages = %+v", sentMessages)
	}
	file, ok := sentMessages[0].Content[0].(types.FileContent)
	if !ok {
		t.Fatalf("sent content part = %T, want types.FileContent", sentMessages[0].Content[0])
	}
	if file.URL != "" {
		t.Fatalf("file.URL = %q, want empty (URL should have been inlined)", file.URL)
	}
	if string(file.Data) != "inlined-bytes" {
		t.Fatalf("file.Data = %q, want the downloaded bytes", file.Data)
	}
}

func TestExperimentalStartBatch_ViaBatchProvider(t *testing.T) {
	mock := &mockBatchV4{
		providerName: "mock.batch",
		startFn: func(_ context.Context, _ provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
			return &provider.BatchV4StartResult{BatchV4Status: provider.BatchV4Status{Status: provider.BatchStatusPending}, BatchID: "job_1"}, nil
		},
	}
	bp := mockBatchProvider{batch: mock}

	result, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: bp,
		Requests: []BatchRequest{textBatchRequest("req-1", "m")},
	})
	if err != nil {
		t.Fatalf("ExperimentalStartBatch() error = %v", err)
	}
	if result.ID != "job_1" {
		t.Fatalf("result.ID = %q", result.ID)
	}
}

type mockBatchProvider struct {
	batch provider.BatchV4
}

func (p mockBatchProvider) ExperimentalBatch() provider.BatchV4 { return p.batch }

func TestExperimentalStartBatch_UnsupportedProviderReturnsError(t *testing.T) {
	_, err := ExperimentalStartBatch(context.Background(), StartBatchOptions{
		Provider: "not-a-batch-provider",
		Requests: []BatchRequest{textBatchRequest("req-1", "m")},
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestExperimentalGetBatchStatus_ValidatesProviderMatch(t *testing.T) {
	mock := &mockBatchV4{providerName: "mock.batch"}
	_, err := ExperimentalGetBatchStatus(context.Background(), GetBatchStatusOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "other.batch"},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (provider mismatch)", err)
	}
}

func TestExperimentalGetBatchStatus_ValidatesVersion(t *testing.T) {
	mock := &mockBatchV4{providerName: "mock.batch"}
	_, err := ExperimentalGetBatchStatus(context.Background(), GetBatchStatusOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 1, ID: "job_1", Provider: "mock.batch"},
	})
	if !isInvalidArgumentError(err) {
		t.Fatalf("err = %v, want InvalidArgumentError (bad version)", err)
	}
}

func TestExperimentalGetBatchStatus_ReturnsStatus(t *testing.T) {
	mock := &mockBatchV4{
		providerName: "mock.batch",
		statusFn: func(_ context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
			if opts.BatchID != "job_1" {
				t.Fatalf("BatchID = %q, want job_1", opts.BatchID)
			}
			return &provider.BatchV4Status{Status: provider.BatchStatusCompleted}, nil
		},
	}
	result, err := ExperimentalGetBatchStatus(context.Background(), GetBatchStatusOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatalf("ExperimentalGetBatchStatus() error = %v", err)
	}
	if result.Status != provider.BatchStatusCompleted {
		t.Fatalf("Status = %v", result.Status)
	}
}

func TestExperimentalGetBatchResults_StreamsAndConverts(t *testing.T) {
	stream := &mockBatchItemStream{items: []provider.BatchV4ItemResult{
		{Type: provider.BatchRequestTypeText, ID: "req-1", Status: provider.BatchItemSucceeded, TextResult: &types.GenerateResult{Text: "hi"}},
		{Type: provider.BatchRequestTypeText, ID: "req-2", Status: provider.BatchItemFailed, Error: &provider.BatchError{Message: "boom"}},
	}}
	mock := &mockBatchV4{
		providerName: "mock.batch",
		resultsFn: func(_ context.Context, _ provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
			return stream, nil
		},
	}

	resultsStream, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatalf("ExperimentalGetBatchResults() error = %v", err)
	}

	item1, err := resultsStream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if item1.ID != "req-1" || item1.Text == nil || item1.Text.Text != "hi" {
		t.Fatalf("item1 = %+v", item1)
	}

	item2, err := resultsStream.Next()
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if item2.ID != "req-2" || item2.Error == nil || item2.Error.Message != "boom" {
		t.Fatalf("item2 = %+v", item2)
	}

	if _, err := resultsStream.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next() at end = %v, want io.EOF", err)
	}

	if err := resultsStream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !stream.closed {
		t.Fatalf("expected underlying stream to be closed")
	}
}

func TestExperimentalCancelBatch_UnsupportedWhenProviderLacksCapability(t *testing.T) {
	mock := &mockBatchV4{providerName: "mock.batch"}
	_, err := ExperimentalCancelBatch(context.Background(), CancelBatchOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestExperimentalCancelBatch_ForwardsToProvider(t *testing.T) {
	inner := &mockBatchV4{providerName: "mock.batch"}
	mock := &mockBatchV4WithCanceller{
		mockBatchV4: inner,
		cancelFn: func(_ context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
			if opts.BatchID != "job_1" {
				t.Fatalf("BatchID = %q", opts.BatchID)
			}
			return &provider.BatchV4CancelResult{ProviderMetadata: map[string]interface{}{"gateway": map[string]interface{}{"cancelled": true}}}, nil
		},
	}

	result, err := ExperimentalCancelBatch(context.Background(), CancelBatchOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatalf("ExperimentalCancelBatch() error = %v", err)
	}
	if result.ProviderMetadata == nil {
		t.Fatalf("ProviderMetadata missing")
	}
}

func TestExperimentalListBatches_UnsupportedWhenProviderLacksCapability(t *testing.T) {
	mock := &mockBatchV4{providerName: "mock.batch"}
	_, err := ExperimentalListBatches(context.Background(), ListBatchesOptions{Provider: mock})
	if !providererrors.IsUnsupportedFunctionalityError(err) {
		t.Fatalf("err = %v, want UnsupportedFunctionalityError", err)
	}
}

func TestExperimentalListBatches_ForwardsToProviderAndConverts(t *testing.T) {
	inner := &mockBatchV4{providerName: "mock.batch"}
	mock := &mockBatchV4WithLister{
		mockBatchV4: inner,
		listFn: func(_ context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
			if opts.Limit != 10 {
				t.Fatalf("Limit = %d, want 10", opts.Limit)
			}
			return &provider.BatchV4ListResult{
				Batches: []provider.BatchV4ListItem{
					{BatchID: "job_1", BatchV4Status: provider.BatchV4Status{Status: provider.BatchStatusCompleted}},
				},
				NextCursor: "cursor-2",
			}, nil
		},
	}

	result, err := ExperimentalListBatches(context.Background(), ListBatchesOptions{Provider: mock, Limit: 10})
	if err != nil {
		t.Fatalf("ExperimentalListBatches() error = %v", err)
	}
	if len(result.Batches) != 1 || result.Batches[0].ID != "job_1" || result.Batches[0].Provider != "mock.batch" {
		t.Fatalf("Batches = %+v", result.Batches)
	}
	if result.NextCursor != "cursor-2" {
		t.Fatalf("NextCursor = %q", result.NextCursor)
	}
}
