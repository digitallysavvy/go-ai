package deepseek

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR86bprovbFilesEmptyObjectAndPurposeRejected pins V061 item 15 for the
// literal fields: TS deepSeekFilesResponseSchema has object
// z.literal('file').nullish() and purpose z.literal('user_data').nullish(),
// so a present "" fails the response validation instead of being read as
// absent; null and absent pass.
func TestR86bprovbFilesEmptyObjectAndPurposeRejected(t *testing.T) {
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"id":"file-1","object":""}`, false},
		{`{"id":"file-1","purpose":""}`, false},
		{`{"id":"file-1","object":null,"purpose":null}`, true},
		{`{"id":"file-1","object":"file","purpose":"user_data"}`, true},
	} {
		p := newDeepseekProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})
		_, err := (&FilesAPI{provider: p}).UploadFile(context.Background(), types.UploadFileOptions{
			Data:      types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 0x50, 0x4e, 0x47}},
			MediaType: "image/png",
		})
		if (err == nil) != tc.ok {
			t.Errorf("body %s: err = %v, want ok %v", tc.body, err, tc.ok)
		}
	}
}
