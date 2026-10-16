package klingai

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS takes currentDate before the request is sent, not after the response.
func TestResponseTimestampIsTakenBeforeTheRequest(t *testing.T) {
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"task_id":"t1","task_status":"submitted"}}`))
	})
	defer server.Close()
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil || time.Since(start.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("DoStart timestamp %v, err %v", start.Response.Timestamp, err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err != nil || time.Since(status.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("DoStatus timestamp %v, err %v", status.Response.Timestamp, err)
	}
}

// TS getFromApi lets a transport failure propagate unchanged.
func TestDoStatusTransportFailureIsNotWrapped(t *testing.T) {
	model, server := newKlingAITestModel(t, "kling-v2.6-t2v", func(w http.ResponseWriter, r *http.Request) {})
	server.Close()
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"taskId":"t","endpointPath":"/v1/videos/text2video"}`)})
	if err == nil || strings.Contains(err.Error(), "failed to check status") {
		t.Fatalf("err = %v", err)
	}
}
