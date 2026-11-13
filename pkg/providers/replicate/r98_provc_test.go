package replicate

import (
	"testing"

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
