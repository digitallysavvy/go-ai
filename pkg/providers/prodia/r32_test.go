package prodia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS postToApi lets a transport failure propagate unchanged.
func TestImageTransportFailureIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).ImageModel("inference.flux-fast.schnell.txt2img.v2")
	server.Close()
	_, err := m.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "p"})
	var perr *providererrors.ProviderError
	if err == nil || errors.As(err, &perr) || !strings.HasPrefix(err.Error(), "Cannot connect to API") {
		t.Fatalf("err = %#v", err)
	}
}

// TS takes currentDate before the request is sent, not after the response.
func TestVideoResponseTimestampIsTakenBeforeTheRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "multipart/form-data; boundary=b")
		_, _ = w.Write([]byte("--b\r\nContent-Disposition: form-data; name=\"job\"\r\nContent-Type: application/json\r\n\r\n{\"id\":\"j\"}\r\n--b\r\nContent-Disposition: form-data; name=\"output\"\r\nContent-Type: video/mp4\r\n\r\nVIDEO\r\n--b--\r\n"))
	}))
	defer server.Close()
	m, _ := New(Config{APIKey: "k", BaseURL: server.URL}).VideoModel("inference.wan2-2.lightning.txt2vid.v0")
	res, err := m.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
	if err != nil || time.Since(res.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("timestamp %v, err %v", res.Response.Timestamp, err)
	}
}

// The failed-response body, parsed data and headers are kept on the error.
func TestProviderErrorKeepsResponseBodyAndData(t *testing.T) {
	err := newProdiaProviderError(400, []byte(`{"message":"m"}`), http.Header{"X-Request-Id": {"r"}})
	if err.ResponseBody != `{"message":"m"}` || err.Data == nil || err.ResponseHeaders["x-request-id"] != "r" || err.Message != "m" {
		t.Fatalf("err = %#v", err)
	}
}
