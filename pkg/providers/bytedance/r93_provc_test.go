package bytedance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR93provcPollNetworkFailureIsConnectError: TS doStatus polls with
// getFromApi, whose handleFetchError turns a connection that failed before
// any response into the retryable "Cannot connect to API" APICallError. Go
// returned the DownloadError fileutil wraps it in.
func TestR93provcPollNetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			_ = conn.Close()
		}
	}))
	defer srv.Close()
	prov, err := New(Config{APIKey: "k", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	vm, err := prov.VideoModel("seedance-1-0-pro-250528")
	if err != nil {
		t.Fatal(err)
	}
	m := vm.(*VideoModel)
	op, _ := json.Marshal(bytedanceOperation{TaskID: "t1"})
	_, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("err = %T %v, want the retryable Cannot connect to API error", err, err)
	}
}
