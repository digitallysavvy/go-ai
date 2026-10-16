package anthropic

import "testing"

func TestBedrockAnthropicRegionResolvedLazilyFromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	p := New(Config{})
	t.Setenv("AWS_REGION", "eu-west-1")
	base, err := p.runtimeBaseURL()
	if err != nil || base != "https://bedrock-runtime.eu-west-1.amazonaws.com" {
		t.Fatalf("runtimeBaseURL = %q, %v", base, err)
	}
}
