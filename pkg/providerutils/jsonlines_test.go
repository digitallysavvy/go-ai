package providerutils

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from packages/provider-utils/src/response-handler.test.ts (TS
// #22096, createJsonLinesResponseHandler maxLineBytes).

func scanAll(t *testing.T, input string, max int) ([]string, error) {
	t.Helper()
	sc := NewJSONLinesScanner(strings.NewReader(input), max)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func TestJSONLinesScannerAllowsRowsAtTheLimit(t *testing.T) {
	// The limit excludes the line feed: a 5-byte row is allowed with max 5,
	// with or without a trailing newline, and a CRLF row counts the CR.
	lines, err := scanAll(t, "aaaaa\nbbbbb", 5)
	if err != nil || len(lines) != 2 {
		t.Fatalf("lines=%v err=%v", lines, err)
	}
	if _, err := scanAll(t, "aaaa\r\nbbbbb\n", 5); err != nil {
		t.Fatalf("CRLF row of 5 bytes incl. CR should pass: %v", err)
	}
}

func TestJSONLinesScannerRejectsOversizedRows(t *testing.T) {
	for _, in := range []string{"aaaaaa\n", "ok\naaaaaa", strings.Repeat("x", 1000)} {
		_, err := scanAll(t, in, 5)
		if err == nil {
			t.Fatalf("input %q: expected error", in)
		}
		wrapped := JSONLinesScanError(err, "https://example.com/results", 5)
		var dl *providererrors.DownloadError
		if !errors.As(wrapped, &dl) {
			t.Fatalf("expected DownloadError, got %T", wrapped)
		}
		if dl.URL != "https://example.com/results" || dl.Message != "JSON Lines response exceeded maximum line size of 5 bytes." {
			t.Fatalf("download error = %#v", dl)
		}
	}
}

func TestJSONLinesScannerDoesNotBufferUnboundedNewlineFreeInput(t *testing.T) {
	// A stream that never sends a newline must fail after max+1 bytes instead
	// of being read to the end.
	r := &countingReader{}
	sc := NewJSONLinesScanner(r, 1024)
	if sc.Scan() {
		t.Fatal("unexpected token")
	}
	if sc.Err() == nil {
		t.Fatal("expected error")
	}
	if r.n > 1024+64*1024 {
		t.Fatalf("read %d bytes before rejecting", r.n)
	}
}

type countingReader struct{ n int }

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n > 1<<24 {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 'x'
	}
	c.n += len(p)
	return len(p), nil
}

func TestBatchResultDownloadsResolveMaxLineBytes(t *testing.T) {
	if n, err := (BatchResultDownloads{}).ResolveMaxLineBytes(); err != nil || n != 64*1024*1024 {
		t.Fatalf("default = %d, %v", n, err)
	}
	if n, err := (BatchResultDownloads{MaxLineBytes: 10}).ResolveMaxLineBytes(); err != nil || n != 10 {
		t.Fatalf("custom = %d, %v", n, err)
	}
	_, err := (BatchResultDownloads{MaxLineBytes: -1}).ResolveMaxLineBytes()
	if !providererrors.IsInvalidArgumentError(err) {
		t.Fatalf("expected InvalidArgumentError, got %v", err)
	}
}
