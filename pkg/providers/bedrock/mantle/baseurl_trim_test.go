package mantle

import "testing"

func TestBaseURLTrimsOnlyOneTrailingSlash(t *testing.T) {
	p := New(ProviderSettings{BaseURL: "https://proxy.test/v1//"})
	got, err := p.baseURLForModel("any-model")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://proxy.test/v1/" {
		t.Fatalf("got %q", got)
	}
}

// TS getBaseURL reads AWS_REGION when the URL is built, not when the provider
// is created.
func TestBaseURLReadsAWSRegionLazily(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	p := New(ProviderSettings{})
	if _, err := p.baseURLForModel("m"); err == nil {
		t.Fatal("expected missing-region error")
	}
	t.Setenv("AWS_REGION", "us-west-2")
	got, err := p.baseURLForModel("m")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://bedrock-mantle.us-west-2.api.aws/v1" {
		t.Fatalf("got %q", got)
	}
}
