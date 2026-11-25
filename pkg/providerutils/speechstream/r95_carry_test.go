package speechstream

import (
	"errors"
	"math"
	"testing"
)

// A finite positive rate passes isAudioFormat (TS keeps the JS number); a
// rate beyond int must not wrap to a negative value.
func TestR95carryAudioRateBeyondIntIsClamped(t *testing.T) {
	for _, rate := range []string{"1e20", "4294967296"} {
		part, ok := ParsePart(`{"type":"audio","audio":"AAA=","audioFormat":{"type":"audio/pcm","rate":` + rate + `}}`)
		if !ok || part.AudioFormat.Rate == nil {
			t.Fatalf("rate %s: part not parsed", rate)
		}
		if got := *part.AudioFormat.Rate; got <= 0 || (rate == "1e20" && got != math.MaxInt) {
			t.Errorf("rate %s: Rate = %d", rate, got)
		}
	}
}

// TS parseSpeechStreamPart keeps an audio part whose base64 is invalid, and
// streamSpeech fails the stream there with convertBase64ToUint8Array's atob
// error. Expected messages from Node 24 atob (after TS maps "-" / "_").
func TestR95carryParsePartStrictInvalidBase64(t *testing.T) {
	frame := func(audio string) string {
		return `{"type":"audio","audio":"` + audio + `","audioFormat":{"type":"audio/pcm"}}`
	}
	for _, tc := range []struct{ audio, want string }{
		{"@@@@", "Invalid character"},
		{"AAAAA", "The string to be decoded is not correctly encoded."},
		{"AAAAA==", "The string to be decoded is not correctly encoded."},
		{"A=A=", "Invalid character"},
		{"AAAA=", "Invalid character"},
		{"A===", "Invalid character"},
		{"AA=", "Invalid character"},
		{`AAAA\u000b`, "Invalid character"},
	} {
		part, ok, err := ParsePartStrict(frame(tc.audio))
		var ice *InvalidCharacterError
		if part != nil || ok || !errors.As(err, &ice) || err.Error() != tc.want || ice.ErrorName() != "InvalidCharacterError" {
			t.Errorf("%q: part %v ok %v err %v, want %q", tc.audio, part, ok, err, tc.want)
		}
		if p, ok := ParsePart(frame(tc.audio)); p != nil || ok {
			t.Errorf("%q: ParsePart accepted the frame", tc.audio)
		}
	}
	// Valid and URL-safe base64 decode; other invalid frames are not errors.
	if part, ok, err := ParsePartStrict(frame("AA-_")); err != nil || !ok || len(part.Audio) != 3 {
		t.Errorf("url-safe audio: %v %v %v", part, ok, err)
	}
	if part, ok, err := ParsePartStrict(`{"type":"audio","audio":"@@@@","audioFormat":{"type":""}}`); part != nil || ok || err != nil {
		t.Errorf("invalid audioFormat must stay a dropped frame: %v %v %v", part, ok, err)
	}
	if _, ok, err := ParsePartStrict(`{"type":"nope"}`); ok || err != nil {
		t.Errorf("unknown part: %v %v", ok, err)
	}
}
