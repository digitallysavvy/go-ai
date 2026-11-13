package cerebras

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provc-05 (TS cerebras-provider.ts:183-189): embedding and image models
// throw NoSuchModelError, and there are no speech / transcription factories.
func TestR91toolsCerebrasUnsupportedModelTypes(t *testing.T) {
	p := New(Config{APIKey: "k"})
	checks := map[string]func() error{
		"embedding":     func() error { _, err := p.EmbeddingModel("x"); return err },
		"image":         func() error { _, err := p.ImageModel("x"); return err },
		"speech":        func() error { _, err := p.SpeechModel("x"); return err },
		"transcription": func() error { _, err := p.TranscriptionModel("x"); return err },
	}
	for name, check := range checks {
		var nsm *providererrors.NoSuchModelError
		if err := check(); !errors.As(err, &nsm) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
