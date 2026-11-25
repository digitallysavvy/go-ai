package providerutils

import (
	"errors"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports create-text-output-capture.test.ts (TS 1d2c2ff2e4).

func TestPortToolsTextOutputCaptureCountsDiscardedAcrossChunks(t *testing.T) {
	c, err := NewTextOutputCapture(4)
	if err != nil {
		t.Fatal(err)
	}
	c.Append("ab")
	c.Append("cdef")
	c.Append("gh")
	if c.Text() != "abcd" || c.Length() != 8 {
		t.Fatalf("text=%q length=%d", c.Text(), c.Length())
	}
}

func TestPortToolsTextOutputCaptureEmptyAndExactLimit(t *testing.T) {
	c, _ := NewTextOutputCapture(4)
	c.Append("")
	if c.Text() != "" || c.Length() != 0 {
		t.Fatalf("text=%q length=%d", c.Text(), c.Length())
	}
	c.Append("abcd")
	if c.Text() != "abcd" || c.Length() != 4 {
		t.Fatalf("text=%q length=%d", c.Text(), c.Length())
	}
}

// Counts UTF-16 code units. TS keeps the lone high surrogate of the cut pair
// ("a\ud83d"); a Go string cannot hold one, so the pair is dropped whole.
func TestPortToolsTextOutputCaptureUsesUTF16Units(t *testing.T) {
	c, _ := NewTextOutputCapture(2)
	c.Append("a😀b")
	if c.Text() != "a" || c.Length() != 4 {
		t.Fatalf("text=%q length=%d", c.Text(), c.Length())
	}
	// A multi-byte sequence split across writes counts once.
	d, _ := NewTextOutputCapture(10)
	_, _ = d.Write([]byte("😀")[:2])
	_, _ = d.Write([]byte("😀")[2:])
	if d.Length() != 2 || d.Text() != "😀" {
		t.Fatalf("text=%q length=%d", d.Text(), d.Length())
	}
}

func TestPortToolsTextOutputCaptureBoundsRetainedBytes(t *testing.T) {
	c, _ := NewTextOutputCapture(1000)
	chunk := strings.Repeat("x", 1<<20)
	for range 8 {
		c.Append(chunk)
	}
	if len(c.Text()) != 1000 || c.Length() != 8<<20 {
		t.Fatalf("len=%d length=%d", len(c.Text()), c.Length())
	}
	if cap(c.buf) > 4*1000+4096 {
		t.Fatalf("retained buffer cap %d", cap(c.buf))
	}
}

func TestPortToolsTextOutputCaptureRejectsInvalidLimits(t *testing.T) {
	for _, limit := range []int64{0, -1, 1<<53 - 1 + 1} {
		_, err := NewTextOutputCapture(limit)
		var ia *providererrors.InvalidArgumentError
		if !errors.As(err, &ia) || ia.Field != "maxOutputLength" {
			t.Errorf("limit %d: err = %v", limit, err)
		}
	}
}
