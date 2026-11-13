package fileutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

func r99provcUAServer(t *testing.T, status int) (*httptest.Server, *string) {
	t.Helper()
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &ua
}

// TS getFromApi appends only its own suffix (Go: the runtime tag) to the
// caller's user agent; download()'s `ai-sdk/<version>` tag is not sent.
func TestR99provcTrustedOriginDownloadUserAgent(t *testing.T) {
	srv, ua := r99provcUAServer(t, http.StatusOK)
	opts := TrustedOriginDownloadOptions(srv.URL, srv.Client().Transport)
	opts.Headers = map[string]string{"User-Agent": "ai-sdk-fal/9"}
	if _, err := DownloadWithMetadata(context.Background(), srv.URL+"/a", opts); err != nil {
		t.Fatal(err)
	}
	if want := "ai-sdk-fal/9 " + providerutils.RuntimeEnvironmentUserAgent(); *ua != want {
		t.Errorf("User-Agent = %q, want %q", *ua, want)
	}
	opts.Headers = nil
	if _, err := DownloadWithMetadata(context.Background(), srv.URL+"/a", opts); err != nil {
		t.Fatal(err)
	}
	if want := providerutils.RuntimeEnvironmentUserAgent(); *ua != want {
		t.Errorf("no caller agent: User-Agent = %q, want %q", *ua, want)
	}
}

// TS downloadBlob sends no headers: no SDK tag.
func TestR99provcNoTimeoutDownloadOptionsNoSDKTag(t *testing.T) {
	srv, ua := r99provcUAServer(t, http.StatusOK)
	opts := NoTimeoutDownloadOptions()
	opts.URLValidator = nil
	opts.Transport = srv.Client().Transport
	if _, err := DownloadWithMetadata(context.Background(), srv.URL+"/a", opts); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(*ua, "ai-sdk") || strings.Contains(*ua, providerutils.RuntimeEnvironmentUserAgent()) {
		t.Errorf("User-Agent = %q, want no SDK tag", *ua)
	}
}

// cr-r99-tools-to-provc: the result carries the status, and a 204 / 205 is
// TS createBinaryResponseHandler's "Response body is empty".
func TestR99provcDownloadStatusCodeAndEmptyBody(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent, http.StatusResetContent} {
		srv, _ := r99provcUAServer(t, status)
		opts := TrustedOriginDownloadOptions(srv.URL, srv.Client().Transport)
		res, err := DownloadWithMetadata(context.Background(), srv.URL+"/a", opts)
		if err != nil || res.StatusCode != status {
			t.Fatalf("%d: res %+v err %v", status, res, err)
		}
		err = EmptyBinaryBodyError("p", res.StatusCode, http.Header(res.Headers))
		if status == http.StatusOK {
			if err != nil {
				t.Errorf("200: %v", err)
			}
			continue
		}
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != status || pe.ResponseBody != "" {
			t.Errorf("%d: %#v", status, err)
		}
	}
}
