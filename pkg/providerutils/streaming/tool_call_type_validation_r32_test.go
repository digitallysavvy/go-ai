package streaming

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

func TestToolCallTrackerTypeValidation(t *testing.T) {
	idx := 0
	name := WireName{Value: "weather", Present: true}
	cases := []struct {
		mode    TypeValidation
		typ     string
		wantErr bool
	}{
		{TypeValidationNone, "other", false},
		{TypeValidationIfPresent, "", false},
		{TypeValidationIfPresent, "function", false},
		{TypeValidationIfPresent, "other", true},
		{TypeValidationRequired, "function", false},
		{TypeValidationRequired, "", true},
		{TypeValidationRequired, "other", true},
	}
	for _, c := range cases {
		tr := NewStreamingToolCallTracker()
		tr.SetTypeValidation(c.mode)
		chunks := tr.TrackWireWithType(&idx, "call_1", name, c.typ, "{}")
		var gotErr error
		for _, ch := range chunks {
			if ch.Type == provider.ChunkTypeError {
				gotErr = ch.Err
			}
		}
		if (gotErr != nil) != c.wantErr {
			t.Errorf("mode %d type %q: err = %v, want error %v", c.mode, c.typ, gotErr, c.wantErr)
		}
		if c.wantErr {
			var invalid *providererrors.InvalidResponseDataError
			if !errors.As(gotErr, &invalid) {
				t.Errorf("mode %d type %q: error %T is not InvalidResponseDataError", c.mode, c.typ, gotErr)
			}
			if len(tr.Flush()) != 0 {
				t.Errorf("mode %d type %q: a rejected delta must not create a call", c.mode, c.typ)
			}
		}
	}
}
