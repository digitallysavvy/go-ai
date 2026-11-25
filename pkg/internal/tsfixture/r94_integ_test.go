package tsfixture

import (
	"testing"
	"time"
)

// TestR94integDateJSON: recorded response timestamps serialize like
// Date#toJSON (expanded years, null for an Invalid Date).
func TestR94integDateJSON(t *testing.T) {
	if got := tsfixtureDateJSON(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)); got != "+010000-01-01T00:00:00.000Z" {
		t.Errorf("year 10000: %v", got)
	}
	if got := tsfixtureDateJSON(time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)); got != "-000001-01-01T00:00:00.000Z" {
		t.Errorf("year -1: %v", got)
	}
	if got := tsfixtureDateJSON(time.UnixMilli(-8_640_000_000_000_001)); got != nil {
		t.Errorf("Invalid Date: %v, want nil", got)
	}
}
