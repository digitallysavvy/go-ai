package bedrock

import (
	"fmt"
	"github.com/digitallysavvy/go-ai/pkg/internal/textdecode"
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

// readJSONResponseBody is readResponseBody for a JSON body: it is decoded
// like TS response.text() (BOM dropped, invalid UTF-8 replaced) before
// JSON.parse.
func readJSONResponseBody(r io.Reader) ([]byte, error) {
	data, err := readResponseBody(r)
	if err != nil {
		return data, err
	}
	return []byte(textdecode.UTF8(data)), nil
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
