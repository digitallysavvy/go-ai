package mantle

import (
	"bytes"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

type sigV4Transport struct {
	base   http.RoundTripper
	signer *bedrock.AWSSigner
}

func (t *sigV4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.signer == nil {
		return t.base.RoundTrip(req)
	}
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}
	if err := t.signer.SignRequest(req, body); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return t.base.RoundTrip(req)
}
