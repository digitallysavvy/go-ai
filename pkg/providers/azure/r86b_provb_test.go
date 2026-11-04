package azure

import "testing"

// TestR86bprovbBaseURLOneTrailingSlash pins TS createAzure's
// withoutTrailingSlash on speechBaseURL / maiBaseURL / baseURL (one "/"
// removed). Wants printed by Node 24.
func TestR86bprovbBaseURLOneTrailingSlash(t *testing.T) {
	p, err := New(Config{APIKey: "k", SpeechBaseURL: "https://s.example//", MaiBaseURL: "https://m.example//", BaseURL: "https://r.openai.azure.com/openai//"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := p.speechBaseURL(); got != "https://s.example/" {
		t.Errorf("speechBaseURL = %q, want https://s.example/", got)
	}
	if got, _ := p.maiBaseURL(); got != "https://m.example/" {
		t.Errorf("maiBaseURL = %q, want https://m.example/", got)
	}
}
