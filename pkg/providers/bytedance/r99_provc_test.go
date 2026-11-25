package bytedance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS reads options.operation.taskId with an exact key: a "TaskID" spelling
// is not the task ID (cr-r99-provg-to-provb-provc).
func TestR99provcStatusOperationKeyExact(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"running"}`))
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = newVideoModel(p, "m").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"TaskID":"wrongcase"}`)})
	if strings.Contains(path, "wrongcase") {
		t.Fatalf("path = %q", path)
	}
}
