package minimax

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r87eintegMetaStream struct {
	*testutil.MockTextStream
}

func (r87eintegMetaStream) ResponseHeaders() map[string]string {
	return map[string]string{"X-A": "1"}
}
func (r87eintegMetaStream) RequestURL() string { return "https://h/v1" }

// The stream wrapper forwards the response headers and request URL of the
// stream it wraps (TS doStream response.headers / request.url, round 87e).
func TestR87eintegStreamWrapperForwardsMetadata(t *testing.T) {
	var s provider.TextStream = &warningsAppendStream{inner: r87eintegMetaStream{testutil.NewMockTextStream(nil)}}
	if rh, ok := s.(provider.StreamResponseHeaders); !ok || rh.ResponseHeaders()["X-A"] != "1" {
		t.Error("ResponseHeaders not forwarded")
	}
	if ru, ok := s.(provider.StreamRequestURL); !ok || ru.RequestURL() != "https://h/v1" {
		t.Error("RequestURL not forwarded")
	}
}
