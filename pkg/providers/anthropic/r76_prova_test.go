package anthropic

import (
	"strconv"
	"testing"
)

// r76provaMixedNames builds a per-request batchToolNames with n entries.
func r76provaMixedNames(n int) *batchToolNames {
	shared := map[string]string{"bash": "anthropic.bash_20250124"}
	by := make(map[string]map[string]string, n)
	for i := 0; i < n; i++ {
		by["req-"+strconv.Itoa(i)] = shared
	}
	return &batchToolNames{byRequest: by}
}

// The store bounds the per-request entries it keeps across batches, not
// only the batch count, and always keeps the newest batch.
func TestR76prova_BatchToolNameStoreBoundsRequestEntries(t *testing.T) {
	var s batchToolNameStore
	per := maxRememberedBatchToolNameRequests / 4
	for i := 0; i < 6; i++ {
		s.put("batch-"+strconv.Itoa(i), r76provaMixedNames(per))
	}
	if s.requests > maxRememberedBatchToolNameRequests {
		t.Fatalf("requests = %d, cap %d", s.requests, maxRememberedBatchToolNameRequests)
	}
	if len(s.names) != 4 || s.get("batch-1") != nil || s.get("batch-2") == nil || s.get("batch-5") == nil {
		t.Fatalf("kept %d batches", len(s.names))
	}

	// One batch over the cap on its own is still kept (it is the newest).
	s.put("huge", r76provaMixedNames(maxRememberedBatchToolNameRequests+1))
	if len(s.names) != 1 || s.get("huge") == nil || s.requests != maxRememberedBatchToolNameRequests+1 {
		t.Fatalf("names=%d requests=%d", len(s.names), s.requests)
	}

	// Re-putting a batch ID replaces its count.
	s.put("huge", &batchToolNames{all: map[string]string{"bash": "x"}})
	if s.requests != 1 || len(s.order) != 1 {
		t.Fatalf("requests=%d order=%d", s.requests, len(s.order))
	}
}
