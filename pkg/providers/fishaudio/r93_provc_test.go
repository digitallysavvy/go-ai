package fishaudio

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93provcEmptyLanguageIsSent: TS appends `language` whenever it is
// `!= null`, so an empty string is sent as an empty form field.
func TestR93provcEmptyLanguageIsSent(t *testing.T) {
	m := &TranscriptionModel{modelID: "default"}
	body, _, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg",
		ProviderOptions: map[string]interface{}{"fishAudio": map[string]interface{}{"language": ""}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), `name="language"`) {
		t.Errorf("no language field in %q", body.String())
	}
	body, _, _ = m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if strings.Contains(body.String(), `name="language"`) {
		t.Error("language sent without the option")
	}
}
