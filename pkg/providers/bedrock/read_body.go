package bedrock

import (
	"fmt"
	"io"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
)

// maxResponseBodyBytes caps a buffered Bedrock response or error body at the
// 2 GiB the TS response handlers allow, the shared HTTP client's default
// (fileutil.DefaultMaxDownloadSize), so a misbehaving endpoint cannot make a
// call buffer an unbounded body.
const maxResponseBodyBytes = fileutil.DefaultMaxDownloadSize

// readResponseBody reads r up to maxResponseBodyBytes and fails when the body
// is larger.
func readResponseBody(r io.Reader) ([]byte, error) {
	return readLimited(r, maxResponseBodyBytes)
}

func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return data, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response body exceeds the maximum size of %d bytes", limit)
	}
	return data, nil
}
