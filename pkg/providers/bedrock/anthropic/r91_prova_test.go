package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// prova-05: the missing-credentials text is the TS text
// (amazon-bedrock-anthropic-provider.ts:220-225), with option names, not Go
// field names.
func TestR91provaMissingCredentialsText(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	model, err := New(Config{Region: "us-east-1", BaseURL: srv.URL, HTTPClient: srv.Client()}).LanguageModel("test-model-id")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
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
