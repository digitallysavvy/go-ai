package anthropicaws

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r91provaHMAC(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(data))
	return h.Sum(nil)
}

// prova-06: the SigV4 canonical request matches aws4fetch (the TS
// anthropic-aws-fetch.ts signer): the path is URI-encoded a second time
// ('/v1/files/a%20b:c' -> '/v1/files/a%2520b%3Ac') and a header value's runs
// of whitespace collapse to one space ('a  b' -> 'a b').
func TestR91provaSigV4Canonicalization(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://aws-external-anthropic.us-west-2.api.aws/v1/files/a%20b:c", nil)
	req.Header.Set("X-Custom", "a  b")
	payload := []byte(`{}`)
	signAWSRequest(req, payload, "us-west-2", Credentials{AccessKeyID: "AKID", SecretAccessKey: "secret"})

	amzDate := req.Header.Get("X-Amz-Date")
	payloadHash := sha256.Sum256(payload)
	canonical := strings.Join([]string{
		"POST",
		"/v1/files/a%2520b%3Ac",
		"",
		"host:aws-external-anthropic.us-west-2.api.aws\nx-amz-date:" + amzDate + "\nx-custom:a b\n",
		"host;x-amz-date;x-custom",
		hex.EncodeToString(payloadHash[:]),
	}, "\n")
	scope := amzDate[:8] + "/us-west-2/aws-external-anthropic/aws4_request"
	canonicalHash := sha256.Sum256([]byte(canonical))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(canonicalHash[:])
	key := r91provaHMAC(r91provaHMAC(r91provaHMAC(r91provaHMAC([]byte("AWS4secret"), amzDate[:8]), "us-west-2"), "aws-external-anthropic"), "aws4_request")
	want := "AWS4-HMAC-SHA256 Credential=AKID/" + scope + ", SignedHeaders=host;x-amz-date;x-custom, Signature=" + hex.EncodeToString(r91provaHMAC(key, stringToSign))
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization =\n %s\nwant\n %s", got, want)
	}
}

// prova-05: the missing-credentials text is the TS text
// (anthropic-aws-provider.ts:195-200), with option names, not Go field names.
func TestR91provaMissingCredentialsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("ANTHROPIC_AWS_API_KEY", "")
	p, err := New(Config{Region: "us-east-1", WorkspaceID: "w", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := p.LanguageModel("claude-sonnet-4-6")
	_, err = m.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	want := "AWS SigV4 authentication requires AWS credentials. Please provide either:\n" +
		"1. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables\n" +
		"2. Provide accessKeyId and secretAccessKey in options\n" +
		"3. Use a credentialProvider function\n" +
		"4. Use API key authentication with ANTHROPIC_AWS_API_KEY or apiKey option\n" +
		"Original error: AWS access key ID setting is missing. Pass it using the 'accessKeyId' parameter or the AWS_ACCESS_KEY_ID environment variable."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
}
