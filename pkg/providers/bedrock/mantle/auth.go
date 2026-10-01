package mantle

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

type sigV4Transport struct {
	base               http.RoundTripper
	signer             *bedrock.AWSSigner
	credentialProvider bedrock.CredentialProvider
	region             string
}

func (t *sigV4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	signer := t.signer
	if t.credentialProvider != nil {
		creds, err := t.credentialProvider(req.Context())
		if err != nil {
			return nil, fmt.Errorf("AWS credential provider failed: %v. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields", err)
		}
		if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			return nil, fmt.Errorf("AWS credential provider failed: incomplete credentials. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields")
		}
		signer = bedrock.NewAWSSigner(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, t.region)
	}
	if signer == nil {
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
	if err := signer.SignRequest(req, body); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return t.base.RoundTrip(req)
}
