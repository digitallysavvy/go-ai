package parallel

import (
	"context"
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS builds the postJsonToApi arguments in order: headers (loadApiKey)
// before body (toParallelState), so without an API key a file part fails
// with LoadAPIKeyError, not UnsupportedFunctionalityError.
func TestR86toolsParallelAPIKeyBeforeState(t *testing.T) {
	t.Setenv("PARALLEL_API_KEY", "")
	m, err := New(Config{BaseURL: "http://127.0.0.1:1"}).DecisionModel("fast")
	if err != nil {
		t.Fatal(err)
	}
	opts := r85toolsOptions()
	opts.State = []provider.DecisionStatePart{{Type: "file"}}
	_, err = m.DoDecide(context.Background(), opts)
	var loadErr *providererrors.LoadAPIKeyError
	if !errors.As(err, &loadErr) {
		t.Fatalf("err = %v (%T), want LoadAPIKeyError", err, err)
	}
}
