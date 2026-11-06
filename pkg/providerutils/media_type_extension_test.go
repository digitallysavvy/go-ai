package providerutils

import (
	"bytes"
	"mime/multipart"
	"strings"
	"testing"
)

func TestMediaTypeToExtension(t *testing.T) {
	for in, want := range map[string]string{
		"audio/mpeg": "mp3", "audio/MPEG": "mp3", "audio/x-wav": "wav", "audio/opus": "ogg",
		"audio/mp4": "m4a", "audio/x-m4a": "m4a", "audio/flac": "flac", "audio/aac": "aac", "audio/wave": "wave", "": "", "audio": "",
	} {
		if got := MediaTypeToExtension(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestCreateTypedFormFileSetsContentType(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := CreateTypedFormFile(w, "file", "audio.mp3", "audio/mpeg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("x"))
	_ = w.Close()
	if s := buf.String(); !strings.Contains(s, "Content-Type: audio/mpeg") || !strings.Contains(s, `filename="audio.mp3"`) {
		t.Fatalf("part = %s", s)
	}
}
