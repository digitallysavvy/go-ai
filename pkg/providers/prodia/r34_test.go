package prodia

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doStream emits a text block for every text content part, an empty text
// part included (`part.type === 'text'`).
func TestR34_LanguageStreamEmitsEmptyTextBlock(t *testing.T) {
	// An empty "output" part (the shared helper skips empty data).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	jh := make(textproto.MIMEHeader)
	jh.Set("Content-Disposition", `form-data; name="job"`)
	jh.Set("Content-Type", "application/json")
	jp, _ := mw.CreatePart(jh)
	_, _ = jp.Write([]byte(`{"id":"job-1"}`))
	oh := make(textproto.MIMEHeader)
	oh.Set("Content-Disposition", `form-data; name="output"`)
	oh.Set("Content-Type", "text/plain")
	_, _ = mw.CreatePart(oh)
	_ = mw.Close()
	body, contentType := buf.Bytes(), mw.FormDataContentType()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	m := NewLanguageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), LanguageModelNanoBananaImgToImgV2)
	stream, err := m.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	var order []provider.ChunkType
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		order = append(order, c.Type)
	}
	want := []provider.ChunkType{provider.ChunkTypeStreamStart, provider.ChunkTypeResponseMetadata, provider.ChunkTypeTextStart, provider.ChunkTypeText, provider.ChunkTypeTextEnd, provider.ChunkTypeFinish}
	if len(order) != len(want) {
		t.Fatalf("chunk order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("chunk order = %v, want %v", order, want)
		}
	}
}

// The multipart handlers in TS throw plain Errors with fixed texts; the Go
// models surface the same texts unwrapped.
func TestR34_MultipartErrorTextsMatchTS(t *testing.T) {
	// A body whose only part is an output (no job part).
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	oh := make(textproto.MIMEHeader)
	oh.Set("Content-Disposition", `form-data; name="output"`)
	oh.Set("Content-Type", "image/png")
	op, _ := mw.CreatePart(oh)
	_, _ = op.Write([]byte("png"))
	_ = mw.Close()
	noJobBody, noJobCT := buf.Bytes(), mw.FormDataContentType()

	cases := []struct {
		name, ctype string
		body        []byte
		want        string
	}{
		{"no boundary", "multipart/form-data", []byte("x"), "Prodia response missing multipart boundary in content-type: multipart/form-data"},
		{"no job", noJobCT, noJobBody, "Prodia multipart response missing job part"},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", c.ctype)
			_, _ = w.Write(c.body)
		}))
		prov := New(Config{APIKey: "k", BaseURL: srv.URL})
		_, imgErr := NewImageModel(prov, "m").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
		_, vidErr := NewVideoModel(prov, "m").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
		_, lmErr := NewLanguageModel(prov, LanguageModelNanoBananaImgToImgV2).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "p"}})
		srv.Close()
		for who, err := range map[string]error{"image": imgErr, "video": vidErr, "language": lmErr} {
			if err == nil || err.Error() != c.want {
				t.Errorf("%s/%s: err = %v, want %q", c.name, who, err, c.want)
			}
		}
	}
}
