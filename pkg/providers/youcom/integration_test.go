package youcom

import (
	"os"
	"testing"
)

func TestIntegrationRequiresYouAPIKey(t *testing.T) {
	if os.Getenv("YDC_API_KEY") == "" {
		t.Skip("YDC_API_KEY not set")
	}

	tool := YouSearch()
	if tool.ProviderExecuted {
		t.Fatal("You.com TypeScript-parity tool must be locally executed")
	}
}
