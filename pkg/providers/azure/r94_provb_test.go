package azure

import "testing"

// TestR94provbSpeechVoiceEmptyLanguageCode: TS resolveVoice computes
// `language ? language.split('-')[0].toLowerCase() : undefined`, so a
// language such as "-US" is the code "" (not undefined): with no voice it has
// no default voice and warns, and with an explicit voice it differs from the
// voice's language and warns.
func TestR94provbSpeechVoiceEmptyLanguageCode(t *testing.T) {
	voice, warning := azureResolveVoice("", "-US")
	if voice != azureDefaultVoice || warning != `No default MAI voice for language "-US". en-US-Harper was used.` {
		t.Fatalf("no voice: got %q, %q", voice, warning)
	}
	voice, warning = azureResolveVoice("de-DE-Mia", "-US")
	if voice != "de-DE-Mia" || warning != `The voice de-DE-Mia selects the language. Language "-US" was ignored.` {
		t.Fatalf("explicit voice: got %q, %q", voice, warning)
	}
	if voice, warning = azureResolveVoice("", ""); voice != azureDefaultVoice || warning != "" {
		t.Fatalf("no language: got %q, %q", voice, warning)
	}
}
