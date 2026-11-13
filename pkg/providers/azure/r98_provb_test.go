package azure

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// TestR98provbMaiJSNumberPlatformInt: a metadata token count is the JS
// number; an integral count outside the platform int (32-bit targets) stays
// a float64 instead of overflowing the int conversion.
func TestR98provbMaiJSNumberPlatformInt(t *testing.T) {
	for in, want := range map[float64]string{3e9: "3000000000", 12: "12", 10.5: "10.5"} {
		b, _ := jsonstr.Marshal(azureMaiJSNumber(in))
		if string(b) != want {
			t.Errorf("%v: %s, want %s", in, b, want)
		}
	}
}
