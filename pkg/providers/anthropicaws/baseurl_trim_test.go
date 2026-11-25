package anthropicaws

import "testing"

func TestBaseURLTrimsOnlyOneTrailingSlash(t *testing.T) {
	p, err := New(Config{BaseURL: "https://proxy.test/v1//", Region: "us-east-1", WorkspaceID: "ws"})
	if err != nil {
		t.Fatal(err)
	}
	if p.baseURL != "https://proxy.test/v1/" {
		t.Fatalf("baseURL = %q", p.baseURL)
	}
}
