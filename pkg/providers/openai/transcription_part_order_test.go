package openai

import (
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getArgs appends `model` before `file`.
func TestTranscriptionMultipartModelBeforeFile(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "whisper-1")
	body, ct, err := m.buildMultipartBody(&provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/mpeg"})
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(ct)
	r := multipart.NewReader(body, params["boundary"])
	var names []string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, p.FormName())
	}
	want := []string{"model", "file", "response_format"}
	if len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Fatalf("got %v want %v", names, want)
	}
}
