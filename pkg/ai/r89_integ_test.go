package ai

import (
	"context"
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createDownload (Transcribe audio and prompt file URLs) sets no timeout;
// Go cut these downloads off after 60 s (the fileutil client timeout). The
// caller's ctx bounds them now (cr-r89-provc-to-gen-integrator).
func TestR89integURLDownloadHasNoTimeout(t *testing.T) {
	rec := &r89provcDeadlineRecorder{}
	restoreTransport, restoreValidator := downloadTransport, downloadURLValidator
	downloadTransport = func() http.RoundTripper { return rec }
	downloadURLValidator = func(string) error { return nil }
	t.Cleanup(func() { downloadTransport, downloadURLValidator = restoreTransport, restoreValidator })

	res, err := CreateURLDownloadWithMetadata(nil)(context.Background(), "https://cdn.example/a.mp3")
	if err != nil || string(res.Data) != "video" {
		t.Fatalf("download = %+v, %v", res, err)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.hadDeadline {
		t.Fatal("URL download carried a client timeout deadline; TS sets none")
	}
}

// TS cosineSimilarity: InvalidArgumentError({value: {vector1Length,
// vector2Length}}); carry r89 moved the lengths out of the message, and the
// error now keeps them as its Value.
func TestR89integCosineSimilarityErrorValue(t *testing.T) {
	_, err := CosineSimilarity([]float64{1}, []float64{1, 2})
	var iae *providererrors.InvalidArgumentError
	if !errors.As(err, &iae) {
		t.Fatalf("err = %T", err)
	}
	v, _ := iae.Value.(map[string]interface{})
	if v["vector1Length"] != 1 || v["vector2Length"] != 2 {
		t.Fatalf("Value = %#v", iae.Value)
	}
}
