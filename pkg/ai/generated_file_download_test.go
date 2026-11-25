package ai

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays generated-file-download.test.ts: URL-backed generated files are
// downloaded (once) before they are exposed.

func urlFile(url string) types.GeneratedFileContent {
	return types.GeneratedFileContent{
		MediaType: "text/plain",
		URL:       url,
		FileData:  types.FileData{Type: types.FileDataTypeURL, URL: url},
	}
}

func fileModel(file types.GeneratedFileContent) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		ProviderName: "mock-provider", ModelName: "mock-model-id",
		DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return &types.GenerateResult{Content: []types.ContentPart{file}, FinishReason: types.FinishReasonStop, Usage: types.Usage{}}, nil
		},
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			f := file
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeFile, GeneratedFileContent: &f},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func fileServer(t *testing.T, status int, hits *atomic.Int32) string {
	t.Helper()
	allowLoopbackDownloadTransport(t)
	restore := downloadURLValidator
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadURLValidator = restore })
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte("Hello World"))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func TestGeneratedFileDownload_StreamEmitsDownloadedContent(t *testing.T) {
	var hits atomic.Int32
	url := fileServer(t, http.StatusOK, &hits)
	result, err := StreamText(context.Background(), StreamTextOptions{Model: fileModel(urlFile(url)), Prompt: "image"})
	if err != nil {
		t.Fatal(err)
	}
	var fileParts []map[string]interface{}
	for _, p := range tsUIParts(t, result) {
		if p["type"] == "file" {
			fileParts = append(fileParts, p)
		}
	}
	if len(fileParts) != 1 || fileParts[0]["url"] != "data:text/plain;base64,SGVsbG8gV29ybGQ=" || fileParts[0]["mediaType"] != "text/plain" {
		t.Fatalf("file parts = %v", fileParts)
	}
	files := result.Files()
	if len(files) != 1 || string(files[0].Data) != "Hello World" || files[0].URL != "" {
		t.Fatalf("files = %+v", files)
	}
	if hits.Load() != 1 {
		t.Fatalf("download count = %d, want 1", hits.Load())
	}
}

func TestGeneratedFileDownload_GenerateEmitsDownloadedContent(t *testing.T) {
	var hits atomic.Int32
	url := fileServer(t, http.StatusOK, &hits)
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile(url)), Prompt: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || string(res.Files[0].Data) != "Hello World" || res.Files[0].URL != "" {
		t.Fatalf("files = %+v", res.Files)
	}
	var inContent bool
	for _, p := range res.Content {
		if f, ok := p.(types.GeneratedFileContent); ok {
			inContent = true
			if string(f.Data) != "Hello World" {
				t.Fatalf("content file = %+v", f)
			}
		}
	}
	if !inContent {
		t.Fatalf("no file in content: %+v", res.Content)
	}
	if hits.Load() != 1 {
		t.Fatalf("download count = %d, want 1", hits.Load())
	}
}

func TestGeneratedFileDownload_HTTPFailureSurfaces(t *testing.T) {
	var hits atomic.Int32
	url := fileServer(t, http.StatusServiceUnavailable, &hits)

	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile(url)), Prompt: "image"})
	var dlErr *providererrors.DownloadError
	if !asDownloadError(err, &dlErr) || dlErr.StatusCode != 503 {
		t.Fatalf("generate error = %v, want DownloadError 503", err)
	}

	result, err := StreamText(context.Background(), StreamTextOptions{Model: fileModel(urlFile(url)), Prompt: "image", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := result.ReadAll()
	if !asDownloadError(readErr, &dlErr) || dlErr.StatusCode != 503 {
		t.Fatalf("stream error = %v, want DownloadError 503", readErr)
	}
}

func TestGeneratedFileDownload_UnsafeURLRejectedBeforeFetch(t *testing.T) {
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: fileModel(urlFile("http://127.0.0.1/file")), Prompt: "image"})
	var dlErr *providererrors.DownloadError
	if !asDownloadError(err, &dlErr) {
		t.Fatalf("error = %v, want DownloadError", err)
	}
	if !providererrors.IsSSRFError(err) {
		t.Fatalf("error = %v, want the SSRF cause preserved", err)
	}
}

func TestGeneratedFileDownload_AbortCancelsDownload(t *testing.T) {
	started := make(chan struct{})
	allowLoopbackDownloadTransport(t)
	restore := downloadURLValidator
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadURLValidator = restore })
	server := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := GenerateText(ctx, GenerateTextOptions{Model: fileModel(urlFile(server.URL)), Prompt: "image"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("download never started")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error after abort")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("download was not cancelled by the abort")
	}
}

func asDownloadError(err error, target **providererrors.DownloadError) bool {
	return errors.As(err, target)
}

// TS getBatchResults -> convertGenerateResult -> convertLanguageModelContent
// downloads URL-backed file parts of a succeeded text result.
func TestGeneratedFileDownload_BatchTextResultDownloadsFiles(t *testing.T) {
	var hits atomic.Int32
	url := fileServer(t, http.StatusOK, &hits)
	stream := &mockBatchItemStream{items: []provider.BatchV4ItemResult{{
		Type: provider.BatchRequestTypeText, ID: "req-1", Status: provider.BatchItemSucceeded,
		TextResult: &types.GenerateResult{Content: []types.ContentPart{urlFile(url)}, FinishReason: types.FinishReasonStop},
	}}}
	mock := &mockBatchV4{
		providerName: "mock.batch",
		resultsFn: func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
			return stream, nil
		},
	}
	rs, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	item, err := rs.Next()
	if err != nil {
		t.Fatal(err)
	}
	f, ok := item.Text.Content[0].(types.GeneratedFileContent)
	if !ok || string(f.Data) != "Hello World" || f.URL != "" || hits.Load() != 1 {
		t.Fatalf("content = %+v hits=%d", item.Text.Content, hits.Load())
	}
}

// A tool-result part that answers no tool call of the response fails the
// batch item conversion (TS "Tool call <id> not found.").
func TestBatchResults_ToolResultWithoutCallRejected(t *testing.T) {
	stream := &mockBatchItemStream{items: []provider.BatchV4ItemResult{{
		Type: provider.BatchRequestTypeText, ID: "req-1", Status: provider.BatchItemSucceeded,
		TextResult: &types.GenerateResult{Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "x", ToolName: "t", Result: "r"}}},
	}}}
	mock := &mockBatchV4{
		providerName: "mock.batch",
		resultsFn: func(context.Context, provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
			return stream, nil
		},
	}
	rs, err := ExperimentalGetBatchResults(context.Background(), GetBatchResultsOptions{
		Provider: mock,
		Batch:    BatchReference{Version: 2, ID: "job_1", Provider: "mock.batch"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Next(); err == nil {
		t.Fatal("want a Tool call not found error")
	}
}
