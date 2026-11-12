package fal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r91provcWhole reads falNumber's int / int64 result back as a float.
func r91provcWhole(t *testing.T, v interface{}) float64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case *int:
		return float64(*n)
	case *int64:
		return float64(*n)
	}
	t.Fatalf("unexpected %T", v)
	return 0
}

// TestR91provcFalNumberKeepsLargeSeeds (provc-03): a fal seed above 2^31-1
// keeps its value on every platform (TS passes the JS number through,
// fal-video-model.ts:357); an in-range value stays an int.
func TestR91provcFalNumberKeepsLargeSeeds(t *testing.T) {
	for _, f := range []float64{3851223452, -3851223452, 42} {
		if got := r91provcWhole(t, falNumber(f)); got != f {
			t.Errorf("falNumber(%v) = %v", f, got)
		}
		ff := f
		if got := r91provcWhole(t, falNumberPtr(&ff)); got != f {
			t.Errorf("falNumberPtr(%v) = %v", f, got)
		}
	}
	if _, ok := falNumber(42).(int); !ok {
		t.Error("an in-range value must stay an int (v0.5.2 type)")
	}
}

// TestR91provcVideoBodyOrderAndStatusHeaders (provc-07, provc-06): the queue
// body keeps the TS buildRequestBody insertion order, and repeated status
// response headers are joined with ", ".
func TestR91provcVideoBodyOrderAndStatusHeaders(t *testing.T) {
	var mu sync.Mutex
	var raw string
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/m", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"id","response_url":"http://`+r.Host+`/fal-ai/m/requests/id"}`)
	})
	mux.HandleFunc("/fal-ai/m/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Test", "a")
		w.Header().Add("X-Test", "b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"video":{"url":"https://cdn.example.com/v.mp4"}}`)
	})
	model, _ := newTestVideoModel(t, "m", mux)
	d := 5.0
	seed := 7
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{
		Prompt: "x", AspectRatio: "16:9", Duration: &d, Seed: &seed,
		Image: &provider.VideoModelV3File{Type: "url", URL: "https://example.com/a.png"},
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{
			"negativePrompt": "dogs", "loop": true, "num_frames": 9,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := raw
	mu.Unlock()
	if want := `{"prompt":"x","image_url":"https://example.com/a.png","aspect_ratio":"16:9","duration":"5s","seed":7,"loop":true,"negative_prompt":"dogs","num_frames":9}`; got != want {
		t.Errorf("body = %s\nwant %s", got, want)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil {
		t.Fatal(err)
	}
	if h := status.Response.Headers["X-Test"]; h != "a, b" {
		t.Errorf("X-Test = %q, want \"a, b\"", h)
	}
}

// TestR91provcTranscriptionNonJSONErrorBody (provc-16): a poll error body
// that is not JSON (or is JSON null) is TS getFromApi's APICallError 'Failed
// to process error response' with the status code.
func TestR91provcTranscriptionNonJSONErrorBody(t *testing.T) {
	for name, body := range map[string]string{"html": "<html>502</html>", "null": "null"} {
		mux := http.NewServeMux()
		mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"request_id":"id"}`)
		})
		mux.HandleFunc("/fal-ai/wizper/requests/id", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, body)
		})
		m, srv := newTestTranscriptionModel(t, mux)
		_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte("a"), MimeType: "audio/wav"})
		srv.Close()
		var perr *providererrors.ProviderError
		if !errors.As(err, &perr) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if perr.Message != "Failed to process error response" || perr.StatusCode != http.StatusBadGateway {
			t.Errorf("%s: message %q status %d", name, perr.Message, perr.StatusCode)
		}
	}
}

// TestR91provcTranscriptionAndImageBodyOrder (provc-07): the transcription
// body is getArgs order then audio_url; the image body is prompt, seed,
// image_size, num_images, files, then the options in transform order.
func TestR91provcTranscriptionAndImageBodyOrder(t *testing.T) {
	var mu sync.Mutex
	var raw string
	mux := http.NewServeMux()
	mux.HandleFunc("/fal-ai/wizper", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		raw = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"request_id":"id"}`)
	})
	mux.HandleFunc("/fal-ai/wizper/requests/id", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"hi","chunks":[]}`)
	})
	m, srv := newTestTranscriptionModel(t, mux)
	defer srv.Close()
	_, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte("a"), MimeType: "audio/wav",
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{"language": "en", "numSpeakers": 2, "chunkLevel": "segment"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := raw
	mu.Unlock()
	if want := `{"task":"transcribe","diarize":true,"chunk_level":"segment","language":"en","version":"3","batch_size":64,"num_speakers":2,"audio_url":"data:audio/wav;base64,YQ=="}`; got != want {
		t.Errorf("transcription body = %s\nwant %s", got, want)
	}

	im := NewImageModel(New(Config{APIKey: "k"}), "fal-fast")
	n, seed := 2, 5
	ob, _, err := im.buildRequest(&provider.ImageGenerateOptions{
		Prompt: "p", N: &n, Seed: &seed, Size: "10x20",
		Mask: &provider.ImageFile{Type: "url", URL: "https://example.com/m.png"},
		ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{
			"zeta": 1, "strength": 0.5, "guidanceScale": 3, "imageUrl": "https://example.com/i.png",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"prompt":"p","seed":5,"image_size":{"width":10,"height":20},"num_images":2,"mask_url":"https://example.com/m.png","image_url":"https://example.com/i.png","guidance_scale":3,"strength":0.5,"zeta":1}`; ob.String() != want {
		t.Errorf("image body = %s\nwant %s", ob.String(), want)
	}
}

// TestR91provcTypedOptionLiterals (typed-option ruling).
func TestR91provcTypedOptionLiterals(t *testing.T) {
	interval, timeout := (&VideoModel{}).VideoStartStatusPollDefaults(map[string]interface{}{"fal": map[string]int32{"pollIntervalMs": 50, "pollTimeoutMs": 900}})
	if interval != 50 || timeout != 900 {
		t.Errorf("poll defaults = %d %d", interval, timeout)
	}
	parsed, err := parseFalTranscriptionOptions(map[string]interface{}{"numSpeakers": int8(2), "language": "en"})
	if err != nil || parsed.NumSpeakers == nil || *parsed.NumSpeakers != 2 {
		t.Errorf("transcription options = %+v %v", parsed, err)
	}
	m := NewVideoModel(New(Config{APIKey: "k"}), "m")
	body, err := m.buildStartRequestBody(&provider.VideoModelV3CallOptions{ProviderOptions: map[string]interface{}{"fal": map[string]string{"negativePrompt": "dogs"}}})
	if err != nil || body.String() != `{"negative_prompt":"dogs"}` {
		t.Errorf("video body = %v %v", body, err)
	}
}
