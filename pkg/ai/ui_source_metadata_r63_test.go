package ai

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r63: TS toUIMessageChunk writes a source part's own providerMetadata on
// source-url / source-document; Go providers (Anthropic web search, among
// others) keep it on SourceContent rather than on the chunk.
func TestR63UISourceChunkCarriesSourceProviderMetadata(t *testing.T) {
	want := map[string]interface{}{"anthropic": map[string]interface{}{"pageAge": "2 days"}}
	sendSources := true
	for _, src := range []types.SourceContent{
		{SourceType: "url", ID: "s", URL: "https://x", ProviderMetadata: []byte(`{"anthropic":{"pageAge":"2 days"}}`)},
		{SourceType: "document", ID: "d", MediaType: "application/pdf", Title: "T", ProviderMetadata: []byte(`{"anthropic":{"pageAge":"2 days"}}`)},
	} {
		src := src
		c, ok := ToUIMessageChunk(provider.StreamChunk{Type: provider.ChunkTypeSource, SourceContent: &src}, UIMessageStreamResultOptions{SendSources: &sendSources})
		if !ok || !reflect.DeepEqual(c["providerMetadata"], want) {
			t.Fatalf("%s: chunk = %#v, want providerMetadata %#v", src.SourceType, c, want)
		}
	}
}
