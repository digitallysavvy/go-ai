package mantle

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// sigV4Transport authenticates each request: a bearer token (settings.APIKey or
// AWS_BEARER_TOKEN_BEDROCK, trimmed) when one is available, otherwise SigV4.
// Everything is read at request time.
type sigV4Transport struct {
	base               http.RoundTripper
	apiKey             string
	accessKeyID        string
	secretAccessKey    string
	sessionToken       string
	credentialProvider bedrock.CredentialProvider
	region             string
}

func (t *sigV4Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if key := providerutils.JSTrim(providerutils.APIKeyOrEnv(t.apiKey, "AWS_BEARER_TOKEN_BEDROCK")); key != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+key)
		return t.base.RoundTrip(req)
	}
	// TS loads the region first, at request time, and fails when it is missing.
	region := firstNonEmpty(t.region, os.Getenv("AWS_REGION"))
	if region == "" {
		return nil, fmt.Errorf("AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable")
	}
	var signer *bedrock.AWSSigner
	if t.credentialProvider != nil {
		creds, err := t.credentialProvider(req.Context())
		if err != nil {
			return nil, fmt.Errorf("AWS credential provider failed: %v. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields", err)
		}
		if creds.AccessKeyID == "" || creds.SecretAccessKey == "" {
			return nil, fmt.Errorf("AWS credential provider failed: incomplete credentials. Please ensure your credential provider returns valid AWS credentials with AccessKeyID and SecretAccessKey fields")
		}
		signer = bedrock.NewAWSSignerForService(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken, region, mantleSigningService)
	} else {
		accessKeyID := firstNonEmpty(t.accessKeyID, os.Getenv("AWS_ACCESS_KEY_ID"))
		secretAccessKey := firstNonEmpty(t.secretAccessKey, os.Getenv("AWS_SECRET_ACCESS_KEY"))
		if accessKeyID == "" {
			return nil, fmt.Errorf("AWS SigV4 authentication requires AWS credentials. Please provide either:\n1. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables\n2. Provide AccessKeyID and SecretAccessKey in settings\n3. Use a CredentialProvider function\n4. Use API key authentication with AWS_BEARER_TOKEN_BEDROCK or APIKey\nOriginal error: AWS access key ID setting is missing. Pass it using the 'accessKeyId' parameter or the AWS_ACCESS_KEY_ID environment variable.") //nolint:staticcheck // message text matches TS
		}
		if secretAccessKey == "" {
			return nil, fmt.Errorf("AWS SigV4 authentication requires both AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. Please ensure both credentials are provided.\nOriginal error: AWS secret access key setting is missing. Pass it using the 'secretAccessKey' parameter or the AWS_SECRET_ACCESS_KEY environment variable.") //nolint:staticcheck // message text matches TS
		}
		sessionToken := firstNonEmpty(t.sessionToken, os.Getenv("AWS_SESSION_TOKEN"))
		signer = bedrock.NewAWSSignerForService(accessKeyID, secretAccessKey, sessionToken, region, mantleSigningService)
	}
	// The wrapped OpenAI client may have set a stale Authorization (e.g. from
	// OPENAI_API_KEY); it must not be part of the signed headers.
	req.Header.Del("Authorization")
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
