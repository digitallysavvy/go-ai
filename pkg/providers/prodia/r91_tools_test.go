package prodia

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// provc-03 (TS fal-video-model.ts:357 class): a whole number above 2^31-1
// keeps its value: int where int is 64 bits, int64 where it is 32 bits.
func TestR91toolsProdiaNumberDoesNotOverflow(t *testing.T) {
	v := prodiaNumber(3851223452)
	if fmt.Sprint(v) != "3851223452" {
		t.Fatalf("prodiaNumber = %v", v)
	}
	switch v.(type) {
	case int:
		if strconv.IntSize != 64 {
			t.Fatalf("int on a %d-bit build", strconv.IntSize)
		}
	case int64:
		if strconv.IntSize != 32 {
			t.Fatalf("int64 on a 64-bit build")
		}
	default:
		t.Fatalf("type %T", v)
	}
	if prodiaNumber(1024) != 1024 || prodiaNumber(1.5) != 1.5 {
		t.Fatal("small values changed")
	}
}

// OVC-01 probe: Go-typed option literals (map[string]int64, the exported
// struct) validate and read like their JSON form.
func TestR91toolsProdiaTypedOptionLiterals(t *testing.T) {
	steps := 2
	for name, options := range map[string]interface{}{
		"map[string]int64": map[string]int64{"steps": 2},
		"struct":           ProdiaImageProviderOptions{Steps: &steps},
	} {
		opts := &provider.ImageGenerateOptions{ProviderOptions: map[string]interface{}{"prodia": options}}
		if err := validateImageOptions(opts.ProviderOptions); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := extractProviderOptions(opts); got == nil || got.Steps == nil || *got.Steps != 2 {
			t.Fatalf("%s: parsed = %+v", name, got)
		}
	}
	if err := validateImageOptions(map[string]interface{}{"prodia": map[string]int64{"steps": 9}}); err == nil {
		t.Fatal("out-of-range typed value accepted")
	}
}
