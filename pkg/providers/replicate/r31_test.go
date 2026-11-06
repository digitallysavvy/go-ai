package replicate

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestReplicateAPIErrorKeepsEmptyDetail(t *testing.T) {
	err := replicateAPIError(400, nil, []byte(`{"detail":""}`))
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "" || perr.StatusCode != 400 {
		t.Fatalf("got %#v", perr)
	}
}
