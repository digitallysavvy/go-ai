package bytedance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func slowServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// TS takes currentDate before the request is sent, not after the response.
func TestResponseTimestampIsTakenBeforeTheRequest(t *testing.T) {
	late := func(ts time.Time) bool { return time.Since(ts) >= 60*time.Millisecond }

	server := slowServer(t, `{"id":"t1"}`)
	video := newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	start, err := video.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil || !late(start.Response.Timestamp) {
		t.Fatalf("DoStart timestamp %v, err %v", start.Response.Timestamp, err)
	}

	server = slowServer(t, `{"id":"t1","status":"running"}`)
	video = newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t1"})
	status, err := video.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || !late(status.Response.Timestamp) {
		t.Fatalf("DoStatus timestamp %v, err %v", status.Response.Timestamp, err)
	}

	server = slowServer(t, `{"data":[{"b64_json":"AA=="}]}`)
	image := newImageModel(providerForServer(t, server.URL), "seedream-4-0-250828")
	res, err := image.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	if err != nil || !late(res.Response.Timestamp) {
		t.Fatalf("image timestamp %v, err %v", res.Response.Timestamp, err)
	}
}

// TS getFromApi lets a transport failure propagate unchanged.
func TestDoStatusTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	video := newVideoModel(providerForServer(t, server.URL), "seedance-1-0-pro-250528")
	server.Close()
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t1"})
	_, err := video.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err == nil || strings.Contains(err.Error(), "failed to check status") {
		t.Fatalf("err = %v", err)
	}
}
