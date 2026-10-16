package fal

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestImageStatusErrorKeepsEmptyMessage(t *testing.T) {
	err := (&ImageModel{}).statusError(400, nil, []byte(`{"message":""}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("got %#v", perr)
	}
}
