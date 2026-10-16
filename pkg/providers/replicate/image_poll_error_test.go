package replicate

import (
	"errors"
	"testing"

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
