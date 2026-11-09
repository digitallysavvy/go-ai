package replicate

import (
	"context"
	"errors"
	"net/http"
	"reflect"
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

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of replicateErrorSchema ({detail?, error?}, undeclared keys
// stripped) for a matching body and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(replicateAPIError(422, nil, []byte(body)), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	if got := data(`{"title":"Invalid","detail":"bad input","status":422}`); !reflect.DeepEqual(got, map[string]interface{}{"detail": "bad input"}) {
		t.Errorf("matched data = %#v", got)
	}
	if got := data(`{"detail":null}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}
