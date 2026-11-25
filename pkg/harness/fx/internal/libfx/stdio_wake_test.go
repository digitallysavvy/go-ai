package libfx

import (
	"context"
	"testing"
	"time"
)

// TestByteQueueWaitSeesPushAfterEmptyRead covers the lost wakeup that hung
// fd_read: a push between the guest's empty read and its wait must wake it.
func TestByteQueueWaitSeesPushAfterEmptyRead(t *testing.T) {
	q := newByteQueue()
	if q.read(16) != nil {
		t.Fatal("queue not empty")
	}
	if err := q.push([]byte("frame\n")); err != nil {
		t.Fatal(err)
	}
	if !q.wait(context.Background(), 200*time.Millisecond) {
		t.Fatal("wait missed data pushed before it started")
	}
}
