package providerutils

import (
	"math"
	"strings"
	"sync"
	"unicode/utf8"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// maxSafeOutputLength is Number.MAX_SAFE_INTEGER; TS createTextOutputCapture
// rejects a larger maxLength.
const maxSafeOutputLength int64 = 1<<53 - 1

// TextOutputCapture keeps a prefix of a text stream written to it while
// counting every UTF-16 code unit, like a JS string length. Output past the
// prefix is not retained, so a noisy command cannot exhaust memory. It is the
// Go twin of TS createTextOutputCapture (1d2c2ff2e4) and is safe for
// concurrent use.
//
// Text copies the retained prefix, so a result built from it does not keep the
// capture's buffer alive.
type TextOutputCapture struct {
	mu       sync.Mutex
	maxChars int64
	maxBytes int64
	buf      []byte
	total    int64
}

// outputCaptureMaxScaled is the largest maxLength whose byte budget is
// maxLength*utf8.UTFMax without overflowing int on a 32-bit platform.
var outputCaptureMaxScaled = min(int64(1<<62)/utf8.UTFMax, int64(math.MaxInt)/utf8.UTFMax)

// NewTextOutputCapture returns a capture that retains at most maxLength UTF-16
// code units. maxLength must be a positive safe integer, otherwise it returns
// an *InvalidArgumentError for "maxOutputLength".
func NewTextOutputCapture(maxLength int64) (*TextOutputCapture, error) {
	if maxLength < 1 || maxLength > maxSafeOutputLength {
		return nil, &providererrors.InvalidArgumentError{Field: "maxOutputLength", Value: maxLength, Message: "Must be a positive safe integer."}
	}
	maxBytes := maxLength
	if maxLength <= outputCaptureMaxScaled {
		maxBytes = maxLength * utf8.UTFMax
	}
	if maxBytes > int64(math.MaxInt) {
		maxBytes = int64(math.MaxInt)
	}
	return &TextOutputCapture{maxChars: maxLength, maxBytes: maxBytes}, nil
}

// Write appends a chunk of output. It never fails.
func (c *TextOutputCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, b := range p {
		switch {
		case b >= 0xF0 && b <= 0xF4:
			c.total += 2 // a 4-byte sequence is a surrogate pair
		case b&0xC0 != 0x80:
			c.total++
		}
	}
	if room := c.maxBytes - int64(len(c.buf)); room > 0 {
		if int64(len(p)) < room {
			room = int64(len(p))
		}
		c.buf = append(c.buf, p[:room]...)
	}
	return len(p), nil
}

// Append appends a string chunk.
func (c *TextOutputCapture) Append(chunk string) { _, _ = c.Write([]byte(chunk)) }

// Text returns a copy of the retained prefix: at most maxLength UTF-16 code
// units. A surrogate pair the cut would split is dropped whole, since UTF-8
// cannot carry the lone half.
func (c *TextOutputCapture) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.total <= c.maxChars {
		return string(c.buf)
	}
	text := string(c.buf)
	var seen int64
	end := len(text)
	for i, r := range text {
		units := int64(1)
		if r >= 0x10000 {
			units = 2
		}
		if seen+units > c.maxChars {
			end = i
			break
		}
		seen += units
	}
	return strings.Clone(text[:end])
}

// Length is the number of UTF-16 code units written, retained or not.
func (c *TextOutputCapture) Length() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}
