package xai

import (
	"errors"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// prova CR (TS provider-utils load-setting.ts): a missing project is a typed
// LoadSettingError with the TS text.
func TestR89provgVertexXaiLoadSettingError(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "")
	p, err := New(Config{})
	if err == nil {
		_, _, err = p.resolve()
	}
	var lse *providererrors.LoadSettingError
	if !errors.As(err, &lse) || err.Error() != "Google Vertex project setting is missing. Pass it using the 'project' parameter or the GOOGLE_VERTEX_PROJECT environment variable." {
		t.Fatalf("error = %T %v", err, err)
	}
}
