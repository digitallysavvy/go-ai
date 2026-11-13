package anthropic

import (
	"sync"
	"testing"
)

// The batch tool-name store keeps one shared map when every request of a
// batch declares the same id-named provider tools, and per-request maps
// (identical sets sharing one value) otherwise, so a large batch does not
// keep a map per request (review round 75, prova fix 2).
func TestR75Integ_BatchToolNamesSharedAndInterned(t *testing.T) {
	b := newBatchToolNamesBuilder(3)
	b.add("r1", map[string]string{"bash": "anthropic.bash_20250124"})
	b.add("r2", map[string]string{"bash": "anthropic.bash_20250124"})
	b.add("r3", map[string]string{"bash": "anthropic.bash_20250124"})
	n := b.build()
	if n == nil || n.all == nil || n.byRequest != nil {
		t.Fatalf("names = %+v, want one shared map", n)
	}
	if got := n.forRequest("anything")["bash"]; got != "anthropic.bash_20250124" {
		t.Fatalf("shared lookup = %q", got)
	}

	// One request without id-named tools: per-request maps, interned.
	b = newBatchToolNamesBuilder(3)
	b.add("r1", map[string]string{"bash": "anthropic.bash_20250124"})
	b.add("r2", nil)
	b.add("r3", map[string]string{"bash": "anthropic.bash_20250124"})
	n = b.build()
	if n == nil || n.all != nil || len(n.byRequest) != 2 {
		t.Fatalf("names = %+v, want per-request maps for r1 and r3", n)
	}
	if n.forRequest("r2") != nil {
		t.Fatalf("r2 = %v, want none (no id-named tools)", n.forRequest("r2"))
	}
	if len(b.interned) != 1 {
		t.Fatalf("interned = %d, want r1 and r3 to share one map", len(b.interned))
	}

	// Different tool sets per request.
	b = newBatchToolNamesBuilder(2)
	b.add("r1", map[string]string{"bash": "anthropic.bash_20250124"})
	b.add("r2", map[string]string{"bash": "anthropic.bash_20241022"})
	n = b.build()
	if n.forRequest("r1")["bash"] != "anthropic.bash_20250124" || n.forRequest("r2")["bash"] != "anthropic.bash_20241022" {
		t.Fatalf("names = %+v", n)
	}

	// No id-named tools at all: nothing to store.
	if got := newBatchToolNamesBuilder(1).build(); got != nil {
		t.Fatalf("build = %+v, want nil", got)
	}
	var nilNames *batchToolNames
	if nilNames.forRequest("r") != nil {
		t.Fatal("nil names lookup")
	}
	var nilStore *batchToolNameStore
	nilStore.put("b", n)
	if nilStore.get("b") != nil {
		t.Fatal("nil store remembered a batch")
	}
}

// Concurrent starts and reads on one Provider's store (run with -race).
func TestR75Integ_BatchToolNameStoreConcurrent(t *testing.T) {
	s := &batchToolNameStore{}
	entry := &batchToolNames{all: map[string]string{"bash": "anthropic.bash_20250124"}}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				id := string(rune('a'+i)) + string(rune(j))
				s.put(id, entry)
				if got := s.get(id); got != nil {
					_ = got.forRequest("r")["bash"]
				}
			}
		}(i)
	}
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.names) != maxRememberedBatchToolNames || len(s.order) != maxRememberedBatchToolNames {
		t.Fatalf("len = %d/%d, want %d", len(s.names), len(s.order), maxRememberedBatchToolNames)
	}
}
