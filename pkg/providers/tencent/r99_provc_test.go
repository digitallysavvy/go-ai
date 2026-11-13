package tencent

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"context"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"testing"
)

// TS drops a caller header when `name.toLowerCase() in protocolHeaders`,
// which also matches the object's inherited "constructor" / "__proto__".
func TestR99provcCloudAPIDropsInheritedKeyHeaders(t *testing.T) {
	srv, calls := cloudServer(t, `{"Response":{"TaskId":"task-1"}}`)
	p := New(Config{VodBaseURL: srv.URL})
	headers := map[string]string{"Constructor": "c", "__proto__": "p", "X-Other": "o"}
	if _, err := p.postTencentCloudAPI(context.Background(), "vod", "CreateAigcVideoTask", "2018-07-17", cloudCreds, cloudBody(), vodCreateTaskResponseSchema, headers, 1794120000); err != nil {
		t.Fatal(err)
	}
	h := calls()[0].headers
	if h.Get("Constructor") != "" || h.Get("__proto__") != "" || h.Get("X-Other") != "o" {
		t.Fatalf("headers = %v", h)
	}
}

// TS reads options.operation.taskId with an exact key.
func TestR99provcStatusOperationKeyExact(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	m, err := NewVideoModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "kling-video-v2-6")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"TaskID":"wrongcase"}`)})
	if path == "" || strings.Contains(path, "wrongcase") {
		t.Fatalf("path = %q", path)
	}
}
