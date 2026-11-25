package openai

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS openai-decision-model.ts 171: isFullMediaType("image/") is false (empty
// subtype), so the media type is detected from the bytes (isFullMediaType
// class sweep, review round 95).
func TestR95integDecisionImageEmptySubtypeIsDetected(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	img, err := decisionImageContent(provider.DecisionStatePart{Type: "file", File: &types.FileContent{MediaType: "image/", Data: png}})
	if err != nil {
		t.Fatal(err)
	}
	if url, _ := img["image_url"].(string); !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("image_url = %v, want the detected image/png", img["image_url"])
	}
}
