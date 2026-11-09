package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provg r90 open item: TS throws the video SSE error event as an
// APICallError whose responseBody is JSON.stringify(event): the parsed event
// in gatewayVideoEventSchema order, undeclared keys stripped, param as
// received, HTML characters literal (gateway-video-model.ts 122-137). Go
// used the data body ({error: {...}}).
func TestR90integVideoSSEErrorResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"statusCode":400,"extra":1,"param":{"z":1,"a":"<x>"},"errorType":"invalid_request_error","message":"bad","type":"error"}`+"\n\n")
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVideoModel(p, "google/veo-3.1").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "A cat"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v, want a ProviderError", err, err)
	}
	const want = `{"type":"error","message":"bad","errorType":"invalid_request_error","statusCode":400,"param":{"z":1,"a":"<x>"}}`
	if pe.ResponseBody != want {
		t.Fatalf("ResponseBody = %s, want %s", pe.ResponseBody, want)
	}
}
