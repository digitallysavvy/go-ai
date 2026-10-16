package alibaba

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

// TS postJsonToApi lets a transport failure propagate unchanged; it is not a
// video generation error.
func TestVideoDoStartCancelledContextIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	model := NewVideoModel(New(Config{APIKey: "k", VideoBaseURL: server.URL}), "wan2.6-t2v")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := model.DoStart(ctx, &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	var vErr *providererrors.VideoGenerationError
	if err == nil || errors.As(err, &vErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %#v", err)
	}
}

// A body that fails the response schema is the final "Invalid JSON response"
// APICallError, not a VideoGenerationError.
func TestVideoDoStatusInvalidJSONIsNotWrapped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_id":5}}`))
	}))
	defer server.Close()
	model := NewVideoModel(New(Config{APIKey: "k", VideoBaseURL: server.URL}), "wan2.6-t2v")
	_, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"taskId":"t"}`)})
	var vErr *providererrors.VideoGenerationError
	var pErr *providererrors.ProviderError
	if err == nil || errors.As(err, &vErr) || !errors.As(err, &pErr) || pErr.Message != "Invalid JSON response" {
		t.Fatalf("err = %#v", err)
	}
}

// The failed-response handler message and the response body are kept on the
// video and embedding errors (APICallError.responseBody).
func TestVideoAndEmbeddingHTTPErrorsCarryBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"code":"Bad","message":"","request_id":"r"}`))
	}))
	defer server.Close()
	prov := New(Config{APIKey: "k", VideoBaseURL: server.URL, EmbeddingBaseURL: server.URL})
	check := func(err error) {
		t.Helper()
		var pErr *providererrors.ProviderError
		if !errors.As(err, &pErr) || pErr.StatusCode != 400 || pErr.Message != "" || pErr.ErrorCode != "Bad" ||
			!strings.Contains(pErr.ResponseBody, "request_id") || pErr.Data == nil {
			t.Fatalf("err = %#v", pErr)
		}
	}
	_, err := NewVideoModel(prov, "wan2.6-t2v").DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	check(err)
	_, err = NewEmbeddingModel(prov, AlibabaEmbeddingTextV4).DoEmbedMany(context.Background(), []string{"a"}, nil)
	check(err)
}

// TS takes currentDate before the request is sent, not after the response.
func TestVideoResponseTimestampIsTakenBeforeTheRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(80 * time.Millisecond)
		_, _ = w.Write([]byte(`{"output":{"task_id":"t","task_status":"PENDING"}}`))
	}))
	defer server.Close()
	model := NewVideoModel(New(Config{APIKey: "k", VideoBaseURL: server.URL}), "wan2.6-t2v")
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "p"}})
	if err != nil || time.Since(start.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("DoStart timestamp %v, err %v", start.Response.Timestamp, err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: []byte(`{"taskId":"t"}`)})
	if err != nil || time.Since(status.Response.Timestamp) < 60*time.Millisecond {
		t.Fatalf("DoStatus timestamp %v, err %v", status.Response.Timestamp, err)
	}
}
