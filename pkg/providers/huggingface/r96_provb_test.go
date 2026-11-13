package huggingface

import (
	"strings"
	"testing"
)

// TestR96provbItemMetadataJSONStringify: Hugging Face item metadata is
// written like JSON.stringify: <, > and & stay literal (main ruling 4, write
// side).
func TestR96provbItemMetadataJSONStringify(t *testing.T) {
	if raw := hfItemMetadata("<a&b>"); !strings.Contains(string(raw), `"itemId":"<a&b>"`) {
		t.Fatalf("metadata = %s", raw)
	}
}
