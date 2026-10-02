package bedrock

import (
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ports amazon-bedrock/src/resolve-amazon-bedrock-base-url.test.ts (TS
// #21842): a region value that could rewrite the request host (e.g.
// "user@internal:8080/#") must be rejected before it's interpolated into
// the generated hostname, while an explicit baseURL/endpoint override never
// even looks at the region.
func TestResolveAmazonBedrockBaseURL_RejectsInvalidRegion(t *testing.T) {
	t.Parallel()

	cases := []string{
		"evil.example.com/#",
		"user@internal:8080/#",
		"169.254.169.254:80/x#",
		"us-east-1/../..",
		"us east 1",
		"",
	}
	for _, region := range cases {
		_, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
			Region:                               region,
			Service:                              "bedrock-runtime",
			ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
		})
		if region == "" {
			// The empty case is already rejected by the existing
			// "AWS region is required" check, not IsValidHostnamePart.
			if err == nil {
				t.Errorf("region %q: expected an error", region)
			}
			continue
		}
		var argErr *providererrors.InvalidArgumentError
		if err == nil {
			t.Fatalf("region %q: expected an error, got nil", region)
		}
		if ae, ok := err.(*providererrors.InvalidArgumentError); !ok {
			t.Fatalf("region %q: err = %v (%T), want *providererrors.InvalidArgumentError", region, err, err)
		} else {
			argErr = ae
		}
		if argErr.Field != "region" {
			t.Errorf("region %q: Field = %q, want %q", region, argErr.Field, "region")
		}
	}
}

func TestResolveAmazonBedrockBaseURL_AcceptsValidRegion(t *testing.T) {
	t.Parallel()

	got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		Region:                               "us-east-1",
		Service:                              "bedrock-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://bedrock-runtime.us-east-1.amazonaws.com"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveAmazonBedrockBaseURL_SkipsRegionValidationWithExplicitBaseURL(t *testing.T) {
	t.Parallel()

	got, err := ResolveAmazonBedrockBaseURL(ResolveBaseURLOptions{
		BaseURL:                              "https://proxy.example/",
		Region:                               "not a region",
		Service:                              "bedrock-agent-runtime",
		ServiceEndpointURLEnvironmentVarName: "AWS_ENDPOINT_URL_BEDROCK_AGENT_RUNTIME",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "https://proxy.example"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
