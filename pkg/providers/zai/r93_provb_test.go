package zai

import "testing"

// JS String(number) text for a numeric error code (1e21 -> "1e+21").
func TestR93provbErrorCodeNumberText(t *testing.T) {
	if got := zaiErrorCode(1e21); got != "1e+21" {
		t.Fatalf("code = %q", got)
	}
}
