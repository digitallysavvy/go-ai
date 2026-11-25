package luma

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// The image download is getFromApi with no headers: the runtime tag only;
// a 204 has a null body (createBinaryResponseHandler).
func TestR99provcLumaDownloadUserAgentAndNoContent(t *testing.T) {
	var ua string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		w.WriteHeader(status)
	}))
	defer srv.Close()
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: srv.URL}), "photon-1")
	if _, _, err := m.downloadImage(context.Background(), srv.URL+"/i.png"); err != nil {
		t.Fatal(err)
	}
	if ua != providerutils.RuntimeEnvironmentUserAgent() {
		t.Errorf("User-Agent = %q", ua)
	}
	status = http.StatusNoContent
	_, _, err := m.downloadImage(context.Background(), srv.URL+"/i.png")
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.Message != "Response body is empty" || pe.StatusCode != 204 {
		t.Fatalf("err = %#v", err)
	}
}
