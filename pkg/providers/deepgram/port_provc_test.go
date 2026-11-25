package deepgram

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS c2ef7138f8 catalog refactor (ruling 13): the voice-family and streaming
// predicates moved into the catalog; unknown IDs must behave as before.
func TestPortProvcVoiceFamilyPredicateOnUnknownIDs(t *testing.T) {
	for id, want := range map[string]bool{
		"aura": true, "aura-2": true,
		"aura-2-thalia-en": false, "aura-3": false, "future-voice": false, "": false,
	} {
		if got := voiceFamilyIDs[id]; got != want {
			t.Errorf("voice family %q = %v, want %v", id, got, want)
		}
	}
}

func TestPortProvcStreamingPredicateOnUnknownIDs(t *testing.T) {
	// Streaming is every model except the flux* prefix; an unknown ID gets
	// past the flux check and fails later on the missing audio format.
	for id, flux := range map[string]bool{"flux-general-en": true, "flux": true, "future-model": false, "nova-3": false, "my-flux": false} {
		m := NewTranscriptionModel(New(Config{APIKey: "k"}), id)
		_, err := m.DoStream(context.Background(), &provider.TranscriptionStreamOptions{})
		var unsupported *providererrors.UnsupportedFunctionalityError
		if got := errors.As(err, &unsupported); got != flux {
			t.Errorf("DoStream(%q) unsupported = %v, want %v (err %v)", id, got, flux, err)
		}
	}
}
