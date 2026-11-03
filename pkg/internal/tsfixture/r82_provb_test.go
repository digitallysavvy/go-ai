package tsfixture

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// prova CR r82 items 1-3: file and reasoning-file parts render their data
// and providerMetadata, and inline file data compares as bytes.
func TestR82provbStreamFilePartsAndCanonData(t *testing.T) {
	got := StreamToTS([]*provider.StreamChunk{
		{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &types.ReasoningFileContent{MediaType: "image/png", Data: []byte("thoughtimg"),
			ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"s"}}`)}},
		{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: "image/png", Data: []byte("x")},
			ProviderMetadata: json.RawMessage(`{"google":{"interactionId":"i"}}`)},
	})
	want := []interface{}{
		map[string]interface{}{"type": "reasoning-file", "mediaType": "image/png", "data": map[string]interface{}{"type": "data", "data": "dGhvdWdodGltZw"},
			"providerMetadata": map[string]interface{}{"google": map[string]interface{}{"thoughtSignature": "s"}}},
		map[string]interface{}{"type": "file", "mediaType": "image/png", "data": map[string]interface{}{"type": "data", "data": "eA"},
			"providerMetadata": map[string]interface{}{"google": map[string]interface{}{"interactionId": "i"}}},
	}
	if d := Diff("", Normalize(want), Normalize(jsonValue(got))); d != "" {
		t.Fatalf("parts (- want, + got):\n%s", d)
	}
}
