package openresponses

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR96provbInfItemIDMetadataNull: an output item id beyond the double
// range reads as +Inf and the reasoning metadata writes it as null, as
// JSON.stringify does (main ruling 4, both sides).
func TestR96provbInfItemIDMetadataNull(t *testing.T) {
	var item OutputItem
	if err := json.Unmarshal([]byte(`{"type":"reasoning","id":1e400,"summary":[]}`), &item); err != nil {
		t.Fatal(err)
	}
	raw := openResponsesReasoningProviderMetadata("open-responses", &item)
	if !strings.Contains(string(raw), `"itemId":null`) {
		t.Fatalf("metadata = %s, want itemId null", raw)
	}
}
