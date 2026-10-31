package providerutils

import (
	"bufio"
	"errors"
	"fmt"
	"io"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// DefaultMaxJSONLineBytes is the default per-row limit for JSON Lines batch
// result downloads: 64 MiB of UTF-8 bytes, excluding the line feed (TS
// DEFAULT_MAX_JSON_LINE_BYTES).
const DefaultMaxJSONLineBytes = 64 * 1024 * 1024

// BatchResultDownloads configures how JSON Lines batch results are
// downloaded (TS provider settings `batchResultDownloads`).
type BatchResultDownloads struct {
	// MaxLineBytes is the maximum UTF-8 bytes per row, excluding the line
	// feed. Zero uses DefaultMaxJSONLineBytes; negative values are rejected.
	MaxLineBytes int
}

// ResolveMaxLineBytes returns the effective per-row limit, or an
// InvalidArgumentError for an invalid value (TS: maxLineBytes must be a
// positive safe integer).
func (b BatchResultDownloads) ResolveMaxLineBytes() (int, error) {
	if b.MaxLineBytes == 0 {
		return DefaultMaxJSONLineBytes, nil
	}
	if b.MaxLineBytes < 0 {
		return 0, &providererrors.InvalidArgumentError{
			Field:   "maxLineBytes",
			Message: "maxLineBytes must be a positive safe integer.",
		}
	}
	return b.MaxLineBytes, nil
}

// NewJSONLinesScanner returns a line scanner over r that rejects any row longer
// than maxLineBytes (excluding the line feed) instead of buffering it. A
// rejected row surfaces as bufio.ErrTooLong from Scanner.Err; convert it with
// JSONLinesScanError.
func NewJSONLinesScanner(r io.Reader, maxLineBytes int) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	// The buffer must hold the row plus its line feed.
	limit := maxLineBytes + 1
	initial := 64 * 1024
	if initial > limit {
		initial = limit
	}
	scanner.Buffer(make([]byte, initial), limit)
	return scanner
}

// JSONLinesScanError converts a scanner error into the error to return to the
// caller: an oversized row becomes a DownloadError (TS DownloadError) naming
// the limit; any other error is returned unchanged.
func JSONLinesScanError(err error, url string, maxLineBytes int) error {
	if errors.Is(err, bufio.ErrTooLong) {
		return &providererrors.DownloadError{
			URL:     url,
			Message: fmt.Sprintf("JSON Lines response exceeded maximum line size of %d bytes.", maxLineBytes),
			Cause:   err,
		}
	}
	return err
}
