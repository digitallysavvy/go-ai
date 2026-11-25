package minimax

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

const r93toolsBOM = "\xEF\xBB\xBF"

// TS createJsonResponseHandler / createJsonErrorResponseHandler read the body
// with response.text() (WHATWG TextDecoder): a leading BOM is dropped before
// the JSON is parsed, for the create, the status poll and an error body.
func TestR93toolsMiniMaxVideoBodiesDropBOM(t *testing.T) {
	statusCode := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(r93toolsBOM + `{"task_id":"t"}`))
			return
		}
		if statusCode != http.StatusOK {
			w.WriteHeader(statusCode)
			_, _ = w.Write([]byte(r93toolsBOM + `{"error":{"message":"quota exceeded"}}`))
			return
		}
		_, _ = w.Write([]byte(r93toolsBOM + `{"task":{"status":"queued"}}`))
	}))
	defer srv.Close()
	model := newVideoModel(providerForMinimaxServer(srv.URL), ModelH3)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *minimaxDefaultCallOptions(nil)})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil || status.Status != provider.VideoOperationStatusPending {
		t.Fatalf("status = %+v, err = %v", status, err)
	}

	statusCode = http.StatusTooManyRequests
	_, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	var mmErr *Error
	if !errors.As(err, &mmErr) || mmErr.Message != "quota exceeded" || mmErr.ResponseBody != `{"error":{"message":"quota exceeded"}}` {
		t.Fatalf("err = %#v", err)
	}
}

func TestR93toolsMiniMaxImageBodyDropsBOM(t *testing.T) {
	srv, _, _ := r91toolsImageServer(t, func() (int, string) {
		return 200, r93toolsBOM + `{"data":[{"url":"IMAGE_URL"}]}`
	}, nil)
	m, _ := New(Config{APIKey: "k", ImageBaseURL: srv.URL + "/v1"}).Image(ModelH3Image)
	res, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", N: r91toolsN(1)})
	if err != nil || len(res.Images) != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}
