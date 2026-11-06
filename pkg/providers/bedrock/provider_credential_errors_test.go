package bedrock

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func clearAWSEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_ENDPOINT_URL", "AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "AWS_ENDPOINT_URL_BEDROCK_AGENT_RUNTIME"} {
		t.Setenv(k, "")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/nonexistent/credentials")
}

func signWith(t *testing.T, cfg Config) error {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://example.test/x", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return New(cfg).authenticateRequest(context.Background(), req, []byte("{}"))
}

// TS createAmazonBedrock loads the region first and reports it with the
// loadSetting text; missing credentials get the SigV4 guidance text.
func TestAuthenticateRequestErrorTextsMatchTS(t *testing.T) {
	clearAWSEnv(t)

	err := signWith(t, Config{AWSAccessKeyID: "AKIA", AWSSecretAccessKey: "secret"})
	want := "AWS region setting is missing. Pass it using the 'region' parameter or the AWS_REGION environment variable."
	if err == nil || err.Error() != want {
		t.Fatalf("missing region error = %v, want %q", err, want)
	}

	err = signWith(t, Config{Region: "us-east-1", AWSSecretAccessKey: "secret"})
	if err == nil || !strings.HasPrefix(err.Error(), "AWS SigV4 authentication requires AWS credentials. Please provide either:\n") ||
		!strings.HasSuffix(err.Error(), "Original error: AWS access key ID setting is missing. Pass it using the 'accessKeyId' parameter or the AWS_ACCESS_KEY_ID environment variable.") {
		t.Fatalf("missing access key error = %v", err)
	}

	err = signWith(t, Config{Region: "us-east-1", AWSAccessKeyID: "AKIA"})
	want = "AWS SigV4 authentication requires both AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY. Please ensure both credentials are provided.\nOriginal error: AWS secret access key setting is missing. Pass it using the 'secretAccessKey' parameter or the AWS_SECRET_ACCESS_KEY environment variable."
	if err == nil || err.Error() != want {
		t.Fatalf("missing secret error = %v, want %q", err, want)
	}
}

// TS createRerankingModel loads the region when the model is created.
func TestRerankingModelRequiresRegionAtCreation(t *testing.T) {
	clearAWSEnv(t)
	_, err := New(Config{BaseURL: "https://example.test"}).RerankingModel("amazon.rerank-v1:0")
	if err == nil || !strings.HasPrefix(err.Error(), "AWS region setting is missing.") {
		t.Fatalf("RerankingModel error = %v", err)
	}
	if _, err := New(Config{Region: "us-west-2"}).RerankingModel("amazon.rerank-v1:0"); err != nil {
		t.Fatalf("RerankingModel with region: %v", err)
	}
}
