package perplexity

import (
	"testing"
	"time"
)

// TS new Date(created_at * 1000): a Date holds whole milliseconds, so the
// sub-millisecond part of a fractional created_at is dropped (TimeClip
// truncates toward zero).
func TestR93provgCreatedAtMillisecondPrecision(t *testing.T) {
	got := perplexityCreatedAt(1700000000.1234567)
	if want := time.UnixMilli(1700000000123); !got.Equal(want) {
		t.Fatalf("created = %v (%d ns), want %v", got, got.UnixNano(), want)
	}
	if got := perplexityCreatedAt(-1.0005); !got.Equal(time.UnixMilli(-1000)) {
		t.Fatalf("negative created = %d ms", got.UnixMilli())
	}
}
