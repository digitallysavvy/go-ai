package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	gatewayerrors "github.com/digitallysavvy/go-ai/pkg/providers/gateway/errors"
)

func generateWithSSE(t *testing.T, sse string) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, sse)
	}))
	defer server.Close()
	p, err := New(Config{APIKey: "k", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewVideoModel(p, "google/veo-3.1").DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{Prompt: "p"})
	if err == nil {
		t.Fatal("expected an error")
	}
	return err
}

// TS asGatewayError turns the SSE error event's data.error into the typed
// gateway error for its errorType.
func TestVideoSSEErrorEventIsTypedGatewayError(t *testing.T) {
	err := generateWithSSE(t, `data: {"type":"error","message":"slow down","errorType":"rate_limit_exceeded","statusCode":429,"param":null}`+"\n\n")
	var rl *gatewayerrors.GatewayRateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %T %v", err, err)
	}
	err = generateWithSSE(t, `data: {"type":"error","message":"nope","errorType":"authentication_error","statusCode":401,"param":null}`+"\n\n")
	var auth *gatewayerrors.GatewayAuthenticationError
	if !errors.As(err, &auth) {
		t.Fatalf("err = %T %v", err, err)
	}
}

// An event that fails gatewayVideoEventSchema (here: no statusCode) is an
// APICallError "Failed to parse video SSE event", which asGatewayError reports
// as a GatewayResponseError carrying the stream's 200 status.
func TestVideoSSEMalformedEventIsGatewayResponseError(t *testing.T) {
	for name, sse := range map[string]string{
		"error without statusCode": `data: {"type":"error","message":"m","errorType":"x"}` + "\n\n",
		"result without videos":    `data: {"type":"result"}` + "\n\n",
		"no data event":            ": keepalive\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			err := generateWithSSE(t, sse)
			var re *gatewayerrors.GatewayResponseError
			if !errors.As(err, &re) {
				t.Fatalf("err = %T %v", err, err)
			}
			if !strings.Contains(re.Error(), "Invalid error response format: Gateway request failed") || re.GetStatusCode() != 200 {
				t.Fatalf("err = %v status %d", re, re.GetStatusCode())
			}
		})
	}
}
