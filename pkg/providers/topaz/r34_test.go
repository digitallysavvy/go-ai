package topaz

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS throws InvalidArgumentError for a missing input video; the v0.5.2
// ValidationError stays reachable through errors.As.
func TestR34_MissingInputVideoIsInvalidArgumentError(t *testing.T) {
	m := NewVideoModel(New(Config{APIKey: "k"}), "starlight-precise-2")
	_, err := m.DoStart(context.Background(), &provider.VideoModelV3StartOptions{})
	var ia *providererrors.InvalidArgumentError
	if !errors.As(err, &ia) || ia.Field != "inputReferences" {
		t.Fatalf("err = %T %v, want InvalidArgumentError for inputReferences", err, err)
	}
	var ve *providererrors.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("v0.5.2 ValidationError no longer reachable: %v", err)
	}
	want := "Invalid argument for parameter inputReferences: Topaz video models require an input video."
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("message = %q, want prefix %q", got, want)
	}
}
