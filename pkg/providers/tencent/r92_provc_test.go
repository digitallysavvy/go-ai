package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS getTokenHubHeaders: resolve(this.config.tokenHub.headers) throws the
// apiKey callback's error as is, before any request. Go must not hand the
// caller the internal FetchRejection marker the client wraps it in.
func TestR92provcTokenHubKeyFuncErrorIsReturnedAsIs(t *testing.T) {
	r92provcSentinel := errors.New("key vault unavailable")
	p := New(Config{
		APIKeyFunc: func(context.Context) (string, error) { return "", r92provcSentinel },
		BaseURL:    "http://127.0.0.1:1/v1",
	})
	m, err := NewVideoModel(p, "kling-video-v3")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: defaultVideoOptions()})
	var rejection *internalhttp.FetchRejection
	if err != r92provcSentinel || errors.As(err, &rejection) {
		t.Fatalf("DoStart err = %#v, want the key function's error as is", err)
	}
	_, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: json.RawMessage(`{"taskId":"task-123"}`)})
	if err != r92provcSentinel || errors.As(err, &rejection) {
		t.Fatalf("DoStatus err = %#v, want the key function's error as is", err)
	}
}
