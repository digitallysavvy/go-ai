package baseten

import (
	"errors"
	"testing"

	providerrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR91provcUnsupportedModelTypes (provc-05): TS createBaseten throws
// NoSuchModelError for imageModel and has no speech or transcription
// factory, so the inherited OpenAI models are not exposed.
func TestR91provcUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "k"})
	var nsm *providerrors.NoSuchModelError
	if m, err := p.ImageModel("x"); m != nil || !errors.As(err, &nsm) || nsm.ModelType != "imageModel" {
		t.Errorf("ImageModel = %v, %v", m, err)
	}
	if m, err := p.SpeechModel("x"); m != nil || !errors.As(err, &nsm) || nsm.ModelType != "speechModel" {
		t.Errorf("SpeechModel = %v, %v", m, err)
	}
	if m, err := p.TranscriptionModel("x"); m != nil || !errors.As(err, &nsm) || nsm.ModelType != "transcriptionModel" {
		t.Errorf("TranscriptionModel = %v, %v", m, err)
	}
}
