package fal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getFromApi checks response.ok: an unfollowed 3xx on the transcription
// result poll fails with the fal API error.
func TestR94provcTranscriptionPollNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
			return
		}
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"text":"hello"}`))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k"})
	p.queueHost = strings.TrimRight(srv.URL, "/")
	tm, err := p.TranscriptionModel("whisper")
	if err != nil {
		t.Fatal(err)
	}
	res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	if err == nil {
		t.Fatalf("result = %+v, want the 300 API error", res)
	}
}

// TS size.split('x').map(Number) leaves the height undefined for a size with
// no "x", and JSON.stringify omits it (node: {"width":1024}).
func TestR94provcImageSizeMissingHeightOmitted(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "fal-ai/flux/dev")
	n := 1
	body, _, err := m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", Size: "1024", N: &n})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(body)
	if !strings.Contains(string(got), `"image_size":{"width":1024}`) {
		t.Fatalf("body = %s, want image_size {\"width\":1024}", got)
	}
	body, _, _ = m.buildRequest(&provider.ImageGenerateOptions{Prompt: "p", Size: "1024xabc", N: &n})
	got, _ = json.Marshal(body)
	if !strings.Contains(string(got), `"image_size":{"width":1024,"height":null}`) {
		t.Fatalf("body = %s, want a NaN height written as null", got)
	}
}

// TS createJsonResponseHandler and response.clone().json() read the body as
// text (TextDecoder drops a UTF-8 BOM).
func TestR94provcResponseBodyIsText(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/fal-ai/flux/dev":
			_, _ = w.Write([]byte(bom + `{"images":[]}`))
		case r.URL.Path == "/fal-ai/whisper" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"request_id":"r1"}`))
		case strings.HasPrefix(r.URL.Path, "/fal-ai/whisper/requests/"):
			_, _ = w.Write([]byte(bom + `{"text":"hello"}`))
		case r.URL.Path == "/done":
			_, _ = w.Write([]byte(bom + `{"video":{"url":"https://x/v.mp4"}}`))
		case r.URL.Path == "/pending":
			w.WriteHeader(400)
			_, _ = w.Write([]byte(bom + `{"detail":"Request is still in progress"}`))
		}
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	p.queueHost = srv.URL
	if _, err := NewImageModel(p, "fal-ai/flux/dev").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err != nil {
		t.Errorf("image: %v", err)
	}
	tm, _ := p.TranscriptionModel("whisper")
	if res, err := tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"}); err != nil || res.Text != "hello" {
		t.Errorf("transcription: %v %+v", err, res)
	}
	vm := NewVideoModel(p, "luma-dream-machine")
	for path, want := range map[string]string{"/done": provider.VideoOperationStatusCompleted, "/pending": provider.VideoOperationStatusPending} {
		op, _ := json.Marshal(falVideoOperation{ResponseURL: srv.URL + path, SubmitURL: srv.URL + "/fal-ai/x"})
		res, err := vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
		if err != nil || res.Status != want {
			t.Errorf("video %s: %v %+v, want %s", path, err, res, want)
		}
	}
}
