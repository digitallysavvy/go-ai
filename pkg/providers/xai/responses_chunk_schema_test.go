package xai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS xaiResponsesChunkSchema is a closed union: unknown or missing `type`
// frames fail the parse and become error parts, while the listed no-op event
// types are accepted silently.
func TestXAIResponsesStream_ChunkSchemaValidation(t *testing.T) {
	for frame, wantErr := range map[string]bool{
		`{"foo":1}`:                     true,
		`{"type":"response.brand_new"}`: true,
		`{"type":"response.content_part.added","item_id":"i1","output_index":0,"content_index":0,"part":{"type":"output_text"}}`: false,
		`{"type":"response.web_search_call.searching","item_id":"i1","output_index":0}`:                                          false,
	} {
		chunks := drainXAIResponses(t, "data: "+frame+"\n\ndata: [DONE]\n\n")
		sawError := false
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeError {
				sawError = true
			}
		}
		if sawError != wantErr {
			t.Errorf("%s: error chunk = %v, want %v", frame, sawError, wantErr)
		}
	}
}
