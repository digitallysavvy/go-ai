package bfl

import (
	"errors"
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR90provcVideoWarningsQuoteLikeTS: the TS warnings are template literals
// with `"${value}"` (black-forest-labs-video-model.ts 305-341), so a value
// with a quote or a non-ASCII rune is copied as is, not Go-escaped (%q).
func TestR90provcVideoWarningsQuoteLikeTS(t *testing.T) {
	m := newVideoTestModel(t)
	_, warnings, err := m.buildVideoArgs(&provider.VideoModelV3CallOptions{
		Prompt:      "a cat",
		Resolution:  `wide"é`,
		AspectRatio: `5"7`,
	})
	if err != nil {
		t.Fatalf("buildVideoArgs: %v", err)
	}
	want := map[string]string{
		"resolution":  `Unrecognized resolution "wide"é". FLUX 3 video supports "hd" and "fhd", or a {width}x{height} value to map onto one.`,
		"aspectRatio": `FLUX 3 video does not support the aspect ratio "5"7". Using the provider default (auto).`,
	}
	got := map[string]string{}
	for _, w := range warnings {
		got[w.Feature] = w.Details
	}
	for feature, details := range want {
		if got[feature] != details {
			t.Errorf("%s warning = %q, want %q", feature, got[feature], details)
		}
	}
}

// TestR90provcErrorDataIsSchemaOutput: TS APICallError.data is the zod
// output of bflErrorSchema (message, detail, status, state; undeclared keys
// stripped) for a matching body and undefined otherwise.
func TestR90provcErrorDataIsSchemaOutput(t *testing.T) {
	data := func(body string) interface{} {
		var perr *providererrors.ProviderError
		if !errors.As(bflStatusError(400, nil, []byte(body)), &perr) {
			t.Fatalf("not a ProviderError for %s", body)
		}
		return perr.Data
	}
	if got := data(`{"detail":"bad","status":"Error","id":"x"}`); !reflect.DeepEqual(got, map[string]interface{}{"detail": "bad", "status": "Error"}) {
		t.Errorf("matched data = %#v", got)
	}
	if got := data(`{"message":7}`); got != nil {
		t.Errorf("off-schema data = %#v, want nil", got)
	}
}
