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
