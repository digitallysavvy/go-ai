package streaming

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r87eintegHeaderStream struct {
	*testutil.MockTextStream
}

func (r87eintegHeaderStream) ResponseHeaders() map[string]string {
	return map[string]string{"X-A": "1"}
}

// The provider stream wrappers forward provider.StreamResponseHeaders (TS
// doStream response.headers, round 87e).
func TestR87eintegStreamWrappersForwardResponseHeaders(t *testing.T) {
	inner := r87eintegHeaderStream{testutil.NewMockTextStream(nil)}
	for name, s := range map[string]provider.TextStream{
		"WarningsStream":         NewWarningsStream(inner, nil),
		"JSONResponseToolStream": NewJSONResponseToolStream(inner, "json"),
	} {
		rh, ok := s.(provider.StreamResponseHeaders)
		if !ok || rh.ResponseHeaders()["X-A"] != "1" {
			t.Errorf("%s: ResponseHeaders not forwarded", name)
		}
	}
}
