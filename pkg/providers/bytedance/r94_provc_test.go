package bytedance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS postJsonToApi checks response.ok: an unfollowed 3xx fails with the
// ByteDance API error instead of parsing the body as a success.
func TestR94provcNon2xxFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(300)
		_, _ = w.Write([]byte(`{"id":"t1","data":[{"b64_json":"aGk="}]}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	vm, _ := p.VideoModel(string(ModelSeedance10Pro))
	if _, err := vm.(provider.VideoModelStarter).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1}}); err == nil {
		t.Error("video start: want the 300 API error")
	}
	im, _ := p.ImageModel("seedream-4-0-250828")
	if res, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err == nil {
		t.Errorf("image: result = %+v, want the 300 API error", res)
	}
}

// TS createJsonResponseHandler and the failed-response handler read the
// body as text (TextDecoder drops a UTF-8 BOM).
func TestR94provcResponseBodyIsText(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/contents/generations/tasks":
			_, _ = w.Write([]byte(bom + `{"id":"t1"}`))
		case r.URL.Path == "/contents/generations/tasks/t1":
			_, _ = w.Write([]byte(bom + `{"id":"t1","status":"succeeded","content":{"video_url":"https://x/v.mp4"}}`))
		case r.URL.Path == "/images/generations":
			_, _ = w.Write([]byte(bom + `{"data":[{"b64_json":"aGk="}]}`))
		default:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(bom + `{"error":{"message":"bad thing"}}`))
		}
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	vm, _ := p.VideoModel(string(ModelSeedance10Pro))
	start, err := vm.(provider.VideoModelStarter).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	res, err := vm.(provider.VideoModelStatusChecker).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil || res.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status: %v %+v", err, res)
	}
	op, _ := json.Marshal(map[string]string{"taskId": "missing"})
	_, err = vm.(provider.VideoModelStatusChecker).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil || err.Error() != "bad thing" {
		t.Errorf("failed poll: err = %v, want bad thing", err)
	}
	im, _ := p.ImageModel("seedream-4-0-250828")
	if _, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"}); err != nil {
		t.Errorf("image: %v", err)
	}
}
