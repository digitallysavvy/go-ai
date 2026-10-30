package registry

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// An unknown provider is a NoSuchModelError (TS NoSuchProviderError extends
// NoSuchModelError), so errors.Is with ErrModelNotFound matches it as it
// matches any NoSuchModelError; the capability errors are plain errors.
func TestR68IntegratorUnknownProviderIsModelNotFound(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.RegisterProvider("known", &testutil.MockProvider{})

	_, err := r.ResolveLanguageModel("missing:model")
	if !errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("errors.Is(%v, ErrModelNotFound) = false, want true", err)
	}
	var nsp *NoSuchProviderError
	if !errors.As(err, &nsp) || nsp.ProviderID != "missing" {
		t.Fatalf("errors.As(*NoSuchProviderError) = %v (%+v), want the v0.5 target", err, nsp)
	}

	_, err = r.Files("known")
	if err == nil {
		t.Fatal("Files(known) without a files API: want an error")
	}
	if errors.Is(err, providererrors.ErrModelNotFound) {
		t.Fatalf("capability error %v matched ErrModelNotFound", err)
	}
	var nsm *providererrors.NoSuchModelError
	if errors.As(err, &nsm) {
		t.Fatalf("capability error %v matched *NoSuchModelError", err)
	}
}
