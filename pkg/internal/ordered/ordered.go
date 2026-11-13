// Package ordered runs indexed work with bounded concurrency, the Go
// equivalent of the TS SDK's mapWithConcurrency used for tool execution.
package ordered

import (
	"context"
	"sync"
)

// Run calls fn(i) for each i in [0, n) with at most limit calls running at
// once. limit <= 0 means no limit, and limit == 1 runs the calls one at a time,
// in index order, on the calling goroutine. Workers claim indexes in order.
// Once ctx is done or a call returns an error, no further index is started;
// calls already running finish. Run returns which indexes were started. Callers
// collect results by index, so results stay in index order whatever order the
// calls finish in.
func Run(ctx context.Context, n, limit int, fn func(i int) error) []bool {
	started := make([]bool, n)
	if n == 0 {
		return started
	}
	workers := n
	if limit > 0 && limit < workers {
		workers = limit
	}

	var (
		mu      sync.Mutex
		next    int
		stopped bool
	)
	// TS mapWithConcurrency uses Promise.all(items.map(execute)) when the
	// limit is unset or covers every item: every call starts up front, so a
	// failure or cancellation never prevents a sibling from starting.
	// A context that is already done on entry still starts nothing.
	unbounded := workers == n
	doneOnEntry := ctx != nil && ctx.Err() != nil
	claim := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if next >= n || (unbounded && doneOnEntry) || (!unbounded && (stopped || (ctx != nil && ctx.Err() != nil))) {
			return 0, false
		}
		i := next
		next++
		started[i] = true
		return i, true
	}
	worker := func() {
		for {
			i, ok := claim()
			if !ok {
				return
			}
			if err := fn(i); err != nil {
				mu.Lock()
				stopped = true
				mu.Unlock()
				return
			}
		}
	}

	if workers == 1 {
		worker()
		return started
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			worker()
		}()
	}
	wg.Wait()
	return started
}
