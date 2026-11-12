package deepgram

import "testing"

// TS: formatLower.split('_') with parts[1] read by parseInt, so trailing
// segments and non-digit suffixes do not defeat the sample rate.
func TestApplyOutputFormatSampleRateLikeParseInt(t *testing.T) {
	for format, want := range map[string]string{
		"wav_44100_extra":   "44100",
		"wav_44100hz":       "44100",
		"linear16_24000x":   "24000",
		"flac_22050_stereo": "22050",
	} {
		query := newDGQuery()
		applyOutputFormat(query, format)
		if query.vals["sample_rate"] != want {
			t.Errorf("%s: sample_rate = %q, want %q (%v)", format, query.vals["sample_rate"], want, query)
		}
	}
	query := newDGQuery()
	applyOutputFormat(query, "wav_hz")
	if _, ok := query.vals["sample_rate"]; ok || query.vals["container"] != "wav" {
		t.Errorf("wav_hz = %v", query)
	}
}
