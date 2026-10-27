package azure

import (
	"strings"
	"testing"
)

// TS interpolates speed/styleDegree as String(number): 1e6 is "1000000",
// not Go's "1e+06".
func TestBuildAzureSSMLNumbersUseJSFormatting(t *testing.T) {
	speed, degree := 1e6, 1e-7
	ssml := buildAzureSSML(azureSSMLOptions{text: "hi", voiceName: "v", locale: "en-US", speed: &speed, style: "cheerful", styleDegree: &degree})
	for _, want := range []string{`rate="1000000"`, `styledegree="1e-7"`} {
		if !strings.Contains(ssml, want) {
			t.Fatalf("ssml = %s, want %s", ssml, want)
		}
	}
}

// TS JSON.stringify writes the definition members as enhancedMode, locales,
// diarization, phraseList.
func TestAzureSpeechDefinitionMemberOrder(t *testing.T) {
	buf, _, err := buildAzureSpeechTranscribeMultipart([]byte("a"), "audio/wav", azureSpeechDefinition{
		EnhancedMode: map[string]interface{}{"enabled": true},
		Locales:      []string{"en"},
		Diarization:  map[string]interface{}{"enabled": true},
		PhraseList:   map[string]interface{}{"phrases": []string{"x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"enhancedMode":{"enabled":true},"locales":["en"],"diarization":{"enabled":true},"phraseList":{"phrases":["x"]}}`
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("body = %s", buf.String())
	}
}
