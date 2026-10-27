package harness

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Image bytes in a turn prompt must never reach lifecycle callbacks or
// telemetry: promptMessages carries the prompt's text only (TS promptToText).
func TestPromptMessagesNeverCarryImageData(t *testing.T) {
	secret := []byte("SECRET-IMAGE-BYTES")
	d := &turnDriver{in: runPromptInput{Prompt: Prompt{Message: &types.Message{
		Role: types.RoleUser,
		Content: []types.ContentPart{
			types.TextContent{Text: "look"},
			types.ImageContent{Image: secret, MimeType: "image/png"},
			types.FileContent{Data: secret, MediaType: "image/png"},
		},
	}}}}
	raw, err := json.Marshal(d.promptMessages())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "U0VDUkVU") {
		t.Fatalf("prompt messages leak image data: %s", raw)
	}
	if !strings.Contains(string(raw), "look") {
		t.Fatalf("text part lost: %s", raw)
	}
}
