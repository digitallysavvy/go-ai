package google

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r64: TS GoogleBatch takes batchGenerateId = config.generateId ?? generateId
// from the provider's languageModelConfig, so createGoogle({generateId})
// names the batch `ai-sdk-batch-${generateId()}` (TS google-batch.test.ts
// generateId: () => 'test-id' -> displayName 'ai-sdk-batch-test-id').
func TestR64_BatchDisplayNameUsesConfigGenerateID(t *testing.T) {
	var gotBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"batches/b1","done":false,"metadata":{"state":"BATCH_STATE_PENDING"}}`))
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL, GenerateID: func() string { return "test-id" }})
	if _, err := p.ExperimentalBatch().DoStartBatch(t.Context(), provider.BatchV4StartOptions{
		Requests: []provider.BatchV4Request{googleTextBatchRequest("france", "gemini-2.5-flash", "hi")},
	}); err != nil {
		t.Fatalf("DoStartBatch: %v", err)
	}
	batch, _ := gotBody["batch"].(map[string]interface{})
	if got := batch["displayName"]; got != "ai-sdk-batch-test-id" {
		t.Fatalf("displayName = %v, want ai-sdk-batch-test-id", got)
	}
}
