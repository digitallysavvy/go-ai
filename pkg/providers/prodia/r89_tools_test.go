package prodia

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS prodia-video-model.ts txt2vid postToApi: content JSON.stringify({type,
// config: {prompt, seed, resolution}}) and headers {...combined, Accept,
// 'Content-Type'} (the fixed Accept wins over a call header).
func TestR89toolsProdiaTxt2VidBodyOrderAndAccept(t *testing.T) {
	var body, accept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body, accept = string(b), r.Header.Get("Accept")
		w.Header().Set("Content-Type", "multipart/form-data; boundary=b")
		_, _ = w.Write([]byte(r33Job + "--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: video/mp4\r\n\r\nVIDEO\r\n--b--\r\n"))
	}))
	defer srv.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: srv.URL}).VideoModel("inference.wan2-2.lightning.txt2vid.v0")
	seed := 7
	_, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt:          "a cat",
		Seed:            &seed,
		Headers:         map[string]string{"Accept": "application/json"},
		ProviderOptions: map[string]interface{}{"prodia": map[string]interface{}{"resolution": "720p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"type":"inference.wan2-2.lightning.txt2vid.v0","config":{"prompt":"a cat","seed":7,"resolution":"720p"}}`; body != want {
		t.Fatalf("body = %s\nwant %s", body, want)
	}
	if accept != "multipart/form-data; video/mp4" {
		t.Fatalf("Accept = %q", accept)
	}
}
