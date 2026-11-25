package replicate

import (
	"context"
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS createJsonResponseHandler: JSON.parse reads 1e999 as Infinity and zod
// strips the undeclared key (ruling 4).
func TestR98provcReplicatePredictionOutOfRangeUndeclared(t *testing.T) {
	body := []byte(`{"id":"p1","status":"succeeded","output":"https://x/a.mp4","urls":{"get":"https://x/p1"},"metrics":{"predict_time":1,"total_time":1e999}}`)
	if err := validateVideoPrediction(200, nil, body); err != nil {
		t.Fatalf("video: want valid, got %v", err)
	}
	img := []byte(`{"id":"p1","status":"succeeded","output":["https://x/a.png"],"urls":{"get":"https://x/p1"},"extra":-1e999}`)
	if err := validateImagePrediction(200, nil, img); err != nil {
		t.Fatalf("image: want valid, got %v", err)
	}
	// A declared z.number() holding 1e999 is Infinity, which zod rejects.
	inf := []byte(`{"id":"p1","status":"succeeded","urls":{"get":"https://x/p1"},"metrics":{"predict_time":1e999}}`)
	if err := validateVideoPrediction(200, nil, inf); !providerutils.IsInvalidJSONResponse(err) {
		t.Fatalf("declared Infinity: want Invalid JSON response, got %v", err)
	}
}

// TS createJsonErrorResponseHandler: a failed poll body that does not match
// the error schema gives response.statusText, the server's reason phrase.
func TestR98provcReplicatePollReasonPhrase(t *testing.T) {
	for name, err := range map[string]error{
		"image": NewImageModel(New(Config{APIKey: "k"}), "m").handlePollError(context.Background(), &providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`[1]`)}),
		"video": NewVideoModel(New(Config{APIKey: "k"}), "m").handlePollError(context.Background(), &providererrors.DownloadError{URL: "https://x/p", StatusCode: 418, StatusText: "Custom Phrase", Body: []byte(`[1]`)}),
	} {
		var pe *providererrors.ProviderError
		if !errors.As(err, &pe) || pe.Message != "Custom Phrase" {
			t.Errorf("%s: got %T %v", name, err, err)
		}
	}
}

// TS downloads every output entry: an empty URL fails getFromApi URL
// validation with DownloadError "Invalid URL: " (Go skipped it).
func TestR98provcReplicateEmptyOutputURL(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "m")
	_, err := m.convertResponse(context.Background(), replicateImagePrediction{Output: []interface{}{""}})
	var dl *providererrors.DownloadError
	if !errors.As(err, &dl) || dl.Message != "Invalid URL: " {
		t.Fatalf("got %T %v", err, err)
	}
}
