package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
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

// Typed-option ruling (probe): Anthropic call options given as Go-typed
// literals (map[string]string, int64) reach the request like the map form.
func TestR91provaTypedOptionLiterals(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "akid")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg","type":"message","role":"assistant","model":"m","content":[],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	model, err := New(Config{Region: "us-east-1", BaseURL: srv.URL, HTTPClient: srv.Client()}).LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, ProviderOptions: map[string]interface{}{
		"anthropic": map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled", "budgetTokens": int64(2048)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, `"budget_tokens":2048`) {
		t.Fatalf("body = %s", body)
	}
	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}, ProviderOptions: map[string]interface{}{
		"anthropic": map[string]string{"structuredOutputMode": "jsonTool"},
	}})
	if err != nil {
		t.Fatalf("map[string]string options: %v", err)
	}
}

// Extra fix (prova-05 class): aws4fetch's AwsV4Signer TypeError text for an
// incomplete credential provider result.
func TestR91provaIncompleteProviderCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	for creds, want := range map[bedrock.Credentials]string{
		{SecretAccessKey: "s"}: "accessKeyId is a required option",
		{AccessKeyID: "a"}:     "secretAccessKey is a required option",
	} {
		c := creds
		model, err := New(Config{Region: "us-east-1", BaseURL: srv.URL, HTTPClient: srv.Client(),
			CredentialProvider: func(context.Context) (bedrock.Credentials, error) { return c, nil }}).LanguageModel("test-model-id")
		if err != nil {
			t.Fatal(err)
		}
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hello"}})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%+v: err = %v, want %q", creds, err, want)
		}
	}
}
