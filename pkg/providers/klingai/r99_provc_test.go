package klingai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS reads options.operation.taskId / endpointPath with exact keys.
func TestR99provcStatusOperationKeyExact(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok"}`))
	}))
	defer srv.Close()
	p, err := New(Config{AccessKey: "a", SecretKey: "s", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := newVideoModel(p, "kling-v2.6-t2v")
	_, _ = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"TaskID":"wrongcase","EndpointPath":"/v1/videos/text2video"}`)})
	if strings.Contains(path, "wrongcase") || strings.Contains(path, "text2video") {
		t.Fatalf("path = %q", path)
	}
}
