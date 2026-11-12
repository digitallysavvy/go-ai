package deepgram

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93provcOutputFormatSampleRateParseInt: TS parseInt(secondPart, 10)
// skips leading JS whitespace (NBSP and BOM included, NEL not): node gives
// parseInt("\u00a024000") = 24000, parseInt("\ufeff8000") = 8000 and
// parseInt("\u0085 8000") = NaN.
func TestR93provcOutputFormatSampleRateParseInt(t *testing.T) {
	m := NewSpeechModel(New(Config{APIKey: "k"}), "aura-2-thalia-en")
	cases := map[string]string{
		"wav_\u00a024000": "24000",
		"wav_\ufeff8000":  "8000",
		"wav_\u0085 8000": "",
		"linear16_ 16000": "16000",
	}
	for format, want := range cases {
		q, _, err := m.buildArgs(&provider.SpeechGenerateOptions{Text: "x", OutputFormat: format})
		if err != nil {
			t.Fatal(err)
		}
		if got := q.get("sample_rate"); got != want {
			t.Errorf("%q: sample_rate = %q, want %q", format, got, want)
		}
	}
}
