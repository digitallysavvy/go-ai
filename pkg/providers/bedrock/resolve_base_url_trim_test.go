package bedrock

import "testing"

func TestResolveAmazonBedrockBaseURLTrimsOnlyOneTrailingSlash(t *testing.T) {
	got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{BaseURL: "https://proxy.test//", Region: "us-east-1", Service: "bedrock-runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://proxy.test/" {
		t.Fatalf("got %q", got)
	}
}
