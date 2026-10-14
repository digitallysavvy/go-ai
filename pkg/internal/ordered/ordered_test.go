package ordered

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRunBoundsConcurrency(t *testing.T) {
	for _, tc := range []struct{ limit, want int }{{1, 1}, {2, 2}, {0, 4}, {10, 4}} {
		var mu sync.Mutex
		active, peak := 0, 0
		release := make(chan struct{})
		var once sync.Once
		started := Run(context.Background(), 4, tc.limit, func(int) error {
			mu.Lock()
			active++
			if active > peak {
				peak = active
			}
			if active == tc.want {
				once.Do(func() { close(release) })
			}
			mu.Unlock()
			select {
			case <-release:
			case <-time.After(200 * time.Millisecond):
			}
			mu.Lock()
			active--
			mu.Unlock()
			return nil
		})
		if peak != tc.want {
			t.Errorf("limit %d: peak = %d, want %d", tc.limit, peak, tc.want)
		}
		for i, s := range started {
			if !s {
				t.Errorf("limit %d: index %d not started", tc.limit, i)
			}
		}
	}
}

func TestRunSequentialKeepsOrder(t *testing.T) {
	var got []int
	Run(context.Background(), 3, 1, func(i int) error {
		got = append(got, i)
		return nil
	})
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("order = %v", got)
	}
}

func TestRunStopsAfterErrorAndCancel(t *testing.T) {
	started := Run(context.Background(), 3, 1, func(i int) error {
		if i == 0 {
			return errors.New("boom")
		}
		return nil
	})
	if !started[0] || started[1] || started[2] {
		t.Fatalf("after error, started = %v", started)
	}

	ctx, cancel := context.WithCancel(context.Background())
	started = Run(ctx, 3, 1, func(i int) error {
		cancel()
		return nil
	})
	if !started[0] || started[1] || started[2] {
		t.Fatalf("after cancel, started = %v", started)
	}
}
