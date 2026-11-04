package minimax

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS postJsonToApi / getFromApi responseHeaders join a header's repeated
// lines with ", "; Go kept only the first value.
func TestR86btoolsResponseHeadersJoinRepeatedLines(t *testing.T) {
	server, _, _ := newMinimaxTestServer(t, minimaxTestTaskID, []string{minimaxSucceededResponse()})
	defer server.Close()
	inner := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["X-Rep"] = []string{"a", "b"}
		inner.ServeHTTP(w, r)
	})
	model := newVideoModel(providerForMinimaxServer(server.URL), ModelH3)
	start, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: *minimaxDefaultCallOptions(map[string]interface{}{})})
	if err != nil {
		t.Fatal(err)
	}
	if got := start.Response.Headers["X-Rep"]; got != "a, b" {
		t.Errorf("start Response.Headers[X-Rep] = %q, want %q", got, "a, b")
	}
}
