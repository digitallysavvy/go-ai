package prodia

import (
	"fmt"
	"strconv"
	"testing"
)

// provc-03 (TS fal-video-model.ts:357 class): a whole number above 2^31-1
// keeps its value: int where int is 64 bits, int64 where it is 32 bits.
func TestR91toolsProdiaNumberDoesNotOverflow(t *testing.T) {
	v := prodiaNumber(3851223452)
	if fmt.Sprint(v) != "3851223452" {
		t.Fatalf("prodiaNumber = %v", v)
	}
	switch v.(type) {
	case int:
		if strconv.IntSize != 64 {
			t.Fatalf("int on a %d-bit build", strconv.IntSize)
		}
	case int64:
		if strconv.IntSize != 32 {
			t.Fatalf("int64 on a 64-bit build")
		}
	default:
		t.Fatalf("type %T", v)
	}
	if prodiaNumber(1024) != 1024 || prodiaNumber(1.5) != 1.5 {
		t.Fatal("small values changed")
	}
}
