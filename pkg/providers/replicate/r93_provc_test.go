package replicate

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// r93provcHangUp closes the connection without a response (TS fetch rejects
// with "fetch failed", cause UND_ERR_SOCKET).
func r93provcHangUp(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func r93provcIsConnectError(t *testing.T, name string, err error) {
	t.Helper()
	r, ok := err.(interface{ IsRetryable() bool })
	if err == nil || !strings.HasPrefix(err.Error(), "Cannot connect to API: ") || !ok || !r.IsRetryable() {
		t.Errorf("%s: %T %v, want the retryable Cannot connect to API error", name, err, err)
	}
}

// TestR93provcPollNetworkFailureIsConnectError: TS polls and downloads with
// getFromApi, whose handleFetchError turns a connection that failed before
// any response into the retryable "Cannot connect to API" APICallError. Go
// returned the DownloadError fileutil wraps it in.
func TestR93provcPollNetworkFailureIsConnectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { r93provcHangUp(w) }))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	op, _ := json.Marshal(replicateVideoOperation{GetURL: srv.URL + "/predictions/p1"})
	_, err := NewVideoModel(p, "minimax/video-01").DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	r93provcIsConnectError(t, "video poll", err)

	im := NewImageModel(p, "black-forest-labs/flux-schnell")
	_, _, err = im.downloadImage(context.Background(), srv.URL+"/out.png")
	r93provcIsConnectError(t, "image download", err)
	_, pollErr := fileutil.PollJSON(context.Background(), srv.URL+"/predictions/p1", fileutil.TrustedOriginDownloadOptions(srv.URL, nil), nil)
	r93provcIsConnectError(t, "image poll", im.handlePollError(context.Background(), pollErr))
}

// TestR93provcImageDownloadStatusIsAPIError: TS downloads the output images
// with getFromApi and replicateFailedResponseHandler, so a non-2xx download
// is the Replicate APICallError (detail message), not a DownloadError.
func TestR93provcImageDownloadStatusIsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"detail":"expired link"}`))
	}))
	defer srv.Close()
	im := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "black-forest-labs/flux-schnell")
	_, _, err := im.downloadImage(context.Background(), srv.URL+"/out.png")
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "expired link" || perr.StatusCode != http.StatusForbidden {
		t.Errorf("err = %T %v, want the Replicate API error", err, err)
	}
}

// TestR93provcModelVersionSplit: TS `const [modelId, version] =
// this.modelId.split(':')` keeps only the second part as the version.
func TestR93provcModelVersionSplit(t *testing.T) {
	cases := map[string][3]string{
		"owner/model":          {"owner/model", "", "false"},
		"owner/model:v1":       {"owner/model", "v1", "true"},
		"owner/model:v1:extra": {"owner/model", "v1", "true"},
		"owner/model:":         {"owner/model", "", "true"},
	}
	for in, want := range cases {
		m, v, ok := splitReplicateModelID(in)
		if m != want[0] || v != want[1] || (ok && want[2] != "true") || (!ok && want[2] != "false") {
			t.Errorf("splitReplicateModelID(%q) = %q, %q, %v", in, m, v, ok)
		}
	}
}

// TestR93provcVideoNullOptionsAreAbsent: TS parseProviderOptions returns
// undefined for a null providerOptions.replicate; Go failed with "expected
// object".
func TestR93provcVideoNullOptionsAreAbsent(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "owner/model")
	input, err := m.buildInput(&provider.VideoModelV3CallOptions{Prompt: "x", ProviderOptions: map[string]interface{}{"replicate": nil}})
	if err != nil || len(input) != 1 || input["prompt"] != "x" {
		t.Errorf("input = %v, err = %v", input, err)
	}
}
