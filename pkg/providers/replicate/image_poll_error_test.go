package replicate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// The image poll path uses the same TS replicateFailedResponseHandler as the
// create request: `detail ?? error`, never the raw body.
func TestImagePollErrorUsesReplicateFailedResponseHandler(t *testing.T) {
	m := &ImageModel{}
	dl := &providererrors.DownloadError{StatusCode: 404, Body: []byte(`{"detail":"Not found"}`)}
	var perr *providererrors.ProviderError
	if err := m.handlePollError(dl); !errors.As(err, &perr) || perr.Message != "Not found" || perr.StatusCode != 404 {
		t.Fatalf("got %v", err)
	}
	dl = &providererrors.DownloadError{StatusCode: 502, Body: []byte(`<html>`)}
	if err := m.handlePollError(dl); !errors.As(err, &perr) || perr.Message != "Bad Gateway" {
		t.Fatalf("got %v", err)
	}
}

// TS `!prediction.output`: an empty output string is also "no video URL".
func TestReplicateVideoEmptyOutputIsNoVideo(t *testing.T) {
	p := newReplicateProviderWithTransport(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			return replicateJSONResponse(200, `{"id":"p","status":"starting","urls":{"get":"https://replicate.example/predictions/p"}}`), nil
		}
		return replicateJSONResponse(200, `{"id":"p","status":"succeeded","output":"","urls":{"get":"https://replicate.example/predictions/p"}}`), nil
	})
	vm := NewVideoModel(p, "vid-ver:v")
	start, err := vm.DoStart(context.Background(), &provider.VideoModelV3StartOptions{VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = vm.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: start.Operation})
	if err == nil || !strings.Contains(err.Error(), "No video URL in response") {
		t.Fatalf("err = %v", err)
	}
}
