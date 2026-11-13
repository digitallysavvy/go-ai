package openresponses

import (
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS createProviderStreamError({type: chunk.type, data: chunk}): the error type
// is the event type and its data is the whole event.
func TestOpenResponsesStreamErrorCarriesEventTypeAndChunkData(t *testing.T) {
	s := newOpenResponsesStream(nopReadCloser{Reader: strings.NewReader(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"boom"}}}

`)}, nil)
	for {
		chunk, err := s.Next()
		if err != nil {
			t.Fatal("no error chunk")
		}
		if chunk.Type != provider.ChunkTypeError {
			continue
		}
		spe, ok := chunk.Err.(*providererrors.StreamProviderError)
		if !ok {
			t.Fatalf("got %T", chunk.Err)
		}
		if spe.Type != "response.failed" {
			t.Fatalf("type = %q", spe.Type)
		}
		data, _ := spe.Data.(map[string]interface{})
		if data["type"] != "response.failed" {
			t.Fatalf("data = %#v, want the whole chunk", spe.Data)
		}
		return
	}
}
