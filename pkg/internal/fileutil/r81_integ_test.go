package fileutil

import (
	"encoding/base64"
	"testing"
)

// cr-r81-prova-to-integrator item 2: TS detectMediaType decodes only the
// prefix of a base64 string (decodePrefix), so an invalid tail is ignored.
func TestR81integDetectMediaTypeSignatureBase64(t *testing.T) {
	if got, ok, err := DetectMediaTypeSignatureBase64("iVBORw0KGgoAAAANSUhEUgAA"+"!!!!", "image"); err != nil || !ok || got != "image/png" {
		t.Fatalf("png with an invalid tail = %q %v %v, want image/png", got, ok, err)
	}
	if _, _, err := DetectMediaTypeSignatureBase64("!!!!!!!!!!!!!!!!!!!!!!!!", "image"); err == nil {
		t.Fatal("an invalid prefix decoded, want the atob error")
	}
	if got, ok, err := DetectMediaTypeSignatureBase64("iVBORw0KGgo", ""); err != nil || !ok || got != "image/png" {
		t.Fatalf("short unpadded png = %q %v %v", got, ok, err)
	}
	// ID3: the scan prefix past the tag is decoded (TS decodes up to
	// MaxID3TagBytes+12 bytes, so a short string is decoded whole).
	tag := make([]byte, 10+20)
	copy(tag, "ID3\x03\x00\x00\x00\x00\x00\x14")
	mp3 := append(tag, 0xFF, 0xFB, 0x90, 0x00)
	s := base64.StdEncoding.EncodeToString(append(mp3, make([]byte, 30)...))
	if got, ok, err := DetectMediaTypeSignatureBase64(s, "audio"); err != nil || !ok || got != "audio/mpeg" {
		t.Fatalf("id3 mp3 = %q %v %v, want audio/mpeg", got, ok, err)
	}
}
