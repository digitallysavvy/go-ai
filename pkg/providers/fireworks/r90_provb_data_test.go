package fireworks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provbErrorDataIsSchemaOutput: TS createJsonErrorResponseHandler
// sets APICallError.data to the error schema output (undeclared keys
// stripped) for a matching body and leaves it undefined otherwise (the
// provc class of 252b3d76).
func TestR90provbErrorDataIsSchemaOutput(t *testing.T) {
	check := func(name string, err error, want interface{}) {
		t.Helper()
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if !reflect.DeepEqual(pe.Data, want) {
			t.Fatalf("%s: data = %#v, want %#v", name, pe.Data, want)
		}
	}
	httpErr := func(body string) error {
		return &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(body)}
	}
	_ = httpErr
	check("object", parseFireworksProviderError(httpErr(`{"error":{"message":"m","object":"error","x":1}}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "object": "error"}})
	check("string", parseFireworksProviderError(httpErr(`{"error":"s","x":1}`)), map[string]interface{}{"error": "s"})
	check("off schema", (&LanguageModel{}).handleError(httpErr(`{"detail":"d"}`)), nil)
}

// TestR90provbImageDownloadErrorUnchanged: a result URL that fails
// validation propagates unchanged (TS getFromApi), without a wrap.
func TestR90provbImageDownloadErrorUnchanged(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), "accounts/fireworks/models/flux-kontext-pro")
	_, err := m.downloadImage(context.Background(), "http://169.254.169.254/x.png")
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || strings.Contains(err.Error(), "failed to download") {
		t.Fatalf("err = %T %v", err, err)
	}
}
