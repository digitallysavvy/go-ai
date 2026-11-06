package streaming

import (
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

type r87integURLStream struct {
	*testutil.MockTextStream
}

func (r87integURLStream) RequestBody() interface{} { return "body" }
func (r87integURLStream) RequestURL() string       { return "https://h/v1/chat/completions" }

// The provider stream wrappers forward provider.StreamRequestURL like
// provider.StreamRequestBody (TS doStream request.url, 934100cad4).
func TestR87integStreamWrappersForwardRequestURL(t *testing.T) {
	inner := r87integURLStream{testutil.NewMockTextStream(nil)}
	for name, s := range map[string]provider.TextStream{
		"WarningsStream":         NewWarningsStream(inner, nil),
		"JSONResponseToolStream": NewJSONResponseToolStream(inner, "json"),
		"WithResponseMetadata":   providerutils.WithResponseMetadata(inner, http.Header{}, "m"),
	} {
		ru, ok := s.(provider.StreamRequestURL)
		if !ok || ru.RequestURL() != "https://h/v1/chat/completions" {
			t.Errorf("%s: RequestURL not forwarded", name)
		}
		if rb, ok := s.(provider.StreamRequestBody); !ok || rb.RequestBody() != "body" {
			t.Errorf("%s: RequestBody not forwarded", name)
		}
	}
	plain := NewWarningsStream(testutil.NewMockTextStream(nil), nil)
	if plain.RequestURL() != "" {
		t.Error("a stream without the capability reports a URL")
	}
}
