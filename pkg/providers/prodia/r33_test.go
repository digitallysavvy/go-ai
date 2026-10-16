package prodia

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const r33Job = "--b\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"j\",\"created_at\":\"\"}\r\n"

func multipartServer(t *testing.T, parts string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "multipart/form-data; boundary=b")
		_, _ = w.Write([]byte(r33Job + parts + "--b--\r\n"))
	}))
}

// TS spreads `created_at != null`, so an empty string stays in the metadata.
func TestR33_EmptyCreatedAtKept(t *testing.T) {
	job := &prodiaJobResponse{}
	if err := job.UnmarshalJSON([]byte(`{"id":"j","created_at":"","updated_at":null}`)); err != nil {
		t.Fatal(err)
	}
	meta := buildProdiaProviderMetadata(job)
	if v, ok := meta["createdAt"]; !ok || v != "" {
		t.Errorf("createdAt = %v (present %v)", v, ok)
	}
	if _, ok := meta["updatedAt"]; ok {
		t.Errorf("null updated_at reported: %v", meta)
	}
}

// A missing output image is an error in TS; an unnamed image/* part is the output.
func TestR33_ImageOutputSelection(t *testing.T) {
	srv := multipartServer(t, "")
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).ImageModel("inference.flux-fast.schnell.txt2img.v2")
	if _, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err == nil || !strings.Contains(err.Error(), "missing output image") {
		t.Fatalf("err = %v", err)
	}

	srv2 := multipartServer(t, "--b\r\nContent-Disposition: form-data; name=\"other\"\r\nContent-Type: image/jpeg\r\n\r\nJPEG\r\n")
	defer srv2.Close()
	m, _ = New(Config{APIKey: "k", BaseURL: srv2.URL}).ImageModel("inference.flux-fast.schnell.txt2img.v2")
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	if err != nil || string(res.Image) != "JPEG" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// The video media type stays video/mp4 unless the output part says video/*.
func TestR33_VideoMediaTypeDefault(t *testing.T) {
	srv := multipartServer(t, "--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: application/octet-stream\r\n\r\nVIDEO\r\n")
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).VideoModel("inference.wan2-2.lightning.txt2vid.v0")
	res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
	if err != nil || res.Videos[0].MediaType != "video/mp4" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// JSON.stringify(detail) normalizes numbers.
func TestR33_ErrorDetailIsJSStringified(t *testing.T) {
	if got := prodiaErrorMessage(400, []byte(`{"detail":{"a":1.0}}`)); got != `{"a":1}` {
		t.Errorf("got %q", got)
	}
}
