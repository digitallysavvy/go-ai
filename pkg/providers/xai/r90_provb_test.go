package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provbXAIErrorCodeType: an {error: {message, code}} body whose code
// is neither a string nor a number matches no member of TS
// xaiErrorDataSchema (xai-error.ts), so there is no message.
func TestR90provbXAIErrorCodeType(t *testing.T) {
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":true}}`)); ok {
		t.Fatalf("matched with %q", msg)
	}
	if msg, ok := xaiErrorMessage([]byte(`{"error":{"message":"m","code":7}}`)); !ok || msg != "m" {
		t.Fatalf("numeric code: %q %v", msg, ok)
	}
}

// TestR90provbTemplateQuotes: TS template literals quote values with plain
// `"${v}"`, so a value with a quote or non-ASCII text appears as is, not Go
// %q-escaped (xai-video-model.ts "Unrecognized resolution").
func TestR90provbTemplateQuotes(t *testing.T) {
	m := &VideoModel{modelID: ModelGrokImagineVideo15}
	warnings := m.checkUnsupportedOptions(&provider.VideoModelV3CallOptions{Resolution: `9"x é`}, &XAIVideoProviderOptions{}, "")
	want := `Unrecognized resolution "9"x é". Use providerOptions.xai.resolution with "480p", "720p", or "1080p" instead.`
	if len(warnings) != 1 || warnings[0].Details != want {
		t.Fatalf("warnings = %#v", warnings)
	}
}
