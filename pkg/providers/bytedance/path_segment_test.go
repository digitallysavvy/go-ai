package bytedance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #23679: the provider-returned task ID is one encoded path segment.
func TestDoStatusEncodesTaskIDAsSinglePathSegment(t *testing.T) {
	const rawID = "abc/../../internal"
	var gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		_, _ = w.Write([]byte(`{"id":"x","status":"succeeded","content":{"video_url":"https://cdn.example.com/v.mp4"},"usage":{}}`))
	}))
	defer server.Close()
	model := newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	op, _ := json.Marshal(bytedanceOperation{TaskID: rawID})
	if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op}); err != nil {
		t.Fatal(err)
	}
	want := "/api/v3/contents/generations/tasks/" + providerutils.EncodePathSegment(rawID)
	if gotURI != want {
		t.Fatalf("request URI = %q, want %q", gotURI, want)
	}
}
