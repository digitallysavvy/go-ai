package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
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

// TestR92provcNonFiniteDurationIsNull (r92 provc-13): TS sends the duration
// as is and JSON.stringify writes NaN / Infinity as null; Go failed to encode
// the body.
func TestR92provcNonFiniteDurationIsNull(t *testing.T) {
	for _, id := range []string{"kling-video-v3", "vod/VS/2.0"} {
		s := newVideoServer(t)
		opts := defaultVideoOptions()
		inf := math.Inf(1)
		opts.Duration = &inf
		start(t, s.model(t, id), opts)
		if body := s.call(0).body; !strings.Contains(body, `"duration":null`) && !strings.Contains(body, `"Duration":null`) {
			t.Errorf("%s body = %s", id, body)
		}
	}
}
