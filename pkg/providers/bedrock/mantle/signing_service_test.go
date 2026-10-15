package mantle

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TS passes 'bedrock-mantle' as the SigV4 service for Mantle requests.
func TestBedrockMantleSigV4UsesBedrockMantleService(t *testing.T) {
	var signed *http.Request
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		signed = req
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	})
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", AccessKeyID: "akid", SecretAccessKey: "secret", HTTPClient: &http.Client{Transport: base}})
	req, _ := http.NewRequest(http.MethodPost, "https://bedrock-mantle.us-east-1.api.aws/v1/chat/completions", nil)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if auth := signed.Header.Get("Authorization"); !strings.Contains(auth, "/us-east-1/bedrock-mantle/aws4_request") {
		t.Fatalf("Authorization = %q, want credential scope with bedrock-mantle service", auth)
	}
}
