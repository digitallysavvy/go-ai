package moonshot

import (
	"strings"
	"testing"
)

// TestR96provbStreamMetadataJSONStringify: Moonshot stream metadata built
// from provider values is written like JSON.stringify: <, > and & stay
// literal (main ruling 4, write side).
func TestR96provbStreamMetadataJSONStringify(t *testing.T) {
	s := &moonshotStream{responseObject: "<a&b>"}
	if raw := s.buildProviderMetadata(); !strings.Contains(string(raw), `"<a&b>"`) {
		t.Fatalf("metadata = %s", raw)
	}
}
