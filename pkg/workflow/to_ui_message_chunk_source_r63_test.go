package workflow

import (
	"reflect"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r63: TS toUIMessageChunk forwards a source part's own providerMetadata (Go
// providers such as Anthropic keep it on SourceContent) and always writes a
// document source's title, which the UI message schema requires.
func TestR63ToUIMessageChunkSourceMetadataAndDocumentTitle(t *testing.T) {
	meta := map[string]interface{}{"anthropic": map[string]interface{}{"pageAge": "2 days"}}
	cases := []struct {
		name string
		part ModelCallStreamPart
		want ai.UIMessageChunk
	}{
		{"url source metadata", ModelCallStreamPart{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "url", ID: "s", URL: "https://x", ProviderMetadata: []byte(`{"anthropic":{"pageAge":"2 days"}}`)}},
			uc("type", "source-url", "sourceId", "s", "url", "https://x", "providerMetadata", meta)},
		{"chunk metadata wins", ModelCallStreamPart{Type: provider.ChunkTypeSource, ProviderMetadata: []byte(`{"anthropic":{"pageAge":"2 days"}}`), SourceContent: &types.SourceContent{SourceType: "url", ID: "s", URL: "https://x", ProviderMetadata: []byte(`{"other":{}}`)}},
			uc("type", "source-url", "sourceId", "s", "url", "https://x", "providerMetadata", meta)},
		{"document empty title and metadata", ModelCallStreamPart{Type: provider.ChunkTypeSource, SourceContent: &types.SourceContent{SourceType: "document", ID: "d", MediaType: "application/pdf", ProviderMetadata: []byte(`{"anthropic":{"pageAge":"2 days"}}`)}},
			uc("type", "source-document", "sourceId", "d", "mediaType", "application/pdf", "title", "", "providerMetadata", meta)},
	}
	for _, tc := range cases {
		got, ok := ToUIMessageChunk(tc.part)
		if !ok || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %#v (%v), want %#v", tc.name, got, ok, tc.want)
		}
	}
}
