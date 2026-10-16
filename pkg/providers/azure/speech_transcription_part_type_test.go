package azure

import (
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

func TestBuildAzureSpeechTranscribeMultipart_AudioPartCarriesMediaType(t *testing.T) {
	buf, ct, err := buildAzureSpeechTranscribeMultipart([]byte("abc"), "audio/wav", map[string]interface{}{})
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(ct)
	mr := multipart.NewReader(strings.NewReader(buf.String()), params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := part.Header.Get("Content-Type"); got != "audio/wav" {
		t.Fatalf("audio part Content-Type = %q, want audio/wav", got)
	}
	if part.FileName() != "audio.wav" {
		t.Fatalf("filename = %q", part.FileName())
	}
}
