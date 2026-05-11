package providerutils

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestResolveProviderReference(t *testing.T) {
	ref := types.ProviderReference{"openai": "file-abc", "anthropic": "file-xyz"}

	got, err := ResolveProviderReference(ref, "anthropic")
	if err != nil {
		t.Fatalf("ResolveProviderReference() err = %v", err)
	}
	if got != "file-xyz" {
		t.Fatalf("ResolveProviderReference() = %q", got)
	}
}

func TestResolveProviderReferenceMissingProvider(t *testing.T) {
	ref := types.ProviderReference{"anthropic": "file-xyz", "google": "file-123"}

	_, err := ResolveProviderReference(ref, "openai")
	if err == nil {
		t.Fatal("expected error")
	}
	var refErr *providererrors.NoSuchProviderReferenceError
	if !errors.As(err, &refErr) {
		t.Fatalf("error type = %T, want NoSuchProviderReferenceError", err)
	}
	if refErr.Provider != "openai" || refErr.Reference["google"] != "file-123" {
		t.Fatalf("unexpected error fields: %#v", refErr)
	}
}
