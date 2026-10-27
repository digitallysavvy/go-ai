package vercel

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Ports the TS #23529 utils.test.ts cases for concurrent template snapshot
// creation.

func fastSnapshotPolling(t *testing.T) {
	t.Helper()
	oldInterval, oldTimeout := snapshotPollInterval, templateSnapshotWaitTimeout
	snapshotPollInterval, templateSnapshotWaitTimeout = 5*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { snapshotPollInterval, templateSnapshotWaitTimeout = oldInterval, oldTimeout })
}

func TestEnsureTemplateSnapshotConcurrentCallersWaitForCreator(t *testing.T) {
	fastSnapshotPolling(t)
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	params := CreateParams{Runtime: "node24", TimeoutMs: 60_000}

	gate := make(chan struct{})
	started := make(chan struct{})
	var onCreateCalls int
	var mu sync.Mutex
	onCreate := func(ctx context.Context, _ *Sandbox) error {
		mu.Lock()
		onCreateCalls++
		mu.Unlock()
		close(started)
		<-gate
		return nil
	}

	type result struct {
		id  string
		err error
	}
	creator := make(chan result, 1)
	go func() {
		id, err := ensureTemplateSnapshot(context.Background(), client, params, "prepared-template", nil, onCreate)
		creator <- result{id, err}
	}()
	<-started

	waiter := make(chan result, 1)
	go func() {
		id, err := ensureTemplateSnapshot(context.Background(), client, params, "prepared-template", nil, onCreate)
		waiter <- result{id, err}
	}()
	// The waiter must observe the unfinished builder and poll; it must not
	// return an incomplete snapshot.
	select {
	case r := <-waiter:
		t.Fatalf("waiter returned before the creator published a snapshot: %+v", r)
	case <-time.After(60 * time.Millisecond):
	}
	close(gate)

	c, w := <-creator, <-waiter
	if c.err != nil || w.err != nil || c.id == "" || c.id != w.id {
		t.Fatalf("creator=%+v waiter=%+v", c, w)
	}
	if onCreateCalls != 1 || f.snapshotReqs != 1 {
		t.Fatalf("onCreate=%d snapshots=%d, want 1/1", onCreateCalls, f.snapshotReqs)
	}
}

func TestEnsureTemplateSnapshotWaitsForWinnerOnNameConflict(t *testing.T) {
	fastSnapshotPolling(t)
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	f.forceCreateError = &APIError{StatusCode: 400, Code: "bad_request", Message: "A sandbox with the name 'prepared-template' already exists for this project."}
	f.onListSnapshots = func(call int) {
		if call == 2 {
			f.mu.Lock()
			f.snapshots = append(f.snapshots, fakeSnapshot{ID: "snap_manual", Name: "prepared-template", Status: "created", CreationMethod: "manual"})
			f.mu.Unlock()
		}
	}
	cache := newSnapshotCache()
	called := false
	id, err := ensureTemplateSnapshot(context.Background(), client, CreateParams{Runtime: "node24"}, "prepared-template", cache, func(context.Context, *Sandbox) error {
		called = true
		return nil
	})
	if err != nil || id != "snap_manual" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if called || f.snapshotReqs != 0 || f.listSnapshots != 2 {
		t.Fatalf("onCreate=%v snapshots=%d lists=%d", called, f.snapshotReqs, f.listSnapshots)
	}
	if cached, _ := cache.get("prepared-template"); cached != "snap_manual" {
		t.Fatalf("cache = %q", cached)
	}
}

func TestEnsureTemplateSnapshotIgnoresAutomaticSnapshotsAndTimesOut(t *testing.T) {
	fastSnapshotPolling(t)
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	f.forceCreateError = &APIError{StatusCode: 409, Message: "sandbox name already exists"}
	f.snapshots = []fakeSnapshot{
		{ID: "snap_auto", Name: "t", Status: "created", CreationMethod: "automatic"},
		{ID: "snap_failed", Name: "t", Status: "failed", CreationMethod: "manual"},
	}
	_, err := ensureTemplateSnapshot(context.Background(), client, CreateParams{Runtime: "node24"}, "t", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Timed out waiting for snapshot") {
		t.Fatalf("err = %v, want timeout (automatic / failed snapshots must be ignored)", err)
	}
}

func TestEnsureTemplateSnapshotOnCreateFailureDoesNotPoll(t *testing.T) {
	fastSnapshotPolling(t)
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	boom := errors.New("prepare failed")
	_, err := ensureTemplateSnapshot(context.Background(), client, CreateParams{Runtime: "node24"}, "t", nil, func(context.Context, *Sandbox) error { return boom })
	if !errors.Is(err, boom) || f.listSnapshots != 0 || f.snapshotReqs != 0 {
		t.Fatalf("err=%v lists=%d snapshots=%d", err, f.listSnapshots, f.snapshotReqs)
	}
}

func TestEnsureTemplateSnapshotOtherCreateErrorsPropagate(t *testing.T) {
	fastSnapshotPolling(t)
	f := newFakeServer()
	defer f.Close()
	client := NewAPIClient(f.URL(), testCreds())
	f.forceCreateError = &APIError{StatusCode: 400, Message: "invalid runtime"}
	_, err := ensureTemplateSnapshot(context.Background(), client, CreateParams{Runtime: "node24"}, "t", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || f.listSnapshots != 0 {
		t.Fatalf("err=%v lists=%d", err, f.listSnapshots)
	}
}
