package alibaba

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provbEmptyStopSequencesSent: TS sends `stop: stopSequences`, so an
// empty array is sent as `stop: []`; only an absent value is left out.
func TestR95provbEmptyStopSequencesSent(t *testing.T) {
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}, StopSequences: []string{}}
	body, _, err := NewLanguageModel(New(Config{APIKey: "k"}), "qwen-plus").buildRequestBodyChecked(opts, false)
	if err != nil {
		t.Fatal(err)
	}
	if stop, ok := body["stop"].([]string); !ok || len(stop) != 0 {
		t.Fatalf("stop = %#v, want []", body["stop"])
	}
}

// TestR95provbLegacyImageFullMediaType (cr-r95-tools-to-provb): TS
// formatImageUrl uses resolveFullMediaType, so "image/" on PNG bytes is sent
// as image/png (the legacy path used image/jpeg only for "image" and
// "image/*" and sent "image/" as is).
func TestR95provbLegacyImageFullMediaType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	for _, mt := range []string{"image", "image/", "image/*"} {
		msgs := ConvertToAlibabaChatMessages([]types.Message{{Role: types.RoleUser, Content: []types.ContentPart{
			types.FileContent{MediaType: mt, Data: png},
		}}}, nil, false)
		parts := msgs[0]["content"].([]map[string]interface{})
		url := parts[0]["image_url"].(map[string]interface{})["url"].(string)
		if !strings.HasPrefix(url, "data:image/png;base64,") {
			t.Errorf("%q: url = %q, want data:image/png", mt, url)
		}
	}
}
