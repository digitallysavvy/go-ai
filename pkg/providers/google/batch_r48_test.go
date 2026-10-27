package google

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS `metadata.state ?? undefined` keeps an empty string: it maps to no known
// state, so the batch stays pending even when done is true. Only an absent or
// null state falls back to `done`.
func TestR48_BatchEmptyStateIsAValueNotAbsent(t *testing.T) {
	for body, want := range map[string]provider.BatchStatusValue{
		`{"name":"batches/b","done":true,"metadata":{"state":""}}`:   provider.BatchStatusPending,
		`{"name":"batches/b","done":true,"metadata":{"state":null}}`: provider.BatchStatusCompleted,
		`{"name":"batches/b","done":true,"metadata":{}}`:             provider.BatchStatusCompleted,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		b := New(Config{APIKey: "k", BaseURL: srv.URL}).ExperimentalBatch()
		status, err := b.DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "batches/b"})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status.Status != want {
			t.Errorf("%s: status = %v, want %v", body, status.Status, want)
		}
	}
}

// TS `metadata.output.responsesFile ?? response.responsesFile`: an empty
// string is not nullish and wins over the fallback.
func TestR48_BatchEmptyResponsesFileWinsOverFallback(t *testing.T) {
	empty, fallback := "", "files/x"
	op := googleBatchOperationWire{Response: &googleBatchOutputWire{ResponsesFile: &fallback}}
	op.Metadata = &struct {
		State      *string                `json:"state"`
		CreateTime string                 `json:"createTime"`
		BatchStats *googleBatchStatsWire  `json:"batchStats"`
		Output     *googleBatchOutputWire `json:"output"`
	}{Output: &googleBatchOutputWire{ResponsesFile: &empty}}
	if got, ok := op.responsesFile(); !ok || got != "" {
		t.Fatalf("responsesFile = %q, %v", got, ok)
	}
}
