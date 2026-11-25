package bytedance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// A cancelled context propagates unchanged (TS lets the abort through).
func TestDoStartCancelledContextIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	model := newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := model.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// zod usage.completion_tokens is nullish: an absent value stays absent.
func TestStatusUsageOmitsAbsentCompletionTokens(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/contents/generations/tasks/t1", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"t1","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"},"usage":{}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	model := newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t1"})
	res, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["bytedance"].(map[string]interface{})
	if b, _ := json.Marshal(meta["usage"]); string(b) != "{}" {
		t.Fatalf("usage = %s", b)
	}
}
