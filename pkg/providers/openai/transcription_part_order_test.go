package openai

import (
	"io"
	"mime"
	"mime/multipart"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getArgs appends `model` before `file`; whisper-1 defaults to
// verbose_json with segment timestamps (TS 9026c7de79).
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
	want := []string{"model", "file", "response_format", "timestamp_granularities[]"}
	if len(names) != 4 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] || names[3] != want[3] {
		t.Fatalf("got %v want %v", names, want)
	}
}
