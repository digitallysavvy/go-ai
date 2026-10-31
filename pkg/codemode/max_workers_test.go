package codemode

import (
	"context"
	"errors"
	"testing"
)

const testMiB = 1024 * 1024

// withAvailableMemory makes the default worker cap see bytes of available
// memory and restores the default cap and the real reading afterwards.
func withAvailableMemory(t *testing.T, bytes uint64, known bool) {
	t.Helper()
	ResetMaxWorkers()
	prev := availableMemory
	availableMemory = func() (uint64, bool) { return bytes, known }
	t.Cleanup(func() {
		availableMemory = prev
		ResetMaxWorkers()
	})
}

func defaultMaxWorkersFor(memoryLimitBytes int) int {
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	return maxWorkersLocked(memoryLimitBytes)
}

// TS run getMaxWorkers: max(1, min(32, floor(availableMemory /
// (memoryLimitBytes + 48 MiB)))).
func TestDefaultMaxWorkers_MatchesRunFormula(t *testing.T) {
	perWorker := uint64(DefaultMemoryLimitBytes + 48*testMiB) // 112 MiB
	cases := []struct {
		name        string
		available   uint64
		known       bool
		memoryLimit int
		want        int
	}{
		{"ten workers of memory", 10 * perWorker, true, DefaultMemoryLimitBytes, 10},
		{"rounds down", 10*perWorker + perWorker - 1, true, DefaultMemoryLimitBytes, 10},
		{"capped at 32", 1000 * perWorker, true, DefaultMemoryLimitBytes, 32},
		{"no memory is still one", 0, true, DefaultMemoryLimitBytes, 1},
		{"less than one worker is one", perWorker - 1, true, DefaultMemoryLimitBytes, 1},
		{"uses the policy memory limit", 10 * perWorker, true, 16 * testMiB, int(10 * perWorker / (64 * testMiB))},
		{"unknown platform uses the cap", 0, false, DefaultMemoryLimitBytes, 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAvailableMemory(t, tc.available, tc.known)
			if got := defaultMaxWorkersFor(tc.memoryLimit); got != tc.want {
				t.Fatalf("default maxWorkers = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultMaxWorkers_OverLimitFailsWithConcurrencyError(t *testing.T) {
	withAvailableMemory(t, 2*uint64(DefaultMemoryLimitBytes+48*testMiB), true)
	for i := 0; i < 2; i++ {
		release, err := acquireWorkerSlot(DefaultMemoryLimitBytes)
		if err != nil {
			t.Fatalf("slot %d: unexpected error: %v", i+1, err)
		}
		defer release()
	}

	_, err := RunCodeMode(context.Background(), RunInput{JS: "return 1;", Tools: ToolSet{}})
	var concurrencyErr *ConcurrencyError
	if !errors.As(err, &concurrencyErr) {
		t.Fatalf("expected *ConcurrencyError, got %#v (%v)", err, err)
	}
	if got, want := err.Error(), "Code mode maxWorkers limit reached (2)."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if got := concurrencyErr.Code; got != "CODE_MODE_CONCURRENCY_LIMIT" {
		t.Fatalf("code = %q", got)
	}
	if got := concurrencyErr.Details.(map[string]interface{})["maxWorkers"]; got != 2 {
		t.Fatalf("details maxWorkers = %#v, want 2", got)
	}
}

// TS run reserveWorkerMemory: an invocation whose reservation does not fit
// the budget taken at the first reservation fails with the active count,
// even below the worker cap; the budget is retaken once every reservation is
// released.
func TestDefaultMaxWorkers_MemoryReservationOverBudget(t *testing.T) {
	withAvailableMemory(t, 200*testMiB, true)
	big, err := acquireWorkerSlot(100 * testMiB) // reserves 148 MiB of the 200 MiB budget
	if err != nil {
		t.Fatalf("first slot: %v", err)
	}
	if got := defaultMaxWorkersFor(8 * testMiB); got != 3 {
		t.Fatalf("cap for an 8 MiB invocation = %d, want 3", got)
	}
	_, err = acquireWorkerSlot(8 * testMiB) // 56 MiB more does not fit
	var concurrencyErr *ConcurrencyError
	if !errors.As(err, &concurrencyErr) || err.Error() != "Code mode maxWorkers limit reached (1)." {
		t.Fatalf("expected the over-budget ConcurrencyError (1), got %v", err)
	}
	big()
	big() // release is idempotent
	small, err := acquireWorkerSlot(8 * testMiB)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	small()
	maxWorkersMu.Lock()
	defer maxWorkersMu.Unlock()
	if activeWorkers != 0 || reservedMemoryBytes != 0 || memoryBudgetBytes != 0 {
		t.Fatalf("active=%d reserved=%d budget=%d after releases, want all 0", activeWorkers, reservedMemoryBytes, memoryBudgetBytes)
	}
}

func TestSetMaxWorkers_OverridesDefault(t *testing.T) {
	withAvailableMemory(t, 0, true) // default would be 1

	if err := SetMaxWorkers(3); err != nil {
		t.Fatal(err)
	}
	var releases []func()
	for i := 0; i < 3; i++ {
		release, err := acquireWorkerSlot(DefaultMemoryLimitBytes)
		if err != nil {
			t.Fatalf("raised cap, slot %d: %v", i+1, err)
		}
		releases = append(releases, release)
	}
	if _, err := acquireWorkerSlot(DefaultMemoryLimitBytes); err == nil || err.Error() != "Code mode maxWorkers limit reached (3)." {
		t.Fatalf("fourth slot: got %v, want the (3) limit error", err)
	}
	for _, release := range releases {
		release()
	}

	// Lowering below a large default.
	availableMemory = func() (uint64, bool) { return 1 << 40, true }
	if err := SetMaxWorkers(1); err != nil {
		t.Fatal(err)
	}
	release, err := acquireWorkerSlot(DefaultMemoryLimitBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireWorkerSlot(DefaultMemoryLimitBytes); err == nil || err.Error() != "Code mode maxWorkers limit reached (1)." {
		t.Fatalf("lowered cap: got %v, want the (1) limit error", err)
	}
	release()

	// 0 is unlimited (kept from v0.5.2).
	if err := SetMaxWorkers(0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		release, err := acquireWorkerSlot(DefaultMemoryLimitBytes)
		if err != nil {
			t.Fatalf("unlimited, slot %d: %v", i+1, err)
		}
		defer release()
	}

	ResetMaxWorkers()
	if got := defaultMaxWorkersFor(DefaultMemoryLimitBytes); got != 32 {
		t.Fatalf("after ResetMaxWorkers the default is %d, want 32", got)
	}
}

// TS run runManaged checks the abort signal before the worker cap, so an
// invocation whose context is already canceled fails with the aborted error,
// not *ConcurrencyError, even when the cap is reached.
func TestRunCodeMode_AbortedBeforeConcurrencyCheck(t *testing.T) {
	withAvailableMemory(t, 1<<34, true)
	if err := SetMaxWorkers(1); err != nil {
		t.Fatal(err)
	}
	release, err := acquireWorkerSlot(DefaultMemoryLimitBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = RunCodeMode(ctx, RunInput{JS: "return 1;", Tools: ToolSet{}})
	var aborted *AbortedError
	if !errors.As(err, &aborted) {
		t.Fatalf("expected *AbortedError, got %#v (%v)", err, err)
	}
	if got, want := err.Error(), "Code mode execution was aborted."; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
