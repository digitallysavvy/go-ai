package replicate

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcImageDownloadValidationErrorUnchanged: TS downloads each output
// with getFromApi({validateUrl: true}), which throws validateDownloadUrl's
// DownloadError as is (replicate-image-model.ts 254-266). Go wrapped every
// non-HTTP download failure in "failed to download image: ...".
func TestR90provcImageDownloadValidationErrorUnchanged(t *testing.T) {
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return replicateJSONResponse(201, `{"id":"p","status":"succeeded","output":"http://127.0.0.1/out.png","urls":{"get":"https://replicate.example/predictions/p"}}`), nil
		}
		return nil, errors.New("unexpected " + r.URL.String())
	})
	_, err := NewImageModel(p, "black-forest-labs/flux-schnell").DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) {
		t.Fatalf("err = %v, want a DownloadError", err)
	}
	if want := "URL with IP address 127.0.0.1 is not allowed"; err.Error() != want {
		t.Fatalf("err = %q, want %q", err.Error(), want)
	}
}
