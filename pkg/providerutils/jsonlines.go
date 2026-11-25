package providerutils

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// DefaultMaxJSONLineBytes is the default per-row limit for JSON Lines batch
// result downloads: 64 MiB of UTF-8 bytes, excluding the line feed (TS
// DEFAULT_MAX_JSON_LINE_BYTES).
const DefaultMaxJSONLineBytes = 64 * 1024 * 1024

// maxSafeInteger is JS Number.MAX_SAFE_INTEGER (2^53-1).
const maxSafeInteger = 1<<53 - 1

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
	// TS Number.isSafeInteger(maxLineBytes) && maxLineBytes > 0: a value
	// above 2^53-1 is rejected too (response-handler.ts:290).
	if b.MaxLineBytes < 0 || int64(b.MaxLineBytes) > maxSafeInteger {
		return 0, providererrors.NewArgumentError("maxLineBytes", "maxLineBytes must be a positive safe integer.", nil)
	}
	return b.MaxLineBytes, nil
}

// NewJSONLinesScanner returns a line scanner over r that rejects any row longer
// than maxLineBytes (excluding the line feed) instead of buffering it. A
// rejected row surfaces as bufio.ErrTooLong from Scanner.Err; convert it with
// JSONLinesScanError.
func NewJSONLinesScanner(r io.Reader, maxLineBytes int) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	// The buffer must hold the row plus its line feed; math.MaxInt (MaxInt32
	// on 32-bit, a safe integer TS accepts) must not wrap negative.
	limit := maxLineBytes
	if limit < math.MaxInt {
		limit++
	}
	initial := 64 * 1024
	if initial > limit {
		initial = limit
	}
	scanner.Buffer(make([]byte, initial), limit)
	scanner.Split(jsonLinesSplit())
	return scanner
}

// JSONLineIsBlank reports whether a JSON Lines row is skipped: TS
// parseJsonLines parses a row only when `line.trim().length > 0` (JS trim:
// U+FEFF and U+00A0 count as blank, U+0085 does not), and then parses the
// row itself, untrimmed, so JSON behind a non-JSON space fails like
// JSON.parse.
func JSONLineIsBlank(line []byte) bool {
	return JSTrim(string(line)) == ""
}

// jsonLinesSplit is bufio.ScanLines over the text TS parseJsonLines decodes
// with TextDecoder (response-handler.ts 279): the stream's leading BOM is
// dropped and each maximal invalid UTF-8 subpart in a row is one U+FFFD.
// A line feed always ends an incomplete sequence, so decoding row by row
// equals the stream decode.
func jsonLinesSplit() bufio.SplitFunc {
	first := true
	return func(data []byte, atEOF bool) (int, []byte, error) {
		advance, token, err := bufio.ScanLines(data, atEOF)
		if token == nil {
			return advance, token, err
		}
		if first {
			first = false
			if textdecode.HasBOM(token) {
				token = token[3:]
			}
		}
		return advance, textdecode.ReplaceInvalid(token), err
	}
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
