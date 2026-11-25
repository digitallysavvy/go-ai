package klingai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS createJsonResponseHandler reads response.text() (TextDecoder drops a
// UTF-8 BOM): BOM-prefixed create and status bodies validate.
func TestR94provcResponseBodyIsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"code":0,"message":"ok","data":{"task_id":"t1"}}`))
			return
		}
		_, _ = w.Write([]byte("\xEF\xBB\xBF" + `{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"succeed","task_result":{"videos":[{"id":"v","url":"https://x/v.mp4"}]}}}`))
	}))
	defer srv.Close()
	clearKlingAICredentialEnv(t)
	p, err := New(Config{AccessKey: "a", SecretKey: "s", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	vm, err := p.VideoModel("kling-v2.6-t2v")
	if err != nil {
		t.Fatal(err)
	}
	start, err := vm.(provider.VideoModelStarter).DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p", N: 1}})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	res, err := vm.(provider.VideoModelStatusChecker).DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil || res.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("status: %v %+v", err, res)
	}
}
