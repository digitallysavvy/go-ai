package xai

import (
	"context"
	"errors"
	"reflect"
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
	check("api", newXAIProviderError("xai", 400, nil, []byte(`{"error":{"message":"m","code":2,"x":1},"y":2}`)), map[string]interface{}{"error": map[string]interface{}{"message": "m", "code": float64(2)}})
	check("responses", newXAIProviderError("xai", 400, nil, []byte(`{"code":"c","error":"e","x":1}`)), map[string]interface{}{"code": "c", "error": "e"})
	check("off schema", newXAIProviderError("xai", 400, nil, []byte(`{"detail":"d"}`)), nil)
}

// TestR90provbImageDownloadErrorUnchanged: a generated-image URL that fails
// validation propagates unchanged (TS getFromApi), not wrapped as a
// provider error (the provc class of f4673dbc).
func TestR90provbImageDownloadErrorUnchanged(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}), "grok-imagine-image")
	_, _, err := m.responseImages(context.Background(), []xaiImageData{{URL: "http://169.254.169.254/x.png"}})
	var dl *providererrors.DownloadError
	var pe *providererrors.ProviderError
	if !errors.As(err, &dl) || errors.As(err, &pe) {
		t.Fatalf("err = %T %v", err, err)
	}
}

// TestR90provbVideoOptionsErrorText: TS parseProviderOptions throws
// InvalidArgumentError with the message "invalid xai provider options" (the
// details stay in the cause). CR from prova (cr-r90-prova-to-provb.md).
func TestR90provbVideoOptionsErrorText(t *testing.T) {
	_, _, err := extractVideoProviderOptions(map[string]interface{}{"xai": map[string]interface{}{"mode": "bogus"}})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Message != "invalid xai provider options" || errors.Unwrap(err) == nil {
		t.Fatalf("err = %#v", err)
	}
}
