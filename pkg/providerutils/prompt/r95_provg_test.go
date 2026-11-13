package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r95-tools-to-provg: TS isFullMediaType treats "image/" (empty subtype)
// like "image" and "image/*"; a URL image part without a full media type is
// sent as image/jpeg.
func TestR95provgGoogleURLImageEmptySubtype(t *testing.T) {
	msgs := ToGoogleMessages([]types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentPart{types.ImageContent{URL: "https://example.com/a", MimeType: "image/"}},
	}}, false)
	b, _ := json.Marshal(msgs)
	if !strings.Contains(string(b), `"mimeType":"image/jpeg"`) {
		t.Fatalf("messages = %s, want image/jpeg", b)
	}
}
