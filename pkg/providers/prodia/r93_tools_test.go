package prodia

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// prova CR r93 class: TS rethrows the apiKey callback error as is; the
// img2vid multipart path returned it wrapped in the internal FetchRejection
// marker.
func TestR93toolsProdiaImg2VidAPIKeyFuncErrorAsIs(t *testing.T) {
	sentinel := errors.New("no key")
	p := New(Config{BaseURL: "http://127.0.0.1:1", APIKeyFunc: func(context.Context) (string, error) { return "", sentinel }})
	vm, _ := p.VideoModel("inference.wan2-2.lightning.img2vid.v0")
	_, err := vm.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "p", Image: &provider.VideoModelV3File{Type: "file", Data: []byte("img"), MediaType: "image/png"},
	})
	if err != sentinel {
		t.Fatalf("err = %T %v, want the key error itself", err, err)
	}
}
