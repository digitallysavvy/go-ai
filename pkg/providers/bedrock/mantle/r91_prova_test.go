package mantle

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
)

// prova-05: the missing-credentials text is the TS text
// (bedrock-mantle-provider.ts:200-207), with option names, not Go field names.
func TestR91provaMissingCredentialsText(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	tr := &sigV4Transport{region: "us-east-1", baseURL: func() (string, error) { return "https://example.com", nil }, base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("unexpected send")
	})}
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	_, err := tr.RoundTrip(req)
	want := "AWS SigV4 authentication requires AWS credentials. Please provide either:\n" +
		"1. Set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY environment variables\n" +
		"2. Provide accessKeyId and secretAccessKey in options\n" +
		"3. Use a credentialProvider function\n" +
		"4. Use API key authentication with AWS_BEARER_TOKEN_BEDROCK or apiKey option\n" +
		"Original error: AWS access key ID setting is missing. Pass it using the 'accessKeyId' parameter or the AWS_ACCESS_KEY_ID environment variable."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
}

// Extra fix (prova-05 class): aws4fetch's AwsV4Signer TypeError text for an
// incomplete credential provider result.
func TestR91provaIncompleteProviderCredentials(t *testing.T) {
	for creds, want := range map[bedrock.Credentials]string{
		{SecretAccessKey: "s"}: "accessKeyId is a required option",
		{AccessKeyID: "a"}:     "secretAccessKey is a required option",
	} {
		c := creds
		tr := &sigV4Transport{region: "us-east-1", baseURL: func() (string, error) { return "https://example.com", nil },
			credentialProvider: func(context.Context) (bedrock.Credentials, error) { return c, nil },
			base:               roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected send") })}
		req, _ := http.NewRequest("POST", "https://example.com", strings.NewReader("{}"))
		_, err := tr.RoundTrip(req)
		if err == nil || err.Error() != want {
			t.Errorf("%+v: err = %v, want %q", creds, err, want)
		}
	}
}
