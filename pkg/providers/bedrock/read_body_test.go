package bedrock

import (
	"strings"
	"testing"
)

func TestReadLimitedRejectsOversizeBody(t *testing.T) {
	if b, err := readLimited(strings.NewReader("abcd"), 4); err != nil || string(b) != "abcd" {
		t.Fatalf("at the limit: %q, %v", b, err)
	}
	if _, err := readLimited(strings.NewReader("abcde"), 4); err == nil {
		t.Fatal("a body over the limit must fail")
	}
}
