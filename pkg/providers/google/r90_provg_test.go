package google

import (
	"math"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR90provgEmbeddingOutputDimensionalityNumber: googleEmbeddingModelOptions
// outputDimensionality is z.number(): every Go integer kind is accepted and
// sent; NaN is not a number.
func TestR90provgEmbeddingOutputDimensionalityNumber(t *testing.T) {
	for _, v := range []interface{}{int32(256), uint16(256), float32(256)} {
		opts, err := parseEmbeddingProviderOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"outputDimensionality": v}}})
		if err != nil {
			t.Fatalf("%T rejected: %v", v, err)
		}
		body := map[string]interface{}{}
		addEmbeddingModelOptions(body, opts)
		if body["outputDimensionality"] != v {
			t.Fatalf("%T: outputDimensionality = %#v", v, body["outputDimensionality"])
		}
	}
	if _, err := parseEmbeddingProviderOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"google": map[string]interface{}{"outputDimensionality": math.NaN()}}}); err == nil {
		t.Fatal("NaN accepted")
	}
}
