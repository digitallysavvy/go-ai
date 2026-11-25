package klingai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS #23679: the provider-returned task ID is one encoded path segment.
func TestDoStatusEncodesTaskIDAsSinglePathSegment(t *testing.T) {
	const rawID = "abc/../../internal"
	var gotURI string
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"x","task_status":"processing"}}`))
	})
	defer server.Close()
	op, _ := json.Marshal(klingaiOperation{TaskID: rawID, EndpointPath: "/v1/videos/text2video"})
	if _, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op}); err != nil {
		t.Fatal(err)
	}
	want := "/v1/videos/text2video/" + providerutils.EncodePathSegment(rawID)
	if gotURI != want {
		t.Fatalf("request URI = %q, want %q", gotURI, want)
	}
}
